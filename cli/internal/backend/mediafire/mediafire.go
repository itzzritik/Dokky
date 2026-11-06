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
	defaultAppID  = "42511"
	rootID        = "myfiles"
	listChunk     = 1000
	minSleep      = 100 * time.Millisecond
	maxSleep      = 2 * time.Second
	decayConstant = 2
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
			Default:  encoder.Display | encoder.EncodeBackSlash | encoder.EncodeInvalidUtf8 | encoder.EncodeRightSpace,
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
	}
	f.features = (&fs.Features{CanHaveEmptyDirectories: true}).Fill(ctx, f)
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
func (f *Fs) Precision() time.Duration { return fs.ModTimeNotSupported }
func (f *Fs) Hashes() hash.Set         { return hash.Set(hash.SHA256) }
func (f *Fs) DirCacheFlush()           { f.dirCache.ResetRoot() }

func (f *Fs) call(ctx context.Context, method string, params url.Values, result apiResult) error {
	for attempt := 0; ; attempt++ {
		token, err := f.sess.Token(ctx)
		if err != nil {
			return err
		}
		p := maps.Clone(params)
		if p == nil {
			p = url.Values{}
		}
		p.Set("session_token", token)
		p.Set("response_format", "json")
		env := struct {
			Response any `json:"response"`
		}{result}
		err = postForm(ctx, f.srv, f.pacer, method, p, &env)
		if err == nil {
			err = result.Err()
		}
		var apiErr *api.Error
		if attempt == 0 && errors.As(err, &apiErr) && apiErr.Code == api.CodeSessionInvalid {
			f.sess.Invalidate(token)
			continue
		}
		return err
	}
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

func (f *Fs) FindLeaf(ctx context.Context, pathID, leaf string) (pathIDOut string, found bool, err error) {
	found, err = f.listAll(ctx, pathID, listFolders, func(folder *api.Folder, _ *api.File) bool {
		if folder.Name == leaf {
			pathIDOut = folder.FolderKey
			return true
		}
		return false
	})
	return pathIDOut, found, err
}

func (f *Fs) CreateDir(ctx context.Context, pathID, leaf string) (newID string, err error) {
	return "", fs.ErrorNotImplemented
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
		if file.Filename == leaf {
			info = file
			return true
		}
		return false
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
	return nil, fs.ErrorNotImplemented
}

func (f *Fs) Mkdir(ctx context.Context, dir string) error {
	return fs.ErrorNotImplemented
}

func (f *Fs) Rmdir(ctx context.Context, dir string) error {
	return fs.ErrorNotImplemented
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
	return fs.ErrorCantSetModTime
}

func (o *Object) Open(ctx context.Context, options ...fs.OpenOption) (io.ReadCloser, error) {
	return nil, fs.ErrorNotImplemented
}

func (o *Object) Update(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) error {
	return fs.ErrorNotImplemented
}

func (o *Object) Remove(ctx context.Context) error {
	return fs.ErrorNotImplemented
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
	_ fs.Object          = (*Object)(nil)
	_ fs.IDer            = (*Object)(nil)
)
