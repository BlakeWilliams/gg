package uictx

import (
	tea "charm.land/bubbletea/v2"
	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/ui/styles"
)

type Context struct {
	Client     *github.CachedClient
	DiffColors styles.DiffColors
	Username   string
	NWO        string // optional repo filter (owner/repo)
}

type View interface {
	Init() tea.Cmd
	Update(tea.Msg) (View, tea.Cmd)
	HandleKey(tea.KeyPressMsg) (View, tea.Cmd, bool)
	View() string
}
