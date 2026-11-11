package topography

import (
	"image"
	"math/rand/v2"

	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// Theme takes dokky's accent: glowing teal lines on dark, teal ink on light. It is the only tie to the app
func Theme(p *ui.Palette) Palette { return Palette{Bg: p.Bg, Line: p.Accent, Light: !p.Dark} }

// band fits the map to a stage or backdrop, following palette changes and reduced motion
type band struct {
	m   *Map
	pal *ui.Palette
}

// NewBand is the full-screen backdrop: the gliding contour map, starting somewhere new each time
func NewBand(w, rows int) ui.Band {
	b := &band{m: NewAt(w, rows, rand.Float64()*900)}
	b.sync()
	return b
}

func (b *band) sync() {
	if p := ui.P(); p != b.pal {
		b.pal = p
		b.m.SetPalette(Theme(p))
	}
	b.m.SetStill(ui.Reduced())
}

func (b *band) Size() (int, int)         { return b.m.Size() }
func (b *band) Step()                    { b.m.Step() }
func (b *band) Calm(r []image.Rectangle) { b.m.Calm(r) }

func (b *band) Lines() []string {
	b.sync()
	return Render(b.m)
}
