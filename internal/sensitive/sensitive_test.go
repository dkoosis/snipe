package sensitive

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuiltinHeuristics(t *testing.T) {
	c := &Classifier{} // no config — built-ins only
	tests := []struct {
		path string
		want []Zone
	}{
		{"internal/auth/login.go", []Zone{ZoneAuth}},
		{"pkg/crypto/cipher.go", []Zone{ZoneCrypto}},
		{"internal/store/migrations/001_init.go", []Zone{ZoneMigration}},
		{"internal/billing/charge.go", []Zone{ZonePayment}},
		{"internal/query/lookup.go", nil}, // ordinary code → no zone
	}
	for _, tt := range tests {
		got := c.Zones(tt.path)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Zones(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestConfigGlobExtendsBuiltins(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".snipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "# project sensitive zones\ninternal/secrets/** secret\n*.sql migration\n"
	if err := os.WriteFile(filepath.Join(dir, ".snipe", "sensitive"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Load(dir)

	if got := c.Zones("internal/secrets/keys.go"); !reflect.DeepEqual(got, []Zone{ZoneSecret}) {
		t.Errorf("glob rule: Zones = %v, want [secret]", got)
	}
	if got := c.Zones("db/0001_init.sql"); !reflect.DeepEqual(got, []Zone{ZoneMigration}) {
		t.Errorf("*.sql rule: Zones = %v, want [migration]", got)
	}
	// Built-ins still fire alongside config.
	if got := c.Zones("internal/auth/jwt.go"); !reflect.DeepEqual(got, []Zone{ZoneAuth}) {
		t.Errorf("built-in still active: Zones = %v, want [auth]", got)
	}
}

func TestGlobMatchesFullSegmentsOnly(t *testing.T) {
	// matchGlob in isolation — paths chosen to avoid built-in keywords.
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"**/widget.go", "internal/ui/widget.go", true},    // ends after a separator
		{"**/widget.go", "widget.go", true},                // whole path
		{"**/widget.go", "a/widget.go/b.go", true},         // between separators
		{"**/widget.go", "internal/ui/mywidget.go", false}, // partial filename — must NOT match
		{"internal/ui/**", "internal/ui/panel.go", true},   // /** prefix dir
		{"internal/ui/**", "internal/uix/panel.go", false}, // prefix must be a real dir boundary
	}
	for _, tc := range cases {
		if got := matchGlob(tc.glob, tc.path); got != tc.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.glob, tc.path, got, tc.want)
		}
	}
}

func TestLoadMissingFileIsFine(t *testing.T) {
	c := Load(t.TempDir()) // no .snipe/sensitive
	if got := c.Zones("internal/auth/x.go"); !reflect.DeepEqual(got, []Zone{ZoneAuth}) {
		t.Errorf("missing config should still apply built-ins, got %v", got)
	}
}

func TestMultipleZones(t *testing.T) {
	c := &Classifier{}
	// path mentions both crypto and token(secret) → both zones, sorted.
	got := c.Zones("internal/crypto/token_store.go")
	want := []Zone{ZoneCrypto, ZoneSecret}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Zones = %v, want %v", got, want)
	}
}
