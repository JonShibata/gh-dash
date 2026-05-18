// Package pollgate decides whether a gh-dash instance should run an
// auto-refresh cycle, so that N copies of gh-dash don't multiply the
// GitHub GraphQL rate budget.
//
// Under kitty (with remote control reachable over KITTY_LISTEN_ON), the
// decision is based on actual on-screen visibility: an instance polls
// iff its kitty window is currently shown — i.e. its tab is the active
// tab and the tab's layout isn't hiding it behind another pane. This is
// strictly better than a focus signal, because a split that's visible
// but not the focused pane still refreshes, while a pane in a background
// tab (or stacked behind another) does not. Every visible pane polls
// independently; no shared state is needed on this path. We key on tab
// visibility rather than OS-window focus on purpose, so an instance keeps
// updating while the user looks at a browser in another OS window.
//
// When kitty remote control isn't available (non-kitty terminal, or
// KITTY_LISTEN_ON unset / remote control disabled), we fall back to a
// PID-claim file: only the most-recently-focused instance holds the
// claim and polls; a missing/dead claim is taken over atomically.
package pollgate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const claimFilename = "poll.claim"

// claimPath mirrors data.getStateFilePath without importing it (avoids a
// tui→data dependency just for the directory convention).
func claimPath() (string, error) {
	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		stateDir = filepath.Join(homeDir, ".local", "state")
	}
	return filepath.Join(stateDir, "gh-dash", claimFilename), nil
}

// Claim writes our PID into the claim file, taking the poll slot from
// any previous holder. Called on tea.FocusMsg so the most-recently-
// focused gh-dash instance becomes the system-wide poller.
func Claim() error {
	p, err := claimPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".poll.claim-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := fmt.Fprintf(tmp, "%d\n", os.Getpid()); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, p); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// ShouldPoll reports whether this process should run an auto-refresh
// cycle on this tick. Under kitty with reachable remote control it
// answers purely from on-screen visibility; otherwise it falls back to
// the PID-claim mechanism (see Poll).
func ShouldPoll() bool {
	if visible, ok := kittyVisibility(); ok {
		return visible
	}
	return Poll()
}

// kittyVisibility returns (visible, true) when running under kitty with a
// reachable control socket, where visible reflects whether our window is
// currently shown on screen. It returns (false, false) — "can't tell" —
// when we're not under kitty, the socket is absent, or the `kitty @ ls`
// probe fails, so the caller can fall back to the claim file.
func kittyVisibility() (visible bool, ok bool) {
	listen := os.Getenv("KITTY_LISTEN_ON")
	winStr := strings.TrimSpace(os.Getenv("KITTY_WINDOW_ID"))
	if listen == "" || winStr == "" {
		return false, false
	}
	winID, err := strconv.Atoi(winStr)
	if err != nil {
		return false, false
	}
	// Talk to kitty over the unix socket (--to), never the TTY — gh-dash
	// holds the TTY in raw mode, so the escape-code transport would
	// corrupt input. Time-box the probe so a wedged kitty can't stall a
	// refresh tick.
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "kitty", "@", "--to", listen, "ls").Output()
	if err != nil {
		return false, false
	}
	return parseKittyVisible(out, winID)
}

// parseKittyVisible computes whether the kitty window with id winID is
// currently on screen, from the JSON emitted by `kitty @ ls`. A window
// is visible iff its tab is the active tab AND the tab's layout isn't
// hiding it: every pane in a split-style layout (grid, tall, fat, …) is
// shown at once, whereas a "stack" layout shows only its active window.
// Returns ok=false if the JSON can't be parsed or our window isn't found
// (e.g. it just closed), so the caller falls back to the claim file.
func parseKittyVisible(data []byte, winID int) (visible bool, ok bool) {
	var osWindows []struct {
		Tabs []struct {
			IsActive bool   `json:"is_active"`
			Layout   string `json:"layout"`
			Windows  []struct {
				ID       int  `json:"id"`
				IsActive bool `json:"is_active"`
			} `json:"windows"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(data, &osWindows); err != nil {
		return false, false
	}
	for _, osw := range osWindows {
		for _, tab := range osw.Tabs {
			for _, w := range tab.Windows {
				if w.ID != winID {
					continue
				}
				return tab.IsActive && (tab.Layout != "stack" || w.IsActive), true
			}
		}
	}
	return false, false
}

// Poll reports whether this process should run an auto-refresh cycle. It
// returns true if we hold the claim, or if the claim is missing/owned
// by a dead process (in which case we atomically take it). Returns
// false when another live gh-dash instance holds the claim.
func Poll() bool {
	p, err := claimPath()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Claim() == nil
		}
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return Claim() == nil
	}
	if pid == os.Getpid() {
		return true
	}
	if !processAlive(pid) {
		return Claim() == nil
	}
	return false
}

// processAlive returns true if pid names a process we can signal.
// Signal(0) is the POSIX existence probe: nil means alive; ESRCH means
// gone; EPERM means alive but owned by another user (treat as alive so
// we never steal a slot across user accounts on a shared host).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
