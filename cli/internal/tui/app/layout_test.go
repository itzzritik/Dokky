package app

import (
	"testing"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/stretchr/testify/require"
)

// the click IDs along the footer, after a render
func footerKeys(a *App) map[string]bool {
	a.render()
	ids := map[string]bool{}
	for x := range a.st.W {
		if id := a.hits.At(x, a.st.H-2); id != "" {
			ids[id] = true
		}
	}
	return ids
}

func TestTabFooterHasMoreAndQuit(t *testing.T) {
	a := newApp(api.State{Settings: api.Settings{Chosen: true}, Drives: []api.Drive{{ID: "d", Name: "Photos", State: api.Connected}}})
	a.update(core.SwitchTab{Tab: core.DrivesTab})
	keys := footerKeys(a)
	require.True(t, keys["key:m"], "a tab always shows More")
	require.True(t, keys["key:q"])
	require.False(t, keys["key:↑↓"])
}
