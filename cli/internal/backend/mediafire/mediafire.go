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
	dst := *srcObj
	dst.fs = f
	dst.remote = remote
	return &dst, nil
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
	dst := *srcObj
	dst.fs = f
	dst.remote = remote
	dst.id = id
	return &dst, nil
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
	if o.size == 0 {
		return io.NopCloser(strings.NewReader("")), nil
	}
	var links api.Links
	params := url.Values{"quick_key": {o.id}, "link_type": {"direct_download"}}
	if err := o.fs.call(ctx, "file/get_links", params, &links); err != nil {
		return nil, fmt.Errorf("couldn't get download link: %w", err)
	}
	if len(links.Links) == 0 || links.Links[0].DirectDownload == "" {
		return nil, fmt.Errorf("mediafire: no download link for %q", o.remote)
	}
	fs.FixRangeOption(options, o.size)
	opts := rest.Opts{Method: "GET", RootURL: links.Links[0].DirectDownload, Options: options}
	var resp *http.Response
	err := o.fs.pacer.Call(func() (bool, error) {
		var err error
		resp, err = o.fs.srv.Call(ctx, &opts)
		return shouldRetry(ctx, resp, err)
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (o *Object) Update(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) error {
	leaf, dirID, err := o.fs.dirCache.FindPath(ctx, o.remote, true)
	if err != nil {
		return err
	}
	return o.upload(ctx, in, src, leaf, dirID)
}

func (o *Object) Remove(ctx context.Context) error {
	var out api.Base
	if err := o.fs.call(ctx, "file/purge", url.Values{"quick_key": {o.id}}, &out); err != nil {
		return fmt.Errorf("couldn't delete %q: %w", o.remote, err)
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
	_ fs.Object          = (*Object)(nil)
	_ fs.IDer            = (*Object)(nil)
)
