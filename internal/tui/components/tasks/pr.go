package tasks

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type SectionIdentifier struct {
	Id   int
	Type string
}

type UpdatePRMsg struct {
	PrNumber         int
	IsClosed         *bool
	NewComment       *data.Comment
	ReadyForReview   *bool
	IsMerged         *bool
	AddedAssignees   *data.Assignees
	RemovedAssignees *data.Assignees
	Labels           *data.PRLabels
}

type UpdateBranchMsg struct {
	Name      string
	IsCreated *bool
	NewPr     *data.PullRequestData
}

func buildTaskId(prefix string, prNumber int) string {
	return fmt.Sprintf("%s_%d", prefix, prNumber)
}

type GitHubTask struct {
	Id           string
	Args         []string
	Section      SectionIdentifier
	StartText    string
	FinishedText string
	Msg          func(c *exec.Cmd, err error) tea.Msg
}

func fireTask(ctx *context.ProgramContext, task GitHubTask) tea.Cmd {
	start := context.Task{
		Id:           task.Id,
		StartText:    task.StartText,
		FinishedText: task.FinishedText,
		State:        context.TaskStart,
		Error:        nil,
	}

	startCmd := ctx.StartTask(start)
	return tea.Batch(startCmd, func() tea.Msg {
		log.Info("Running task", "cmd", "gh "+strings.Join(task.Args, " "))
		c := exec.Command("gh", task.Args...)

		err := c.Run()
		return constants.TaskFinishedMsg{
			TaskId:      task.Id,
			SectionId:   task.Section.Id,
			SectionType: task.Section.Type,
			Err:         err,
			Msg:         task.Msg(c, err),
		}
	})
}

func OpenBranchPR(ctx *context.ProgramContext, section SectionIdentifier, branch string) tea.Cmd {
	return fireTask(ctx, GitHubTask{
		Id: fmt.Sprintf("branch_open_%s", branch),
		Args: []string{
			"pr",
			"view",
			"--web",
			branch,
			"-R",
			ctx.RepoUrl,
		},
		Section:      section,
		StartText:    fmt.Sprintf("Opening PR for branch %s", branch),
		FinishedText: fmt.Sprintf("PR for branch %s has been opened", branch),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			return UpdatePRMsg{}
		},
	})
}

func ReopenPR(ctx *context.ProgramContext, section SectionIdentifier, pr data.RowData) tea.Cmd {
	prNumber := pr.GetNumber()
	return fireTask(ctx, GitHubTask{
		Id: buildTaskId("pr_reopen", prNumber),
		Args: []string{
			"pr",
			"reopen",
			fmt.Sprint(prNumber),
			"-R",
			pr.GetRepoNameWithOwner(),
		},
		Section:      section,
		StartText:    fmt.Sprintf("Reopening PR #%d", prNumber),
		FinishedText: fmt.Sprintf("PR #%d has been reopened", prNumber),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			return UpdatePRMsg{
				PrNumber: prNumber,
				IsClosed: utils.BoolPtr(false),
			}
		},
	})
}

func ClosePR(ctx *context.ProgramContext, section SectionIdentifier, pr data.RowData) tea.Cmd {
	prNumber := pr.GetNumber()
	return fireTask(ctx, GitHubTask{
		Id: buildTaskId("pr_close", prNumber),
		Args: []string{
			"pr",
			"close",
			fmt.Sprint(prNumber),
			"-R",
			pr.GetRepoNameWithOwner(),
		},
		Section:      section,
		StartText:    fmt.Sprintf("Closing PR #%d", prNumber),
		FinishedText: fmt.Sprintf("PR #%d has been closed", prNumber),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			return UpdatePRMsg{
				PrNumber: prNumber,
				IsClosed: utils.BoolPtr(true),
			}
		},
	})
}

func PRReady(ctx *context.ProgramContext, section SectionIdentifier, pr data.RowData) tea.Cmd {
	prNumber := pr.GetNumber()
	return fireTask(ctx, GitHubTask{
		Id: buildTaskId("pr_ready", prNumber),
		Args: []string{
			"pr",
			"ready",
			fmt.Sprint(prNumber),
			"-R",
			pr.GetRepoNameWithOwner(),
		},
		Section:      section,
		StartText:    fmt.Sprintf("Marking PR #%d as ready for review", prNumber),
		FinishedText: fmt.Sprintf("PR #%d has been marked as ready for review", prNumber),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			return UpdatePRMsg{
				PrNumber:       prNumber,
				ReadyForReview: utils.BoolPtr(true),
			}
		},
	})
}

func MergePR(ctx *context.ProgramContext, section SectionIdentifier, pr data.RowData) tea.Cmd {
	prNumber := pr.GetNumber()
	c := exec.Command(
		"gh",
		"pr",
		"merge",
		fmt.Sprint(prNumber),
		"-R",
		pr.GetRepoNameWithOwner(),
	)

	taskId := fmt.Sprintf("merge_%d", prNumber)
	task := context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf("Merging PR #%d", prNumber),
		FinishedText: fmt.Sprintf("PR #%d has been merged", prNumber),
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := ctx.StartTask(task)

	return tea.Batch(startCmd, tea.ExecProcess(c, func(err error) tea.Msg {
		isMerged := err == nil && c.ProcessState.ExitCode() == 0

		return constants.TaskFinishedMsg{
			SectionId:   section.Id,
			SectionType: section.Type,
			TaskId:      taskId,
			Err:         err,
			Msg: UpdatePRMsg{
				PrNumber: prNumber,
				IsMerged: &isMerged,
			},
		}
	}))
}

func CreatePR(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	branchName string,
	title string,
) tea.Cmd {
	c := exec.Command(
		"gh",
		"pr",
		"create",
		"--title",
		title,
		"-R",
		ctx.RepoUrl,
	)

	taskId := fmt.Sprintf("create_pr_%s", title)
	task := context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf(`Creating PR "%s"`, title),
		FinishedText: fmt.Sprintf(`PR "%s" has been created`, title),
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := ctx.StartTask(task)

	return tea.Batch(startCmd, tea.ExecProcess(c, func(err error) tea.Msg {
		isCreated := err == nil && c.ProcessState.ExitCode() == 0

		return constants.TaskFinishedMsg{
			SectionId:   section.Id,
			SectionType: section.Type,
			TaskId:      taskId,
			Err:         nil,
			Msg:         UpdateBranchMsg{Name: branchName, IsCreated: &isCreated},
		}
	}))
}

func UpdatePR(ctx *context.ProgramContext, section SectionIdentifier, pr data.RowData) tea.Cmd {
	prNumber := pr.GetNumber()
	return fireTask(ctx, GitHubTask{
		Id: buildTaskId("pr_update", prNumber),
		Args: []string{
			"pr",
			"update-branch",
			fmt.Sprint(prNumber),
			"-R",
			pr.GetRepoNameWithOwner(),
		},
		Section:      section,
		StartText:    fmt.Sprintf("Updating PR #%d", prNumber),
		FinishedText: fmt.Sprintf("PR #%d has been updated", prNumber),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			return UpdatePRMsg{
				PrNumber: prNumber,
				IsClosed: utils.BoolPtr(true),
			}
		},
	})
}

func AssignPR(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	usernames []string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	args := []string{
		"pr",
		"edit",
		fmt.Sprint(prNumber),
		"-R",
		pr.GetRepoNameWithOwner(),
	}
	for _, assignee := range usernames {
		args = append(args, "--add-assignee", assignee)
	}
	return fireTask(ctx, GitHubTask{
		Id:           buildTaskId("pr_assign", prNumber),
		Args:         args,
		Section:      section,
		StartText:    fmt.Sprintf("Assigning pr #%d to %s", prNumber, usernames),
		FinishedText: fmt.Sprintf("pr #%d has been assigned to %s", prNumber, usernames),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			returnedAssignees := data.Assignees{Nodes: []data.Assignee{}}
			for _, assignee := range usernames {
				returnedAssignees.Nodes = append(
					returnedAssignees.Nodes,
					data.Assignee{Login: assignee},
				)
			}
			return UpdatePRMsg{
				PrNumber:       prNumber,
				AddedAssignees: &returnedAssignees,
			}
		},
	})
}

func UnassignPR(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	usernames []string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	args := []string{
		"pr",
		"edit",
		fmt.Sprint(prNumber),
		"-R",
		pr.GetRepoNameWithOwner(),
	}
	for _, assignee := range usernames {
		args = append(args, "--remove-assignee", assignee)
	}
	return fireTask(ctx, GitHubTask{
		Id:           buildTaskId("pr_unassign", prNumber),
		Args:         args,
		Section:      section,
		StartText:    fmt.Sprintf("Unassigning %s from pr #%d", usernames, prNumber),
		FinishedText: fmt.Sprintf("%s unassigned from pr #%d", usernames, prNumber),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			returnedAssignees := data.Assignees{Nodes: []data.Assignee{}}
			for _, assignee := range usernames {
				returnedAssignees.Nodes = append(
					returnedAssignees.Nodes,
					data.Assignee{Login: assignee},
				)
			}
			return UpdatePRMsg{
				PrNumber:         prNumber,
				RemovedAssignees: &returnedAssignees,
			}
		},
	})
}

func CommentOnPR(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	body string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	return fireTask(ctx, GitHubTask{
		Id: buildTaskId("pr_comment", prNumber),
		Args: []string{
			"pr",
			"comment",
			fmt.Sprint(prNumber),
			"-R",
			pr.GetRepoNameWithOwner(),
			"-b",
			body,
		},
		Section:      section,
		StartText:    fmt.Sprintf("Commenting on PR #%d", prNumber),
		FinishedText: fmt.Sprintf("Commented on PR #%d", prNumber),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			return UpdatePRMsg{
				PrNumber: prNumber,
				NewComment: &data.Comment{
					Author:    struct{ Login string }{Login: ctx.User},
					Body:      body,
					UpdatedAt: time.Now(),
				},
			}
		},
	})
}

func ApprovePR(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	comment string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	args := []string{
		"pr",
		"review",
		"-R",
		pr.GetRepoNameWithOwner(),
		fmt.Sprint(prNumber),
		"--approve",
	}
	if comment != "" {
		args = append(args, "--body", comment)
	}
	return fireTask(ctx, GitHubTask{
		Id:           buildTaskId("pr_approve", prNumber),
		Args:         args,
		Section:      section,
		StartText:    fmt.Sprintf("Approving pr #%d", prNumber),
		FinishedText: fmt.Sprintf("pr #%d has been approved", prNumber),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			return UpdatePRMsg{
				PrNumber: prNumber,
			}
		},
	})
}

func ApproveWorkflows(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
) tea.Cmd {
	prNumber := pr.GetNumber()
	repo := pr.GetRepoNameWithOwner()
	taskId := buildTaskId("pr_approve_workflows", prNumber)

	task := context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf("Approving workflows for PR #%d", prNumber),
		FinishedText: fmt.Sprintf("Workflows for PR #%d have been approved", prNumber),
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := ctx.StartTask(task)

	return tea.Batch(startCmd, func() tea.Msg {
		// Step 1: Get head SHA
		shaCmd := exec.Command("gh", "pr", "view", fmt.Sprint(prNumber),
			"-R", repo, "--json", "headRefOid", "--jq", ".headRefOid")
		shaOut, err := shaCmd.Output()
		if err != nil {
			return constants.TaskFinishedMsg{
				TaskId:      taskId,
				SectionId:   section.Id,
				SectionType: section.Type,
				Err:         fmt.Errorf("failed to get head SHA: %w", err),
				Msg:         UpdatePRMsg{PrNumber: prNumber},
			}
		}
		sha := strings.TrimSpace(string(shaOut))

		// Step 2: Get workflow run IDs awaiting approval
		runsCmd := exec.Command("gh", "api",
			fmt.Sprintf("repos/%s/actions/runs?status=action_required&head_sha=%s", repo, sha),
			"--jq", ".workflow_runs[].id")
		runsOut, err := runsCmd.Output()
		if err != nil {
			return constants.TaskFinishedMsg{
				TaskId:      taskId,
				SectionId:   section.Id,
				SectionType: section.Type,
				Err:         fmt.Errorf("failed to get workflow runs: %w", err),
				Msg:         UpdatePRMsg{PrNumber: prNumber},
			}
		}

		runIds := strings.Fields(strings.TrimSpace(string(runsOut)))
		if len(runIds) == 0 {
			return constants.TaskFinishedMsg{
				TaskId:      taskId,
				SectionId:   section.Id,
				SectionType: section.Type,
				Err:         fmt.Errorf("no workflows awaiting approval"),
				Msg:         UpdatePRMsg{PrNumber: prNumber},
			}
		}

		// Step 3: Approve each run (best-effort)
		var lastErr error
		approved := 0
		for _, runId := range runIds {
			log.Info("Approving workflow run", "runId", runId, "pr", prNumber)
			approveCmd := exec.Command("gh", "api", "-X", "POST",
				fmt.Sprintf("repos/%s/actions/runs/%s/approve", repo, runId))
			output, err := approveCmd.CombinedOutput()
			if err != nil {
				outStr := string(output)
				if strings.Contains(outStr, "not from a fork pull request") {
					lastErr = fmt.Errorf(
						"workflow not approvable via API (only fork PR workflows can be approved)",
					)
				} else {
					lastErr = fmt.Errorf("failed to approve run %s: %w", runId, err)
				}
			} else {
				approved++
			}
		}

		return constants.TaskFinishedMsg{
			TaskId:      taskId,
			SectionId:   section.Id,
			SectionType: section.Type,
			Err:         lastErr,
			Msg:         UpdatePRMsg{PrNumber: prNumber},
		}
	})
}

// RerunFailedChecksOnPR re-runs the failed jobs on every workflow run that
// has a failed/cancelled/timed-out check on this PR's latest commit. The
// pipeline is multi-step (list checks → extract run IDs → POST rerun) and
// has no single `gh` subcommand, so we shell out to a bash one-liner
// rather than threading three task invocations together.
func RerunFailedChecksOnPR(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
) tea.Cmd {
	prNumber := pr.GetNumber()
	repo := pr.GetRepoNameWithOwner()
	taskId := buildTaskId("pr_rerun_failed", prNumber)
	task := context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf("Rerunning failed checks on PR #%d", prNumber),
		FinishedText: fmt.Sprintf("Reran failed checks on PR #%d", prNumber),
		State:        context.TaskStart,
		Error:        nil,
	}
	jcfg := ctx.Config.Defaults.Extensions.Jenkins
	owner, name := splitOwnerRepo(repo)
	startCmd := ctx.StartTask(task)
	return tea.Batch(startCmd, func() tea.Msg {
		// Layered rerun strategy. GitHub's check-run rerequest API
		// (which the web UI's "Re-run failed checks" dropdown uses)
		// returns 404 for integrations that didn't register a
		// rerequest webhook handler — Deepfield's JenkinsMergeQueues
		// app is one such case, so the API is useless for FI Tests
		// failures. Instead we fan out by failure type:
		//
		//   1. FI sub-job failures (parsed from the FI Tests CheckRun's
		//      markdown summary table) → POST to gha_fi_test_manager
		//      with COMMENT=<sub-job-name>, one trigger per failed sub-
		//      job. Mirrors gplm's pattern.
		//   2. GitHub Actions workflow run failures → `gh run rerun
		//      --failed <id>`, the standard GHA per-run mechanism.
		//   3. Other CheckRuns (rare; e.g. third-party integrations) →
		//      try check-run rerequest, ignore 404s.
		//
		// Skipped: standalone StatusContext failures with no parent
		// CheckRun, since neither rerequest nor Jenkins-direct gives
		// us a way to selectively retry one of those.
		jenkinsBlock := ""
		if jcfg.URL != "" && jcfg.Job != "" {
			// Per-sub-job Jenkins triggers. Python regex over the FI
			// Tests check-run summary picks failed rows reliably across
			// emoji vs word status formats. crumb is fetched once and
			// reused for every POST in the loop.
			jenkinsBlock = fmt.Sprintf(
				// URL must be quoted — `?per_page=100` would otherwise
				// be globbed by bash and silently expand to nothing.
				`fi_summary=$(gh api "repos/%[2]s/commits/$sha/check-runs?per_page=100" --paginate `+
					`--jq '[.check_runs[] | select(.name=="FI Tests")] | sort_by(.started_at) | last | .output.summary // empty'); `+
					`failed_fi=$(echo "$fi_summary" | python3 -c "`+
					`import re,sys`+"\n"+
					`for ln in sys.stdin:`+"\n"+
					// Alternation MUST be a real | — \| in python regex
					// is a literal pipe, not alternation, so the earlier
					// version matched nothing on every PR.
					`    m=re.match(r'\\| \\[([^\\]]+)\\].*\\| .*(❌|failure).*\\|', ln)`+"\n"+
					`    if m: print(m.group(1))" 2>/dev/null); `+
					`if [ -n "$failed_fi" ]; then `+
					`crumb=$(curl -fsS -n %[3]s/crumbIssuer/api/json | jq -r '.crumb // empty'); `+
					`if [ -z "$crumb" ]; then echo "no Jenkins crumb (~/.netrc?)" >&2; else `+
					`meta=$(gh pr view %[1]d -R %[2]s --json headRefOid,headRefName,baseRefName); `+
					`pr_sha=$(echo "$meta" | jq -r .headRefOid); `+
					`head=$(echo "$meta" | jq -r .headRefName); `+
					`base=$(echo "$meta" | jq -r .baseRefName); `+
					`echo "$failed_fi" | while IFS= read -r job; do `+
					`[ -z "$job" ] && continue; `+
					`curl -fsS -n -X POST -H "Jenkins-Crumb: $crumb" `+
					`--data-urlencode "PIPEDREAM_SHA=$pr_sha" `+
					`--data-urlencode "PIPEDREAM_BRANCH=$head" `+
					`--data-urlencode "PIPEDREAM_TARGET_BRANCH=$base" `+
					`--data-urlencode "REPO_OWNER=%[5]s" `+
					`--data-urlencode "REPO_NAME=%[6]s" `+
					`--data-urlencode "EVENT_NAME=manual.gh-dash" `+
					`--data-urlencode "COMMENT=$job" `+
					`%[3]s/job/%[4]s/buildWithParameters >/dev/null && echo "  reran FI: $job" >&2; `+
					`done; `+
					`fi; fi; `,
				prNumber, repo, jcfg.URL, jcfg.Job, owner, name,
			)
		}
		script := fmt.Sprintf(
			`set -uo pipefail; `+
				`sha=$(gh pr view %[1]d -R %[2]s --json headRefOid --jq .headRefOid); `+
				jenkinsBlock+
				// GHA path: rerun failed jobs of any failed workflow run.
				`gha_runs=$(gh api repos/%[2]s/actions/runs?head_sha=$sha --paginate `+
				`--jq '.workflow_runs[]? | select(.conclusion=="failure" or .conclusion=="cancelled" or .conclusion=="timed_out") | .id' `+
				`| sort -u); `+
				`if [ -n "$gha_runs" ]; then `+
				`echo "$gha_runs" | xargs -I{} gh run rerun --failed -R %[2]s {} && `+
				`echo "  reran GHA workflow runs" >&2; `+
				`fi; `+
				// Other CheckRuns: try rerequest, swallow 404s.
				`other_ids=$(gh api repos/%[2]s/commits/$sha/check-runs --paginate `+
				`--jq '.check_runs[]? | select((.conclusion=="failure" or .conclusion=="cancelled" or .conclusion=="timed_out") and .name != "FI Tests") | .id'); `+
				`if [ -n "$other_ids" ]; then `+
				`echo "$other_ids" | while IFS= read -r id; do `+
				`gh api -X POST repos/%[2]s/check-runs/$id/rerequest --silent 2>/dev/null && `+
				`echo "  rerequested check-run $id" >&2; `+
				`done; `+
				`fi`,
			prNumber, repo,
		)
		log.Info("Rerunning failed checks", "pr", prNumber, "repo", repo,
			"jenkins_configured", jcfg.URL != "")
		c := exec.Command("bash", "-c", script)
		out, err := c.CombinedOutput()
		if err != nil {
			log.Error("rerun script failed", "err", err, "out", string(out))
		} else {
			log.Info("rerun script ok", "out", string(out))
		}
		return constants.TaskFinishedMsg{
			TaskId:      taskId,
			SectionId:   section.Id,
			SectionType: section.Type,
			Err:         err,
			Msg:         UpdatePRMsg{PrNumber: prNumber},
		}
	})
}

// TriggerJenkinsRerun POSTs to a configured Jenkins job (typically
// Deepfield's gha_fi_test_manager) to re-run FI tests directly, bypassing
// the GitHub Actions → proxy → Jenkins chain. No PR comment is created.
//
// Why a fork-only feature: gh-dash users at large don't run a private
// Jenkins; only this fork does. Hence config-gated by extensions.jenkins
// — when URL is empty the keypress is a no-op.
//
// Auth: ~/.netrc entry for the Jenkins host. We shell out to `curl -n`
// because Go's stdlib has no netrc support and reimplementing it is more
// risk than the convenience saves.
func TriggerJenkinsRerun(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
) tea.Cmd {
	prNumber := pr.GetNumber()
	repo := pr.GetRepoNameWithOwner()
	jcfg := ctx.Config.Defaults.Extensions.Jenkins
	taskId := buildTaskId("pr_jenkins_rerun", prNumber)
	task := context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf("Triggering Jenkins job %s for PR #%d", jcfg.Job, prNumber),
		FinishedText: fmt.Sprintf("Jenkins job queued for PR #%d", prNumber),
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := ctx.StartTask(task)
	return tea.Batch(startCmd, func() tea.Msg {
		if jcfg.URL == "" || jcfg.Job == "" {
			return constants.TaskFinishedMsg{
				TaskId:      taskId,
				SectionId:   section.Id,
				SectionType: section.Type,
				Err:         fmt.Errorf("jenkins extension not configured (set extensions.jenkins.url and .job)"),
			}
		}
		// Two-step pipeline: fetch CSRF crumb, then POST. Bash + curl
		// here because curl's -n flag handles netrc auth without bringing
		// netrc parsing into Go.
		owner, name := splitOwnerRepo(repo)
		script := fmt.Sprintf(
			`set -euo pipefail; `+
				`crumb=$(curl -fsS -n %[1]s/crumbIssuer/api/json | jq -r '.crumb // empty'); `+
				`[ -n "$crumb" ] || { echo "no crumb (check ~/.netrc for %[1]s)" >&2; exit 1; }; `+
				`meta=$(gh pr view %[2]d -R %[3]s --json headRefOid,headRefName,baseRefName,number,url,title); `+
				`sha=$(echo "$meta" | jq -r .headRefOid); `+
				`head=$(echo "$meta" | jq -r .headRefName); `+
				`base=$(echo "$meta" | jq -r .baseRefName); `+
				`curl -fsS -n -X POST -H "Jenkins-Crumb: $crumb" `+
				`--data-urlencode "PIPEDREAM_SHA=$sha" `+
				`--data-urlencode "PIPEDREAM_BRANCH=$head" `+
				`--data-urlencode "PIPEDREAM_TARGET_BRANCH=$base" `+
				`--data-urlencode "REPO_OWNER=%[4]s" `+
				`--data-urlencode "REPO_NAME=%[5]s" `+
				`--data-urlencode "EVENT_NAME=manual.gh-dash" `+
				`%[1]s/job/%[6]s/buildWithParameters >/dev/null`,
			jcfg.URL, prNumber, repo, owner, name, jcfg.Job,
		)
		log.Info("Triggering Jenkins rerun", "pr", prNumber, "url", jcfg.URL, "job", jcfg.Job)
		c := exec.Command("bash", "-c", script)
		err := c.Run()
		return constants.TaskFinishedMsg{
			TaskId:      taskId,
			SectionId:   section.Id,
			SectionType: section.Type,
			Err:         err,
			Msg:         UpdatePRMsg{PrNumber: prNumber},
		}
	})
}

// ReplyToReviewComment posts a reply under an existing inline review
// comment, using the v3 REST endpoint
// `/repos/{o}/{r}/pulls/{n}/comments/{cid}/replies`. There is no `gh pr`
// subcommand for this — only `gh api` — so we shell out the same way
// CommentOnPR does for top-level comments.
//
// commentDatabaseId is the integer REST id of the *root* thread comment
// (or any prior comment in the thread; GitHub re-anchors to the root).
// We don't post a UpdatePRMsg.NewComment back because the activity
// renderer pulls from the enriched ReviewThreads tree on the next refresh
// — synthesizing a reply node here would require knowing the thread
// index, which the task layer doesn't have.
func ReplyToReviewComment(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	commentDatabaseId int,
	body string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	repo := pr.GetRepoNameWithOwner()
	return fireTask(ctx, GitHubTask{
		Id: buildTaskId("pr_reply_review", prNumber),
		Args: []string{
			"api",
			"-X", "POST",
			fmt.Sprintf("repos/%s/pulls/%d/comments/%d/replies", repo, prNumber, commentDatabaseId),
			"-f", "body=" + body,
		},
		Section:      section,
		StartText:    fmt.Sprintf("Replying to review comment on PR #%d", prNumber),
		FinishedText: fmt.Sprintf("Replied to review comment on PR #%d", prNumber),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			return UpdatePRMsg{PrNumber: prNumber}
		},
	})
}

// RequestReviewers adds one or more users as requested reviewers on the
// PR via `gh pr edit --add-reviewer`. Mirrors AssignPR's shape: one
// `--add-reviewer USER` flag per username (gh accepts repeats). Returns
// an UpdatePRMsg so the section can refetch reviewer state on completion.
func RequestReviewers(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	usernames []string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	args := []string{
		"pr", "edit", fmt.Sprint(prNumber),
		"-R", pr.GetRepoNameWithOwner(),
	}
	for _, u := range usernames {
		args = append(args, "--add-reviewer", u)
	}
	return fireTask(ctx, GitHubTask{
		Id:           buildTaskId("pr_request_review", prNumber),
		Args:         args,
		Section:      section,
		StartText:    fmt.Sprintf("Requesting review on PR #%d from %s", prNumber, usernames),
		FinishedText: fmt.Sprintf("Review requested on PR #%d from %s", prNumber, usernames),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			return UpdatePRMsg{PrNumber: prNumber}
		},
	})
}

// ResolveReviewThread marks an inline-review thread as resolved via the
// GraphQL `resolveReviewThread` mutation. Takes the thread's GraphQL
// node id (from `ReviewThread.Id`), not a comment databaseId.
func ResolveReviewThread(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	threadId string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	return fireTask(ctx, GitHubTask{
		Id: buildTaskId("pr_resolve_thread", prNumber),
		Args: []string{
			"api", "graphql",
			"-f", "query=mutation($id: ID!) { resolveReviewThread(input: {threadId: $id}) { thread { id isResolved } } }",
			"-f", "id=" + threadId,
		},
		Section:      section,
		StartText:    fmt.Sprintf("Resolving review thread on PR #%d", prNumber),
		FinishedText: fmt.Sprintf("Resolved review thread on PR #%d", prNumber),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			return UpdatePRMsg{PrNumber: prNumber}
		},
	})
}

// UnresolveReviewThread re-opens a previously resolved thread. Same shape
// as ResolveReviewThread; needed for the X-toggle behavior so a press on
// an already-resolved thread reverses the action instead of being a
// no-op.
func UnresolveReviewThread(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	pr data.RowData,
	threadId string,
) tea.Cmd {
	prNumber := pr.GetNumber()
	return fireTask(ctx, GitHubTask{
		Id: buildTaskId("pr_unresolve_thread", prNumber),
		Args: []string{
			"api", "graphql",
			"-f", "query=mutation($id: ID!) { unresolveReviewThread(input: {threadId: $id}) { thread { id isResolved } } }",
			"-f", "id=" + threadId,
		},
		Section:      section,
		StartText:    fmt.Sprintf("Unresolving review thread on PR #%d", prNumber),
		FinishedText: fmt.Sprintf("Unresolved review thread on PR #%d", prNumber),
		Msg: func(c *exec.Cmd, err error) tea.Msg {
			return UpdatePRMsg{PrNumber: prNumber}
		},
	})
}

// splitOwnerRepo splits "owner/repo" into its two halves. Returns
// (owner, name); if no slash is present, both fall back to the whole
// string and the input as the owner — Jenkins POST then errors out
// with "missing repo", which is fine.
func splitOwnerRepo(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i], s[i+1:]
		}
	}
	return s, s
}
