package data

import (
	graphql "github.com/cli/shurcooL-graphql"
)

type BranchProtectionRules struct {
	Nodes []struct {
		RequiredApprovingReviewCount int
		RequiresApprovingReviews     graphql.Boolean
		RequiresCodeOwnerReviews     graphql.Boolean
		RequiresStatusChecks         graphql.Boolean
		RequiredStatusCheckContexts  []graphql.String
	}
}

// DefaultBranchRequirements is the slice of a repository needed to know which checks its
// default branch requires, from rulesets and from classic branch protection.
type DefaultBranchRequirements struct {
	DefaultBranchRef struct {
		Name                 string
		BranchProtectionRule struct {
			RequiredStatusCheckContexts []graphql.String
		}
		Rules struct {
			Nodes []struct {
				Type       string
				Parameters struct {
					RequiredStatusChecks struct {
						RequiredStatusChecks []struct {
							Context graphql.String
						}
					} `graphql:"... on RequiredStatusChecksParameters"`
				}
			}
		} `graphql:"rules(first: 20)"`
	}
}

// Name is the default branch's name, or "" when unknown.
func (d DefaultBranchRequirements) Name() string {
	return d.DefaultBranchRef.Name
}

// RequiredChecks is the set of check names the default branch requires.
func (d DefaultBranchRequirements) RequiredChecks() map[string]bool {
	required := make(map[string]bool)
	for _, c := range d.DefaultBranchRef.BranchProtectionRule.RequiredStatusCheckContexts {
		required[string(c)] = true
	}
	for _, rule := range d.DefaultBranchRef.Rules.Nodes {
		if rule.Type != "REQUIRED_STATUS_CHECKS" {
			continue
		}
		for _, c := range rule.Parameters.RequiredStatusChecks.RequiredStatusChecks {
			required[string(c.Context)] = true
		}
	}
	return required
}

type GitHubLogin string

type Owner struct {
	Login string
}

type Repository struct {
	Name                  string
	Owner                 Owner
	NameWithOwner         string
	IsArchived            bool
	BranchProtectionRules BranchProtectionRules `graphql:"branchProtectionRules(first: 1)"`
}
