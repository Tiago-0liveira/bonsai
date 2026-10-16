package vterm

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/vt"
)

// xtermMod is a key whose modified form x/vt does not encode, so we emit the
// xterm sequence ourselves: ESC [ <param> ; <mod> <final>.
type xtermMod struct {
	param string // "1" for cursor keys, the number for ~ keys
	final string
	mod   int // 1 + shift(1) + alt(2) + ctrl(4), before the alt flag is added
}

const (
	modShift = 2
	modCtrl  = 5
	modCS    = 6
)

var xtermKeys = map[tea.KeyType]xtermMod{
	tea.KeyShiftUp:        {"1", "A", modShift},
	tea.KeyShiftDown:      {"1", "B", modShift},
	tea.KeyShiftRight:     {"1", "C", modShift},
	tea.KeyShiftLeft:      {"1", "D", modShift},
	tea.KeyCtrlUp:         {"1", "A", modCtrl},
	tea.KeyCtrlDown:       {"1", "B", modCtrl},
	tea.KeyCtrlRight:      {"1", "C", modCtrl},
	tea.KeyCtrlLeft:       {"1", "D", modCtrl},
	tea.KeyCtrlShiftUp:    {"1", "A", modCS},
	tea.KeyCtrlShiftDown:  {"1", "B", modCS},
	tea.KeyCtrlShiftRight: {"1", "C", modCS},
	tea.KeyCtrlShiftLeft:  {"1", "D", modCS},
	tea.KeyShiftHome:      {"1", "H", modShift},
	tea.KeyShiftEnd:       {"1", "F", modShift},
	tea.KeyCtrlHome:       {"1", "H", modCtrl},
	tea.KeyCtrlEnd:        {"1", "F", modCtrl},
	tea.KeyCtrlShiftHome:  {"1", "H", modCS},
	tea.KeyCtrlShiftEnd:   {"1", "F", modCS},
	tea.KeyCtrlPgUp:       {"5", "~", modCtrl},
	tea.KeyCtrlPgDown:     {"6", "~", modCtrl},
}

// plainKeys maps bubbletea key types to the emulator key code they encode as.
// Aliases (KeyCtrlI == KeyTab, KeyCtrlM == KeyEnter, KeyCtrlOpenBracket ==
// KeyEsc) collapse onto one entry, which is the encoding they share anyway.
var plainKeys = map[tea.KeyType]rune{
	tea.KeyEnter:     vt.KeyEnter,
	tea.KeyTab:       vt.KeyTab,
	tea.KeyBackspace: vt.KeyBackspace,
	tea.KeyEsc:       vt.KeyEscape,
	tea.KeyDelete:    vt.KeyDelete,
	tea.KeyInsert:    vt.KeyInsert,
	tea.KeyHome:      vt.KeyHome,
	tea.KeyEnd:       vt.KeyEnd,
	tea.KeyPgUp:      vt.KeyPgUp,
	tea.KeyPgDown:    vt.KeyPgDown,
	tea.KeyUp:        vt.KeyUp,
	tea.KeyDown:      vt.KeyDown,
	tea.KeyLeft:      vt.KeyLeft,
	tea.KeyRight:     vt.KeyRight,
}

var fnKeys = [...]rune{
	vt.KeyF1, vt.KeyF2, vt.KeyF3, vt.KeyF4, vt.KeyF5, vt.KeyF6,
	vt.KeyF7, vt.KeyF8, vt.KeyF9, vt.KeyF10, vt.KeyF11, vt.KeyF12,
}

// ctrlPunct maps the control codes above ctrl+z that have a printable base key.
var ctrlPunct = map[tea.KeyType]rune{
	tea.KeyCtrlBackslash:  '\\',
	tea.KeyCtrlCaret:      '^',
	tea.KeyCtrlUnderscore: '_',
}

// toKeyEvent translates a bubbletea key. Exactly one of ev (encoded by the
// emulator, which honors DECCKM and keypad modes) or raw (an xterm sequence
// x/vt cannot produce) is meaningful when ok is true.
func toKeyEvent(k tea.KeyMsg) (ev vt.KeyPressEvent, raw string, ok bool) {
	var mod vt.KeyMod
	if k.Alt {
		mod = vt.ModAlt
	}

	if x, found := xtermKeys[k.Type]; found {
		m := x.mod
		if k.Alt {
			m += 2
		}
		return ev, "\x1b[" + x.param + ";" + strconv.Itoa(m) + x.final, true
	}
	if code, found := plainKeys[k.Type]; found {
		return vt.KeyPressEvent{Code: code, Mod: mod}, "", true
	}
	if code, found := ctrlPunct[k.Type]; found {
		return vt.KeyPressEvent{Code: code, Mod: mod | vt.ModCtrl}, "", true
	}

	switch {
	case k.Type == tea.KeyRunes:
		if len(k.Runes) == 0 {
			return ev, "", false
		}
		return vt.KeyPressEvent{Code: k.Runes[0], Text: string(k.Runes), Mod: mod}, "", true
	case k.Type == tea.KeySpace:
		return vt.KeyPressEvent{Code: vt.KeySpace, Text: " ", Mod: mod}, "", true
	case k.Type == tea.KeyShiftTab:
		return vt.KeyPressEvent{Code: vt.KeyTab, Mod: mod | vt.ModShift}, "", true
	case k.Type == tea.KeyCtrlAt:
		return vt.KeyPressEvent{Code: vt.KeySpace, Mod: mod | vt.ModCtrl}, "", true
	case k.Type >= tea.KeyCtrlA && k.Type <= tea.KeyCtrlZ:
		return vt.KeyPressEvent{Code: 'a' + rune(k.Type-tea.KeyCtrlA), Mod: mod | vt.ModCtrl}, "", true
	case k.Type <= tea.KeyF1 && k.Type >= tea.KeyF12:
		// Bubbletea numbers F keys downward; F13-F20 have no x/vt encoding.
		return vt.KeyPressEvent{Code: fnKeys[tea.KeyF1-k.Type], Mod: mod}, "", true
	}
	// Includes ctrl+] (KeyCtrlCloseBracket): the TUI owns it as the prefix key.
	return ev, "", false
}
