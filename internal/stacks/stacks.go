// Package stacks reads GitHub's native stacked pull requests: which PRs form a
// stack, in what order, and which of them are still open. gh-dash uses it to
// show a stack badge and to merge a stack together instead of one PR at a time.
//
// The data comes from GET /repos/{owner}/{repo}/stacks, the endpoint the
// `gh stack` extension uses. It is a preview API, so every failure here is
// non-fatal: callers fall back to treating PRs as unstacked.
package stacks

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

// PR is one member of a stack.
type PR struct {
	Number  int
	State   string // "open" or "closed"
	Draft   bool
	Merged  bool
	HeadRef string
}

// Stack is an ordered chain of PRs, bottom (closest to the base) first.
type Stack struct {
	Number int
	Base   string
	Open   bool
	PRs    []PR
}

// Info is where one PR sits in its stack.
type Info struct {
	Stack    int
	Position int // 1-based, counted from the bottom
	Size     int
	Merged   int   // members already merged, anywhere in the stack
	Below    []int // members below this PR that are not merged, bottom first
}

// Short is the compact badge for the list, such as "stack 3/5".
func (i Info) Short() string {
	return fmt.Sprintf("stack %d/%d", i.Position, i.Size)
}

// Long is the badge for the sidebar, such as "stack #65129 · 5/5 · 4 merged".
func (i Info) Long() string {
	s := fmt.Sprintf("stack #%d · %d/%d", i.Stack, i.Position, i.Size)
	if i.Merged > 0 {
		s += fmt.Sprintf(" · %d merged", i.Merged)
	}
	return s
}

// MergePrompt is the confirmation text for merging pr together with the open
// PRs below it. It is empty when nothing is below, so the plain prompt applies.
func (i Info) MergePrompt(pr int) string {
	if len(i.Below) == 0 {
		return ""
	}
	nums := append(slices.Clone(i.Below), pr)
	parts := make([]string, len(nums))
	for n, num := range nums {
		parts[n] = fmt.Sprintf("#%d", num)
	}
	list := strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	return fmt.Sprintf("#%d is in stack #%d. Merge %s together, all or nothing? (Y/n) ", pr, i.Stack, list)
}

// Index maps each PR number in the open stacks to its place in its stack.
func Index(all []Stack) map[int]Info {
	idx := map[int]Info{}
	for _, s := range all {
		if !s.Open {
			continue
		}
		merged := 0
		for _, p := range s.PRs {
			if p.Merged {
				merged++
			}
		}
		var below []int
		for pos, p := range s.PRs {
			idx[p.Number] = Info{
				Stack:    s.Number,
				Position: pos + 1,
				Size:     len(s.PRs),
				Merged:   merged,
				Below:    slices.Clone(below),
			}
			if !p.Merged {
				below = append(below, p.Number)
			}
		}
	}
	return idx
}

type apiPR struct {
	Number   int     `json:"number"`
	State    string  `json:"state"`
	Draft    bool    `json:"draft"`
	MergedAt *string `json:"merged_at"`
	Head     struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

type apiStack struct {
	Number int `json:"number"`
	Base   struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Open         bool    `json:"open"`
	PullRequests []apiPR `json:"pull_requests"`
}

// Parse decodes the stacks endpoint's body. It accepts one JSON array of
// stacks, or an array of such arrays as `gh api --paginate --slurp` prints.
func Parse(body []byte) ([]Stack, error) {
	var top []json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, fmt.Errorf("parsing stacks: %w", err)
	}
	var out []Stack
	add := func(list []apiStack) {
		for _, a := range list {
			s := Stack{Number: a.Number, Base: a.Base.Ref, Open: a.Open}
			for _, p := range a.PullRequests {
				s.PRs = append(s.PRs, PR{
					Number:  p.Number,
					State:   p.State,
					Draft:   p.Draft,
					Merged:  p.MergedAt != nil,
					HeadRef: p.Head.Ref,
				})
			}
			out = append(out, s)
		}
	}
	for _, raw := range top {
		trimmed := strings.TrimSpace(string(raw))
		if strings.HasPrefix(trimmed, "[") {
			var page []apiStack
			if err := json.Unmarshal(raw, &page); err != nil {
				return nil, fmt.Errorf("parsing stacks page: %w", err)
			}
			add(page)
			continue
		}
		var one apiStack
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, fmt.Errorf("parsing stack: %w", err)
		}
		add([]apiStack{one})
	}
	return out, nil
}

// Fetch lists the stacks of repo ("owner/name") through the gh CLI.
func Fetch(repo string) ([]Stack, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "api", "--paginate", "--slurp",
		fmt.Sprintf("repos/%s/stacks?per_page=100", repo)).Output()
	if err != nil {
		return nil, fmt.Errorf("listing stacks of %s: %w", repo, err)
	}
	return Parse(out)
}

const (
	// refreshAfter is how long a fetched stack list is reused. Stacks change
	// when PRs are added, merged or unstacked, so this is short; a merge also
	// invalidates it right away.
	refreshAfter = 20 * time.Second
	// backoff is how long to leave a repo alone after a failed fetch (a repo
	// without stacks, an outage, a missing preview).
	backoff = 5 * time.Minute
)

// Store caches the stack index per repo. A nil *Store reports no stacks.
type Store struct {
	mu       sync.Mutex
	byRepo   map[string]map[int]Info
	fetched  map[string]time.Time
	inflight map[string]bool
	retryAt  map[string]time.Time
	now      func() time.Time
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{
		byRepo:   map[string]map[int]Info{},
		fetched:  map[string]time.Time{},
		inflight: map[string]bool{},
		retryAt:  map[string]time.Time{},
		now:      time.Now,
	}
}

// Info returns the cached stack position of a PR.
func (s *Store) Info(repo string, pr int) (Info, bool) {
	if s == nil {
		return Info{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.byRepo[repo][pr]
	return info, ok
}

// Below returns the cached unmerged PRs below pr in its stack.
func (s *Store) Below(repo string, pr int) []int {
	info, _ := s.Info(repo, pr)
	return info.Below
}

// Begin reports whether the caller should fetch repo now, and marks the fetch
// as running. It is false while one is running, while the cache is fresh and
// while backing off from a failure. Pair every true with a call to Done.
func (s *Store) Begin(repo string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.inflight[repo] || now.Before(s.retryAt[repo]) {
		return false
	}
	if t, ok := s.fetched[repo]; ok && now.Sub(t) < refreshAfter {
		return false
	}
	s.inflight[repo] = true
	return true
}

// Done records the result of a fetch started by Begin and reports whether the
// cached positions changed. On error the old cache stays and the repo backs off.
func (s *Store) Done(repo string, fetched []Stack, err error) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, repo)
	if err != nil {
		s.retryAt[repo] = s.now().Add(backoff)
		return false
	}
	idx := Index(fetched)
	s.fetched[repo] = s.now()
	delete(s.retryAt, repo)
	if reflect.DeepEqual(idx, s.byRepo[repo]) {
		return false
	}
	s.byRepo[repo] = idx
	return true
}

// Invalidate makes the next Begin for repo fetch again, for after a merge
// that changed the stack.
func (s *Store) Invalidate(repo string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.fetched, repo)
	delete(s.retryAt, repo)
}
