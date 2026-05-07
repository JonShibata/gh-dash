package prview

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

// fiTestCheckName is the GitHub check-run name posted by Deepfield's
// gha_fi_test_manager Jenkins job. We only attempt to expand the markdown
// summary table when this exact name matches; every other check-run is
// rendered normally as a single row.
const fiTestCheckName = "FI Tests"

// fiSubJobRowRe matches one row of the markdown table that the FI Tests
// summary posts: `| [JobName](url) | <status emoji + word> | <duration> |
// <test count> |`. The status cell is matched leniently — the parser
// classifies it via emoji presence, not exact string match.
var fiSubJobRowRe = regexp.MustCompile(`\| \[([^\]]+)\]\(([^)]+)\) \| (.+?) \| (.+?) \| (.+?) \|`)

// fiProgressRe pulls the "Progress: N/M jobs complete" line that
// gha_fi_test_manager prepends to its summary so we can render
// "FI Tests (N/M)" while the run is still in flight.
var fiProgressRe = regexp.MustCompile(`Progress: (\d+/\d+ jobs complete)`)

// fiSubJob is one parsed sub-job row from the FI Tests summary table.
type fiSubJob struct {
	Name     string
	URL      string
	Category CheckCategory
	Duration string
	Tests    string // free-form, e.g. "50 tests run, 50 passed"
}

// parseFITests pulls per-job rows out of the FI Tests check-run's
// markdown summary. Returns nil for non-FI checks or when the summary is
// empty — caller falls through to rendering the single FI Tests row.
func parseFITests(checkRun data.CheckRun) (jobs []fiSubJob, progress string) {
	if string(checkRun.Name) != fiTestCheckName {
		return nil, ""
	}
	summary := string(checkRun.Output.Summary)
	if summary == "" {
		return nil, ""
	}

	if m := fiProgressRe.FindStringSubmatch(summary); m != nil {
		progress = m[1]
	}

	for _, line := range strings.Split(summary, "\n") {
		m := fiSubJobRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name, url, status, duration, tests := m[1], m[2], m[3], m[4], m[5]
		jobs = append(jobs, fiSubJob{
			Name:     name,
			URL:      url,
			Category: classifyFIStatus(status),
			Duration: strings.TrimSpace(duration),
			Tests:    strings.TrimSpace(tests),
		})
	}
	return jobs, progress
}

// classifyFIStatus maps the markdown status cell to our CheckCategory.
// FI Tests uses unicode glyphs (✅/❌/⏳) plus optional words. The check
// for substrings is intentionally loose — the table format has shifted
// across Jenkins job versions and we'd rather over-match than miss.
func classifyFIStatus(status string) CheckCategory {
	s := strings.ToLower(status)
	switch {
	case strings.Contains(status, "✅"):
		return CheckSuccess
	case strings.Contains(status, "❌"), strings.Contains(s, "failure"), strings.Contains(s, "failed"):
		return CheckFailure
	case strings.Contains(status, "⏳"), strings.Contains(s, "running"), strings.Contains(s, "pending"):
		return CheckWaiting
	default:
		return CheckWaiting
	}
}

// renderFISubJobRow formats one parsed sub-job for the checks list. The
// extra "tests" cell goes in dim text after the name so the badge column
// stays aligned with the surrounding non-FI checks.
func (m *Model) renderFISubJobRow(job fiSubJob) string {
	badge := m.renderCheckBadge(job.Category)
	dim := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)
	tail := ""
	if job.Tests != "" {
		tail = "  " + dim.Render(job.Tests)
	}
	if job.Duration != "" {
		tail += "  " + dim.Render("("+job.Duration+")")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, badge, " ", job.Name, tail)
}

