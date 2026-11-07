package engine

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/rc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func transferring(t *testing.T, s *accounting.StatsInfo) map[string]int64 {
	t.Helper()
	out, err := s.RemoteStats(false)
	require.NoError(t, err)
	got := map[string]int64{}
	list, _ := out["transferring"].([]rc.Params)
	for _, tr := range list {
		b, _ := tr["bytes"].(int64)
		got[tr["name"].(string)] = b
	}
	return got
}

func TestTransferStaysListedWhileAnotherOfSameNameEnds(t *testing.T) {
	ctx := context.Background()
	s := accounting.NewStats(ctx)
	first := s.NewTransferRemoteSize("big.bin", 100, nil, nil)
	second := s.NewTransferRemoteSize("big.bin", 100, nil, nil)
	in := second.Account(ctx, io.NopCloser(strings.NewReader(strings.Repeat("x", 40))))
	_, err := io.Copy(io.Discard, in)
	require.NoError(t, err)
	require.Contains(t, transferring(t, s), "big.bin")

	first.Done(ctx, nil)
	assert.Equal(t, map[string]int64{"big.bin": 40}, transferring(t, s))

	second.Done(ctx, nil)
	assert.Empty(t, transferring(t, s))
}

func TestTransferListedUntilLaterOneEnds(t *testing.T) {
	ctx := context.Background()
	s := accounting.NewStats(ctx)
	first := s.NewTransferRemoteSize("big.bin", 100, nil, nil)
	second := s.NewTransferRemoteSize("big.bin", 100, nil, nil)

	second.Done(ctx, nil)
	assert.Contains(t, transferring(t, s), "big.bin")

	first.Done(ctx, nil)
	assert.Empty(t, transferring(t, s))
}
