package localdiff

import "github.com/blakewilliams/ghq/internal/comments"

type ViewState = comments.ViewState
type ActiveState = comments.ActiveState

var (
	LoadViewState   = comments.LoadViewState
	SaveViewState   = comments.SaveViewState
	LoadActiveState = comments.LoadActiveState
	SaveActiveState = comments.SaveActiveState
)
