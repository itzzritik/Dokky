package mediafire

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/itzzritik/dokky/cli/internal/backend/mediafire/api"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/configstruct"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/fserrors"
	"github.com/rclone/rclone/fs/fshttp"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/lib/dircache"
	"github.com/rclone/rclone/lib/encoder"
	"github.com/rclone/rclone/lib/pacer"
	"github.com/rclone/rclone/lib/rest"
)

const (
	defaultAppID = "42511"
	rootID       = "myfiles"
	listChunk    = 1000
	openTries    = 3
	linkTTL      = 10 * time.Minute
	changeBlock  = 500
	// far behind, refreshing everything is cheaper than reading every block
	maxChangeBlocks = 20
	maxSeen         = 100000
	minSleep        = 100 * time.Millisecond
	maxSleep        = 2 * time.Second
	dlMinSleep      = 10 * time.Millisecond
	dlMaxSleep      = 200 * time.Millisecond
	decayConstant   = 2
)

var apiURL = "https://www.mediafire.com/api/1.5"

var retryErrorCodes = []int{429, 500, 502, 503, 504}

var (
	listFolders = []string{"folders"}
	listFiles   = []string{"files"}
	listBoth    = []string{"folders", "files"}
)

func init() {
	fs.Register(&fs.RegInfo{
		Name:        "mediafire",
		Description: "MediaFire",
		NewFs:       NewFs,
		Options: []fs.Option{{
			Name:      "email",
			Help:      "MediaFire account email.",
			Required:  true,
			Sensitive: true,
		}, {
			Name:       "password",
			Help:       "MediaFire account password.",
			Required:   true,
			IsPassword: true,
		}, {
			Name:     "app_id",
			Help:     "MediaFire application ID used to sign in.",
			Default:  defaultAppID,
			Advanced: true,
			Hide:     fs.OptionHideConfigurator,
		}, {
			Name:     "upload_concurrency",
			Help:     "How many upload units to send at once.",
			Default:  4,
			Advanced: true,
		}, {
			Name:     config.ConfigEncoding,
			Help:     config.ConfigEncodingHelp,
			Advanced: true,
			Default: encoder.Display |
				encoder.EncodeBackSlash |
				encoder.EncodeWin |
				encoder.EncodeLeftSpace |
				encoder.EncodeRightSpace |
				encoder.EncodeRightPeriod |
				encoder.EncodeLeftCrLfHtVt |
				encoder.EncodeRightCrLfHtVt |
				encoder.EncodeInvalidUtf8,
		}},
	})
}

type Options struct {
	Email             string               `config:"email"`
	Password          string               `config:"password"`
	AppID             string               `config:"app_id"`
	UploadConcurrency int                  `config:"upload_concurrency"`
	Enc               encoder.MultiEncoder `config:"encoding"`
}

type Fs struct {
	name     string
	root     string
	opt      Options
	features *fs.Features
	srv      *rest.Client
	pacer    *fs.Pacer
	dlPacer  *fs.Pacer
	sess     *session
	dirCache *dircache.DirCache
}

type Object struct {
	fs      *Fs
	remote  string
	id      string
	size    int64
	sha256  string
	modTime time.Time
	linkMu  sync.Mutex
	link    string
	linkAt  time.Time
}

type apiResult interface{ Err() error }

func NewFs(ctx context.Context, name, root string, m configmap.Mapper) (fs.Fs, error) {
	opt := new(Options)
	if err := configstruct.Set(m, opt); err != nil {
		return nil, err
	}
	password, err := obscure.Reveal(opt.Password)
	if err != nil {
		return nil, fmt.Errorf("mediafire: couldn't decrypt password: %w", err)
	}
	root = strings.Trim(root, "/")
	f := &Fs{
		name:  name,
		root:  root,
		opt:   *opt,
		srv:   rest.NewClient(fshttp.NewClient(ctx)).SetErrorHandler(errorHandler),
		pacer: newPacer(ctx),
		// download hosts often reset new connections; retrying them must not slow every API call
		dlPacer: fs.NewPacer(ctx, pacer.NewDefault(pacer.MinSleep(dlMinSleep), pacer.MaxSleep(dlMaxSleep), pacer.DecayConstant(decayConstant))),
	}
	f.features = (&fs.Features{CanHaveEmptyDirectories: true, CaseInsensitive: true, DuplicateFiles: true}).Fill(ctx, f)
	f.sess = getSession(opt.Email, password, cmp.Or(opt.AppID, defaultAppID), f.srv, f.pacer)
	f.dirCache = dircache.New(root, rootID, f)

	if err := f.dirCache.FindRoot(ctx, false); err != nil {
		newRoot, remote := dircache.SplitPath(root)
		tempF := *f
		tempF.dirCache = dircache.New(newRoot, rootID, &tempF)
		tempF.root = newRoot
		if err := tempF.dirCache.FindRoot(ctx, false); err != nil {
			return f, nil
		}
		if _, err := tempF.NewObject(ctx, remote); err != nil {
			if errors.Is(err, fs.ErrorObjectNotFound) {
				return f, nil
			}
			return nil, err
		}
		f.features.Fill(ctx, &tempF)
		f.dirCache = tempF.dirCache
		f.root = tempF.root
		return f, fs.ErrorIsFile
	}
	return f, nil
}

func (f *Fs) Name() string             { return f.name }
func (f *Fs) Root() string             { return f.root }
func (f *Fs) String() string           { return fmt.Sprintf("mediafire root '%s'", f.root) }
func (f *Fs) Features() *fs.Features   { return f.features }
func (f *Fs) Precision() time.Duration { return time.Second }
func (f *Fs) Hashes() hash.Set         { return hash.Set(hash.SHA256) }
func (f *Fs) DirCacheFlush()           { f.dirCache.ResetRoot() }

func (f *Fs) withToken(ctx context.Context, fn func(token string) error) error {
	for attempt := 0; ; attempt++ {
		token, err := f.sess.Token(ctx)
		if err != nil {
			return err
		}
		err = fn(token)
		var apiErr *api.Error
		if attempt == 0 && errors.As(err, &apiErr) && apiErr.Code == api.CodeSessionInvalid {
			f.sess.Invalidate(token)
			continue
		}
		return err
	}
}

func (f *Fs) call(ctx context.Context, method string, params url.Values, result apiResult) error {
	return f.withToken(ctx, func(token string) error {
		p := maps.Clone(params)
		if p == nil {
			p = url.Values{}
		}
		p.Set("session_token", token)
		p.Set("response_format", "json")
		env := struct {
			Response any `json:"response"`
		}{result}
		if err := postForm(ctx, f.srv, f.pacer, method, p, &env); err != nil {
			return err
		}
		return result.Err()
	})
}

func (f *Fs) listAll(ctx context.Context, dirID string, kinds []string, fn func(folder *api.Folder, file *api.File) bool) (found bool, err error) {
	for _, kind := range kinds {
		for chunk := 1; ; chunk++ {
			params := url.Values{
				"folder_key":   {dirID},
				"content_type": {kind},
				"chunk":        {strconv.Itoa(chunk)},
				"chunk_size":   {strconv.Itoa(listChunk)},
				"details":      {"yes"},
			}
			var out api.FolderContent
			if err := f.call(ctx, "folder/get_content", params, &out); err != nil {
				return false, fmt.Errorf("couldn't list folder: %w", err)
			}
			c := &out.FolderContent
			for i := range c.Folders {
				c.Folders[i].Name = f.opt.Enc.ToStandardName(c.Folders[i].Name)
				if fn(&c.Folders[i], nil) {
					return true, nil
				}
			}
			for i := range c.Files {
				c.Files[i].Filename = f.opt.Enc.ToStandardName(c.Files[i].Filename)
				if fn(nil, &c.Files[i]) {
					return true, nil
				}
			}
			if c.MoreChunks != "yes" {
				break
			}
		}
	}
	return false, nil
}

// MediaFire ignores case in names, so another case is the same item unless an exact match exists
func sameName(name, want string, exact *bool) bool {
	if name == want {
		*exact = true
		return true
	}
	return strings.EqualFold(name, want)
}

func (f *Fs) FindLeaf(ctx context.Context, pathID, leaf string) (pathIDOut string, found bool, err error) {
	_, err = f.listAll(ctx, pathID, listFolders, func(folder *api.Folder, _ *api.File) bool {
		var exact bool
		if sameName(folder.Name, leaf, &exact) {
			pathIDOut, found = folder.FolderKey, true
		}
		return exact
	})
	return pathIDOut, found, err
}

func (f *Fs) CreateDir(ctx context.Context, pathID, leaf string) (newID string, err error) {
	var out api.FolderCreate
	params := url.Values{"foldername": {f.opt.Enc.FromStandardName(leaf)}, "parent_key": {pathID}}
	if err := f.call(ctx, "folder/create", params, &out); err != nil {
		return "", fmt.Errorf("couldn't create folder %q: %w", leaf, err)
	}
	return out.FolderKey, nil
}

func (f *Fs) List(ctx context.Context, dir string) (entries fs.DirEntries, err error) {
	dirID, err := f.dirCache.FindDir(ctx, dir, false)
	if err != nil {
		return nil, err
	}
	var iErr error
	_, err = f.listAll(ctx, dirID, listBoth, func(folder *api.Folder, file *api.File) bool {
		if folder != nil {
			remote := path.Join(dir, folder.Name)
			f.dirCache.Put(remote, folder.FolderKey)
			entries = append(entries, fs.NewDir(remote, parseTime(folder.CreatedUTC)).SetID(folder.FolderKey))
			return false
		}
		o, err := f.newObjectWithInfo(path.Join(dir, file.Filename), file)
		if err != nil {
			iErr = err
			return true
		}
		entries = append(entries, o)
		return false
	})
	if err != nil {
		return nil, err
	}
	if iErr != nil {
		return nil, iErr
	}
	return entries, nil
}

func (f *Fs) newObjectWithInfo(remote string, info *api.File) (*Object, error) {
	o := &Object{fs: f, remote: remote}
	if err := o.setMetaData(info); err != nil {
		return nil, err
	}
	return o, nil
}

func (f *Fs) NewObject(ctx context.Context, remote string) (fs.Object, error) {
	leaf, dirID, err := f.dirCache.FindPath(ctx, remote, false)
	if err != nil {
		if errors.Is(err, fs.ErrorDirNotFound) {
			return nil, fs.ErrorObjectNotFound
		}
		return nil, err
	}
	var info *api.File
	_, err = f.listAll(ctx, dirID, listFiles, func(_ *api.Folder, file *api.File) bool {
		var exact bool
		if sameName(file.Filename, leaf, &exact) {
			info = file
		}
		return exact
	})
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fs.ErrorObjectNotFound
	}
	return f.newObjectWithInfo(remote, info)
}

func (f *Fs) Put(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	leaf, dirID, err := f.dirCache.FindPath(ctx, src.Remote(), true)
	if err != nil {
		return nil, err
	}
	o := &Object{fs: f, remote: src.Remote()}
	if err := o.upload(ctx, in, src, leaf, dirID); err != nil {
		return nil, err
	}
	return o, nil
}

func (f *Fs) Mkdir(ctx context.Context, dir string) error {
	_, err := f.dirCache.FindDir(ctx, dir, true)
	return err
}

func (f *Fs) Rmdir(ctx context.Context, dir string) error {
	return f.purgeCheck(ctx, dir, true)
}

func (f *Fs) Purge(ctx context.Context, dir string) error {
	return f.purgeCheck(ctx, dir, false)
}

func (f *Fs) purgeCheck(ctx context.Context, dir string, check bool) error {
	if path.Join(f.root, dir) == "" {
		return errors.New("mediafire: can't remove the root folder")
	}
	dirID, err := f.dirCache.FindDir(ctx, dir, false)
	if err != nil {
		return err
	}
	if check {
		found, err := f.listAll(ctx, dirID, listBoth, func(*api.Folder, *api.File) bool { return true })
		if err != nil {
			return err
		}
		if found {
			return fs.ErrorDirectoryNotEmpty
		}
	}
	// MediaFire only purges folders that are already in its trash
	for _, method := range []string{"folder/delete", "folder/purge"} {
		var out api.Base
		if err := f.call(ctx, method, url.Values{"folder_key": {dirID}}, &out); err != nil {
			return fmt.Errorf("couldn't remove folder %q: %w", dir, err)
		}
	}
	f.dirCache.FlushDir(dir)
	return nil
}

func (f *Fs) renameFile(ctx context.Context, id, leaf string) error {
	var out api.Base
	params := url.Values{"quick_key": {id}, "filename": {f.opt.Enc.FromStandardName(leaf)}}
	if err := f.call(ctx, "file/update", params, &out); err != nil {
		return fmt.Errorf("couldn't rename file to %q: %w", leaf, err)
	}
	return nil
}

func (f *Fs) Move(ctx context.Context, src fs.Object, remote string) (fs.Object, error) {
	srcObj, ok := src.(*Object)
	if !ok {
		return nil, fs.ErrorCantMove
	}
	srcLeaf, srcDirID, err := srcObj.fs.dirCache.FindPath(ctx, srcObj.remote, false)
	if err != nil {
		return nil, err
	}
	leaf, dirID, err := f.dirCache.FindPath(ctx, remote, true)
	if err != nil {
		return nil, err
	}
	if dirID != srcDirID {
		var out api.Base
		if err := f.call(ctx, "file/move", url.Values{"quick_key": {srcObj.id}, "folder_key": {dirID}}, &out); err != nil {
			return nil, fmt.Errorf("couldn't move file: %w", err)
		}
	}
	if leaf != srcLeaf {
		if err := f.renameFile(ctx, srcObj.id, leaf); err != nil {
			return nil, err
		}
	}
	return &Object{fs: f, remote: remote, id: srcObj.id, size: srcObj.size, sha256: srcObj.sha256, modTime: srcObj.modTime}, nil
}

func (f *Fs) DirMove(ctx context.Context, src fs.Fs, srcRemote, dstRemote string) error {
	srcFs, ok := src.(*Fs)
	if !ok {
		return fs.ErrorCantDirMove
	}
	srcID, srcDirectoryID, srcLeaf, dstDirectoryID, dstLeaf, err := f.dirCache.DirMove(ctx, srcFs.dirCache, srcFs.root, srcRemote, f.root, dstRemote)
	if err != nil {
		return err
	}
	if srcDirectoryID != dstDirectoryID {
		var out api.Base
		if err := f.call(ctx, "folder/move", url.Values{"folder_key_src": {srcID}, "folder_key_dst": {dstDirectoryID}}, &out); err != nil {
			return fmt.Errorf("couldn't move folder: %w", err)
		}
	}
	if srcLeaf != dstLeaf {
		var out api.Base
		params := url.Values{"folder_key": {srcID}, "foldername": {f.opt.Enc.FromStandardName(dstLeaf)}}
		if err := f.call(ctx, "folder/update", params, &out); err != nil {
			return fmt.Errorf("couldn't rename folder to %q: %w", dstLeaf, err)
		}
	}
	srcFs.dirCache.FlushDir(srcRemote)
	return nil
}

func (f *Fs) Copy(ctx context.Context, src fs.Object, remote string) (fs.Object, error) {
	srcObj, ok := src.(*Object)
	if !ok {
		return nil, fs.ErrorCantCopy
	}
	leaf, dirID, err := f.dirCache.FindPath(ctx, remote, true)
	if err != nil {
		return nil, err
	}
	old, err := f.NewObject(ctx, remote)
	if err != nil && !errors.Is(err, fs.ErrorObjectNotFound) {
		return nil, err
	}
	var out api.FileCopy
	if err := f.call(ctx, "file/copy", url.Values{"quick_key": {srcObj.id}, "folder_key": {dirID}}, &out); err != nil {
		return nil, fmt.Errorf("couldn't copy file: %w", err)
	}
	if len(out.NewQuickKeys) == 0 {
		return nil, fmt.Errorf("mediafire: copy of %q returned no file key", srcObj.remote)
	}
	id := out.NewQuickKeys[0]
	if leaf != path.Base(srcObj.remote) {
		if err := f.renameFile(ctx, id, leaf); err != nil {
			return nil, err
		}
	}
	// like Drive, the copy takes the name and the file it replaces goes
	if old != nil && old.(*Object).id != id && old.(*Object).id != srcObj.id {
		if err := old.Remove(ctx); err != nil {
			fs.Errorf(old, "mediafire: couldn't remove the file the copy replaced: %v", err)
		}
	}
	return &Object{fs: f, remote: remote, id: id, size: srcObj.size, sha256: srcObj.sha256, modTime: srcObj.modTime}, nil
}

func (f *Fs) About(ctx context.Context) (*fs.Usage, error) {
	var out api.UserInfo
	if err := f.call(ctx, "user/get_info", nil, &out); err != nil {
		return nil, fmt.Errorf("couldn't read account info: %w", err)
	}
	total, err := strconv.ParseInt(out.UserInfo.StorageLimit, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("couldn't read storage limit: %w", err)
	}
	used, err := strconv.ParseInt(out.UserInfo.UsedStorageSize, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("couldn't read used storage: %w", err)
	}
	return &fs.Usage{
		Total: fs.NewUsageValue(total),
		Used:  fs.NewUsageValue(used),
		Free:  fs.NewUsageValue(total - used),
	}, nil
}

func parseTime(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC); err == nil {
		return t
	}
	return time.Time{}
}

func (o *Object) setMetaData(info *api.File) error {
	size, err := strconv.ParseInt(info.Size, 10, 64)
	if err != nil {
		return fmt.Errorf("%q has a bad size %q: %w", o.remote, info.Size, err)
	}
	o.id = info.QuickKey
	o.size = size
	o.sha256 = strings.ToLower(info.Hash)
	o.modTime = parseTime(info.CreatedUTC)
	return nil
}

func (o *Object) Fs() fs.Info                           { return o.fs }
func (o *Object) Remote() string                        { return o.remote }
func (o *Object) Size() int64                           { return o.size }
func (o *Object) ModTime(ctx context.Context) time.Time { return o.modTime }
func (o *Object) Storable() bool                        { return true }
func (o *Object) ID() string                            { return o.id }

func (o *Object) String() string {
	if o == nil {
		return "<nil>"
	}
	return o.remote
}

func (o *Object) Hash(ctx context.Context, t hash.Type) (string, error) {
	if t != hash.SHA256 {
		return "", hash.ErrUnsupported
	}
	return o.sha256, nil
}

func (o *Object) SetModTime(ctx context.Context, modTime time.Time) error {
	var out api.Base
	params := url.Values{"quick_key": {o.id}, "mtime": {mtimeParam(modTime)}}
	if err := o.fs.call(ctx, "file/update", params, &out); err != nil {
		return fmt.Errorf("couldn't set modification time: %w", err)
	}
	o.modTime = modTime.Truncate(time.Second)
	return nil
}

// RFC 3339 in UTC: a plain date and time is read in the account's own time zone
func mtimeParam(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

func (o *Object) Open(ctx context.Context, options ...fs.OpenOption) (io.ReadCloser, error) {
	if o.size == 0 {
		return io.NopCloser(strings.NewReader("")), nil
	}
	fs.FixRangeOption(options, o.size)
	var err error
	for try := range openTries {
		link, cached, lerr := o.downloadLink(ctx, try > 0)
		if lerr != nil {
			return nil, lerr
		}
		var body io.ReadCloser
		if body, err = o.get(ctx, link, options); err == nil {
			return body, nil
		}
		o.forgetLink(link)
		if !cached && !fserrors.ShouldRetry(err) && !fserrors.IsRetryError(err) {
			break
		}
	}
	return nil, err
}

func (o *Object) get(ctx context.Context, link string, options []fs.OpenOption) (io.ReadCloser, error) {
	opts := rest.Opts{Method: "GET", RootURL: link, Options: options}
	var resp *http.Response
	err := o.fs.dlPacer.Call(func() (bool, error) {
		var err error
		resp, err = o.fs.srv.Call(ctx, &opts)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return nil, err
	}
	// an expired link can answer with a web page, which must never pass as file data
	rng := resp.Request.Header.Get("Range")
	full := rng == "" || rng == "bytes=0-" || rng == fmt.Sprintf("bytes=0-%d", o.size-1)
	sized := resp.ContentLength == o.size || (rng == "" && resp.ContentLength < 0)
	if resp.StatusCode != http.StatusPartialContent && (!full || !sized) {
		_ = resp.Body.Close()
		return nil, fserrors.RetryErrorf("mediafire: download of %q answered %s with %d bytes", o.remote, resp.Status, resp.ContentLength)
	}
	return resp.Body, nil
}

func (o *Object) downloadLink(ctx context.Context, fresh bool) (link string, cached bool, err error) {
	o.linkMu.Lock()
	defer o.linkMu.Unlock()
	if !fresh && o.link != "" && now().Sub(o.linkAt) < linkTTL {
		return o.link, true, nil
	}
	var links api.Links
	params := url.Values{"quick_key": {o.id}, "link_type": {"direct_download"}}
	if err := o.fs.call(ctx, "file/get_links", params, &links); err != nil {
		return "", false, fmt.Errorf("couldn't get download link: %w", err)
	}
	if len(links.Links) == 0 || links.Links[0].DirectDownload == "" {
		return "", false, fmt.Errorf("mediafire: no download link for %q", o.remote)
	}
	o.link, o.linkAt = links.Links[0].DirectDownload, now()
	return o.link, false, nil
}

func (o *Object) forgetLink(link string) {
	o.linkMu.Lock()
	if o.link == link {
		o.link = ""
	}
	o.linkMu.Unlock()
}

func (o *Object) Update(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) error {
	leaf, dirID, err := o.fs.dirCache.FindPath(ctx, o.remote, true)
	if err != nil {
		return err
	}
	return o.upload(ctx, in, src, leaf, dirID)
}

func (o *Object) Remove(ctx context.Context) error {
	// file/purge on a file outside the trash answers Success and keeps it
	for _, method := range []string{"file/delete", "file/purge"} {
		var out api.Base
		if err := o.fs.call(ctx, method, url.Values{"quick_key": {o.id}}, &out); err != nil {
			var apiErr *api.Error
			if errors.As(err, &apiErr) && (apiErr.Code == api.CodeFileUnknown || apiErr.Code == api.CodeFileMissing) {
				// already gone, for example deleted on another device
				return nil
			}
			return fmt.Errorf("couldn't delete %q: %w", o.remote, err)
		}
	}
	return nil
}

func newPacer(ctx context.Context) *fs.Pacer {
	return fs.NewPacer(ctx, pacer.NewDefault(pacer.MinSleep(minSleep), pacer.MaxSleep(maxSleep), pacer.DecayConstant(decayConstant)))
}

func shouldRetry(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if fserrors.ContextError(ctx, &err) {
		return false, err
	}
	return fserrors.ShouldRetry(err) || fserrors.ShouldRetryHTTP(resp, retryErrorCodes), err
}

func errorHandler(resp *http.Response) error {
	body, err := rest.ReadBody(resp)
	if err != nil {
		return fmt.Errorf("mediafire: HTTP %s: %w", resp.Status, err)
	}
	var out struct {
		Response api.Base `json:"response"`
	}
	if json.Unmarshal(body, &out) == nil && out.Response.Result != "" {
		if err := out.Response.Err(); err != nil {
			return err
		}
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		text = text[:200]
	}
	return fmt.Errorf("mediafire: HTTP %s: %s", resp.Status, text)
}

func postForm(ctx context.Context, srv *rest.Client, p *fs.Pacer, method string, params url.Values, out any) error {
	body := params.Encode()
	opts := rest.Opts{
		Method:      "POST",
		RootURL:     apiURL,
		Path:        "/" + method + ".php",
		ContentType: "application/x-www-form-urlencoded",
	}
	return p.Call(func() (bool, error) {
		opts.Body = strings.NewReader(body)
		resp, err := srv.CallJSON(ctx, &opts, nil, out)
		return shouldRetry(ctx, resp, err)
	})
}

var (
	_ fs.Fs              = (*Fs)(nil)
	_ fs.Abouter         = (*Fs)(nil)
	_ fs.DirCacheFlusher = (*Fs)(nil)
	_ fs.Purger          = (*Fs)(nil)
	_ fs.Mover           = (*Fs)(nil)
	_ fs.DirMover        = (*Fs)(nil)
	_ fs.Copier          = (*Fs)(nil)
	_ fs.ChangeNotifier  = (*Fs)(nil)
	_ fs.Object          = (*Object)(nil)
	_ fs.IDer            = (*Object)(nil)
)

// ChangeNotify polls MediaFire's change list, so edits made on the web or another device show up like they do on Drive
func (f *Fs) ChangeNotify(ctx context.Context, notify func(string, fs.EntryType), intervals <-chan time.Duration) {
	go func() {
		var st changeState
		if err := f.changes(ctx, &st, notify); err != nil {
			fs.Infof(f, "mediafire: couldn't read the change list: %v", err)
		}
		var ticker *time.Ticker
		var tick <-chan time.Time
		for {
			select {
			case d, ok := <-intervals:
				if ticker != nil {
					ticker.Stop()
					ticker, tick = nil, nil
				}
				if !ok {
					return
				}
				if d > 0 {
					ticker = time.NewTicker(d)
					tick = ticker.C
				}
			case <-tick:
				if err := f.changes(ctx, &st, notify); err != nil {
					fs.Infof(f, "mediafire: couldn't read the change list: %v", err)
				}
			}
		}
	}()
}

// what the change list last showed; its per-item revisions don't follow the account revision, so items are compared instead
type changeState struct {
	rev  int64
	seen map[string]string
}

type changedItem struct {
	key, state, parent, name string
	dir                      bool
}

// changes reports what differs since the last call; a failed call changes nothing, so the next one covers it
func (f *Fs) changes(ctx context.Context, st *changeState, notify func(string, fs.EntryType)) error {
	var status api.DeviceStatus
	if err := f.call(ctx, "device/get_status", nil, &status); err != nil {
		return err
	}
	cur, err := strconv.ParseInt(status.DeviceRevision, 10, 64)
	if err != nil {
		return fmt.Errorf("mediafire: bad revision %q", status.DeviceRevision)
	}
	first := st.seen == nil
	if !first && cur == st.rev {
		return nil
	}
	from := st.rev
	if first || (cur-st.rev)/changeBlock >= maxChangeBlocks || len(st.seen) > maxSeen {
		from = cur
	}
	var items []changedItem
	for start := from - from%changeBlock; start <= cur; start += changeBlock {
		var ch api.Changes
		if err := f.call(ctx, "device/get_changes", url.Values{"revision": {strconv.FormatInt(start, 10)}}, &ch); err != nil {
			return err
		}
		items = appendChanged(items, &ch.Updated)
		items = appendChanged(items, &ch.Deleted)
	}
	if first || from == cur && st.rev != cur {
		if !first {
			notify("", fs.EntryDirectory)
		}
		st.rev, st.seen = cur, map[string]string{}
		for _, it := range items {
			st.seen[it.key] = it.state
		}
		return nil
	}
	reported := false
	present := make(map[string]bool, len(items))
	for _, it := range items {
		present[it.key] = true
	}
	// a purge removes an item from the list without a trace, so anything gone from the blocks read was removed
	for key, old := range st.seen {
		if !present[key] && listedFrom(old) >= from-from%changeBlock {
			delete(st.seen, key)
			reported = true
			f.notifyChanged(changedFrom(key, old), notify)
		}
	}
	for _, it := range items {
		old, known := st.seen[it.key]
		if old == it.state {
			continue
		}
		st.seen[it.key] = it.state
		reported = true
		f.notifyChanged(it, notify)
		if known {
			// a move or rename: the old place changed too
			f.notifyChanged(changedFrom(it.key, old), notify)
		}
	}
	if !reported {
		// the revision moved but nothing listed changed, like a purge, so every listing is refreshed
		notify("", fs.EntryDirectory)
	}
	st.rev = cur
	return nil
}

func appendChanged(items []changedItem, set *api.ChangeSet) []changedItem {
	for _, file := range set.Files {
		items = append(items, changedItem{
			key:    "f" + file.QuickKey,
			state:  strings.Join([]string{file.Revision + "/" + file.CreatedUTC, file.ParentFolderKey, file.Filename}, "\x00"),
			parent: file.ParentFolderKey, name: file.Filename,
		})
	}
	for _, folder := range set.Folders {
		if folder.FolderKey == "trash" {
			continue
		}
		items = append(items, changedItem{
			key:    "d" + folder.FolderKey,
			state:  strings.Join([]string{folder.Revision, folder.ParentFolderKey, folder.FolderKey}, "\x00"),
			parent: folder.ParentFolderKey, name: folder.FolderKey, dir: true,
		})
	}
	return items
}

func changedFrom(key, state string) changedItem {
	parts := strings.SplitN(state, "\x00", 3)
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	return changedItem{key: key, parent: parts[1], name: parts[2], dir: strings.HasPrefix(key, "d")}
}

// the revision an item is listed under, which decides the block it shows up in
func listedFrom(state string) int64 {
	rev, _, _ := strings.Cut(state, "\x00")
	rev, _, _ = strings.Cut(rev, "/")
	n, err := strconv.ParseInt(rev, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

func (f *Fs) notifyChanged(it changedItem, notify func(string, fs.EntryType)) {
	dirOf := func(key string) (string, bool) {
		if key == "" {
			key = rootID
		}
		return f.dirCache.GetInv(key)
	}
	if it.dir {
		if p, ok := dirOf(strings.TrimPrefix(it.key, "d")); ok {
			notify(p, fs.EntryDirectory)
		} else if parent, ok := dirOf(it.parent); ok && it.parent != "trash" {
			notify(parent, fs.EntryDirectory)
		}
		return
	}
	if dir, ok := dirOf(it.parent); ok {
		notify(path.Join(dir, f.opt.Enc.ToStandardName(it.name)), fs.EntryObject)
	}
}
