package platform

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSocketAddrPerStateDir(t *testing.T) {
	a, b := SocketAddr(`C:\Users\r\AppData\Local\dokky`), SocketAddr(`C:\Temp\other`)
	require.True(t, strings.HasPrefix(a, `\\.\pipe\dokky-`))
	require.NotEqual(t, a, b, "each state folder gets its own pipe")
	require.Equal(t, a, SocketAddr(`C:\Users\r\AppData\Local\dokky`))
}
