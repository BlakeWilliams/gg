package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/terminal"
	"github.com/blakewilliams/ghq/internal/ui/commandbar"
	"github.com/blakewilliams/ghq/internal/ui/prdetail"
	"github.com/blakewilliams/ghq/internal/ui/prlist"
	"github.com/blakewilliams/ghq/internal/ui/styles"
	"github.com/blakewilliams/ghq/internal/ui/uictx"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type inputMode int

const (
	modeNormal inputMode = iota
	modeCommand
)

const chromeHeight = 2

const (
	iconGit     = "\U000f02a2" // 󰊢 nf-md-git
	iconPR      = "\U000f0041" // 󰁁 nf-md-arrow_top_right
	iconChevron = "\U000f0142" // 󰅂 nf-md-chevron_right
)

type Model struct {
	activeView uictx.View
	prList     prlist.Model // retained so we can restore it on back-navigation
	mode       inputMode
	commandBar commandbar.Model
	ctx        *uictx.Context
	palette    terminal.Palette
	width      int
	height     int
}

func NewApp(client *github.CachedClient) Model {
	ctx := &uictx.Context{Client: client}
	pl := prlist.New(ctx)
	return Model{
		activeView: pl,
		prList:     pl,
		commandBar: commandbar.New(),
		ctx:        ctx,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.activeView.Init(), m.ctx.Client.GCTickCmd(), queryPaletteCmd(), tea.RequestBackgroundColor)
}

// queryPaletteCmd sends OSC 4 queries through Bubble Tea's output buffer.
// Uses DCS passthrough when in tmux.
func queryPaletteCmd() tea.Cmd {
	inTmux := os.Getenv("TMUX") != ""
	var cmds []tea.Cmd
	for i := 0; i < 16; i++ {
		var seq string
		if inTmux {
			seq = fmt.Sprintf("\x1bPtmux;\x1b\x1b]4;%d;?\x07\x1b\\", i)
		} else {
			seq = fmt.Sprintf("\x1b]4;%d;?\x07", i)
		}
		cmds = append(cmds, tea.Raw(seq))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle palette responses.
	if cmd, handled := terminal.HandleMessage(msg, &m.palette); handled {
		if m.palette.Complete() {
			m.ctx.DiffColors = styles.ComputeDiffColors(m.palette)
		}
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Y == 0 {
			return m.handleBreadcrumbClick(msg.X)
		}

	case github.GCTickMsg:
		m.ctx.Client.GC()
		return m, m.ctx.Client.GCTickCmd()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.commandBar.SetWidth(msg.Width)
		contentMsg := tea.WindowSizeMsg{Width: msg.Width, Height: msg.Height - chromeHeight}
		var cmd tea.Cmd
		m.activeView, cmd = m.activeView.Update(contentMsg)
		return m, cmd

	case commandbar.CommandMsg:
		m.mode = modeNormal
		return m.handleCommand(msg)

	case commandbar.CancelledMsg:
		m.mode = modeNormal
		return m, nil

	case tea.KeyPressMsg:
		// Hard globals — always handled regardless of view/mode.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// Command mode owns all input.
		if m.mode == modeCommand {
			var cmd tea.Cmd
			m.commandBar, cmd = m.commandBar.Update(msg)
			return m, cmd
		}

		// Delegate to active view first.
		view, cmd, handled := m.activeView.HandleKey(msg)
		if handled {
			m.activeView = view
			return m, cmd
		}

		// Global shortcuts (view didn't claim the key).
		switch msg.String() {
		case ":":
			m.mode = modeCommand
			return m, m.commandBar.Focus()
		case "esc":
			if _, ok := m.activeView.(prdetail.Model); ok {
				return m.navigateToList()
			}
		}

	default:
		if m.mode == modeCommand {
			var cmd tea.Cmd
			m.commandBar, cmd = m.commandBar.Update(msg)
			return m, cmd
		}

	case prlist.PRSelectedMsg:
		// Save prList state before switching away.
		if pl, ok := m.activeView.(prlist.Model); ok {
			m.prList = pl
		}
		m.activeView = prdetail.New(msg.PR, m.ctx, m.width, m.height-chromeHeight)
		return m, m.activeView.Init()
	}

	// Forward non-key messages to active view.
	var cmd tea.Cmd
	m.activeView, cmd = m.activeView.Update(msg)
	return m, cmd
}

func (m Model) navigateToList() (tea.Model, tea.Cmd) {
	m.activeView = m.prList
	// Re-send the current window size so the list adjusts if the terminal was resized.
	resize := tea.WindowSizeMsg{Width: m.width, Height: m.height - chromeHeight}
	m.activeView, _ = m.activeView.Update(resize)
	return m, nil
}

func (m Model) handleCommand(msg commandbar.CommandMsg) (tea.Model, tea.Cmd) {
	switch msg.Command {
	case "q", "quit":
		return m, tea.Quit
	case "refresh":
		if _, ok := m.activeView.(prlist.Model); ok {
			m.ctx.Client.InvalidateAll()
			return m, m.ctx.Client.ListPullRequests()
		}
	case "back":
		if _, ok := m.activeView.(prdetail.Model); ok {
			return m.navigateToList()
		}
	}
	return m, nil
}

func (m Model) View() tea.View {
	header := m.renderHeader()

	contentHeight := m.height - chromeHeight
	if contentHeight < 0 {
		contentHeight = 0
	}

	content := lipgloss.NewStyle().Height(contentHeight).Render(m.activeView.View())

	var bar string
	if m.mode == modeCommand {
		bar = m.commandBar.View()
	} else {
		bar = m.renderStatusBar()
	}

	v := tea.NewView(header + "\n" + content + "\n" + bar)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) renderHeader() string {
	repo := styles.HeaderRepo.Render(iconGit + " " + m.ctx.Client.RepoFullName())
	sep := styles.HeaderSep.Render(" " + iconChevron + " ")

	pulls := styles.HeaderSection.Render(iconPR + " Pulls")
	crumb := repo + sep + pulls

	if detail, ok := m.activeView.(prdetail.Model); ok {
		crumb += sep + styles.HeaderSection.Render(fmt.Sprintf("#%d %s", detail.PRNumber(), detail.PRTitle()))
		crumb += sep + styles.HeaderSection.Render(detail.Tab())
	}

	return crumb
}

func (m Model) handleBreadcrumbClick(x int) (tea.Model, tea.Cmd) {
	repoWidth := lipgloss.Width(styles.HeaderRepo.Render(iconGit + " " + m.ctx.Client.RepoFullName()))
	sepWidth := lipgloss.Width(styles.HeaderSep.Render(" " + iconChevron + " "))
	pullsWidth := lipgloss.Width(styles.HeaderSection.Render(iconPR + " Pulls"))

	pullsStart := repoWidth + sepWidth
	pullsEnd := pullsStart + pullsWidth

	if _, ok := m.activeView.(prdetail.Model); ok && x < pullsEnd {
		return m.navigateToList()
	}

	return m, nil
}

func (m Model) renderStatusBar() string {
	var leftHints, rightHints []string

	switch v := m.activeView.(type) {
	case prlist.Model:
		leftHints = []string{":  cmd", "/  filter", "enter  open"}
	case prdetail.Model:
		leftHints, rightHints = v.StatusHints()
	}

	left := formatHints(leftHints)
	right := formatHints(rightHints)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	return left + strings.Repeat(" ", gap) + right
}

func formatHints(hints []string) string {
	var parts []string
	for _, h := range hints {
		// Split on first space: "key desc"
		idx := strings.IndexByte(h, ' ')
		if idx > 0 {
			key := h[:idx]
			desc := h[idx+1:]
			parts = append(parts, styles.StatusBarKey.Render(key)+" "+styles.StatusBarHint.Render(desc))
		} else {
			parts = append(parts, styles.StatusBarHint.Render(h))
		}
	}
	return strings.Join(parts, styles.StatusBarHint.Render("  "))
}

func hint(key, desc string) string {
	return styles.StatusBarKey.Render(key) + " " + styles.StatusBarHint.Render(desc)
}
