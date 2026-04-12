package uictx

import (
	tea "charm.land/bubbletea/v2"
	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/ui/styles"
)

// QueryErrMsg is sent when a GitHub API query fails.
type QueryErrMsg struct {
	Err error
}

// PRLoadedMsg is sent when a single PR is loaded (shared by navigation flows).
type PRLoadedMsg struct {
	PR github.PullRequest
}

// FetchPR returns a tea.Cmd that fetches a single PR by owner/repo/number.
func FetchPR(c *github.CachedClient, owner, repo string, number int) tea.Cmd {
	return func() tea.Msg {
		pr, err := c.FetchPR(owner, repo, number)
		if err != nil {
			return QueryErrMsg{Err: err}
		}
		return PRLoadedMsg{PR: pr}
	}
}

// FetchPRByBranch returns a tea.Cmd that finds an open PR for the given branch.
func FetchPRByBranch(c *github.CachedClient, branch string) tea.Cmd {
	return func() tea.Msg {
		pr, err := c.FetchPRByBranch(branch)
		if err != nil {
			return QueryErrMsg{Err: err}
		}
		return PRLoadedMsg{PR: pr}
	}
}

// CachedCmd turns a cache query result into a tea.Cmd. If cached data is
// available it is returned immediately; if a refetch is needed it runs in the
// background. Errors produce QueryErrMsg.
func CachedCmd[T any](data T, found bool, refetch func() (T, error), wrap func(T) tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	if found {
		msg := wrap(data)
		cmds = append(cmds, func() tea.Msg { return msg })
	}

	if refetch != nil {
		fn := refetch
		cmds = append(cmds, func() tea.Msg {
			result, err := fn()
			if err != nil {
				return QueryErrMsg{Err: err}
			}
			return wrap(result)
		})
	}

	return tea.Batch(cmds...)
}

type Context struct {
	Client     *github.CachedClient
	DiffColors styles.DiffColors
	Username   string
	NWO        string // optional repo filter (owner/repo)
}

// KeyBinding describes a key and what it does, for help display.
type KeyBinding struct {
	Key         string // e.g. "j / k", "ctrl+d"
	Description string // e.g. "Move cursor down / up"
	Keywords    []string // extra search terms for fuzzy matching
}

type View interface {
	Init() tea.Cmd
	Update(tea.Msg) (View, tea.Cmd)
	HandleKey(tea.KeyPressMsg) (View, tea.Cmd, bool)
	View() string
	// KeyBindings returns the keybindings for this view, for the help picker.
	KeyBindings() []KeyBinding
}
