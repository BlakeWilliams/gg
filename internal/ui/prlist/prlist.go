package prlist

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/ui/uictx"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type PRSelectedMsg struct {
	PR github.PullRequest
}

type prItem struct {
	pr github.PullRequest
}

func (i prItem) Title() string {
	return i.pr.Title
}

func (i prItem) Description() string {
	return i.pr.User.Login
}

func (i prItem) FilterValue() string {
	return i.pr.Title
}

const (
	iconMerge      = "\U000f0261" // 󰉡 nf-md-source_merge
	iconClose      = "\U000f0156" // 󰅖 nf-md-close
	iconDraft      = "\U000f0613" // 󰘓 nf-md-pencil
	iconOpen       = "\U000f0130" // 󰄰 nf-md-checkbox_blank_circle_outline
	iconArrowRight = "\U000f0054" // 󰁔 nf-md-arrow_right
)

var (
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
)

// rowStyles holds the lipgloss styles for a single row, parameterized by
// an optional background color for the selected state.
type rowStyles struct {
	dim    lipgloss.Style
	number lipgloss.Style
	title  lipgloss.Style
	label  lipgloss.Style
	row    lipgloss.Style // full-width row wrapper
}

func makeRowStyles(bg color.Color) rowStyles {
	base := lipgloss.NewStyle()
	if bg != nil {
		base = base.Background(bg)
	}
	return rowStyles{
		dim:    base.Foreground(lipgloss.BrightBlack),
		number: base.Foreground(lipgloss.BrightBlack),
		title:  base.Bold(true),
		label:  base.Foreground(lipgloss.BrightBlack),
		row:    base,
	}
}

func prVerb(pr github.PullRequest, bg color.Color) string {
	base := lipgloss.NewStyle()
	if bg != nil {
		base = base.Background(bg)
	}
	switch {
	case pr.Merged:
		return base.Foreground(lipgloss.Magenta).Render(iconMerge + " merged")
	case pr.State == "closed":
		return base.Foreground(lipgloss.Red).Render(iconClose + " closed")
	case pr.Draft:
		return base.Foreground(lipgloss.Yellow).Render(iconDraft + " drafted")
	default:
		return base.Foreground(lipgloss.Green).Render(iconOpen + " opened")
	}
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	}
}

func renderLabels(labels []github.Label, s rowStyles) string {
	if len(labels) == 0 {
		return ""
	}
	var parts []string
	for _, l := range labels {
		parts = append(parts, s.label.Render(l.Name))
	}
	return strings.Join(parts, s.dim.Render(" · "))
}

type Model struct {
	list   list.Model
	ctx    *uictx.Context
	width  int
	height int
	err    error
}

func New(ctx *uictx.Context) Model {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.NormalTitle = lipgloss.NewStyle()
	delegate.Styles.NormalDesc = lipgloss.NewStyle()
	delegate.Styles.SelectedTitle = lipgloss.NewStyle()
	delegate.Styles.SelectedDesc = lipgloss.NewStyle()
	delegate.SetSpacing(0)
	delegate.SetHeight(3)

	l := list.New(nil, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	l.SetSpinner(spinner.Dot)
	l.Styles.Spinner = lipgloss.NewStyle().Foreground(lipgloss.Magenta)

	return Model{
		list: l,
		ctx:  ctx,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.list.StartSpinner(), m.ctx.Client.ListPullRequests())
}

func (m Model) Update(msg tea.Msg) (uictx.View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width, msg.Height-1)

	case github.PRsLoadedMsg:
		items := make([]list.Item, len(msg.PRs))
		for i, pr := range msg.PRs {
			items[i] = prItem{pr: pr}
		}
		cmd := m.list.SetItems(items)
		m.list.StopSpinner()
		return m, cmd

	case github.QueryErrMsg:
		m.err = msg.Err
		m.list.StopSpinner()

	case tea.KeyPressMsg:
		var cmd tea.Cmd
		var handled bool
		m, cmd, handled = m.handleKey(msg)
		if handled {
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) HandleKey(msg tea.KeyPressMsg) (uictx.View, tea.Cmd, bool) {
	return m.handleKey(msg)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "enter":
		if !m.list.SettingFilter() {
			if item := m.list.SelectedItem(); item != nil {
				if pi, ok := item.(prItem); ok {
					return m, func() tea.Msg {
						return PRSelectedMsg{PR: pi.pr}
					}, true
				}
			}
		}
	}
	return m, nil, false
}

func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err)
	}

	normalStyles := makeRowStyles(nil)
	selectedStyles := makeRowStyles(m.ctx.DiffColors.SelectColor)

	items := m.list.VisibleItems()
	selected := m.list.Index()
	w := m.width
	if w < 20 {
		w = 80
	}

	// Compute visible window. Each item is 3 lines (2 content + 1 separator),
	// except the last visible which is 2. Reserve 1 line for the status footer.
	const linesPerItem = 3
	maxVisible := (m.height - 1) / linesPerItem
	if maxVisible < 1 {
		maxVisible = 1
	}

	start := 0
	if selected >= maxVisible {
		start = selected - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(items) {
		end = len(items)
		start = end - maxVisible
		if start < 0 {
			start = 0
		}
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		item := items[i]
		pi := item.(prItem)
		pr := pi.pr
		isSelected := i == selected

		s := normalStyles
		if isSelected {
			s = selectedStyles
		}

		verb := prVerb(pr, m.ctx.DiffColors.SelectColor)
		if !isSelected {
			verb = prVerb(pr, nil)
		}

		num := s.number.Render(fmt.Sprintf("#%d", pr.Number))
		title := pr.Title
		if isSelected {
			title = s.title.Render(title)
		}
		age := s.dim.Render(relativeTime(pr.CreatedAt))

		// Line 1: #number title                             time
		line1 := " " + num + " " + title
		gap1 := w - lipgloss.Width(line1) - lipgloss.Width(age) - 1
		if gap1 < 1 {
			gap1 = 1
		}
		line1 += s.row.Render(strings.Repeat(" ", gap1)) + age

		// Line 2: @user opened · branch → base · labels
		user := s.dim.
			UnderlineStyle(lipgloss.UnderlineDotted).
			Hyperlink(fmt.Sprintf("https://github.com/%s", pr.User.Login)).
			Render(pr.User.Login)
		branch := s.dim.Render(pr.Head.Ref + " " + iconArrowRight + " " + pr.Base.Ref)
		line2 := " " + user + " " + verb + s.dim.Render(" · ") + branch
		if labels := renderLabels(pr.Labels, s); labels != "" {
			line2 += s.dim.Render(" · ") + labels
		}

		if isSelected {
			b.WriteString(padLine(line1, w, s.row) + "\n")
			b.WriteString(padLine(line2, w, s.row) + "\n")
		} else {
			b.WriteString(line1 + "\n")
			b.WriteString(line2 + "\n")
		}

		if i < end-1 {
			b.WriteString("\n")
		}
	}

	total := len(m.list.Items())
	visible := len(items)
	status := fmt.Sprintf(" %d pull requests", total)
	if visible != total {
		status = fmt.Sprintf(" %d of %d pull requests", visible, total)
	}
	b.WriteString("\n" + normalStyles.dim.Render(status))

	return b.String()
}

// padLine pads a line to full width with the row style's background.
func padLine(s string, width int, rowStyle lipgloss.Style) string {
	pad := width - lipgloss.Width(s)
	if pad < 0 {
		pad = 0
	}
	return s + rowStyle.Render(strings.Repeat(" ", pad))
}
