package gui

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/mudkipme/maibooru/internal/touchid"
)

const (
	minPINLength  = 4
	pinIterations = 100_000
)

// lockState is the lock over the window, asking for the PIN or Touch ID.
type lockState struct {
	locked  bool
	pin     string
	err     string
	touchID bool // Touch ID may unlock this time
	asked   bool // Touch ID was asked for since the lock
	busy    bool // Touch ID is asking
	cancel  context.CancelFunc
}

// pinSetup is the dialog choosing a PIN.
type pinSetup struct {
	open       bool
	pin, again string
}

// Lock locks the window, when the user chose a PIN.
func (a *App) Lock() {
	if a.s.PINHash == "" || a.lock.locked {
		return
	}
	a.lock = lockState{locked: true, touchID: a.touchIDReady()}
	a.viewer.suspend()
}

// tryPIN unlocks the app with the PIN typed.
func (a *App) tryPIN() {
	if checkPIN(a.s.PINHash, a.lock.pin) {
		a.unlock()
		return
	}
	a.lock.pin, a.lock.err = "", "Wrong PIN."
}

func (a *App) unlock() {
	if a.lock.cancel != nil {
		a.lock.cancel()
	}
	a.lock = lockState{}
	a.viewer.resume(a)
	a.flushWaiting()
}

// touchIDReady reports whether Touch ID may unlock the app now.
func (a *App) touchIDReady() bool {
	return a.s.TouchID && a.opts.TouchID != nil && a.opts.TouchID.Available()
}

func (a *App) askTouchID() {
	lk := &a.lock
	ctx, cancel := context.WithCancel(context.Background())
	lk.busy, lk.cancel, lk.err = true, cancel, ""
	go func() {
		err := a.opts.TouchID.Authenticate(ctx, "unlock your library")
		a.post(func() {
			cancel()
			if !lk.locked {
				return
			}
			lk.busy, lk.cancel = false, nil
			switch {
			case err == nil:
				a.unlock()
			case errors.Is(err, touchid.ErrCanceled), errors.Is(err, context.Canceled):
			case errors.Is(err, touchid.ErrFailed):
				lk.err = "Touch ID didn't recognize you."
			default:
				lk.err = err.Error()
			}
		})
	}()
}

func (a *App) lockView(c *ui.Context) {
	t := c.Theme()
	lk := &a.lock
	if lk.touchID && !lk.asked && c.WindowActive() {
		lk.asked = true
		a.askTouchID()
	}
	bar := c.TitleBar()
	ui.Column(c).Fill().Children(func() {
		ui.Box(c).Height(max(bar.Height, 0)).DragWindow()
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Column(c).Width(280).Gap(14).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconLock).FontSize(40).TextColor(t.Accent)
				ui.Text(c, "Maibooru is locked").FontSize(20).Bold()
				in := ui.TextInput(c.Key("pin"), &lk.pin).Placeholder("PIN").Label("PIN").Password().AutoFocus().FillWidth()
				lk.pin = digits(lk.pin)
				if in.Changed() {
					lk.err = ""
				}
				ok := len(lk.pin) >= minPINLength
				if (ui.PrimaryButton(c, "Unlock").Disabled(!ok).FillWidth().Clicked() || in.Submitted()) && ok {
					a.tryPIN()
				}
				if lk.touchID && ui.Button(c, "Use Touch ID").Disabled(lk.busy).FillWidth().Clicked() {
					a.askTouchID()
				}
				if lk.err != "" {
					ui.Text(c, lk.err).FontSize(12).TextColor(t.Danger)
				}
			})
		})
	})
}

// lockSettings is the section of the settings choosing the PIN.
func (a *App) lockSettings(c *ui.Context) (changed bool) {
	s := a.s
	on := s.PINHash != ""
	if ui.Checkbox(c.Key("pin"), &on, "Ask for a PIN on opening and after the screen locks").Changed() {
		if on {
			a.pinSetup = pinSetup{open: true}
		} else {
			s.PINHash, s.TouchID = "", false
			changed = true
		}
	}
	if s.PINHash == "" {
		return changed
	}
	if a.touchIDAvailable {
		changed = ui.Checkbox(c.Key("touchid"), &s.TouchID, "Unlock with Touch ID").Changed() || changed
	}
	if ui.Button(c, "Change PIN…").Clicked() {
		a.pinSetup = pinSetup{open: true}
	}
	return changed
}

func (a *App) pinSetupView(c *ui.Context) {
	d := &a.pinSetup
	if !d.open {
		return
	}
	t := c.Theme()
	ui.Modal(c, &d.open, func() {
		ui.Text(c, "Choose a PIN").FontSize(16).Bold()
		ui.TextInput(c.Key("pin"), &d.pin).Placeholder(fmt.Sprintf("At least %d digits", minPINLength)).Label("PIN").Password().AutoFocus().Width(280)
		again := ui.TextInput(c.Key("again"), &d.again).Placeholder("PIN again").Label("PIN again").Password().Width(280)
		d.pin, d.again = digits(d.pin), digits(d.again)
		if d.again != "" && d.pin != d.again {
			ui.Text(c, "The PINs differ.").FontSize(12).TextColor(t.Danger)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").Clicked() {
				d.open = false
			}
			ok := len(d.pin) >= minPINLength && d.pin == d.again
			if (ui.PrimaryButton(c, "Save").Disabled(!ok).Clicked() || again.Submitted()) && ok {
				a.s.PINHash = hashPIN(d.pin)
				a.save()
				*d = pinSetup{}
			}
		})
	})
}

func digits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// hashPIN returns the PIN's hash, salted, as checkPIN reads it.
func hashPIN(pin string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key, err := pbkdf2.Key(sha256.New, pin, salt, pinIterations, 32)
	if err != nil {
		panic(err)
	}
	enc := base64.RawStdEncoding
	return "pbkdf2-sha256$" + strconv.Itoa(pinIterations) + "$" + enc.EncodeToString(salt) + "$" + enc.EncodeToString(key)
}

// checkPIN reports whether pin is the one hashed.
func checkPIN(hash, pin string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pin, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}
