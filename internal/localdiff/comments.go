// Package localdiff re-exports comment types from the shared comments package.
package localdiff

import "github.com/blakewilliams/ghq/internal/comments"

// Re-export types and functions so existing code doesn't break.
type LocalComment = comments.LocalComment
type CommentStore = comments.CommentStore

var (
	IDToInt      = comments.IDToInt
	LoadComments = comments.LoadComments
)
