package mediafire

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/itzzritik/dokky/cli/internal/backend/mediafire/api"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/fserrors"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/lib/rest"
)

const hashMemLimit = 10 * fs.Mebi

var (
	pollInterval = time.Second
	pollTimeout  = 30 * time.Minute
)

func sha256Of(in io.Reader, size, memLimit int64) (sum string, out io.Reader, cleanup func(), err error) {
	h := sha256.New()
	tee := io.TeeReader(in, h)
	cleanup = func() {}
	if size <= memLimit {
		b, err := io.ReadAll(tee)
		if err != nil {
			return "", nil, cleanup, err
		}
		return hex.EncodeToString(h.Sum(nil)), bytes.NewReader(b), cleanup, nil
	}
	tmp, err := os.CreateTemp("", "rclone-mediafire-")
	if err != nil {
		return "", nil, cleanup, err
	}
	cleanup = func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}
	if _, err := io.Copy(tmp, tee); err != nil {
		return "", nil, cleanup, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", nil, cleanup, err
	}
	return hex.EncodeToString(h.Sum(nil)), tmp, cleanup, nil
}

func unitDone(words []string, unitID int) bool {
	i := unitID / 16
	if i >= len(words) {
		return false
	}
	w, err := strconv.ParseUint(words[i], 10, 64)
	return err == nil && w>>(unitID%16)&1 == 1
}

func (o *Object) upload(ctx context.Context, in io.Reader, src fs.ObjectInfo, leaf, dirID string) error {
	size := src.Size()
	if size < 0 {
		return errors.New("mediafire: can't upload a file of unknown size")
	}
	sum, _ := src.Hash(ctx, hash.SHA256)
	if sum == "" {
		var wrap accounting.WrapFn
		in, wrap = accounting.UnWrap(in)
		var cleanup func()
		var err error
		sum, in, cleanup, err = sha256Of(in, size, int64(hashMemLimit))
		defer cleanup()
		if err != nil {
			return fmt.Errorf("couldn't hash upload: %w", err)
		}
		in = wrap(in)
	}
	name := o.fs.opt.Enc.FromStandardName(leaf)
	modTime := src.ModTime(ctx)
	target := url.Values{"folder_key": {dirID}, "mtime": {mtimeParam(modTime)}}
	if o.id != "" {
		target.Set("quick_key", o.id)
	} else {
		target.Set("action_on_duplicate", "replace")
	}

	var check api.UploadCheck
	params := url.Values{
		"filename":   {name},
		"size":       {strconv.FormatInt(size, 10)},
		"hash":       {sum},
		"folder_key": {dirID},
		"resumable":  {"yes"},
	}
	if err := o.fs.call(ctx, "upload/check", params, &check); err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) && apiErr.Code == api.CodeFileTooBig {
			return fserrors.NoRetryError(fmt.Errorf("mediafire: %q is larger than MediaFire allows for this account", o.remote))
		}
		return fmt.Errorf("couldn't start upload: %w", err)
	}
	if err := checkSpace(&check, size, o.id == ""); err != nil {
		return err
	}

	if check.HashExists == "yes" {
		// instant ignores quick_key in a folder and makes a new root file without one, so replace by name
		p := url.Values{
			"folder_key":          {dirID},
			"action_on_duplicate": {"replace"},
			"filename":            {name},
			"size":                {strconv.FormatInt(size, 10)},
			"hash":                {sum},
			"mtime":               {mtimeParam(modTime)},
		}
		var out api.Instant
		if err := o.fs.call(ctx, "upload/instant", p, &out); err != nil {
			return fmt.Errorf("couldn't upload: %w", err)
		}
		return o.uploaded(out.QuickKey, size, sum, modTime)
	}

	key, err := o.uploadUnits(ctx, in, &check, name, size, sum, target)
	if err != nil {
		return err
	}
	done, err := o.poll(ctx, key)
	if err != nil {
		return err
	}
	// what MediaFire stored, so a damaged upload is sent again instead of kept
	d := done.Doupload
	if d.Hash != "" && !strings.EqualFold(d.Hash, sum) {
		return fserrors.RetryErrorf("mediafire: %q arrived with a different sha256", o.remote)
	}
	if got, err := strconv.ParseInt(d.Size, 10, 64); d.Size != "" && (err != nil || got != size) {
		return fserrors.RetryErrorf("mediafire: %q arrived as %s bytes, sent %d", o.remote, d.Size, size)
	}
	return o.uploaded(d.QuickKey, size, sum, modTime)
}

func checkSpace(check *api.UploadCheck, size int64, newFile bool) error {
	if check.StorageLimitExceeded == "yes" {
		return fserrors.NoRetryError(errors.New("mediafire: the account is over its storage limit"))
	}
	if free, err := strconv.ParseInt(check.AvailableSpace, 10, 64); newFile && err == nil && free < size {
		return fserrors.NoRetryError(fmt.Errorf("mediafire: not enough space: need %s, %s free", fs.SizeSuffix(size), fs.SizeSuffix(free)))
	}
	return nil
}

func (o *Object) uploaded(quickKey string, size int64, sum string, modTime time.Time) error {
	if quickKey == "" {
		return fmt.Errorf("mediafire: upload of %q finished without a file key", o.remote)
	}
	o.linkMu.Lock()
	o.id, o.link = quickKey, ""
	o.size, o.sha256, o.modTime = size, strings.ToLower(sum), modTime.Truncate(time.Second)
	o.linkMu.Unlock()
	return nil
}

func (o *Object) uploadUnits(ctx context.Context, in io.Reader, check *api.UploadCheck, name string, size int64, sum string, target url.Values) (string, error) {
	ru := check.ResumableUpload
	units, err := strconv.Atoi(ru.NumberOfUnits)
	if err != nil {
		return "", fmt.Errorf("mediafire: bad unit count %q", ru.NumberOfUnits)
	}
	unitSize, err := strconv.ParseInt(ru.UnitSize, 10, 64)
	if err != nil || unitSize < 1 {
		return "", fmt.Errorf("mediafire: bad unit size %q", ru.UnitSize)
	}
	in, wrap := accounting.UnWrap(in)
	sem := make(chan struct{}, max(1, o.fs.opt.UploadConcurrency))
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		key      = ru.UploadKey
	)
	fail := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}
	failed := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return firstErr != nil
	}
	remaining := size
	for id := 0; id < units && !failed(); id++ {
		n := min(remaining, unitSize)
		remaining -= n
		if unitDone(ru.Bitmap.Words, id) {
			if _, err := io.CopyN(io.Discard, in, n); err != nil {
				fail(fmt.Errorf("couldn't read source: %w", err))
			}
			continue
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(in, buf); err != nil {
			fail(fmt.Errorf("couldn't read source: %w", err))
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(id int, buf []byte) {
			defer func() {
				<-sem
				wg.Done()
			}()
			k, err := o.uploadUnit(ctx, id, buf, wrap, name, size, sum, target)
			if err != nil {
				fail(fmt.Errorf("couldn't upload part %d of %q: %w", id, o.remote, err))
				return
			}
			if k != "" {
				mu.Lock()
				key = k
				mu.Unlock()
			}
		}(id, buf)
	}
	wg.Wait()
	return key, firstErr
}

func (o *Object) uploadUnit(ctx context.Context, id int, buf []byte, wrap accounting.WrapFn, name string, size int64, sum string, target url.Values) (string, error) {
	unitSum := sha256.Sum256(buf)
	n := int64(len(buf))
	var out api.Upload
	err := o.fs.withToken(ctx, func(token string) error {
		q := maps.Clone(target)
		q.Set("session_token", token)
		q.Set("response_format", "json")
		opts := rest.Opts{
			Method:        "POST",
			RootURL:       apiURL,
			Path:          "/upload/resumable.php",
			Parameters:    q,
			ContentType:   "application/octet-stream",
			ContentLength: &n,
			ExtraHeaders: map[string]string{
				"x-filename":  name,
				"x-filesize":  strconv.FormatInt(size, 10),
				"x-filehash":  sum,
				"x-unit-id":   strconv.Itoa(id),
				"x-unit-size": strconv.FormatInt(n, 10),
				"x-unit-hash": hex.EncodeToString(unitSum[:]),
			},
		}
		env := struct {
			Response *api.Upload `json:"response"`
		}{&out}
		err := o.fs.pacer.Call(func() (bool, error) {
			opts.Body = wrap(bytes.NewReader(buf))
			resp, err := o.fs.srv.CallJSON(ctx, &opts, nil, &env)
			return shouldRetry(ctx, resp, err)
		})
		if err != nil {
			return err
		}
		if err := out.Err(); err != nil {
			return err
		}
		if r := out.Doupload.Result; r != "" && r != "0" {
			return fmt.Errorf("mediafire: part %d rejected with result %s: %s", id, r, out.Doupload.Description)
		}
		return nil
	})
	return out.Doupload.Key, err
}

func (o *Object) poll(ctx context.Context, key string) (*api.Upload, error) {
	deadline := time.Now().Add(pollTimeout)
	for {
		var out api.Upload
		if err := o.fs.call(ctx, "upload/poll_upload", url.Values{"key": {key}}, &out); err != nil {
			return nil, fmt.Errorf("couldn't check upload of %q: %w", o.remote, err)
		}
		d := out.Doupload
		if d.FileError != "" {
			return nil, fmt.Errorf("mediafire: upload of %q failed with file error %s", o.remote, d.FileError)
		}
		if d.Result != "" && d.Result != "0" {
			return nil, fmt.Errorf("mediafire: upload of %q failed with result %s: %s", o.remote, d.Result, d.Description)
		}
		if d.Status == "99" {
			return &out, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("mediafire: upload of %q not finished after %v (status %s: %s)", o.remote, pollTimeout, d.Status, d.Description)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
