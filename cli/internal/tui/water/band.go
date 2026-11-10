package water

import "github.com/itzzritik/dokky/cli/internal/tui/ui"

// band fits the water to the stage under the gate's card, following palette changes and reduced motion
type band struct {
	t   *Water
	pal *ui.Palette
}

// NewBand is the stage's band: the water in dokky's colours
func NewBand(w, rows int) ui.Band {
	b := &band{t: New(w, rows)}
	b.sync()
	return b
}

func (b *band) sync() {
	if p := ui.P(); p != b.pal {
		b.pal = p
		b.t.SetPalette(Theme(p))
	}
	b.t.SetStill(ui.Reduced())
}

func (b *band) Size() (int, int) { return b.t.Size() }
func (b *band) Step()            { b.t.Step() }

func (b *band) Lines() []string {
	b.sync()
	return Render(b.t)
}

func (b *band) Particles(top int) []ui.Particle {
	drops := b.t.Spray(top)
	out := make([]ui.Particle, 0, len(drops))
	for _, d := range drops {
		out = append(out, ui.Particle{X: d.X, Row: d.Row, Glyph: d.Glyph(), Fg: d.Fg})
	}
	return out
}
