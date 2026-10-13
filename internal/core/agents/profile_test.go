package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"
)

// profileTestProvider stores two defaults, "model" and "mode", as plain keys.
type profileTestProvider struct {
	accountServiceTestProvider
}

func (p *profileTestProvider) ProfileFields() []LaunchField {
	return ProfileFieldsFrom([]LaunchField{{ID: "mode", Label: "Mode", Kind: LaunchFieldSelect, Options: []FieldOption{{Value: "a", Label: "A"}, {Value: "b", Label: "B"}}}})
}

func (p *profileTestProvider) ProfileDefaults(account Account) map[string]string {
	var values map[string]string
	_ = json.Unmarshal(account.Settings, &values)
	return values
}

func (p *profileTestProvider) UpdateProfile(account Account, changes map[string]*string) (json.RawMessage, error) {
	merged := map[string]any{}
	for id, value := range changes {
		if value == nil || *value == "" {
			merged[id] = nil
			continue
		}
		if id == "mode" && *value != "a" && *value != "b" {
			return nil, fmt.Errorf("unsupported mode %q", *value)
		}
		merged[id] = *value
	}
	return MergeSettings(account.Settings, merged)
}

func newProfileTestService(t *testing.T) (*AccountService, AccountStore, Account) {
	t.Helper()
	provider := &profileTestProvider{}
	accounts, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	account := testAccount("acct_profile", provider.ID(), "work")
	account.Settings = json.RawMessage(`{"auth_mode":"token","mode":"a"}`)
	if err := accounts.Create(account); err != nil {
		t.Fatal(err)
	}
	return &AccountService{Store: accounts, Sessions: sessions, Registry: registry}, accounts, account
}

func ptr(s string) *string { return &s }

// sameAccount compares accounts by value: stored settings are re-indented and
// stored times lose their monotonic reading.
func sameAccount(a, b Account) bool {
	var sa, sb any
	_ = json.Unmarshal(a.Settings, &sa)
	_ = json.Unmarshal(b.Settings, &sb)
	return a.ID == b.ID && a.Provider == b.Provider && a.Name == b.Name &&
		a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt) && reflect.DeepEqual(sa, sb)
}

func TestProfileFieldsFromAddsModelAndProviderDefault(t *testing.T) {
	fields := ProfileFieldsFrom([]LaunchField{
		{ID: "effort", Label: "Effort", Kind: LaunchFieldSelect, Default: "high", Options: []FieldOption{{Value: "", Label: "Profile default"}, {Value: "high", Label: "high"}}},
		{ID: "preset", Label: "Preset", Kind: LaunchFieldSelect, Default: "x", Options: []FieldOption{{Value: "x", Label: "X", Set: map[string]string{"k": "v"}}}},
		{ID: "full_access", Label: "Full access", Kind: LaunchFieldToggle, Options: []FieldOption{{Value: "false", Label: "Off"}, {Value: "true", Label: "On"}}},
	})
	if got := FieldIDs(fields); !slices.Equal(got, []string{"model", "effort", "preset", "full_access"}) {
		t.Fatalf("ids = %v", got)
	}
	if fields[0].Kind != LaunchFieldModel {
		t.Fatalf("model kind = %q", fields[0].Kind)
	}
	for _, field := range fields[1:3] {
		if field.Default != "" || field.Options[0].Value != "" || field.Options[0].Label != ProviderDefaultLabel {
			t.Fatalf("%s: %+v", field.ID, field)
		}
		for _, option := range field.Options[1:] {
			if option.Value == "" {
				t.Fatalf("%s has two empty options", field.ID)
			}
		}
	}
	if got := FieldValues(fields[3]); !slices.Equal(got, []string{"false", "true"}) {
		t.Fatalf("toggle options = %v", got)
	}
	// The launch field's preset map is not shared.
	fields[2].Options[1].Set["k"] = "changed"
	if again := ProfileFieldsFrom([]LaunchField{{ID: "preset", Kind: LaunchFieldSelect, Options: []FieldOption{{Value: "x", Set: map[string]string{"k": "v"}}}}}); again[1].Options[1].Set["k"] != "v" {
		t.Fatal("preset map shared")
	}
}

func TestPresetValueAndSettings(t *testing.T) {
	field := LaunchField{ID: "permissions", Label: "Permissions", Kind: LaunchFieldSelect, Options: []FieldOption{
		{Value: "", Label: ProviderDefaultLabel},
		{Value: "auto", Set: map[string]string{"sandbox": "workspace-write", "approval": "on-request"}},
		{Value: "bypass", Set: map[string]string{"sandbox": "bypass"}},
	}}
	cases := []struct {
		values map[string]string
		want   string
	}{
		{map[string]string{"sandbox": "workspace-write", "approval": "on-request"}, "auto"},
		{map[string]string{"sandbox": "bypass"}, "bypass"},
		{map[string]string{"sandbox": "bypass", "approval": "never"}, ""},
		{map[string]string{"sandbox": "workspace-write"}, ""},
		{map[string]string{}, ""},
	}
	for _, c := range cases {
		if got := PresetValue(field, c.values); got != c.want {
			t.Errorf("PresetValue(%v) = %q, want %q", c.values, got, c.want)
		}
	}
	got, err := PresetSettings(field, "bypass")
	if err != nil || !reflect.DeepEqual(got, map[string]string{"sandbox": "bypass", "approval": ""}) {
		t.Fatalf("bypass = %v, %v", got, err)
	}
	got, err = PresetSettings(field, "")
	if err != nil || !reflect.DeepEqual(got, map[string]string{"sandbox": "", "approval": ""}) {
		t.Fatalf("unset = %v, %v", got, err)
	}
	if _, err := PresetSettings(field, "yolo"); err == nil || err.Error() != `unsupported permissions "yolo" (use auto, bypass)` {
		t.Fatalf("unknown preset error = %v", err)
	}
}

func TestMergeSettingsKeepsOtherKeys(t *testing.T) {
	raw, err := MergeSettings(json.RawMessage(`{"auth_mode":"token","seeded_from":"/x","future":{"a":1},"effort":"low"}`), map[string]any{"effort": nil, "model": "m"})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"auth_mode":"token","future":{"a":1},"model":"m","seeded_from":"/x"}` {
		t.Fatalf("merged = %s", raw)
	}
	if raw, err := MergeSettings(json.RawMessage(`{"model":"m"}`), map[string]any{"model": nil}); err != nil || raw != nil {
		t.Fatalf("empty merge = %s, %v", raw, err)
	}
	if raw, err := MergeSettings(nil, map[string]any{"model": "m"}); err != nil || string(raw) != `{"model":"m"}` {
		t.Fatalf("merge into nothing = %s, %v", raw, err)
	}
	if _, err := MergeSettings(json.RawMessage(`[1]`), map[string]any{"model": "m"}); err == nil {
		t.Fatal("array settings accepted")
	}
}

func TestAccountServiceUpdateProfile(t *testing.T) {
	service, store, account := newProfileTestService(t)
	ctx := context.Background()

	updated, err := service.UpdateProfile(ctx, account.ID, ProfileUpdate{Name: "renamed", Defaults: map[string]*string{"model": ptr("m1"), "mode": nil}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "renamed" || string(updated.Settings) != `{"auth_mode":"token","model":"m1"}` {
		t.Fatalf("updated = %+v (%s)", updated, updated.Settings)
	}
	if !updated.UpdatedAt.After(account.UpdatedAt) || !updated.CreatedAt.Equal(account.CreatedAt) {
		t.Fatalf("timestamps = %v / %v", updated.CreatedAt, updated.UpdatedAt)
	}
	stored, _ := store.Get(account.ID)
	if !sameAccount(stored, updated) {
		t.Fatalf("stored = %+v (%s)", stored, stored.Settings)
	}

	// Rejections write nothing, the rename included.
	for name, update := range map[string]ProfileUpdate{
		"bad value":     {Name: "other", Defaults: map[string]*string{"mode": ptr("c")}},
		"unknown field": {Name: "other", Defaults: map[string]*string{"auth_mode": ptr("login")}},
	} {
		_, err := service.UpdateProfile(ctx, account.ID, update)
		if !errors.Is(err, ErrInvalidProfile) {
			t.Fatalf("%s: err = %v", name, err)
		}
		if again, _ := store.Get(account.ID); !sameAccount(again, stored) {
			t.Fatalf("%s changed the account: %+v", name, again)
		}
	}
	if _, err := service.UpdateProfile(ctx, account.ID, ProfileUpdate{Defaults: map[string]*string{"auth_mode": ptr("login")}}); err == nil || err.Error() != `unknown profile default "auth_mode" (use model, mode)` {
		t.Fatalf("unknown field error = %v", err)
	}

	other := testAccount("acct_other", account.Provider, "taken")
	if err := store.Create(other); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateProfile(ctx, account.ID, ProfileUpdate{Name: "taken"}); !errors.Is(err, ErrAccountExists) {
		t.Fatalf("taken name err = %v", err)
	}
	if _, err := service.UpdateProfile(ctx, account.ID, ProfileUpdate{Name: "-bad"}); !errors.Is(err, ErrInvalidAccountName) {
		t.Fatalf("bad name err = %v", err)
	}
	if _, err := service.UpdateDefaults(ctx, "acct_missing", map[string]*string{"model": ptr("m")}); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("missing account err = %v", err)
	}
}

func TestAccountServiceUpdateProfileWithoutEditor(t *testing.T) {
	provider := &accountServiceTestProvider{}
	service, store, _, _ := newAccountServiceTestRuntime(t, provider, nil)
	account := testAccount("acct_plain", provider.ID(), "plain")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateDefaults(context.Background(), account.ID, map[string]*string{"model": ptr("m")}); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("err = %v", err)
	}
	// A rename alone needs no editor.
	if renamed, err := service.UpdateProfile(context.Background(), account.ID, ProfileUpdate{Name: "renamed"}); err != nil || renamed.Name != "renamed" {
		t.Fatalf("rename = %+v, %v", renamed, err)
	}
}

func TestAccountServiceRemoveRefusesAccountInUse(t *testing.T) {
	service, store, account := newProfileTestService(t)
	dir := store.AccountDir(account.ID)
	var probed AccountID
	_, err := service.Remove(context.Background(), account.ID, RemoveOptions{InUse: func(id AccountID) []string {
		probed = id
		return []string{"s1", "s2"}
	}})
	var inUse *AccountInUseError
	if !errors.Is(err, ErrAccountInUse) || !errors.As(err, &inUse) || !slices.Equal(inUse.SessionIDs, []string{"s1", "s2"}) {
		t.Fatalf("err = %v", err)
	}
	if probed != account.ID {
		t.Fatalf("probed %q", probed)
	}
	if _, err := store.Get(account.ID); err != nil {
		t.Fatalf("account removed: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("account dir removed: %v", err)
	}
	if _, err := service.Remove(context.Background(), account.ID, RemoveOptions{InUse: func(AccountID) []string { return nil }}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(account.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("account kept: %v", err)
	}
}

func TestAccountServiceSessionRecords(t *testing.T) {
	service, _, account := newProfileTestService(t)
	if records, err := service.SessionRecords(account.ID); err != nil || len(records) != 0 {
		t.Fatalf("records = %v, %v", records, err)
	}
	first, err := service.Sessions.Create(account, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sessions.Create(Account{ID: "acct_else", Provider: account.Provider, Name: "else"}, ""); err != nil {
		t.Fatal(err)
	}
	records, err := service.SessionRecords(account.ID)
	if err != nil || len(records) != 1 || records[0].ID != first.ID {
		t.Fatalf("records = %v, %v", records, err)
	}
	if err := service.Sessions.Cleanup(first); err != nil {
		t.Fatal(err)
	}
	if records, _ := service.SessionRecords(account.ID); len(records) != 0 {
		t.Fatalf("records after cleanup = %v", records)
	}
}

func TestFileAccountStoreModifyKeepsIdentity(t *testing.T) {
	store, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount("acct_modify", "fake", "a")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Modify(account.ID, func(a Account) (Account, error) { a.Provider = "other"; return a, nil }); err == nil {
		t.Fatal("provider changed")
	}
	if _, err := store.Modify(account.ID, func(a Account) (Account, error) { a.ID = "acct_x"; return a, nil }); err == nil {
		t.Fatal("id changed")
	}
	sentinel := errors.New("stop")
	if _, err := store.Modify(account.ID, func(Account) (Account, error) { return Account{}, sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v", err)
	}
	if got, _ := store.Get(account.ID); got.Name != "a" || !got.UpdatedAt.Equal(account.UpdatedAt) {
		t.Fatalf("account changed: %+v", got)
	}
}
