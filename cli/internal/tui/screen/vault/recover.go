package vault

import (
	"context"
	"image/color"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

const keyChars = 34 // after DK1-, 32 for the key and 2 for its check

// the recovery key, typed or pasted any way, and a new master password
type recoverForm struct {
	key  *ui.Secret
	np   newPassword
	at   string // the focused stop
	err  string
	req  core.Req
	join bool // the account's key: during sign in or while sync waits for it
	ks   keyState
	kn   int // the key's characters so far
}

func newRecover(join bool) *recoverForm {
	k := ui.NewSecret("", "DK1-XXXX-XXXX-"+ui.G.Ellipsis, 96)
	k.Reveal, k.Show = true, groupKey
	np := makeNew("", "")
	np.pw.Hint, np.again.Hint = "", ""
	np.pw.Eye = true
	return &recoverForm{key: k, np: np, at: "field:0", join: join}
}

// checked on each change, not each frame
func (r *recoverForm) rekey() {
	rs := r.key.Runes()
	r.ks, r.kn = checkKey(rs)
	clear(rs)
}

// what parsing ignores goes, O and I or L read as 0 and 1, the rest is upper case
func normalKey(rs []rune) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(string(rs)) {
		switch r {
		case ' ', '\t', '-', '_':
		case 'O':
			b.WriteByte('0')
		case 'I', 'L':
			b.WriteByte('1')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// the key's own characters, without DK1 in front
func keyBody(rs []rune) string {
	n := normalKey(rs)
	if strings.HasPrefix(n, "DK1") && len(n) > 3 {
		return n[3:]
	}
	return n
}

// shown as DK1-ABCD-EFGH-…, however it was typed
func groupKey(rs []rune) string {
	n := normalKey(rs)
	if n == "" {
		return ""
	}
	body, head := n, ""
	if strings.HasPrefix(n, "DK1") {
		body, head = n[3:], "DK1"
	}
	var parts []string
	if head != "" {
		parts = append(parts, head)
	}
	for i := 0; i < len(body); i += 4 {
		parts = append(parts, body[i:min(i+4, len(body))])
	}
	return strings.Join(parts, "-")
}

type keyState int

const (
	keyEmpty keyState = iota
	keyShort
	keyLong
	keyTypo
	keyGood
)

// how far along the typed key is, checked against its own check characters once whole
func checkKey(rs []rune) (keyState, int) {
	n := len(keyBody(rs))
	raw, err := vault.ParseRecoveryKey(string(rs))
	clear(raw)
	switch {
	case err == nil:
		return keyGood, n
	case n == 0:
		return keyEmpty, 0
	case n < keyChars:
		return keyShort, n
	case n > keyChars:
		return keyLong, n
	}
	return keyTypo, n
}

func keyLine(s keyState, n, w int, bg color.Color) string {
	p := ui.P()
	l := ui.L(bg)
	switch s {
	case keyEmpty:
		l.T("Any case, with or without the dashes.", p.Faint)
	case keyShort:
		l.T(strconv.Itoa(n)+" of "+strconv.Itoa(keyChars)+" characters", p.Faint)
	case keyLong:
		l.T("Too long. Check for extra characters.", p.Coral)
	case keyTypo:
		l.T("Has a typo. Check each character.", p.Coral)
	case keyGood:
		l.B(ui.G.Check, p.Kelp).Sp(1).T("Looks right", p.Kelp)
	}
	return l.Pad(w)
}

func (r *recoverForm) update(msg tea.Msg, f *Flow, st *core.State) tea.Cmd {
	switch m := msg.(type) {
	case core.Reconnected:
		r.req = 0
	case core.Reply:
		if m.Req != r.req || r.req == 0 {
			return nil
		}
		r.req = 0
		if m.Err != nil {
			r.err = Explain(m.Err, st)
			switch code(m.Err) {
			case api.ErrBadRecoveryKey:
				r.at = "field:0"
			case api.ErrWeakPassword:
				r.at = "field:1"
			}
			return nil
		}
		return tea.Batch(core.Say("Master password changed"), f.finish(st))
	case core.Click:
		if r.req != 0 {
			return nil
		}
		if slices.Contains(recoverStops, m.ID) {
			r.at = m.ID
		}
		return r.press(m.ID, st)
	case tea.KeyPressMsg, tea.PasteMsg:
		if r.req != 0 {
			return nil
		}
		k := ""
		if km, ok := msg.(tea.KeyPressMsg); ok {
			k = km.String()
		}
		switch {
		case k == "esc":
			return f.back(st)
		case ui.Pressed(k, r.at):
			return r.press(r.at, st)
		case k == "enter" && r.filled(fieldAt(r.at)):
			r.at = fieldID(fieldAt(r.at) + 1)
		case k == "enter":
			return r.submit(st)
		case ui.Walk(recoverStops, &r.at, k):
		default:
			if fl := r.field(); fl != nil && fl.Type(msg) {
				r.err = ""
				r.np.recheck()
				r.rekey()
			}
		}
	}
	return nil
}

var recoverStops = []string{"field:0", "field:1", ui.EyeID("field:1"), "field:2", "btn:recover"}

func (r *recoverForm) press(id string, st *core.State) tea.Cmd {
	switch id {
	case ui.EyeID("field:1"):
		r.np.flip()
	case "btn:recover":
		return r.submit(st)
	}
	return nil
}

func (r *recoverForm) fields() []*ui.Secret { return []*ui.Secret{r.key, r.np.pw, r.np.again} }

// the focused field, nil while the button has focus
func (r *recoverForm) field() *ui.Secret {
	if i := fieldAt(r.at); i >= 0 {
		return r.fields()[i]
	}
	return nil
}

// enter moves on from a field that has something and the next one empty
func (r *recoverForm) filled(i int) bool {
	fs := r.fields()
	return i >= 0 && i < 2 && fs[i].Len() > 0 && fs[i+1].Len() == 0
}

// the key first, then the new password's rules; -1 when both can go
func (r *recoverForm) problem() (int, string) {
	switch r.ks {
	case keyEmpty:
		return 0, "Type your recovery key."
	case keyShort:
		return 0, "The recovery key is too short."
	case keyLong, keyTypo:
		return 0, "Check the recovery key, it isn't right."
	}
	if at, msg := r.np.problem(); at >= 0 {
		return at + 1, msg
	}
	return -1, ""
}

func (r *recoverForm) submit(st *core.State) tea.Cmd {
	if at, msg := r.problem(); at >= 0 {
		r.at, r.err = fieldID(at), msg
		return nil
	}
	key, pw, join := r.key.String(), r.np.pw.String(), r.join
	r.req, r.err = core.NextReq(), ""
	return core.Do(st, r.req, func(ctx context.Context, c *client.Client) (any, error) {
		if join {
			return nil, c.VaultJoinRecover(ctx, key, pw)
		}
		return nil, c.VaultRecover(ctx, key, pw)
	})
}

func (r *recoverForm) view(st *core.State, win *ui.Window, maxH int) int {
	p := ui.P()
	win.Title = "Use your recovery key"
	sub(win, "Then choose a new master password.", r.err)
	win.Buttons = []ui.Btn{{ID: "recover", Label: busyLabel(st, r.req != 0, "Reset password", "Resetting"), Primary: true}}
	win.Active = 0
	cw, bg := win.Inner(), p.Window
	at := r.at
	if r.req != 0 {
		at = ""
	}
	// labels above the fields, as on the gate's other cards
	body := ui.NewBox()
	body.Put(0, 0, ui.L(bg).T("Recovery key", p.Muted).Pad(cw))
	body.Add(0, 1, r.key.View("field:0", cw, at, bg))
	body.Put(0, 2, keyLine(r.ks, r.kn, cw, bg))
	rows := 4 + r.np.labelled(body, 4, cw, [2]string{"New master password", "Confirm new master password"}, [2]string{"field:1", "field:2"}, at)
	win.Body = body
	return rows
}

func (r *recoverForm) actions(*core.State) []core.Action {
	if r.req != 0 {
		return nil
	}
	enter := core.Action{Key: "enter", Label: "Reset"}
	if r.filled(fieldAt(r.at)) {
		enter.Label = "Next"
	}
	return []core.Action{enter, {Key: "↑↓", Label: "Move"}}
}

func (r *recoverForm) typing() bool               { return fieldAt(r.at) >= 0 }
func (r *recoverForm) holding() bool              { return r.req != 0 }
func (r *recoverForm) animating(*core.State) bool { return r.req != 0 && !ui.Reduced() }

func (r *recoverForm) wipe() {
	r.key.Wipe()
	r.np.wipe()
	r.rekey()
}
