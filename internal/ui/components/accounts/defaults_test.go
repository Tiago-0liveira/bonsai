package accounts

import (
	"errors"
	"strings"
	"testing"
)

// editPermissionsAndFullAccess changes two fields on the Defaults tab of c1:
// permissions to full-access and the full-access toggle on. The cursor ends
// on the toggle.
func editPermissionsAndFullAccess(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = press(m, "3", "enter", "down", "enter")
	m, _ = press(m, "down", "down", "enter")
	return m
}

func TestDefaultsSaveSendsExactlyChangedKeys(t *testing.T) {
	api := newFake()
	m := editPermissionsAndFullAccess(t, opened(t, api, 104, 34))
	if out := plain(m); !strings.Contains(out, "● unsaved") {
		t.Fatalf("edits not marked unsaved:\n%s", out)
	}
	m, _ = press(m, "ctrl+s")

	updates := api.Updates()
	if len(updates) != 1 {
		t.Fatalf("updates = %+v", updates)
	}
	u := updates[0]
	if u.ID != "c1" || len(u.Values) != 2 {
		t.Fatalf("update = %+v, want exactly permissions and full_access", u)
	}
	if v := u.Values["permissions"]; v == nil || *v != "full-access" {
		t.Fatalf("permissions = %v", v)
	}
	if v := u.Values["full_access"]; v == nil || *v != "true" {
		t.Fatalf("full_access = %v", v)
	}
	if _, ok := u.Values["effort"]; ok {
		t.Fatalf("unchanged effort was sent: %+v", u.Values)
	}
	if out := plain(m); !strings.Contains(out, "Saved") || strings.Contains(out, "● unsaved") {
		t.Fatalf("after save:\n%s", out)
	}
	if m.df.form.Dirty() {
		t.Fatal("form still dirty after the reload")
	}
}

func TestDefaultsSaveButtonSavesWhenDirty(t *testing.T) {
	api := newFake()
	m := editPermissionsAndFullAccess(t, opened(t, api, 104, 34))
	m, _ = press(m, "down", "enter") // last field -> Reset/Save row -> Save defaults
	if len(api.Updates()) != 1 {
		t.Fatalf("Save defaults button did not save: %+v", api.Updates())
	}
}

func TestDefaultsSaveNoopWhenClean(t *testing.T) {
	api := newFake()
	m := opened(t, api, 104, 34)
	m, _ = press(m, "3", "ctrl+s")
	if got := api.Updates(); len(got) != 0 {
		t.Fatalf("clean form saved: %+v", got)
	}
}

func TestDefaultsResetRestoresProviderDefaults(t *testing.T) {
	api := newFake()
	m := editPermissionsAndFullAccess(t, opened(t, api, 104, 34))
	m, _ = press(m, "down", "left", "enter") // to the Reset button and press it
	if out := plain(m); strings.Contains(out, "● unsaved") {
		t.Fatalf("reset left edits behind:\n%s", out)
	}
	if got := api.Updates(); len(got) != 0 {
		t.Fatalf("reset must not save: %+v", got)
	}
}

func TestDefaultsSaveErrorKeepsEdits(t *testing.T) {
	api := newFake()
	api.UpdateErr = errors.New("disk full")
	m := editPermissionsAndFullAccess(t, opened(t, api, 104, 34))
	m, _ = press(m, "ctrl+s")
	out := plain(m)
	if !strings.Contains(out, "Couldn't save: disk full") || !strings.Contains(out, "● unsaved") {
		t.Fatalf("save error must keep the edits and say why:\n%s", out)
	}
	if strings.Contains(out, "Saved") {
		t.Fatalf("failed save shows Saved:\n%s", out)
	}
}

func TestDefaultsOverviewValuesFollowSave(t *testing.T) {
	api := newFake()
	m := editPermissionsAndFullAccess(t, opened(t, api, 104, 34))
	m, _ = press(m, "ctrl+s", "1")
	if out := plain(m); !strings.Contains(out, "Default effort") || !strings.Contains(out, "Provider default") {
		t.Fatalf("overview defaults:\n%s", out)
	}
}
