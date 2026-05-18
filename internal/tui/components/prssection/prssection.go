package prssection

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

const SectionType = "pr"

type Model struct {
	section.BaseModel
	Prs []prrow.Data
	// SilentNextFetch marks the next FetchNextPageSectionRows call as
	// background — the resulting context.Task gets Silent=true so the
	// status footer doesn't flash "Fetching PRs..." on every auto-tick.
	// Cleared once the task is created so subsequent user-initiated
	// fetches show normally.
	SilentNextFetch bool
}

func NewModel(
	id int,
	ctx *context.ProgramContext,
	cfg config.PrsSectionConfig,
	lastUpdated time.Time,
	createdAt time.Time,
) Model {
	m := Model{}
	m.BaseModel = section.NewModel(
		ctx,
		section.NewSectionOptions{
			Id:          id,
			Config:      cfg.ToSectionConfig(),
			Type:        SectionType,
			Columns:     GetSectionColumns(cfg, ctx),
			Singular:    m.GetItemSingularForm(),
			Plural:      m.GetItemPluralForm(),
			LastUpdated: lastUpdated,
			CreatedAt:   createdAt,
		},
	)
	m.Prs = []prrow.Data{}

	return m
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	var cmd tea.Cmd
	var err error

	switch msg := msg.(type) {
	case tea.KeyMsg:

		if m.IsSearchFocused() {
			switch msg.String() {
			case "ctrl+c", "esc":
				m.SearchBar.SetValue(m.SearchValue)
				blinkCmd := m.SetIsSearching(false)
				return m, blinkCmd

			case "enter":
				m.SearchValue = m.SearchBar.Value()
				m.SyncSmartFilterWithSearchValue()
				m.SetIsSearching(false)
				m.ResetRows()
				return m, tea.Batch(m.FetchNextPageSectionRows()...)
			}

			break
		}

		if m.IsPromptConfirmationFocused() {
			switch msg.String() {
			case "ctrl+c", "esc":
				m.PromptConfirmationBox.Reset()
				cmd = m.SetIsPromptConfirmationShown(false)
				return m, cmd

			case "enter":
				input := m.PromptConfirmationBox.Value()
				action := m.GetPromptConfirmationAction()
				pr := m.GetCurrRow()
				sid := tasks.SectionIdentifier{Id: m.Id, Type: SectionType}
				if input == "" || input == "Y" || input == "y" {
					switch action {
					case "close":
						cmd = tasks.ClosePR(m.Ctx, sid, pr)
					case "reopen":
						cmd = tasks.ReopenPR(m.Ctx, sid, pr)
					case "ready":
						cmd = tasks.PRReady(m.Ctx, sid, pr)
					case "markDraft":
						cmd = tasks.PRMarkDraft(m.Ctx, sid, pr)
					case "merge":
						cmd = tasks.MergePR(m.Ctx, sid, pr)
					case "update":
						cmd = tasks.UpdatePR(m.Ctx, sid, pr)
					case "approveWorkflows":
						cmd = tasks.ApproveWorkflows(m.Ctx, sid, pr)
					case "rerunFailedChecks":
						cmd = tasks.RerunFailedChecksOnPR(m.Ctx, sid, pr)
					case "jenkinsRerun":
						cmd = tasks.TriggerJenkinsRerun(m.Ctx, sid, pr)
					default:
						// Thread-resolve actions encode the GraphQL
						// thread id after a colon: "resolveThread:<id>"
						// or "unresolveThread:<id>". Parsed here because
						// prssection is the prompt dispatcher and
						// per-action data isn't otherwise plumbed
						// through.
						if tid, ok := strings.CutPrefix(action, "resolveThread:"); ok && tid != "" {
							cmd = tasks.ResolveReviewThread(m.Ctx, sid, pr, tid)
						} else if tid, ok := strings.CutPrefix(action, "unresolveThread:"); ok && tid != "" {
							cmd = tasks.UnresolveReviewThread(m.Ctx, sid, pr, tid)
						}
					}
				}

				m.PromptConfirmationBox.Reset()
				blinkCmd := m.SetIsPromptConfirmationShown(false)

				return m, tea.Batch(cmd, blinkCmd)
			}

			break
		}

		switch {
		case key.Matches(msg, keys.PRKeys.Diff):
			cmd = m.diff()

		case key.Matches(msg, keys.PRKeys.ToggleSmartFiltering):
			before := m.IsFilteredByCurrentRemote

			// If we're filtering by the current repo - we want to remove it
			// If there's no repo filter we want to add the current repo filter.
			if m.HasCurrentRepoNameInConfiguredFilter() || !m.HasRepoNameInConfiguredFilter() {
				m.IsFilteredByCurrentRemote = !before
			}
			log.Debug(
				"toggled smart filtering",
				"before",
				before,
				"after",
				m.IsFilteredByCurrentRemote,
			)
			searchValue := m.GetSearchValue()
			if m.SearchValue != searchValue {
				m.SearchValue = searchValue
				m.SearchBar.SetValue(searchValue)
				m.SetIsSearching(false)
				m.ResetRows()
				return m, tea.Batch(m.FetchNextPageSectionRows()...)
			}

		case key.Matches(msg, keys.PRKeys.Checkout):
			cmd, err = m.checkout()
			if err != nil {
				m.Ctx.Error = err
			}

		case key.Matches(msg, keys.PRKeys.WatchChecks):
			cmd = m.watchChecks()
		}

	case tasks.UpdatePRMsg:
		for i, currPr := range m.Prs {
			if currPr.Primary.Number != msg.PrNumber {
				continue
			}

			if msg.IsClosed != nil {
				if *msg.IsClosed {
					currPr.Primary.State = "CLOSED"
				} else {
					currPr.Primary.State = "OPEN"
				}
			}
			if msg.NewComment != nil {
				currPr.Enriched.Comments.Nodes = append(
					currPr.Enriched.Comments.Nodes, *msg.NewComment)
			}
			if msg.AddedAssignees != nil {
				currPr.Primary.Assignees.Nodes = addAssignees(
					currPr.Primary.Assignees.Nodes, msg.AddedAssignees.Nodes)
			}
			if msg.RemovedAssignees != nil {
				currPr.Primary.Assignees.Nodes = removeAssignees(
					currPr.Primary.Assignees.Nodes, msg.RemovedAssignees.Nodes)
			}
			if msg.Labels != nil {
				currPr.Primary.Labels.Nodes = msg.Labels.Nodes
			}
			if msg.ReadyForReview != nil {
				currPr.Primary.IsDraft = !*msg.ReadyForReview
			}
			if msg.IsMerged != nil && *msg.IsMerged {
				currPr.Primary.State = "MERGED"
				currPr.Primary.Mergeable = ""
			}
			if msg.IsInMergeQueue != nil && *msg.IsInMergeQueue {
				// Enqueue success: PR is still OPEN, just sitting in
				// the queue. Flip the flag so RenderState shows
				// "Queued" — without this the row keeps rendering as
				// plain "Open" until the next fetch.
				currPr.Primary.IsInMergeQueue = true
			}
			m.Prs[i] = currPr
			m.SetIsLoading(false)
			m.Table.SetRows(m.BuildRows())
			break
		}

	case SectionPullRequestsFetchedMsg:
		if m.LastFetchTaskId == msg.TaskId {
			// Capture the selected PR's identity (URL is unique across
			// repos; PR number is not) before the slice is replaced. The
			// list is sorted sort:updated, so any action that bumps a PR's
			// updatedAt (ready/comment/approve/assign/merge) reorders it to
			// the top on the next refetch. Without re-pinning the cursor by
			// identity, the fixed row index would land on a different PR and
			// the dashboard would auto-open it.
			isReplace := m.PageInfo == nil
			prevSelectedUrl, prevIdx := "", m.Table.GetCurrItem()
			if cur := m.GetCurrRow(); cur != nil {
				prevSelectedUrl = cur.GetUrl()
			}

			if !isReplace {
				m.Prs = append(m.Prs, msg.Prs...)
			} else {
				// Index the previous PRs by number so we can carry the
				// enriched payload forward when the same PR shows up in
				// the new page. Without this, every soft refresh resets
				// the sidebar's checks/reviews/activity to "Loading..."
				// because the freshly-fetched row has IsEnriched=false.
				prev := make(map[int]prrow.Data, len(m.Prs))
				for _, p := range m.Prs {
					if p.IsEnriched {
						prev[p.GetNumber()] = p
					}
				}
				for i, p := range msg.Prs {
					if old, ok := prev[p.GetNumber()]; ok {
						msg.Prs[i].IsEnriched = true
						msg.Prs[i].Enriched = old.Enriched
					}
				}
				m.Prs = msg.Prs
			}
			m.TotalCount = msg.TotalCount
			m.PageInfo = &msg.PageInfo
			m.SetIsLoading(false)
			m.Table.SetRows(m.BuildRows())
			m.Table.UpdateLastUpdated(time.Now())
			m.UpdateTotalItemsCount(m.TotalCount)

			// Re-pin the cursor to the same PR after a full-list replace.
			// If that PR is gone (left a filtered section), fall back to the
			// previous index, which SetCurrItem clamps to a valid neighbor
			// instead of leaving the cursor out of bounds.
			if isReplace && prevSelectedUrl != "" {
				newIdx := prevIdx
				for i, p := range m.Prs {
					if p.Primary.Url == prevSelectedUrl {
						newIdx = i
						break
					}
				}
				m.Table.SetCurrItem(newIdx)
			}
		}
	}

	search, searchCmd := m.SearchBar.Update(msg)
	m.Table.SetRows(m.BuildRows())
	m.SearchBar = search

	prompt, promptCmd := m.PromptConfirmationBox.Update(msg)
	m.PromptConfirmationBox = prompt

	table, tableCmd := m.Table.Update(msg)
	m.Table = table

	return m, tea.Batch(cmd, searchCmd, promptCmd, tableCmd)
}

func (m *Model) EnrichPR(data data.EnrichedPullRequestData) {
	for i, currPr := range m.Prs {
		if currPr.Primary.Number != data.Number {
			continue
		}

		m.Prs[i].IsEnriched = true
		m.Prs[i].Enriched = data
	}
}

func GetSectionColumns(
	cfg config.PrsSectionConfig,
	ctx *context.ProgramContext,
) []table.Column {
	dLayout := ctx.Config.Defaults.Layout.Prs
	sLayout := cfg.Layout

	updatedAtLayout := config.MergeColumnConfigs(
		dLayout.UpdatedAt,
		sLayout.UpdatedAt,
	)
	createdAtLayout := config.MergeColumnConfigs(
		dLayout.CreatedAt,
		sLayout.CreatedAt,
	)
	repoLayout := config.MergeColumnConfigs(dLayout.Repo, sLayout.Repo)
	titleLayout := config.MergeColumnConfigs(dLayout.Title, sLayout.Title)
	authorLayout := config.MergeColumnConfigs(dLayout.Author, sLayout.Author)
	assigneesLayout := config.MergeColumnConfigs(
		dLayout.Assignees,
		sLayout.Assignees,
	)
	baseLayout := config.MergeColumnConfigs(dLayout.Base, sLayout.Base)
	numCommentsLayout := config.MergeColumnConfigs(
		dLayout.NumComments,
		sLayout.NumComments,
	)
	reviewStatusLayout := config.MergeColumnConfigs(
		dLayout.ReviewStatus,
		sLayout.ReviewStatus,
	)
	stateLayout := config.MergeColumnConfigs(dLayout.State, sLayout.State)
	ciLayout := config.MergeColumnConfigs(dLayout.Ci, sLayout.Ci)
	labelsLayout := config.MergeColumnConfigs(dLayout.Labels, sLayout.Labels)
	linesLayout := config.MergeColumnConfigs(dLayout.Lines, sLayout.Lines)

	if !ctx.Config.Theme.Ui.Table.Compact {
		return []table.Column{
			{
				Title:  "",
				Width:  utils.IntPtr(3),
				Hidden: stateLayout.Hidden,
			},
			{
				Title:  "Title",
				Grow:   utils.BoolPtr(true),
				Hidden: titleLayout.Hidden,
			},
			{
				Title:  constants.LabelsIcon,
				Width:  labelsLayout.Width,
				Hidden: labelsLayout.Hidden,
			},
			{
				Title:  "Assignees",
				Width:  assigneesLayout.Width,
				Hidden: assigneesLayout.Hidden,
			},
			{
				Title:  "Base",
				Width:  baseLayout.Width,
				Hidden: baseLayout.Hidden,
			},
			{
				Title:  constants.CommentsIcon,
				Width:  utils.IntPtr(4),
				Hidden: numCommentsLayout.Hidden,
			},
			{
				Title:  "󰯢",
				Width:  utils.IntPtr(4),
				Hidden: reviewStatusLayout.Hidden,
			},
			{
				Title:  "",
				Width:  &ctx.Styles.PrSection.CiCellWidth,
				Grow:   new(bool),
				Hidden: ciLayout.Hidden,
			},
			{
				Title:  "",
				Width:  linesLayout.Width,
				Hidden: linesLayout.Hidden,
			},
			{
				Title:  "󱦻",
				Width:  updatedAtLayout.Width,
				Hidden: updatedAtLayout.Hidden,
			},
			{
				Title:  "󱡢",
				Width:  createdAtLayout.Width,
				Hidden: createdAtLayout.Hidden,
			},
		}
	}

	return []table.Column{
		{
			Title:  "",
			Width:  utils.IntPtr(3),
			Hidden: stateLayout.Hidden,
		},
		{
			Title:  "",
			Width:  repoLayout.Width,
			Hidden: repoLayout.Hidden,
		},
		{
			Title:  "Title",
			Grow:   utils.BoolPtr(true),
			Hidden: titleLayout.Hidden,
		},
		{
			Title:  "Author",
			Width:  authorLayout.Width,
			Hidden: authorLayout.Hidden,
		},
		{
			Title:  constants.LabelsIcon,
			Width:  labelsLayout.Width,
			Hidden: labelsLayout.Hidden,
		},
		{
			Title:  "Assignees",
			Width:  assigneesLayout.Width,
			Hidden: assigneesLayout.Hidden,
		},
		{
			Title:  "Base",
			Width:  baseLayout.Width,
			Hidden: baseLayout.Hidden,
		},
		{
			Title:  constants.CommentsIcon,
			Width:  utils.IntPtr(4),
			Hidden: numCommentsLayout.Hidden,
		},
		{
			Title:  "󰯢",
			Width:  utils.IntPtr(4),
			Hidden: reviewStatusLayout.Hidden,
		},
		{
			Title:  "",
			Width:  &ctx.Styles.PrSection.CiCellWidth,
			Grow:   new(bool),
			Hidden: ciLayout.Hidden,
		},
		{
			Title:  "",
			Width:  linesLayout.Width,
			Hidden: linesLayout.Hidden,
		},
		{
			Title:  "󱦻",
			Width:  updatedAtLayout.Width,
			Hidden: updatedAtLayout.Hidden,
		},
		{
			Title:  "󱡢",
			Width:  createdAtLayout.Width,
			Hidden: createdAtLayout.Hidden,
		},
	}
}

func (m Model) BuildRows() []table.Row {
	var rows []table.Row
	currItem := m.Table.GetCurrItem()
	for i, currPr := range m.Prs {
		prModel := prrow.PullRequest{
			Ctx:     m.Ctx,
			Data:    &currPr,
			Columns: m.Table.Columns, ShowAuthorIcon: m.ShowAuthorIcon,
		}
		rows = append(
			rows,
			prModel.ToTableRow(currItem == i),
		)
	}

	if rows == nil {
		rows = []table.Row{}
	}

	return rows
}

func (m *Model) NumRows() int {
	return len(m.Prs)
}

type SectionPullRequestsFetchedMsg struct {
	Prs        []prrow.Data
	TotalCount int
	PageInfo   data.PageInfo
	TaskId     string
}

func (m *Model) GetCurrRow() data.RowData {
	idx := m.Table.GetCurrItem()
	if idx < 0 || idx >= len(m.Prs) {
		return nil
	}
	pr := m.Prs[idx]
	return &pr
}

func (m *Model) FetchNextPageSectionRows() []tea.Cmd {
	if m == nil {
		return nil
	}

	if m.PageInfo != nil && !m.PageInfo.HasNextPage {
		return nil
	}

	var cmds []tea.Cmd

	startCursor := time.Now().String()
	if m.PageInfo != nil {
		startCursor = m.PageInfo.StartCursor
	}
	taskId := fmt.Sprintf("fetching_prs_%d_%s", m.Id, startCursor)
	isFirstFetch := m.LastFetchTaskId == ""
	m.LastFetchTaskId = taskId
	silent := m.SilentNextFetch
	m.SilentNextFetch = false
	task := context.Task{
		Id:        taskId,
		StartText: fmt.Sprintf(`Fetching PRs for "%s"`, m.Config.Title),
		FinishedText: fmt.Sprintf(
			`PRs for "%s" have been fetched`,
			m.Config.Title,
		),
		State:  context.TaskStart,
		Error:  nil,
		Silent: silent,
	}
	startCmd := m.Ctx.StartTask(task)
	cmds = append(cmds, startCmd)

	fetchCmd := func() tea.Msg {
		limit := m.Config.Limit
		if limit == nil {
			limit = &m.Ctx.Config.Defaults.PrsLimit
		}

		res, err := data.FetchPullRequests(m.GetFilters(), *limit, m.PageInfo)
		if err != nil {
			return constants.TaskFinishedMsg{
				SectionId:   m.Id,
				SectionType: m.Type,
				TaskId:      taskId,
				Err:         err,
			}
		}

		prs := make([]prrow.Data, 0)
		for _, pr := range res.Prs {
			prs = append(prs, prrow.Data{Primary: &pr})
		}
		return constants.TaskFinishedMsg{
			SectionId:   m.Id,
			SectionType: m.Type,
			TaskId:      taskId,
			Msg: SectionPullRequestsFetchedMsg{
				Prs:        prs,
				TotalCount: res.TotalCount,
				PageInfo:   res.PageInfo,
				TaskId:     taskId,
			},
		}
	}
	cmds = append(cmds, fetchCmd)

	m.IsLoading = true
	if isFirstFetch {
		m.SetIsLoading(true)
		cmds = append(cmds, m.Table.StartLoadingSpinner())
	}

	return cmds
}

func (m *Model) ResetRows() {
	m.Prs = nil
	m.BaseModel.ResetRows()
}

// SoftReset prepares the section for a re-fetch without blanking the
// visible PR list. We reset PageInfo so the next fetch is treated as a
// first-page request and REPLACES m.Prs atomically when its message
// arrives (see ~line 205). LastFetchTaskId is intentionally LEFT alone
// so the fetch path's `isFirstFetch` check returns false — that gates
// the loading spinner, which we don't want for a soft refresh.
// SilentNextFetch suppresses the status-bar churn for this fetch.
// Together: existing rows stay rendered, no spinner, no footer flash.
func (m *Model) SoftReset() {
	m.PageInfo = nil
	m.SilentNextFetch = true
}

func FetchAllSections(
	ctx *context.ProgramContext,
	prs []section.Section,
) (sections []section.Section, fetchAllCmd tea.Cmd) {
	fetchPRsCmds := make([]tea.Cmd, 0, len(ctx.Config.PRSections))
	sections = make([]section.Section, 0, len(ctx.Config.PRSections))
	for i, sectionConfig := range ctx.Config.PRSections {
		sectionModel := NewModel(
			i+1, // 0 is the search section
			ctx,
			sectionConfig,
			time.Now(),
			time.Now(),
		)
		if len(prs) > 0 && len(prs) >= i+1 && prs[i+1] != nil {
			oldSection := prs[i+1].(*Model)
			sectionModel.Prs = oldSection.Prs
			sectionModel.LastFetchTaskId = oldSection.LastFetchTaskId
		}
		if sectionConfig.Layout.AuthorIcon.Hidden != nil {
			sectionModel.ShowAuthorIcon = !*sectionConfig.Layout.AuthorIcon.Hidden
		}
		sections = append(sections, &sectionModel)
		fetchPRsCmds = append(
			fetchPRsCmds,
			sectionModel.FetchNextPageSectionRows()...)
	}
	return sections, tea.Batch(fetchPRsCmds...)
}

func addAssignees(assignees, addedAssignees []data.Assignee) []data.Assignee {
	newAssignees := assignees
	for _, assignee := range addedAssignees {
		if !assigneesContains(newAssignees, assignee) {
			newAssignees = append(newAssignees, assignee)
		}
	}

	return newAssignees
}

func removeAssignees(
	assignees, removedAssignees []data.Assignee,
) []data.Assignee {
	newAssignees := []data.Assignee{}
	for _, assignee := range assignees {
		if !assigneesContains(removedAssignees, assignee) {
			newAssignees = append(newAssignees, assignee)
		}
	}

	return newAssignees
}

func assigneesContains(assignees []data.Assignee, assignee data.Assignee) bool {
	return slices.Contains(assignees, assignee)
}

func (m Model) GetItemSingularForm() string {
	return "PR"
}

func (m Model) GetItemPluralForm() string {
	return "PRs"
}

func (m Model) GetTotalCount() int {
	return m.TotalCount
}

func (m *Model) SetIsLoading(val bool) {
	m.IsLoading = val
	m.Table.SetIsLoading(val)
}

func (m Model) GetPagerContent() string {
	pagerContent := ""
	timeElapsed := utils.TimeElapsed(m.LastUpdated())
	if timeElapsed == "now" {
		timeElapsed = "just now"
	} else {
		timeElapsed = fmt.Sprintf("~%v ago", timeElapsed)
	}
	if m.TotalCount > 0 {
		pagerContent = fmt.Sprintf(
			"%v Updated %v • %v %v/%v (fetched %v)",
			constants.WaitingIcon,
			timeElapsed,
			m.SingularForm,
			m.Table.GetCurrItem()+1,
			m.TotalCount,
			len(m.Table.Rows),
		)
	}
	pager := m.Ctx.Styles.ListViewPort.PagerStyle.Render(pagerContent)
	return pager
}
