// Package unread is the shared store of unread PR activity.
//
// It is shared with the gh-pr-watch daemon (~/settings/gh-pr-watch), which
// records new comments and reviews and shows the total in the herdr tab bar.
// gh-dash reads the same store to show a per-PR count in the PR list and
// writes to it when the Activity tab shows a PR.
//
// Layout, under $XDG_STATE_HOME/gh-pr-watch/prs/<owner>/<repo>/:
//
//	<number>.events  Appended by the daemon only. One line per item the
//	                 daemon notified about: "<RFC3339 ts>\t<kind>\t<id>".
//	<number>.read    Written by gh-dash (and `gh-pr-watch --clear`): the
//	                 RFC3339 timestamp the PR has been read through. The
//	                 value only ever moves forward.
//
// An event is unread while its timestamp is after the .read timestamp. Each
// file has one writer, so the daemon and gh-dash never overwrite each
// other's updates, and marking read needs no edit to the events file.
package unread

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	eventsExt = ".events"
	readExt   = ".read"
)

// Store reads and updates the unread store. The zero value and a nil *Store
// are valid and report no unread items, so callers need no nil checks.
type Store struct {
	root string

	mu     sync.RWMutex
	counts map[string]int
	total  int
	// marked remembers the newest read-through time written per PR so a
	// repeated MarkRead (the sidebar re-renders often) costs no disk access.
	marked map[string]time.Time
}

// New returns a Store at the default location, shared with gh-pr-watch.
func New() *Store {
	return NewAt(DefaultRoot())
}

// NewAt returns a Store rooted at dir.
func NewAt(dir string) *Store {
	return &Store{root: dir, counts: map[string]int{}, marked: map[string]time.Time{}}
}

// DefaultRoot is the directory gh-pr-watch keeps its per-PR state in.
func DefaultRoot() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "gh-pr-watch", "prs")
}

func key(repo string, number int) string {
	return repo + "#" + strconv.Itoa(number)
}

func (s *Store) base(repo string, number int) string {
	return filepath.Join(s.root, filepath.FromSlash(repo), strconv.Itoa(number))
}

// Count returns the number of unread items for a PR from the last Load or
// MarkRead. repo is "owner/name".
func (s *Store) Count(repo string, number int) int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.counts[key(repo, number)]
}

// Total returns the unread items across all PRs.
func (s *Store) Total() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.total
}

// Load rescans the store and reports whether any count changed.
func (s *Store) Load() bool {
	if s == nil || s.root == "" {
		return false
	}
	counts := map[string]int{}
	total := 0

	owners, _ := os.ReadDir(s.root)
	for _, owner := range owners {
		repos, _ := os.ReadDir(filepath.Join(s.root, owner.Name()))
		for _, repo := range repos {
			dir := filepath.Join(s.root, owner.Name(), repo.Name())
			files, _ := os.ReadDir(dir)
			for _, f := range files {
				name := f.Name()
				if !strings.HasSuffix(name, eventsExt) {
					continue
				}
				number, err := strconv.Atoi(strings.TrimSuffix(name, eventsExt))
				if err != nil || number <= 0 {
					continue
				}
				repoName := owner.Name() + "/" + repo.Name()
				n := countUnread(filepath.Join(dir, name), readThrough(filepath.Join(dir, strings.TrimSuffix(name, eventsExt)+readExt)))
				if n > 0 {
					counts[key(repoName, number)] = n
					total += n
				}
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	changed := total != s.total || len(counts) != len(s.counts)
	if !changed {
		for k, v := range counts {
			if s.counts[k] != v {
				changed = true
				break
			}
		}
	}
	s.counts, s.total = counts, total
	return changed
}

// MarkRead records that a PR's activity up to and including through has been
// seen, and drops the matching items from Count right away. It reports
// whether the read-through time moved; an older through than one already
// recorded does nothing.
func (s *Store) MarkRead(repo string, number int, through time.Time) (bool, error) {
	if s == nil || s.root == "" || through.IsZero() {
		return false, nil
	}
	k := key(repo, number)
	through = through.UTC().Truncate(time.Second)

	s.mu.Lock()
	defer s.mu.Unlock()
	if !through.After(s.marked[k]) {
		return false, nil
	}

	base := s.base(repo, number)
	readFile := base + readExt
	if cur := readThrough(readFile); !through.After(cur) {
		s.marked[k] = cur
		return false, nil
	}
	if err := writeAtomic(readFile, through.Format(time.RFC3339)+"\n"); err != nil {
		return false, err
	}

	s.marked[k] = through
	n := countUnread(base+eventsExt, through)
	s.total += n - s.counts[k]
	if n > 0 {
		s.counts[k] = n
	} else {
		delete(s.counts, k)
	}
	return true, nil
}

// writeAtomic replaces path with content via a rename, so a concurrent
// reader (the daemon's --count, another gh-dash) never sees a partial file.
func writeAtomic(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".read-*")
	if err != nil {
		return err
	}
	fail := func(err error) error {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// readThrough returns the timestamp in a .read file, or the zero time when
// the file is missing or unreadable.
func readThrough(path string) time.Time {
	b, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	if err != nil {
		return time.Time{}
	}
	return t
}

// countUnread counts the distinct events in an events file newer than
// through. Malformed lines are skipped.
func countUnread(path string, through time.Time) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	seen := map[string]struct{}{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		if len(fields) < 3 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, fields[0])
		if err != nil || !ts.After(through) {
			continue
		}
		seen[fields[1]+"\t"+fields[2]] = struct{}{}
	}
	return len(seen)
}
