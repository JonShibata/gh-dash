package data

import (
	"sync"

	gh "github.com/cli/go-gh/v2/pkg/api"
	graphql "github.com/cli/shurcooL-graphql"
)

var (
	repoUserCache = make(map[string][]User)
	userCacheMu   sync.RWMutex

	repoTeamCache = make(map[string][]Team)
	teamCacheMu   sync.RWMutex
)

type User struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

// Team is a GitHub organization team that can be requested as a PR
// reviewer. Slug + Org compose the `org/slug` reviewer reference that
// `gh pr edit --add-reviewer` accepts. Name and Description are
// display-only.
type Team struct {
	Slug        string
	Name        string
	Description string
	Org         string
}

// Reviewer returns the canonical "org/slug" form used by
// `gh pr edit --add-reviewer` and shown to the user as a single token.
func (t Team) Reviewer() string {
	return t.Org + "/" + t.Slug
}

type MentionableUsersResponse struct {
	Repository struct {
		MentionableUsers struct {
			Nodes []User
		} `graphql:"mentionableUsers(first: $limit)"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

func CachedRepoUsers(repoNameWithOwner string) ([]User, bool) {
	userCacheMu.RLock()
	defer userCacheMu.RUnlock()
	users, ok := repoUserCache[repoNameWithOwner]
	return users, ok
}

// FetchRepoUsers fetches users that can be mentioned in a repository.
// It uses the publicly available mentionableUsers field which includes
// anyone who can interact with the repository (issue/PR authors, commenters, etc.)
func FetchRepoUsers(owner, repoName string) ([]User, error) {
	// Check cache first
	repo := owner + "/" + repoName
	if cachedUsers, ok := CachedRepoUsers(repo); ok {
		return cachedUsers, nil
	}

	// Initialize client if needed
	if client == nil {
		var err error
		client, err = gh.DefaultGraphQLClient()
		if err != nil {
			return nil, err
		}
	}

	// Query only publicly available mentionable users
	// This includes anyone who has interacted with the repo (issues, PRs, comments)
	var result MentionableUsersResponse
	variables := map[string]any{
		"owner": graphql.String(owner),
		"name":  graphql.String(repoName),
		"limit": graphql.Int(100),
	}

	err := client.Query("GetMentionableUsers", &result, variables)
	if err != nil {
		return nil, err
	}

	users := result.Repository.MentionableUsers.Nodes

	userCacheMu.Lock()
	defer userCacheMu.Unlock()

	repoUserCache[repo] = users
	return users, nil
}

func ClearUserCache() {
	userCacheMu.Lock()
	defer userCacheMu.Unlock()
	repoUserCache = make(map[string][]User)
}

func ClearRepoUserCache(repoNameWithOwner string) {
	userCacheMu.Lock()
	defer userCacheMu.Unlock()
	delete(repoUserCache, repoNameWithOwner)
}

type OrgTeamsResponse struct {
	Organization struct {
		Teams struct {
			Nodes []struct {
				Slug        string
				Name        string
				Description string
			}
		} `graphql:"teams(first: 100)"`
	} `graphql:"organization(login: $owner)"`
}

func CachedRepoTeams(repoNameWithOwner string) ([]Team, bool) {
	teamCacheMu.RLock()
	defer teamCacheMu.RUnlock()
	teams, ok := repoTeamCache[repoNameWithOwner]
	return teams, ok
}

// FetchRepoTeams returns the teams of the repo's owner-org that the
// caller can see. Used to populate review-request suggestions with
// `org/slug` entries. Failure modes (owner is a personal account, the
// viewer isn't an org member, the org has no teams) are all silent —
// the function returns an empty slice and caches the empty result so
// subsequent autocomplete keystrokes don't re-query.
func FetchRepoTeams(owner, repoName string) ([]Team, error) {
	repo := owner + "/" + repoName
	if cached, ok := CachedRepoTeams(repo); ok {
		return cached, nil
	}

	if client == nil {
		var err error
		client, err = gh.DefaultGraphQLClient()
		if err != nil {
			return nil, err
		}
	}

	var result OrgTeamsResponse
	variables := map[string]any{
		"owner": graphql.String(owner),
	}
	err := client.Query("GetOrgTeams", &result, variables)
	if err != nil {
		// Owner is a user, not org, or viewer lacks org-member
		// access. Cache an empty slice so we don't retry on every
		// keystroke; the user can `Ctrl+f` to refresh later.
		teamCacheMu.Lock()
		repoTeamCache[repo] = []Team{}
		teamCacheMu.Unlock()
		return []Team{}, nil
	}

	teams := make([]Team, 0, len(result.Organization.Teams.Nodes))
	for _, n := range result.Organization.Teams.Nodes {
		teams = append(teams, Team{
			Slug:        n.Slug,
			Name:        n.Name,
			Description: n.Description,
			Org:         owner,
		})
	}

	teamCacheMu.Lock()
	repoTeamCache[repo] = teams
	teamCacheMu.Unlock()
	return teams, nil
}

func ClearRepoTeamCache(repoNameWithOwner string) {
	teamCacheMu.Lock()
	defer teamCacheMu.Unlock()
	delete(repoTeamCache, repoNameWithOwner)
}

func UserLogins(users []User) []string {
	logins := make([]string, len(users))
	for i, user := range users {
		logins[i] = user.Login
	}
	return logins
}

// splitRepoName splits "owner/repo" into ["owner", "repo"]
func splitRepoName(repoNameWithOwner string) []string {
	parts := make([]string, 0, 2)
	start := 0
	for i, c := range repoNameWithOwner {
		if c == '/' {
			if i > start {
				parts = append(parts, repoNameWithOwner[start:i])
			}
			start = i + 1
		}
	}
	if start < len(repoNameWithOwner) {
		parts = append(parts, repoNameWithOwner[start:])
	}
	return parts
}
