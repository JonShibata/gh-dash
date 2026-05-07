package prview

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	ghchecks "github.com/dlvhdr/x/gh-checks"
)

type checkSectionStatus int

const (
	statusSuccess checkSectionStatus = iota
	statusFailure
	statusWaiting
	statusNonRequested
)

func (m *Model) renderChecksOverview() string {
	w := m.getIndentedContentWidth()

	if m.pr.Data.Primary.State == "MERGED" {
		return m.viewMergedStatus()
	}

	if m.pr.Data.Primary.State == "CLOSED" {
		return m.viewClosedStatus()
	}

	review, rStatus := m.viewReviewStatus()
	checks, cStatus := m.viewChecksStatus()
	merge, mStatus := m.viewMergeStatus()

	borderColor := m.ctx.Theme.FaintBorder
	if rStatus == statusFailure || cStatus == statusFailure || mStatus == statusFailure {
		borderColor = m.ctx.Theme.ErrorText
	} else if rStatus == statusSuccess && cStatus == statusSuccess && mStatus == statusSuccess {
		borderColor = m.ctx.Theme.SuccessText
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(w)
	parts := make([]string, 0)
	if review != "" {
		parts = append(parts, review)
	}
	if checks != "" {
		parts = append(parts, checks)
	}
	if merge != "" {
		parts = append(parts, merge)
	}

	return box.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (m *Model) viewChecksStatus() (string, checkSectionStatus) {
	checks := ""

	if !m.pr.Data.IsEnriched {
		return m.viewCheckCategory(
			m.ctx.Styles.Common.WaitingGlyph,
			"Loading...",
			"",
			false,
		), statusWaiting
	}

	stats := m.getChecksStats()
	var icon, title string
	var status checkSectionStatus

	statStrs := make([]string, 0)
	if stats.failed > 0 {
		icon = m.ctx.Styles.Common.FailureGlyph
		title = "Some checks were not successful"
		status = statusFailure
	} else if stats.awaitingApproval > 0 {
		icon = m.ctx.Styles.Common.ActionRequiredGlyph
		title = "Workflows awaiting approval"
		status = statusWaiting
	} else if stats.inProgress > 0 {
		icon = m.ctx.Styles.Common.WaitingGlyph
		title = "Some checks haven’t completed yet"
		status = statusWaiting
	} else if stats.succeeded > 0 {
		icon = m.ctx.Styles.Common.SuccessGlyph
		title = "All checks have passed"
		status = statusSuccess
	} else {
		return "", statusWaiting
	}

	if stats.failed > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d failing", stats.failed))
	}
	if stats.awaitingApproval > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d awaiting approval", stats.awaitingApproval))
	}
	if stats.inProgress > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d in progress", stats.inProgress))
	}
	if stats.skipped > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d skipped", stats.skipped))
	}
	if stats.neutral > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d neutral", stats.neutral))
	}
	if stats.succeeded > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d successful", stats.succeeded))
	}
	if title != "" {
		checksBar := m.viewChecksBar()
		checksBottom := lipgloss.JoinVertical(
			lipgloss.Left,
			strings.Join(statStrs, ", "),
			checksBar,
		)
		checks = m.viewCheckCategory(icon, title, checksBottom, false)
	}
	return checks, status
}

func (m *Model) viewMergeStatus() (string, checkSectionStatus) {
	var icon, title, subtitle string
	var status checkSectionStatus
	numReviewOwners := m.numRequestedReviewOwners()
	if m.pr.Data.Primary.MergeStateStatus == "CLEAN" ||
		m.pr.Data.Primary.MergeStateStatus == "UNSTABLE" {
		icon = m.ctx.Styles.Common.SuccessGlyph
		title = "No conflicts with base branch"
		subtitle = "Changes can be cleanly merged"
		status = statusSuccess
	} else if m.pr.Data.Primary.IsDraft {
		icon = m.ctx.Styles.Common.DraftGlyph
		title = "This pull request is still a work in progress"
		subtitle = "Draft pull requests cannot be merged"
		status = statusWaiting
	} else if m.pr.Data.Primary.MergeStateStatus == "BLOCKED" {
		icon = m.ctx.Styles.Common.FailureGlyph
		title = "Merging is blocked"
		if numReviewOwners > 0 {
			subtitle = "Waiting on code owner review"
		}
		status = statusFailure
	} else if m.pr.Data.Primary.Mergeable == "CONFLICTING" {
		icon = m.ctx.Styles.Common.FailureGlyph
		title = "This branch has conflicts that must be resolved"
		status = statusFailure
		if m.pr.Data.Primary.MergeStateStatus == "CLEAN" {
			subtitle = "Changes can be cleanly merged"
		}
	}
	return m.viewCheckCategory(icon, title, subtitle, true), status
}

func (m *Model) viewMergedStatus() string {
	w := m.getIndentedContentWidth()
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Styles.Colors.MergedPR).
		Width(w)
	return box.Render(m.viewCheckCategory(
		m.ctx.Styles.Common.MergedGlyph,
		"Pull request successfully merged and closed",
		"The branch has been merged",
		true,
	))
}

func (m *Model) viewClosedStatus() string {
	w := m.getIndentedContentWidth()
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Theme.FaintBorder).
		Width(w)
	return box.Render(m.viewCheckCategory(
		"",
		"Closed with unmerged commits",
		"This pull request is closed",
		true,
	))
}

func (m *Model) viewReviewStatus() (string, checkSectionStatus) {
	pr := m.pr
	if pr.Data == nil {
		return "", statusWaiting
	}

	var icon, title, subtitle string
	var status checkSectionStatus
	numReviewOwners := m.numRequestedReviewOwners()

	numApproving, numChangesRequested, numPending, numCommented := 0, 0, 0, 0

	for _, node := range pr.Data.Primary.Reviews.Nodes {
		switch node.State {
		case "APPROVED":
			numApproving++
		case "CHANGES_REQUESTED":
			numChangesRequested++
		case "PENDING":
			numPending++
		case "COMMENTED":
			numCommented++
		}
	}

	switch pr.Data.Primary.ReviewDecision {
	case "APPROVED":
		icon = m.ctx.Styles.Common.SuccessGlyph
		title = "Changes approved"
		subtitle = fmt.Sprintf("%d approving reviews", numApproving)
		status = statusSuccess
	case "CHANGES_REQUESTED":
		icon = m.ctx.Styles.Common.FailureGlyph
		title = "Changes requested"
		subtitle = fmt.Sprintf("%d requested changes", numChangesRequested)
		status = statusFailure
	case "REVIEW_REQUIRED":
		icon = pr.Ctx.Styles.Common.WaitingGlyph
		title = "Review Required"

		branchRules := m.pr.Data.Primary.Repository.BranchProtectionRules.Nodes
		if len(branchRules) > 0 && branchRules[0].RequiresCodeOwnerReviews && numApproving < 1 {
			subtitle = "Code owner review required"
			status = statusFailure
		} else if numApproving < numReviewOwners {
			subtitle = "Code owner review required"
			status = statusFailure
		} else if len(branchRules) > 0 && numApproving <
			branchRules[0].RequiredApprovingReviewCount {
			subtitle = fmt.Sprintf("Need %d more approval",
				branchRules[0].RequiredApprovingReviewCount-numApproving)
			status = statusWaiting
		} else if numCommented > 0 {
			subtitle = fmt.Sprintf("%d reviewers left comments", numCommented)
			status = statusWaiting
		}
	default:
		icon = pr.Ctx.Styles.Common.PersonGlyph
		title = "Reviews"
		subtitle = "None requested"
		status = statusNonRequested
	}

	return m.viewCheckCategory(icon, title, subtitle, false), status
}

func (m *Model) viewCheckCategory(icon, title, subtitle string, isLast bool) string {
	w := m.getIndentedContentWidth() - 2
	part := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, !isLast, false).
		BorderForeground(m.ctx.Theme.FaintBorder).
		Width(w).
		Padding(1)

	sTitle := lipgloss.NewStyle().Bold(true)
	sSub := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)

	category := lipgloss.JoinHorizontal(lipgloss.Top, icon, " ", sTitle.Render(title))

	if subtitle != "" {
		category = lipgloss.JoinVertical(
			lipgloss.Left,
			category,
			sSub.MarginLeft(2).Render(subtitle),
		)
	}
	if category == "" {
		return ""
	}
	return part.Render(category)
}

func (m *Model) viewChecksBar() string {
	w := m.getIndentedContentWidth() - 6
	stats := m.getChecksStats()
	total := float64(
		stats.failed + stats.skipped + stats.neutral + stats.succeeded + stats.inProgress + stats.awaitingApproval,
	)
	numSections := 0
	if stats.failed > 0 {
		numSections++
	}
	if stats.awaitingApproval > 0 {
		numSections++
	}
	if stats.inProgress > 0 {
		numSections++
	}
	if stats.skipped > 0 || stats.neutral > 0 {
		numSections++
	}
	if stats.succeeded > 0 {
		numSections++
	}
	// subtract number of spacers
	w -= numSections - 1
	if w < 0 {
		w = 0
	}

	sections := make([]string, 0)
	if stats.failed > 0 {
		failWidth := int(math.Floor((float64(stats.failed) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(failWidth).Foreground(
			m.ctx.Theme.ErrorText).Height(1).Render(strings.Repeat("▃", failWidth)))
	}
	if stats.awaitingApproval > 0 {
		awWidth := int(math.Floor((float64(stats.awaitingApproval) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(awWidth).Foreground(
			m.ctx.Theme.WarningText).Height(1).Render(strings.Repeat("▃", awWidth)))
	}
	if stats.inProgress > 0 {
		ipWidth := int(math.Floor((float64(stats.inProgress) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(ipWidth).Foreground(
			m.ctx.Theme.WarningText).Height(1).Render(strings.Repeat("▃", ipWidth)))
	}
	if stats.skipped > 0 || stats.neutral > 0 {
		skipWidth := int(math.Floor((float64(stats.skipped+stats.neutral) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(skipWidth).Foreground(
			m.ctx.Theme.FaintText).Height(1).Render(strings.Repeat("▃", skipWidth)))
	}
	if stats.succeeded > 0 {
		succWidth := int(math.Floor((float64(stats.succeeded) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(succWidth).Foreground(
			m.ctx.Theme.SuccessText).Height(1).Render(strings.Repeat("▃", succWidth)))
	}

	return strings.Join(sections, " ")
}

func renderCheckRunName(checkRun data.CheckRun) string {
	var parts []string
	creator := strings.TrimSpace(string(checkRun.CheckSuite.Creator.Login))
	if creator != "" {
		parts = append(parts, creator)
	}

	workflow := strings.TrimSpace(string(checkRun.CheckSuite.WorkflowRun.Workflow.Name))
	if workflow != "" {
		parts = append(parts, workflow)
	}

	name := strings.TrimSpace(string(checkRun.Name))
	if name != "" {
		parts = append(parts, name)
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		strings.Join(parts, "/"),
	)
}

type CheckCategory int

const (
	CheckWaiting CheckCategory = iota
	CheckFailure
	CheckSuccess
)

// renderCheckBadge returns a solid-background pill for a check status, e.g.
// " ✓ PASS ", " ✗ FAIL ", " ● PEND ". Heavier visual weight than a plain
// glyph so failures and pending counts pop in a busy checks list.
func (m *Model) renderCheckBadge(category CheckCategory) string {
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.ctx.Theme.InvertedText).
		Padding(0, 1)
	switch category {
	case CheckFailure:
		return style.Background(m.ctx.Theme.ErrorText).Render("✗ FAIL")
	case CheckWaiting:
		return style.Background(m.ctx.Theme.WarningText).Render("● PEND")
	case CheckSuccess:
		return style.Background(m.ctx.Theme.SuccessText).Render("✓ PASS")
	default:
		return style.Background(m.ctx.Theme.FaintText).Render("· SKIP")
	}
}

func (m *Model) renderCheckRunConclusion(checkRun data.CheckRun) (CheckCategory, string) {
	if ghchecks.IsStatusWaiting(string(checkRun.Status)) {
		return CheckWaiting, m.renderCheckBadge(CheckWaiting)
	}

	if ghchecks.IsConclusionAFailure(string(checkRun.Conclusion)) {
		return CheckFailure, m.renderCheckBadge(CheckFailure)
	}

	return CheckSuccess, m.renderCheckBadge(CheckSuccess)
}

func (m *Model) renderStatusContextConclusion(
	statusContext data.StatusContext,
) (CheckCategory, string) {
	conclusionStr := string(statusContext.State)
	if ghchecks.IsStatusWaiting(conclusionStr) {
		return CheckWaiting, m.renderCheckBadge(CheckWaiting)
	}

	if ghchecks.IsConclusionAFailure(conclusionStr) {
		return CheckFailure, m.renderCheckBadge(CheckFailure)
	}

	return CheckSuccess, m.renderCheckBadge(CheckSuccess)
}

func renderStatusContextName(statusContext data.StatusContext) string {
	var parts []string
	creator := strings.TrimSpace(string(statusContext.Creator.Login))
	if creator != "" {
		parts = append(parts, creator)
	}

	context := strings.TrimSpace(string(statusContext.Context))
	if context != "" && context != "/" {
		parts = append(parts, context)
	}
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		strings.Join(parts, "/"),
	)
}

func (sidebar *Model) renderChecks() string {
	title := sidebar.ctx.Styles.Common.MainTextStyle.MarginBottom(1).
		Underline(true).
		Render(" All Checks")

	commits := sidebar.pr.Data.Enriched.Commits.Nodes
	if len(commits) == 0 {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			"Loading...",
		)
	}

	failures := make([]string, 0)
	waiting := make([]string, 0)
	rest := make([]string, 0)
	awaitingApproval := make([]string, 0)
	pending := make([]string, 0)

	lastCommit := commits[0]

	// Collect check suites that don't appear in statusCheckRollup
	for _, suite := range lastCommit.Commit.CheckSuites.Nodes {
		workflowName := strings.TrimSpace(string(suite.WorkflowRun.Workflow.Name))
		if workflowName == "" {
			workflowName = strings.TrimSpace(string(suite.App.Name))
		}
		if workflowName == "" {
			workflowName = "Workflow"
		}

		if suite.Conclusion == "ACTION_REQUIRED" {
			// Workflow requires approval before it can run
			check := lipgloss.JoinHorizontal(
				lipgloss.Top,
				sidebar.ctx.Styles.Common.ActionRequiredGlyph,
				" ",
				workflowName,
			)
			awaitingApproval = append(awaitingApproval, check)
		} else if suite.Conclusion == "" && suite.CheckRuns.TotalCount > 0 &&
			(suite.Status == "QUEUED" || suite.Status == "PENDING" || suite.Status == "WAITING") {
			// Workflow is genuinely queued/pending (no conclusion yet,
			// has produced check-runs in the past or is about to). Skip
			// when conclusion is set (already finished) OR when no
			// check-runs exist (integration subscription noise like
			// Cursor / Figma that never actually run anything).
			check := lipgloss.JoinHorizontal(
				lipgloss.Top,
				sidebar.ctx.Styles.Common.WaitingGlyph,
				" ",
				workflowName,
			)
			pending = append(pending, check)
		}
	}

	// Build a set of reported check names to compare against required checks
	reportedChecks := make(map[string]bool)
	// Dedup by check name within this render pass. CheckRun and StatusContext
	// can mirror each other for the same check (Jenkins does this routinely),
	// which would otherwise render the same row twice. Mirrors the `seen` map
	// in getChecksStats.
	seen := make(map[string]bool)

	// Pre-pass: collect FI sub-job names from any "FI Tests" check-run.
	// gha_fi_test_manager publishes both an aggregate "FI Tests" check
	// AND a per-sub-job CheckRun (Auth_FI_Tests, BGP_FI_Tests, etc.)
	// posted via dfJenkinsGithubUser. Without this dedup, sub-jobs render
	// twice — once expanded under FI Tests, once as standalone rows.
	// gplm uses the same fi_job_names skip set for the same reason.
	fiSubJobNames := make(map[string]bool)
	for _, node := range lastCommit.Commit.StatusCheckRollup.Contexts.Nodes {
		if node.Typename != "CheckRun" {
			continue
		}
		if subJobs, _ := parseFITests(node.CheckRun); len(subJobs) > 0 {
			for _, j := range subJobs {
				fiSubJobNames[j.Name] = true
			}
		}
	}

	for _, node := range lastCommit.Commit.StatusCheckRollup.Contexts.Nodes {
		var category CheckCategory
		var check string
		var checkName string
		var checkUrl string
		switch node.Typename {
		case "CheckRun":
			checkRun := node.CheckRun
			// FI Tests posts a markdown table of per-job results in its
			// summary. Expand it into one row per sub-job (matching the
			// per-check rendering style) so the user sees individual
			// failures without leaving gh-dash. Falls through to normal
			// rendering when the summary is empty / the parser yields
			// nothing.
			if subJobs, _ := parseFITests(checkRun); len(subJobs) > 0 {
				for _, j := range subJobs {
					row := sidebar.renderFISubJobRow(j)
					reportedChecks[j.Name] = true
					switch j.Category {
					case CheckWaiting:
						waiting = append(waiting, row)
					case CheckFailure:
						failures = append(failures, row)
						if j.URL != "" {
							urlLine := lipgloss.NewStyle().
								Foreground(sidebar.ctx.Theme.FaintText).
								PaddingLeft(8).
								Render("→ " + j.URL)
							failures = append(failures, urlLine)
						}
					default:
						rest = append(rest, row)
					}
				}
				continue
			}
			// Skip per-sub-job CheckRuns whose name we already rendered as
			// part of the expanded FI Tests block (see fiSubJobNames pre-
			// pass above).
			if fiSubJobNames[string(checkRun.Name)] {
				continue
			}
			var renderedStatus string
			category, renderedStatus = sidebar.renderCheckRunConclusion(checkRun)
			checkName = string(checkRun.Name)
			checkUrl = string(checkRun.DetailsUrl)
			name := renderCheckRunName(checkRun)
			check = lipgloss.JoinHorizontal(lipgloss.Top, renderedStatus, " ", name)
		case "StatusContext":
			statusContext := node.StatusContext
			// Skip the StatusContext duplicates of FI sub-jobs too.
			// `gh pr checks` blends statuses with check-runs; without
			// this filter every Auth_FI_Tests / BGP_FI_Tests etc. would
			// also show up under its StatusContext form.
			if fiSubJobNames[string(statusContext.Context)] {
				continue
			}
			var status string
			category, status = sidebar.renderStatusContextConclusion(statusContext)
			checkName = string(statusContext.Context)
			checkUrl = string(statusContext.TargetUrl)
			check = lipgloss.JoinHorizontal(
				lipgloss.Top,
				status,
				" ",
				renderStatusContextName(statusContext),
			)
		}

		if checkName != "" {
			if seen[checkName] {
				continue
			}
			seen[checkName] = true
		}
		reportedChecks[checkName] = true

		switch category {
		case CheckWaiting:
			waiting = append(waiting, check)
		case CheckFailure:
			failures = append(failures, check)
			// Drilldown URL on its own indented line under each failure.
			// Lets the reader copy/click the build log without opening a
			// separate view; mirrors gplm's "→ <url>" pattern.
			if checkUrl != "" {
				urlLine := lipgloss.NewStyle().
					Foreground(sidebar.ctx.Theme.FaintText).
					PaddingLeft(8).
					Render("→ " + checkUrl)
				failures = append(failures, urlLine)
			}
		default:
			rest = append(rest, check)
		}
	}

	// Check for required status checks that haven't been reported yet
	branchRules := sidebar.pr.Data.Primary.Repository.BranchProtectionRules.Nodes
	if len(branchRules) > 0 {
		for _, requiredContext := range branchRules[0].RequiredStatusCheckContexts {
			contextName := string(requiredContext)
			if !reportedChecks[contextName] {
				// Required check hasn't been reported yet
				check := lipgloss.JoinHorizontal(
					lipgloss.Top,
					sidebar.ctx.Styles.Common.WaitingGlyph,
					" ",
					contextName,
				)
				pending = append(pending, check)
			}
		}
	}

	if len(awaitingApproval)+len(pending)+len(waiting)+len(failures)+len(rest) == 0 {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			lipgloss.NewStyle().
				Italic(true).
				PaddingLeft(2).
				Width(sidebar.getIndentedContentWidth()).
				Render("No checks to display..."),
		)
	}

	parts := make([]string, 0)

	// Show awaiting approval workflows first
	if len(awaitingApproval) > 0 {
		sectionHeader := lipgloss.NewStyle().
			Bold(true).
			Foreground(sidebar.ctx.Theme.WarningText).
			Render(fmt.Sprintf("Awaiting Approval (%d)", len(awaitingApproval)))
		parts = append(parts, sectionHeader)
		parts = append(parts, awaitingApproval...)
		parts = append(parts, "") // spacing
	}

	// Show pending workflows
	if len(pending) > 0 {
		sectionHeader := lipgloss.NewStyle().
			Bold(true).
			Foreground(sidebar.ctx.Theme.WarningText).
			Render(fmt.Sprintf("Pending (%d)", len(pending)))
		parts = append(parts, sectionHeader)
		parts = append(parts, pending...)
		parts = append(parts, "") // spacing
	}

	parts = append(parts, failures...)
	parts = append(parts, waiting...)
	parts = append(parts, rest...)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		lipgloss.NewStyle().PaddingLeft(2).Width(sidebar.getIndentedContentWidth()).Render(
			lipgloss.JoinVertical(lipgloss.Left, parts...)),
	)
}

type checksStats struct {
	succeeded        int
	neutral          int
	failed           int
	skipped          int
	inProgress       int
	awaitingApproval int
}

func (m *Model) getStatusCheckRollupStats(rollup data.StatusCheckRollupStats) checksStats {
	var res checksStats
	allChecks := make([]data.ContextCountByState, 0)
	allChecks = append(allChecks, rollup.Contexts.CheckRunCountsByState...)
	allChecks = append(allChecks, rollup.Contexts.StatusContextCountsByState...)

	for _, count := range allChecks {
		state := string(count.State)
		if ghchecks.IsStatusWaiting(state) {
			res.inProgress += int(count.Count)
		} else if ghchecks.IsConclusionAFailure(state) {
			res.failed += int(count.Count)
		} else if ghchecks.IsConclusionASkip(state) {
			res.skipped += int(count.Count)
		} else if ghchecks.IsConclusionNeutral(state) {
			res.neutral += int(count.Count)
		} else if ghchecks.IsConclusionASuccess(state) {
			res.succeeded += int(count.Count)
		}
	}

	return res
}

func (m *Model) getChecksStats() checksStats {
	var res checksStats
	commits := m.pr.Data.Enriched.Commits.Nodes
	if len(commits) == 0 {
		return res
	}

	lastCommit := commits[0]

	// Walk the actual node list (with the same dedup the renderer uses)
	// rather than summing CheckRunCountsByState + StatusContextCountsByState.
	// Those aggregates double-count when a check appears in both CheckRun
	// and StatusContext form — exactly what Jenkins does for FI sub-jobs,
	// which led to "2 failing" when only 1 had failed.
	nodes := lastCommit.Commit.StatusCheckRollup.Contexts.Nodes

	// Pre-pass: identify FI sub-jobs (mirrors the renderer's fiSubJobNames
	// set). Their parent "FI Tests" CheckRun is dropped from counts since
	// each sub-job is counted individually below.
	fiSubJobNames := make(map[string]bool)
	for _, node := range nodes {
		if node.Typename != "CheckRun" {
			continue
		}
		if subJobs, _ := parseFITests(node.CheckRun); len(subJobs) > 0 {
			for _, j := range subJobs {
				fiSubJobNames[j.Name] = true
				switch j.Category {
				case CheckFailure:
					res.failed++
				case CheckWaiting:
					res.inProgress++
				case CheckSuccess:
					res.succeeded++
				}
			}
		}
	}

	// Main pass: dedup by check name (CheckRun and StatusContext can mirror
	// each other for the same check) and skip both the FI Tests aggregate
	// and per-sub-job duplicates.
	seen := make(map[string]bool)
	for _, node := range nodes {
		var name, state string
		switch node.Typename {
		case "CheckRun":
			cr := node.CheckRun
			name = string(cr.Name)
			if name == fiTestCheckName && len(fiSubJobNames) > 0 {
				continue
			}
			if fiSubJobNames[name] {
				continue
			}
			if seen[name] {
				continue
			}
			if ghchecks.IsStatusWaiting(string(cr.Status)) {
				state = string(cr.Status)
			} else {
				state = string(cr.Conclusion)
			}
		case "StatusContext":
			sc := node.StatusContext
			name = string(sc.Context)
			if fiSubJobNames[name] {
				continue
			}
			if seen[name] {
				continue
			}
			state = string(sc.State)
		default:
			continue
		}
		seen[name] = true
		switch {
		case ghchecks.IsStatusWaiting(state):
			res.inProgress++
		case ghchecks.IsConclusionAFailure(state):
			res.failed++
		case ghchecks.IsConclusionASkip(state):
			res.skipped++
		case ghchecks.IsConclusionNeutral(state):
			res.neutral++
		case ghchecks.IsConclusionASuccess(state):
			res.succeeded++
		}
	}

	// Count check suites that don't appear in statusCheckRollup. A suite
	// with a non-empty conclusion has FINISHED — even if its `status`
	// field still says QUEUED/PENDING/WAITING (which happens when GitHub
	// returns slightly inconsistent state after a workflow completes).
	// Without the conclusion guard, post-completion suites get phantom-
	// counted as "in progress" even though their CheckRuns already
	// reported success/failure and were tallied in the main pass above.
	for _, suite := range lastCommit.Commit.CheckSuites.Nodes {
		if suite.Conclusion == "ACTION_REQUIRED" {
			res.awaitingApproval++
			continue
		}
		if suite.Conclusion != "" {
			continue // suite is done; its CheckRuns were already counted
		}
		// Skip integration-subscription suites (Cursor, Figma, etc.) that
		// register a CheckSuite per commit but never run checks. They sit
		// at status=QUEUED forever with zero CheckRuns. GitHub's web UI
		// hides them; we should too, otherwise the "in progress" stat is
		// permanently inflated by the number of installed-but-passive
		// GitHub Apps in the repo.
		if suite.CheckRuns.TotalCount == 0 {
			continue
		}
		if suite.Status == "QUEUED" || suite.Status == "PENDING" || suite.Status == "WAITING" {
			res.inProgress++
		}
	}

	return res
}

func (m *Model) numRequestedReviewOwners() int {
	numOwners := 0

	for _, node := range m.pr.Data.Primary.ReviewRequests.Nodes {
		if node.AsCodeOwner {
			numOwners++
		}
	}

	return numOwners
}
