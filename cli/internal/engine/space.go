package engine

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/cache"
)

// S3 has no free space call, so a NAS can publish "<total> <free>" in bytes in this file
const spaceFile = ".dokky-space"

func useServerSpace(ctx context.Context, fsName string) {
	f, err := getFs(ctx, fsName)
	if err != nil || f.Features().About != nil {
		return
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := f.NewObject(probe, spaceFile); err != nil {
		return
	}
	// ponytail: patches the backend's own feature set, which the mount reads; wrap the backend if rclone ever copies it
	f.Features().About = func(ctx context.Context) (*fs.Usage, error) { return readSpace(ctx, f) }
}

// rclone keeps the context an fs is made with for its later token refreshes, so the fs is made on one that is never canceled
func getFs(ctx context.Context, name string) (fs.Fs, error) {
	type made struct {
		f   fs.Fs
		err error
	}
	ch := make(chan made, 1)
	go func() {
		f, err := cache.Get(context.Background(), name)
		ch <- made{f, err}
	}()
	select {
	case m := <-ch:
		return m.f, m.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func readSpace(ctx context.Context, f fs.Fs) (*fs.Usage, error) {
	o, err := f.NewObject(ctx, spaceFile)
	if err != nil {
		return nil, err
	}
	r, err := o.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var total, free int64
	if _, err := fmt.Fscan(io.LimitReader(r, 64), &total, &free); err != nil {
		return nil, fmt.Errorf("%s: %w", spaceFile, err)
	}
	used := total - free
	return &fs.Usage{Total: &total, Free: &free, Used: &used}, nil
}
