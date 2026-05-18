package pollgate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPollClaimsWhenFileMissing(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if !Poll() {
		t.Fatal("Poll() returned false on empty state dir; expected claim")
	}
	if !Poll() {
		t.Fatal("Poll() returned false after we already hold the claim")
	}
}

func TestPollSkipsWhenForeignLivePIDHoldsClaim(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	// pid 1 (init) is reliably alive on Linux; this simulates a
	// different gh-dash instance currently holding the slot.
	writeClaim(t, dir, "1\n")

	if Poll() {
		t.Fatal("Poll() returned true while a live foreign PID holds the claim")
	}
}

func TestPollTakesOverStaleClaim(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	// Very large PID highly unlikely to be alive — simulates a
	// previous gh-dash process that exited without cleanup.
	writeClaim(t, dir, "2147483600\n")

	if !Poll() {
		t.Fatal("Poll() did not take over a dead-PID claim")
	}
	if !Poll() {
		t.Fatal("Poll() did not retain claim after takeover")
	}
}

func TestClaimOverwrites(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	writeClaim(t, dir, "1\n")
	if err := Claim(); err != nil {
		t.Fatalf("Claim() error: %v", err)
	}
	if !Poll() {
		t.Fatal("Poll() returned false after explicit Claim()")
	}
}

// kittyLs builds a minimal `kitty @ ls` JSON payload with a single OS
// window holding one tab (active state + layout) and the given windows.
func kittyLs(tabActive bool, layout string, windows ...map[string]any) string {
	wins := make([]string, 0, len(windows))
	for _, w := range windows {
		wins = append(wins, fmt.Sprintf(`{"id":%d,"is_active":%t}`, w["id"], w["active"]))
	}
	return fmt.Sprintf(
		`[{"tabs":[{"is_active":%t,"layout":%q,"windows":[%s]}]}]`,
		tabActive, layout, strings.Join(wins, ","),
	)
}

func TestParseKittyVisible(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		winID       int
		wantVisible bool
		wantOK      bool
	}{
		{
			// The reported bug: gh-dash is an unfocused split in the
			// active tab. Grid shows every pane, so it's visible.
			name:        "unfocused split in active grid tab is visible",
			json:        kittyLs(true, "grid", map[string]any{"id": 30, "active": true}, map[string]any{"id": 42, "active": false}),
			winID:       42,
			wantVisible: true,
			wantOK:      true,
		},
		{
			name:        "window in a background tab is hidden",
			json:        kittyLs(false, "grid", map[string]any{"id": 42, "active": true}),
			winID:       42,
			wantVisible: false,
			wantOK:      true,
		},
		{
			// Stack layout shows only its active window; a non-active
			// window in the active tab is stacked behind and hidden.
			name:        "stacked-behind window in active tab is hidden",
			json:        kittyLs(true, "stack", map[string]any{"id": 49, "active": true}, map[string]any{"id": 36, "active": false}),
			winID:       36,
			wantVisible: false,
			wantOK:      true,
		},
		{
			name:        "active window of an active stack tab is visible",
			json:        kittyLs(true, "stack", map[string]any{"id": 49, "active": true}, map[string]any{"id": 36, "active": false}),
			winID:       49,
			wantVisible: true,
			wantOK:      true,
		},
		{
			name:        "missing window reports not-ok so caller falls back",
			json:        kittyLs(true, "grid", map[string]any{"id": 30, "active": true}),
			winID:       999,
			wantVisible: false,
			wantOK:      false,
		},
		{
			name:        "malformed json reports not-ok",
			json:        `not json`,
			winID:       42,
			wantVisible: false,
			wantOK:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			visible, ok := parseKittyVisible([]byte(tt.json), tt.winID)
			if visible != tt.wantVisible || ok != tt.wantOK {
				t.Fatalf("parseKittyVisible() = (%t, %t), want (%t, %t)",
					visible, ok, tt.wantVisible, tt.wantOK)
			}
		})
	}
}

func writeClaim(t *testing.T, stateDir, contents string) {
	t.Helper()
	dir := filepath.Join(stateDir, "gh-dash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, claimFilename), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
