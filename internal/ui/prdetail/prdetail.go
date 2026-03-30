package prdetail

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/ui/components"
	"github.com/blakewilliams/ghq/internal/ui/styles"
	"github.com/blakewilliams/ghq/internal/ui/uictx"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
)

// Nerdfont icon constants.
const (
	iconCheckCircle = "\U000f05e0" // 󰗠 nf-md-check_circle
	iconXCircle     = "\U000f0159" // 󰅙 nf-md-close_circle
	iconComment     = "\U000f0188" // 󰆈 nf-md-comment
	iconSlash       = "\U000f0737" // 󰜷 nf-md-cancel
	iconClock       = "\U000f0954" // 󰥔 nf-md-clock_outline
	iconReview      = "\U000f0567" // 󰕧 nf-md-shield_account
	iconComments    = "\U000f0e1c" // 󰸜 nf-md-comment_multiple
	iconAuthor      = "\U000f0004" // 󰀄 nf-md-account
	iconFile        = "\U000f0214" // 󰈔 nf-md-file
	iconFileTree    = "\U000f0253" // 󰉓 nf-md-file_tree
	iconFolder      = "\U000f024b" // 󰉋 nf-md-folder
	iconArrowUp     = "\U000f005d" // 󰁝 nf-md-arrow_up
	iconArrowDown   = "\U000f0045" // 󰁅 nf-md-arrow_down
	iconLoading     = "\U000f0772" // 󰝲 nf-md-loading
	iconGit         = "\U000f02a2" // 󰊢 nf-md-git
	iconPR          = "\U000f0041" // 󰁁 nf-md-arrow_top_right (source-branch)
	iconMerge       = "\U000f0261" // 󰉡 nf-md-source_merge (call_merge)
	iconClose       = "\U000f0156" // 󰅖 nf-md-close
	iconDraft       = "\U000f0613" // 󰘓 nf-md-pencil
	iconOpen        = "\U000f0130" // 󰄰 nf-md-checkbox_blank_circle_outline
	iconPlus        = "\U000f0415" // 󰐕 nf-md-plus
	iconMinus       = "\U000f0374" // 󰍴 nf-md-minus
	iconRename      = "\U000f0453" // 󰑓 nf-md-rename_box
	iconChevron     = "\U000f0142" // 󰅂 nf-md-chevron_right
	iconArrowRight  = "\U000f0054" // 󰁔 nf-md-arrow_right
	iconPointer     = "\U000f0142" // 󰅂 nf-md-chevron_right (cursor)
	iconChecks      = "\U000f0134" // 󰄴 nf-md-check
)

type prTab int

const (
	tabOverview prTab = iota
	tabCode
)

type overviewSection int

const (
	sectionComments overviewSection = iota
	sectionReviews
	sectionChecks
)

type descRenderedMsg struct {
	content  string
	prNumber int
}

type fileRenderedMsg struct {
	content  string
	index    int
	prNumber int
}

// prefetchDoneMsg signals that background prefetch of file contents completed.
type prefetchDoneMsg struct{}

type Model struct {
	pr     github.PullRequest
	ctx    *uictx.Context
	width  int
	height int
	tab    prTab

	// Overview tab
	overviewVP      viewport.Model
	overviewReady   bool
	descContent     string
	reviews         []github.Review
	comments        []github.IssueComment
	reviewComments  []github.ReviewComment
	checkRuns       []github.CheckRun
	overviewSection overviewSection

	// Code tab
	codeVP         viewport.Model
	codeReady      bool
	files          []github.PullRequestFile
	renderedFiles  []string
	filesRendered  int
	filesLoading   bool
	currentFileIdx int

	// File tree
	showTree      bool
	treeEntries   []components.FileTreeEntry
	treeCursor    int
	treeWidth     int

	// Shared
	filesListLoaded bool
	waitingG        bool
}

func New(pr github.PullRequest, ctx *uictx.Context, width, height int) Model {
	return Model{
		pr:     pr,
		ctx:    ctx,
		width:  width,
		height: height,
		tab:    tabOverview,
	}
}

func (m Model) PRNumber() int {
	return m.pr.Number
}

func (m Model) PRTitle() string {
	return m.pr.Title
}

func (m *Model) activeViewport() *viewport.Model {
	if m.tab == tabCode {
		return &m.codeVP
	}
	return &m.overviewVP
}

// StatusHints returns left and right hint groups for the status bar.
func (m Model) StatusHints() (left, right []string) {
	switch m.tab {
	case tabCode:
		left = append(left, "f "+iconFileTree+" tree")
		left = append(left, "tab overview")
		if len(m.files) > 0 {
			right = append(right, fmt.Sprintf(iconFile+" %d/%d", m.currentFileIdx+1, len(m.files)))
		}
	case tabOverview:
		left = append(left, "1 comments")
		left = append(left, "2 reviews")
		left = append(left, "3 checks")
		left = append(left, "tab code")
	}
	left = append(left, "esc back")
	return
}

func (m Model) Tab() string {
	if m.tab == tabCode {
		return "Code"
	}
	return "Overview"
}

func (m Model) Init() tea.Cmd {
	body := m.pr.Body
	width := m.descWidth()
	prNumber := m.pr.Number
	return tea.Batch(
		func() tea.Msg {
			rendered := renderMarkdown(body, width)
			return descRenderedMsg{content: rendered, prNumber: prNumber}
		},
		m.ctx.Client.GetPullRequestFiles(m.pr.Number),
		m.ctx.Client.GetReviews(m.pr.Number),
		m.ctx.Client.GetIssueComments(m.pr.Number),
		m.ctx.Client.GetReviewComments(m.pr.Number),
		m.ctx.Client.GetCheckRuns(m.pr.Head.SHA),
	)
}

func (m Model) Update(msg tea.Msg) (uictx.View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.overviewVP.SetWidth(m.width)
		m.overviewVP.SetHeight(m.height)
		m.codeVP.SetWidth(m.width)
		m.codeVP.SetHeight(m.height)
		body := m.pr.Body
		width := m.descWidth()
		prNumber := m.pr.Number
		cmds := []tea.Cmd{func() tea.Msg {
			rendered := renderMarkdown(body, width)
			return descRenderedMsg{content: rendered, prNumber: prNumber}
		}}
		// Re-render diff files at the new width.
		if m.filesListLoaded {
			m.filesRendered = 0
			m.renderedFiles = make([]string, len(m.files))
			cmds = append(cmds, m.renderFileCmd(0))
		}
		return m, tea.Batch(cmds...)

	case tea.MouseClickMsg:
		if m.tab == tabCode && m.showTree && msg.X < m.treeWidth {
			if idx, ok := m.treeEntryIndexAtY(msg.Y); ok {
				e := m.treeEntries[idx]
				if !e.IsDir && e.FileIndex >= 0 {
					m.treeCursor = idx
					m.currentFileIdx = e.FileIndex
					m.rebuildCode()
					m.codeVP.GotoTop()
				}
			}
			return m, nil
		}

	case tea.KeyPressMsg:
		var cmd tea.Cmd
		var handled bool
		m, cmd, handled = m.handleKey(msg)
		if handled {
			return m, cmd
		}

	case descRenderedMsg:
		if msg.prNumber == m.pr.Number {
			m.descContent = msg.content
			m.rebuildOverview()
		}
		return m, nil

	case github.ReviewsLoadedMsg:
		if msg.Number == m.pr.Number {
			m.reviews = msg.Reviews
			m.rebuildOverview()
		}
		return m, nil

	case github.CommentsLoadedMsg:
		if msg.Number == m.pr.Number {
			// Reverse so newest comments appear first.
			comments := msg.Comments
			for i, j := 0, len(comments)-1; i < j; i, j = i+1, j-1 {
				comments[i], comments[j] = comments[j], comments[i]
			}
			m.comments = comments
			m.rebuildOverview()
		}
		return m, nil

	case github.ReviewCommentsLoadedMsg:
		if msg.Number == m.pr.Number {
			m.reviewComments = msg.Comments
			// Re-render all already-rendered files to include comments.
			if m.filesRendered > 0 {
				m.filesRendered = 0
				m.renderedFiles = make([]string, len(m.files))
				return m, m.renderFileCmd(0)
			}
		}
		return m, nil

	case github.CheckRunsLoadedMsg:
		if msg.Ref == m.pr.Head.SHA {
			m.checkRuns = msg.CheckRuns
			m.rebuildOverview()
		}
		return m, nil

	case github.PRFilesLoadedMsg:
		m.files = msg.Files
		m.renderedFiles = make([]string, len(msg.Files))
		m.filesListLoaded = true
		m.treeEntries = components.BuildFileTree(m.files)
		m.syncTreeCursor()
		// Rebuild overview to show file summary.
		m.rebuildOverview()
		// Prefetch first 3 files into cache.
		cmds := m.prefetchFiles(3)
		// If already on Code tab, start rendering.
		if m.tab == tabCode {
			cmds = append(cmds, m.startFileRendering())
		}
		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case prefetchDoneMsg:
		return m, nil

	case fileRenderedMsg:
		if msg.prNumber != m.pr.Number || msg.index >= len(m.renderedFiles) {
			return m, nil
		}
		m.renderedFiles[msg.index] = msg.content
		m.filesRendered = msg.index + 1
		if m.filesRendered >= len(m.files) {
			m.filesLoading = false
		}
		m.rebuildCode()
		if m.filesRendered < len(m.files) {
			return m, m.renderFileCmd(m.filesRendered)
		}
		return m, nil

	case github.QueryErrMsg:
		return m, nil
	}

	if m.tab == tabOverview && m.overviewReady {
		var cmd tea.Cmd
		m.overviewVP, cmd = m.overviewVP.Update(msg)
		return m, cmd
	}
	if m.tab == tabCode && m.codeReady {
		var cmd tea.Cmd
		m.codeVP, cmd = m.codeVP.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) HandleKey(msg tea.KeyPressMsg) (uictx.View, tea.Cmd, bool) {
	return m.handleKey(msg)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "tab":
		if m.tab == tabOverview {
			m.tab = tabCode
			if m.filesListLoaded && !m.codeReady {
				return m, m.startFileRendering(), true
			}
			m.rebuildCode()
		} else {
			m.tab = tabOverview
		}
		return m, nil, true
	case "shift+tab":
		if m.tab == tabCode {
			m.tab = tabOverview
		} else {
			m.tab = tabCode
			if m.filesListLoaded && !m.codeReady {
				return m, m.startFileRendering(), true
			}
			m.rebuildCode()
		}
		return m, nil, true
	case "f":
		if m.tab == tabCode && m.filesListLoaded {
			m.showTree = !m.showTree
			if m.showTree {
				if m.treeWidth == 0 {
					m.treeWidth = 35
				}
				m.syncTreeCursor()
			}
			m.rebuildCode()
			return m, nil, true
		}
	case "j", "down":
		if m.tab == tabCode && m.showTree {
			m.treeMoveCursor(1)
			return m, nil, true
		}
	case "k", "up":
		if m.tab == tabCode && m.showTree {
			m.treeMoveCursor(-1)
			return m, nil, true
		}
	case "enter":
		if m.tab == tabCode && m.showTree {
			e := m.treeEntries[m.treeCursor]
			if !e.IsDir && e.FileIndex >= 0 {
				m.currentFileIdx = e.FileIndex
				m.rebuildCode()
				m.codeVP.GotoTop()
			}
			return m, nil, true
		}
	case "p", "h", "left":
		if m.tab == tabCode && !m.showTree && m.currentFileIdx > 0 {
			m.currentFileIdx--
			m.rebuildCode()
			m.codeVP.GotoTop()
			return m, nil, true
		}
	case "n", "l", "right":
		if m.tab == tabCode && !m.showTree && m.currentFileIdx < len(m.files)-1 {
			m.currentFileIdx++
			m.rebuildCode()
			m.codeVP.GotoTop()
			return m, nil, true
		}
	case "1":
		if m.tab == tabOverview {
			m.overviewSection = sectionComments
			m.rebuildOverview()
			return m, nil, true
		}
	case "2":
		if m.tab == tabOverview {
			m.overviewSection = sectionReviews
			m.rebuildOverview()
			return m, nil, true
		}
	case "3":
		if m.tab == tabOverview {
			m.overviewSection = sectionChecks
			m.rebuildOverview()
			return m, nil, true
		}
	case "G":
		m.waitingG = false
		m.activeViewport().GotoBottom()
		return m, nil, true
	case "g":
		if m.waitingG {
			m.waitingG = false
			m.activeViewport().GotoTop()
			return m, nil, true
		}
		m.waitingG = true
		return m, nil, true
	default:
		m.waitingG = false
	}
	return m, nil, false
}

var (
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	separatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

func (m Model) View() string {
	if m.tab == tabCode && m.codeReady {
		if m.showTree {
			return m.renderCodeWithTree()
		}
		return m.codeVP.View()
	}
	if m.overviewReady {
		return m.overviewVP.View()
	}
	return ""
}

func (m Model) renderCodeWithTree() string {
	treeW := m.treeWidth
	divider := lipgloss.NewStyle().Foreground(lipgloss.BrightBlack).Render("│")

	treeLines := components.RenderFileTree(m.treeEntries, m.files, m.treeCursor, m.currentFileIdx, treeW, m.height)
	diffLines := strings.Split(m.codeVP.View(), "\n")

	var b strings.Builder
	for i := 0; i < m.height; i++ {
		tl := ""
		if i < len(treeLines) {
			tl = treeLines[i]
		}
		dl := ""
		if i < len(diffLines) {
			dl = diffLines[i]
		}
		b.WriteString(tl + divider + dl)
		if i < m.height-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// --- Overview tab ---

// overviewPad is the left margin for overview content.
const overviewPad = 2

// indent prefixes every line of s with n spaces.
func indent(s string, n int) string {
	prefix := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}

// descWidth returns the available width for description/body markdown.
func (m Model) descWidth() int {
	return m.width - overviewPad*2
}

func (m *Model) rebuildOverview() {
	var content strings.Builder

	// Status + metadata line.
	meta := styles.PRStatusBadge(m.pr.State, m.pr.Draft, m.pr.Merged) +
		" " + m.renderMeta()
	content.WriteString("\n" + indent(meta, overviewPad) + "\n")

	// Title.
	prURL := fmt.Sprintf("https://github.com/%s/pull/%d", m.ctx.Client.RepoFullName(), m.pr.Number)
	title := lipgloss.NewStyle().Bold(true).
		UnderlineStyle(lipgloss.UnderlineDotted).
		Hyperlink(prURL).
		Render(fmt.Sprintf("#%d %s", m.pr.Number, m.pr.Title))
	content.WriteString("\n" + indent(title, overviewPad) + "\n")

	// Description body.
	descBody := strings.TrimSpace(m.descContent)
	if descBody == "" {
		descBody = dimStyle.Render("No description provided.")
	}
	content.WriteString("\n" + indent(descBody, overviewPad))

	// Reviews/Comments bordered section.
	content.WriteString("\n\n" + m.renderReviewsComments())

	if !m.overviewReady {
		m.overviewVP = viewport.New()
		m.overviewReady = true
	}
	m.overviewVP.SetWidth(m.width)
	m.overviewVP.SetHeight(m.height)
	m.overviewVP.SetContent(content.String())
}

// --- Code tab ---

func (m Model) startFileRendering() tea.Cmd {
	if len(m.files) == 0 || m.filesRendered > 0 {
		return nil
	}
	m.filesLoading = true
	return m.renderFileCmd(0)
}

func (m Model) renderFileCmd(index int) tea.Cmd {
	f := m.files[index]
	ref := m.pr.Head.SHA
	prNumber := m.pr.Number
	width := m.width
	client := m.ctx.Client
	colors := m.ctx.DiffColors
	fileComments := m.commentsForFile(f.Filename)

	return func() tea.Msg {
		var fileContent string
		if f.Status != "removed" && f.Patch != "" {
			if content, err := client.FetchFileContent(f.Filename, ref); err == nil {
				fileContent = content
			}
		}
		rendered := components.RenderDiffFile(f, fileContent, width, colors, fileComments)
		return fileRenderedMsg{content: rendered, index: index, prNumber: prNumber}
	}
}

// commentsForFile returns review comments that belong to the given file.
func (m Model) commentsForFile(filename string) []github.ReviewComment {
	var result []github.ReviewComment
	for _, c := range m.reviewComments {
		if c.Path == filename {
			result = append(result, c)
		}
	}
	return result
}

func (m Model) codeWidth() int {
	if m.showTree {
		return m.width - m.treeWidth - 1 // -1 for divider
	}
	return m.width
}

func (m *Model) rebuildCode() {
	w := m.codeWidth()
	var content strings.Builder

	idx := m.currentFileIdx

	// Previous file hint above.
	if idx > 0 {
		prev := m.files[idx-1]
		content.WriteString(dimStyle.Render("  " + iconArrowUp + " " + prev.Filename))
		content.WriteString("\n\n")
	}

	if idx < m.filesRendered {
		content.WriteString(m.renderedFiles[idx])
	} else {
		content.WriteString(dimStyle.Render("  " + iconLoading + " Loading..."))
	}

	// Next file hint below.
	if idx < len(m.files)-1 {
		next := m.files[idx+1]
		content.WriteString("\n\n")
		content.WriteString(dimStyle.Render("  " + iconArrowDown + " " + next.Filename))
	}

	if !m.codeReady {
		m.codeVP = viewport.New()
		m.codeReady = true
	}
	m.codeVP.SetWidth(w)
	m.codeVP.SetHeight(m.height)
	m.codeVP.SetContent(content.String())
}

// prefetchFiles kicks off background fetches for the first n files' content,
// warming the cache so Code tab renders are fast.
func (m Model) prefetchFiles(n int) []tea.Cmd {
	limit := n
	if limit > len(m.files) {
		limit = len(m.files)
	}
	if limit == 0 {
		return nil
	}

	ref := m.pr.Head.SHA
	client := m.ctx.Client
	var cmds []tea.Cmd
	for i := 0; i < limit; i++ {
		f := m.files[i]
		if f.Status == "removed" || f.Patch == "" {
			continue
		}
		filename := f.Filename
		cmds = append(cmds, func() tea.Msg {
			client.FetchFileContent(filename, ref)
			return prefetchDoneMsg{}
		})
	}
	return cmds
}

// --- Comments ---

var (
	commentBodyStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderTop(false).
				BorderForeground(lipgloss.Color("245")).
				Padding(0, 1)

	authorBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Black).
			Background(lipgloss.Yellow)

	borderColor = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// nickColors is the palette used for weechat-style username coloring.
// These are chosen to be distinct and readable on dark backgrounds.
var nickColors = []color.Color{
	lipgloss.Color("1"),   // red
	lipgloss.Color("2"),   // green
	lipgloss.Color("3"),   // yellow
	lipgloss.Color("4"),   // blue
	lipgloss.Color("5"),   // magenta
	lipgloss.Color("6"),   // cyan
	lipgloss.Color("9"),   // bright red
	lipgloss.Color("10"),  // bright green
	lipgloss.Color("11"),  // bright yellow
	lipgloss.Color("12"),  // bright blue
	lipgloss.Color("13"),  // bright magenta
	lipgloss.Color("14"),  // bright cyan
	lipgloss.Color("208"), // orange
	lipgloss.Color("172"), // dark orange
	lipgloss.Color("141"), // purple
	lipgloss.Color("167"), // salmon
	lipgloss.Color("109"), // steel blue
	lipgloss.Color("150"), // sage
}

// nickColor returns a consistent color for a given username using
// a djb2-style hash, similar to weechat's nick coloring algorithm.
func nickColor(name string) color.Color {
	var hash uint32 = 5381
	for _, c := range name {
		hash = hash*33 + uint32(c)
	}
	return nickColors[hash%uint32(len(nickColors))]
}

// coloredAuthor renders a username with a consistent hash-based color.
func coloredAuthor(login string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(nickColor(login)).
		UnderlineStyle(lipgloss.UnderlineDotted).
		Hyperlink(fmt.Sprintf("https://github.com/%s", login)).
		Render("@" + login)
}

// --- Reviews / Comments ---

func (m Model) hasReviewContent() bool {
	return len(m.reviews) > 0 || len(m.pr.RequestedReviewers) > 0
}

var (
	reviewApproved  = lipgloss.NewStyle().Foreground(lipgloss.Green).Bold(true)
	reviewChanges   = lipgloss.NewStyle().Foreground(lipgloss.Red).Bold(true)
	reviewCommented = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	reviewPending   = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
)

func reviewStateIcon(state string) string {
	switch state {
	case "APPROVED":
		return reviewApproved.Render(iconCheckCircle + " approved")
	case "CHANGES_REQUESTED":
		return reviewChanges.Render(iconXCircle + " changes requested")
	case "COMMENTED":
		return reviewCommented.Render(iconComment + " commented")
	case "DISMISSED":
		return reviewCommented.Render(iconSlash + " dismissed")
	default:
		return reviewPending.Render(iconClock + " pending")
	}
}

// renderReviewsComments renders a single bordered box with two labels in the
// top border. The active section is bold, the inactive one is dimmed. Press
// 1/2 to swap.
func (m Model) renderReviewsComments() string {
	innerW := m.width - 4 // border + padding

	// Build the top border with all three labels.
	commentLabel := iconComments + " Comments"
	if len(m.comments) > 0 {
		commentLabel += fmt.Sprintf(" (%d)", len(m.comments))
	}
	reviewLabel := iconReview + " Reviews"
	checksLabel := iconChecks + " Checks"
	if len(m.checkRuns) > 0 {
		checksLabel += fmt.Sprintf(" (%d)", len(m.checkRuns))
	}

	renderLabel := func(label string, section overviewSection) string {
		if m.overviewSection == section {
			return " " + lipgloss.NewStyle().Bold(true).Render(label) + " "
		}
		return " " + dimStyle.Render(label) + " "
	}

	div := borderColor.Render("│")
	labels := renderLabel(commentLabel, sectionComments) + div +
		renderLabel(reviewLabel, sectionReviews) + div +
		renderLabel(checksLabel, sectionChecks)
	labelsW := lipgloss.Width(labels)
	fillW := m.width - 2 - labelsW - 1 // ╭─ + labels + ─fill─ + ╮
	if fillW < 0 {
		fillW = 0
	}
	topBorder := borderColor.Render("╭─") + labels +
		borderColor.Render(strings.Repeat("─", fillW) + "╮")

	// Build body based on active section.
	var lines []string
	switch m.overviewSection {
	case sectionComments:
		lines = m.buildCommentLines(innerW)
		if len(lines) == 0 {
			lines = []string{dimStyle.Render("No comments yet.")}
		}
	case sectionReviews:
		lines = m.buildReviewLines(innerW)
		if len(lines) == 0 {
			lines = []string{dimStyle.Render("No reviews yet.")}
		}
	case sectionChecks:
		lines = m.buildCheckLines()
		if len(lines) == 0 {
			lines = []string{dimStyle.Render("No checks yet.")}
		}
	}

	sep := dimStyle.Render(strings.Repeat("─", innerW))
	body := strings.Join(lines, "\n"+sep+"\n")

	bottom := commentBodyStyle.Width(m.width).Render(body)
	return topBorder + "\n" + bottom
}

// buildReviewLines builds the content lines for the reviews section.
func (m Model) buildReviewLines(innerW int) []string {
	// Deduplicate reviews — keep only the latest per user.
	latestByUser := make(map[string]github.Review)
	for _, r := range m.reviews {
		if r.State == "PENDING" {
			continue
		}
		existing, ok := latestByUser[r.User.Login]
		if !ok || r.SubmittedAt.After(existing.SubmittedAt) {
			latestByUser[r.User.Login] = r
		}
	}

	var lines []string
	for _, r := range m.reviews {
		latest, ok := latestByUser[r.User.Login]
		if !ok || latest.ID != r.ID {
			continue
		}
		delete(latestByUser, r.User.Login)

		author := coloredAuthor(r.User.Login)
		line := author + " " + reviewStateIcon(r.State)
		if r.Body != "" {
			body := renderMarkdown(r.Body, innerW)
			line += "\n" + body
		}
		lines = append(lines, line)
	}

	// Requested reviewers (haven't reviewed yet).
	for _, u := range m.pr.RequestedReviewers {
		if _, reviewed := latestByUser[u.Login]; reviewed {
			continue
		}
		alreadyRendered := false
		for _, r := range m.reviews {
			if r.User.Login == u.Login {
				alreadyRendered = true
				break
			}
		}
		if alreadyRendered {
			continue
		}
		author := coloredAuthor(u.Login)
		lines = append(lines, author+" "+reviewPending.Render(iconClock+" awaiting review"))
	}

	return lines
}

// buildCommentLines builds the content lines for the comments section.
func (m Model) buildCommentLines(innerW int) []string {
	var lines []string
	for _, c := range m.comments {
		author := coloredAuthor(c.User.Login)
		if c.User.Login == m.pr.User.Login {
			author += " " + authorBadge.Render(" "+iconAuthor+" Author ")
		}
		age := dimStyle.Render(relativeTime(c.CreatedAt))

		line := author + " " + age
		if c.Body != "" {
			body := renderMarkdown(c.Body, innerW)
			line += "\n" + body
		}
		lines = append(lines, line)
	}
	return lines
}

// buildCheckLines builds the content lines for the checks section.
func (m Model) buildCheckLines() []string {
	var lines []string
	for _, c := range m.checkRuns {
		var icon string
		switch {
		case c.Status != "completed":
			icon = reviewPending.Render(iconClock + " in progress")
		case c.Conclusion != nil:
			switch *c.Conclusion {
			case "success":
				icon = reviewApproved.Render(iconCheckCircle + " passed")
			case "failure":
				icon = reviewChanges.Render(iconXCircle + " failed")
			case "cancelled":
				icon = dimStyle.Render(iconSlash + " cancelled")
			case "skipped":
				icon = dimStyle.Render(iconSlash + " skipped")
			case "neutral":
				icon = dimStyle.Render(iconCheckCircle + " neutral")
			default:
				icon = reviewPending.Render(iconClock + " " + *c.Conclusion)
			}
		default:
			icon = reviewPending.Render(iconClock + " pending")
		}

		name := lipgloss.NewStyle().Bold(true).Render(c.Name)
		lines = append(lines, name+" "+icon)
	}
	return lines
}

// --- File Tree ---

func (m *Model) treeMoveCursor(delta int) {
	if len(m.treeEntries) == 0 {
		return
	}
	// Skip directory entries.
	for {
		m.treeCursor += delta
		if m.treeCursor < 0 {
			m.treeCursor = 0
			return
		}
		if m.treeCursor >= len(m.treeEntries) {
			m.treeCursor = len(m.treeEntries) - 1
			return
		}
		if !m.treeEntries[m.treeCursor].IsDir {
			return
		}
	}
}

// treeScrollStart returns the first visible entry index, matching RenderFileTree's scroll logic.
func (m Model) treeScrollStart() int {
	start := 0
	if m.treeCursor > m.height-2 {
		start = m.treeCursor - m.height/2
		if start < 0 {
			start = 0
		}
	}
	end := start + m.height
	if end > len(m.treeEntries) {
		end = len(m.treeEntries)
		start = end - m.height
		if start < 0 {
			start = 0
		}
	}
	return start
}

// treeEntryIndexAtY maps a screen Y coordinate to a tree entry index.
func (m Model) treeEntryIndexAtY(y int) (int, bool) {
	idx := m.treeScrollStart() + y
	if idx < 0 || idx >= len(m.treeEntries) {
		return 0, false
	}
	return idx, true
}

func (m *Model) syncTreeCursor() {
	for i, e := range m.treeEntries {
		if !e.IsDir && e.FileIndex == m.currentFileIdx {
			m.treeCursor = i
			return
		}
	}
}

// --- Separator ---

func (m Model) renderFileSeparator() string {
	w := m.width
	if w < 10 {
		w = 10
	}

	fileCount := len(m.files)
	var totalAdd, totalDel int
	for _, f := range m.files {
		totalAdd += f.Additions
		totalDel += f.Deletions
	}

	left := fmt.Sprintf("%d File", fileCount)
	if fileCount != 1 {
		left += "s"
	}

	additions := fmt.Sprintf("+%d", totalAdd)
	deletions := fmt.Sprintf("-%d", totalDel)
	right := lipgloss.NewStyle().Foreground(lipgloss.Green).Render(additions) +
		" " +
		lipgloss.NewStyle().Foreground(lipgloss.Red).Render(deletions)

	rightPlain := fmt.Sprintf("+%d -%d", totalAdd, totalDel)
	gap := w - lipgloss.Width(left) - len(rightPlain)
	if gap < 1 {
		gap = 1
	}

	line := separatorStyle.Render(left) + strings.Repeat(" ", gap) + right
	separator := separatorStyle.Render(strings.Repeat("─", w))

	return "\n" + separator + "\n" + line + "\n"
}

// --- Meta / User ---

func (m Model) renderMeta() string {
	pr := m.pr
	author := coloredAuthor(pr.User.Login)

	if pr.Merged && pr.MergedBy != nil {
		if pr.MergedBy.Login == pr.User.Login {
			return dimStyle.Render(relativeTime(*pr.MergedAt)+" by ") + author
		}
		merger := coloredAuthor(pr.MergedBy.Login)
		return dimStyle.Render(relativeTime(*pr.MergedAt)+" by ") + merger
	}

	if pr.State == "closed" && pr.ClosedAt != nil {
		return dimStyle.Render(relativeTime(*pr.ClosedAt)+" by ") + author
	}

	return dimStyle.Render(relativeTime(pr.CreatedAt)+" by ") + author
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 30*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case d < 365*24*time.Hour:
		months := int(d.Hours() / 24 / 30)
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	default:
		years := int(d.Hours() / 24 / 365)
		if years == 1 {
			return "1 year ago"
		}
		return fmt.Sprintf("%d years ago", years)
	}
}

// --- Glamour ---

var markdownStyle = ansi.StyleConfig{
	Document: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{},
	},
	Heading: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockSuffix: "\n",
			Color:       stringPtr("5"), // magenta
			Bold:        boolPtr(true),
		},
	},
	H1: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Bold: boolPtr(true),
		},
	},
	H2: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "## ",
			Bold:   boolPtr(true),
		},
	},
	H3: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "### ",
			Bold:   boolPtr(true),
		},
	},
	Emph: ansi.StylePrimitive{
		Italic: boolPtr(true),
	},
	Strong: ansi.StylePrimitive{
		Bold: boolPtr(true),
	},
	Strikethrough: ansi.StylePrimitive{
		CrossedOut: boolPtr(true),
	},
	HorizontalRule: ansi.StylePrimitive{
		Color:  stringPtr("8"), // bright black
		Format: "\n────────\n",
	},
	Item: ansi.StylePrimitive{
		BlockPrefix: "• ",
	},
	Enumeration: ansi.StylePrimitive{
		BlockPrefix: ". ",
	},
	Task: ansi.StyleTask{
		Ticked:   "[✓] ",
		Unticked: "[ ] ",
	},
	Link: ansi.StylePrimitive{
		Color:     stringPtr("6"), // cyan
		Underline: boolPtr(true),
	},
	LinkText: ansi.StylePrimitive{
		Color: stringPtr("4"), // blue
		Bold:  boolPtr(true),
	},
	Code: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:  stringPtr("3"), // yellow
			Prefix: "`",
			Suffix: "`",
		},
	},
	CodeBlock: ansi.StyleCodeBlock{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: stringPtr("8"), // bright black
			},
			Margin: uintPtr(2),
		},
	},
	BlockQuote: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:  stringPtr("8"), // bright black
			Italic: boolPtr(true),
		},
		Indent:      uintPtr(1),
		IndentToken: stringPtr("│ "),
	},
	List: ansi.StyleList{
		LevelIndent: 4,
	},
	Table: ansi.StyleTable{
		CenterSeparator: stringPtr("│"),
		ColumnSeparator: stringPtr("│"),
		RowSeparator:    stringPtr("─"),
	},
}

func boolPtr(b bool) *bool       { return &b }
func stringPtr(s string) *string { return &s }
func uintPtr(u uint) *uint       { return &u }

func renderMarkdown(body string, width int) string {
	if width <= 0 || body == "" {
		return body
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyle),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return body
	}
	rendered, err := renderer.Render(body)
	if err != nil {
		return body
	}
	return strings.TrimSpace(rendered)
}
