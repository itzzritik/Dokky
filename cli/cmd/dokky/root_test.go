package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})
	require.NoError(t, cmd.Execute())
	require.Equal(t, "dokky 0.0.0-dev\n", out.String())
}
