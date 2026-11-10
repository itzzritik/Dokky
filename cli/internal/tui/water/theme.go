package water

import (
	"image/color"

	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// Theme builds the water from Dokky's palette: deep navy to white surf on dark,
// clear tropical teal on light. It is the only tie to the app, so the sim stays portable.
func Theme(p *ui.Palette) Palette {
	r := p.Ramp
	if p.Dark {
		return Palette{
			Bg:      p.Bg,
			Deep:    ui.Mix(p.Bg, r[0], .7),
			Body:    r[1],
			Shallow: ui.Mix(r[2], p.Accent, .45),
			Light:   r[4],
			Foam:    ui.Mix(r[5], color.White, .4),
		}
	}
	return Palette{
		Bg:      p.Bg,
		Deep:    ui.Mix(r[4], r[5], .15),
		Body:    ui.Mix(r[3], r[2], .15),
		Shallow: ui.Mix(r[2], r[3], .5),
		Light:   r[2],
		Foam:    ui.Mix(r[0], r[1], .35),
	}
}
