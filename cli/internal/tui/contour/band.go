package contour

import (
	"math/rand/v2"

	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// Theme builds the map from dokky's palette: deep teal lines brightening to mint on dark, soft to deep teal on light.
// It is the only tie to the app, so the map stays portable
func Theme(p *ui.Palette) Palette {
	r := p.Ramp
	if p.Dark {
		return Palette{Bg: p.Bg, Lo: r[1], Hi: ui.Mix(r[4], r[5], .45), Tint: ui.Mix(p.Bg, r[0], .55)}
	}
	return Palette{Bg: p.Bg, Lo: ui.Mix(r[1], r[2], .45), Hi: r[5], Tint: ui.Mix(p.Bg, r[0], .5)}
}

// band fits the map to the stage under the gate's card, following palette changes and reduced motion
type band struct {
	m   *Map
	pal *ui.Palette
}

// NewBand is the stage's band: the breathing contour map, starting somewhere new each time dokky opens
func NewBand(w, rows int) ui.Band {
	b := &band{m: NewAt(w, rows, rand.Float64()*600)}
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

func (b *band) Size() (int, int) { return b.m.Size() }
func (b *band) Step()            { b.m.Step() }

func (b *band) Lines() []string {
	b.sync()
	return Render(b.m)
}

func (b *band) Particles(int) []ui.Particle { return nil }
