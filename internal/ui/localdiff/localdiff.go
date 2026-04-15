package localdiff

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/blakewilliams/ghq/internal/review/comments"
	"github.com/blakewilliams/ghq/internal/review/copilot"
	"github.com/blakewilliams/ghq/internal/git"
	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/ui/components"
	"github.com/blakewilliams/ghq/internal/ui/diffviewer"
	"github.com/blakewilliams/ghq/internal/ui/picker"
	"github.com/blakewilliams/ghq/internal/ui/styles"
	"github.com/blakewilliams/ghq/internal/ui/uictx"
	"github.com/blakewilliams/ghq/internal/git/watcher"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/google/uuid"
)

// Messages.
type diffLoadedMsg struct {
	files []github.PullRequestFile
	mode  git.DiffMode
}

type diffErrorMsg struct {
	err error
}

type fileHighlightedMsg struct {
	highlight components.HighlightedDiff
	index     int
}

type watchReadyMsg struct{}
type copilotTickMsg struct{}

// GoToLineMsg is sent from the command bar to jump to a source line number.
type GoToLineMsg struct {
	Line int
}

// SwitchToPRMsg is sent when the user selects the PR view from the mode picker.
type SwitchToPRMsg struct {
	PR github.PullRequest
}

// OpenViewPickerMsg is sent to app.go to open the view mode picker.
type OpenViewPickerMsg struct {
	Items []picker.Item
}

// SwitchModeMsg is sent from app.go back to localdiff to change the diff mode.
type SwitchModeMsg struct {
	Mode git.DiffMode
}

// prDetectedMsg is a localdiff-internal message for PR auto-detection.
// This is separate from uictx.PRLoadedMsg so the app doesn't intercept it.
type prDetectedMsg struct {
	PR github.PullRequest
}

// prDetectFailedMsg means no PR was found for this branch.
type prDetectFailedMsg struct{}

var dimStyle = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)

type Model struct {
	// Embedded diff viewer (shared with prdetail).
	dv  diffviewer.DiffViewer
	ctx *uictx.Context // alias for dv.Ctx — avoids m.dv.Ctx everywhere

	// Git state.
	repoRoot string
	branch   string
	mode     git.DiffMode

	// Layout per file (for cursor positioning).
	fileLayouts []components.DiffLayout

	// Render cache per file (for splice-based updates).
	fileRenderCache []*components.DiffRenderResult

	// Comments.
	commentStore *comments.CommentStore
	replyToID    string

	// File watcher.
	watcher *watcher.Watcher

	// Restore state from previous session.
	savedFilename string
	savedLineNo   int
	savedSide     string

	// Per-file cursor memory (session only, not persisted).
	fileCursors map[string]int // filename -> diffCursor

	// Fast filename->index lookup (rebuilt on diff load).
	filePathIndex map[string]int

	// PR detection.
	pr       *github.PullRequest // nil if no PR for this branch
	prLoaded bool                // true once checked

	// Render cache.
	lastFormattedStreamLen int // length of copilot reply buffer at last formatFile

	// Staging ops counter.
	stagingInFlight int // number of staging ops in progress
}

func New(ctx *uictx.Context, repoRoot string, width, height int) Model {
	branch, _ := git.CurrentBranch(repoRoot)
	w, _ := watcher.New(repoRoot, nil)
	cp, _ := copilot.New(repoRoot)
	active := comments.LoadActiveState(repoRoot, branch)
	vs := comments.LoadViewState(repoRoot, branch, active.Mode)
	return Model{
		ctx: ctx,
		dv: diffviewer.DiffViewer{
			Ctx:             ctx,
			Width:           width,
			Height:          height,
			CurrentFileIdx:  -1,
			SelectionAnchor: -1,
			Tree: components.FileTree{
				Width:   35,
				Height:  height - 2,
				Focused: true,
			},
			Copilot:         cp,
			CopilotReplyBuf: make(map[string]string),
		},
		repoRoot:      repoRoot,
		branch:        branch,
		mode:          active.Mode,
		watcher:       w,
		commentStore:  comments.LoadComments(repoRoot),
		fileCursors:   make(map[string]int),
		filePathIndex: make(map[string]int),
		savedFilename: vs.Filename,
		savedLineNo:   vs.LineNo,
		savedSide:     vs.Side,
	}
}

func (m Model) BranchName() string              { return m.branch }
func (m Model) DiffMode() git.DiffMode          { return m.mode }
func (m Model) PR() *github.PullRequest         { return m.pr }
func (m Model) Files() []github.PullRequestFile { return m.dv.Files }

// restoreSavedPosition finds the saved file by name and restores cursor
// to the diff line matching the saved source line number.
func (m *Model) restoreSavedPosition() {
	for i, f := range m.dv.Files {
		if f.Filename == m.savedFilename {
			m.dv.CurrentFileIdx = i
			m.dv.DiffCursor = m.findDiffLineBySourceLine(i, m.savedLineNo, m.savedSide)
			// Set tree cursor to match the file.
			m.dv.Tree.Cursor = m.dv.Tree.IndexForFile(i)
			m.dv.Tree.Focused = false
			break
		}
	}
	// Clear saved state so it only applies once.
	m.savedFilename = ""
}

// findDiffLineBySourceLine finds the diff line index closest to the given
// source line number. This is stable across code changes — if line 42 moves
// to diff index 15 instead of 12, we still land on line 42.
func (m Model) findDiffLineBySourceLine(fileIdx, lineNo int, side string) int {
	if fileIdx >= len(m.dv.FileDiffs) || lineNo == 0 {
		return 0
	}
	lines := m.dv.FileDiffs[fileIdx]
	best := 0
	bestDist := -1
	for i, dl := range lines {
		if dl.Type == components.LineHunk {
			continue
		}
		var srcLine int
		if side == "LEFT" {
			srcLine = dl.OldLineNo
		} else {
			srcLine = dl.NewLineNo
		}
		dist := lineNo - srcLine
		if dist < 0 {
			dist = -dist
		}
		if bestDist < 0 || dist < bestDist {
			best = i
			bestDist = dist
		}
	}
	return best
}

// saveViewState persists the current position for next session.
// Stores the source line number at the cursor (not diff index) so
// the position survives code changes that shift diff lines.
func (m Model) saveViewState() {
	var filename, side string
	var lineNo int
	if m.dv.CurrentFileIdx >= 0 && m.dv.CurrentFileIdx < len(m.dv.Files) {
		filename = m.dv.Files[m.dv.CurrentFileIdx].Filename
		if m.dv.CurrentFileIdx < len(m.dv.FileDiffs) && m.dv.DiffCursor < len(m.dv.FileDiffs[m.dv.CurrentFileIdx]) {
			dl := m.dv.FileDiffs[m.dv.CurrentFileIdx][m.dv.DiffCursor]
			if dl.Type == components.LineDel {
				lineNo = dl.OldLineNo
				side = "LEFT"
			} else {
				lineNo = dl.NewLineNo
				side = "RIGHT"
			}
		}
	}
	comments.SaveViewState(m.repoRoot, m.branch, m.mode, comments.ViewState{
		Filename: filename,
		LineNo:   lineNo,
		Side:     side,
	})
	comments.SaveActiveState(m.repoRoot, m.branch, comments.ActiveState{Mode: m.mode})
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadDiff()}
	if m.watcher != nil {
		cmds = append(cmds, m.watcher.WaitCmd())
	}
	if m.dv.Copilot != nil {
		cmds = append(cmds, m.dv.Copilot.ListenCmd())
	}
	// Auto-detect PR for this branch (uses internal msg type so app doesn't intercept).
	if !m.prLoaded {
		client := m.ctx.Client
		branch := m.branch
		cmds = append(cmds, func() tea.Msg {
			pr, err := client.FetchPRByBranch(m.ctx.Owner, m.ctx.Repo, branch)
			if err != nil {
				return prDetectFailedMsg{}
			}
			return prDetectedMsg{PR: pr}
		})
	}
	return tea.Batch(cmds...)
}

// watchAfterCooldown waits a bit before re-arming the watcher, to avoid
// feedback loops where git-diff touching .git/ files re-triggers immediately.
func (m Model) watchAfterCooldown() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return watchReadyMsg{}
	})
}

func (m Model) loadDiff() tea.Cmd {
	repoRoot := m.repoRoot
	mode := m.mode
	return func() tea.Msg {
		rawDiff, err := git.Diff(repoRoot, mode)
		if err != nil {
			return diffErrorMsg{err: err}
		}
		files := git.ParseDiffToFiles(rawDiff)
		return diffLoadedMsg{files: files, mode: mode}
	}
}

func (m Model) Update(msg tea.Msg) (uictx.View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.dv.Width = msg.Width
		m.dv.Height = msg.Height
		m.dv.Tree.Height = msg.Height - 2
		// Width changed — layouts need recomputing.
		if m.dv.FilesListLoaded {
			m.reformatAllFiles()
		}
		m.rebuildContent()
		return m, nil

	case tea.MouseClickMsg:
		if msg.X < m.dv.Tree.Width {
			if idx, ok := m.dv.Tree.EntryIndexAtY(msg.Y); ok {
				if idx == 0 {
					m.dv.Tree.Cursor = 0
					m.dv.CurrentFileIdx = -1
					m.rebuildContent()
					m.dv.VP.GotoTop()
					m.saveViewState()
				} else if idx >= 2 {
					eIdx := idx - 2
					if eIdx >= 0 && eIdx < len(m.dv.Tree.Entries) {
						e := m.dv.Tree.Entries[eIdx]
						if !e.IsDir && e.FileIndex >= 0 {
							m.dv.Tree.Cursor = idx
							m.dv.CurrentFileIdx = e.FileIndex
							m.rebuildContent()
							m.dv.VP.GotoTop()
							m.saveViewState()
						}
					}
				}
			}
			return m, nil
		}

	case diffLoadedMsg:
		// Build index of old patches by filename for incremental updates.
		oldPatches := make(map[string]string, len(m.dv.Files))
		oldHighlights := make(map[string]components.HighlightedDiff)
		oldRendered := make(map[string]string)
		for i, f := range m.dv.Files {
			oldPatches[f.Filename] = f.Patch
			if i < len(m.dv.HighlightedFiles) && m.dv.HighlightedFiles[i].File.Filename != "" {
				oldHighlights[f.Filename] = m.dv.HighlightedFiles[i]
			}
			if i < len(m.dv.RenderedFiles) {
				oldRendered[f.Filename] = m.dv.RenderedFiles[i]
			}
		}

		m.dv.Files = msg.files
		m.dv.HighlightedFiles = make([]components.HighlightedDiff, len(msg.files))
		m.dv.RenderedFiles = make([]string, len(msg.files))
		m.dv.FileDiffs = make([][]components.DiffLine, len(msg.files))
		m.dv.FileDiffOffsets = make([][]int, len(msg.files))
		m.dv.FileCommentPositions = make([][]components.CommentPosition, len(msg.files))
		m.fileLayouts = make([]components.DiffLayout, len(msg.files))
		m.fileRenderCache = make([]*components.DiffRenderResult, len(msg.files))
		m.rebuildFilePathIndex()
		m.dv.FilesListLoaded = true

		// Reuse cached highlights for files whose patch hasn't changed.
		var needHighlight []int
		for i, f := range msg.files {
			m.dv.FileDiffs[i] = components.ParsePatchLines(f.Patch)
			if old, ok := oldPatches[f.Filename]; ok && old == f.Patch {
				if hl, ok := oldHighlights[f.Filename]; ok {
					m.dv.HighlightedFiles[i] = hl
					m.dv.RenderedFiles[i] = oldRendered[f.Filename]
					continue
				}
			}
			// Keep stale rendered content so the viewport doesn't flash
			// a skeleton while the new highlight is in progress.
			if rendered, ok := oldRendered[f.Filename]; ok {
				m.dv.RenderedFiles[i] = rendered
			}
			needHighlight = append(needHighlight, i)
		}

		m.dv.FilesHighlighted = len(msg.files) - len(needHighlight)
		m.dv.Tree.SetFiles(m.dv.Files)

		// Update watcher to cover directories with changed files.
		if m.watcher != nil {
			var filenames []string
			for _, f := range msg.files {
				filenames = append(filenames, f.Filename)
			}
			m.watcher.UpdateDirs(watcher.DirsFromFiles(filenames))
		}

		// Restore saved position from previous session.
		savedOffset := m.dv.VP.YOffset()
		if m.savedFilename != "" {
			m.restoreSavedPosition()
		} else if m.dv.CurrentFileIdx >= len(m.dv.Files) {
			m.dv.CurrentFileIdx = -1
			m.dv.Tree.Cursor = 0
		}

		// Only re-format the current file if it kept its highlights.
		// Other files will be formatted lazily when navigated to.
		if m.dv.CurrentFileIdx >= 0 && m.dv.CurrentFileIdx < len(msg.files) {
			f := msg.files[m.dv.CurrentFileIdx]
			if _, ok := oldHighlights[f.Filename]; ok && oldPatches[f.Filename] == f.Patch {
				m.formatFile(m.dv.CurrentFileIdx)
			}
		}

		m.rebuildContentIfChanged()
		// Preserve scroll position on file-watcher reloads (not initial load).
		if m.savedFilename == "" && savedOffset > 0 {
			m.dv.VP.SetYOffset(savedOffset)
		}

		// Only highlight files that actually changed.
		if len(needHighlight) > 0 {
			// Prioritize the current file if it needs highlighting.
			for i, idx := range needHighlight {
				if idx == m.dv.CurrentFileIdx {
					needHighlight[0], needHighlight[i] = needHighlight[i], needHighlight[0]
					break
				}
			}
			return m, m.highlightFileCmd(needHighlight[0])
		}
		m.dv.FilesLoading = false
		return m, nil

	case diffErrorMsg:
		return m, nil

	case SwitchModeMsg:
		m.saveViewState()
		m.mode = msg.Mode
		m.dv.FilesListLoaded = false
		m.dv.FilesHighlighted = 0
		m.dv.FilesLoading = true
		m.dv.CurrentFileIdx = -1
		m.dv.Tree.Cursor = 0
		m.fileCursors = make(map[string]int)
		vs := comments.LoadViewState(m.repoRoot, m.branch, m.mode)
		m.savedFilename = vs.Filename
		m.savedLineNo = vs.LineNo
		m.savedSide = vs.Side
		return m, m.loadDiff()

	case prDetectedMsg:
		m.pr = &msg.PR
		m.prLoaded = true
		return m, nil

	case prDetectFailedMsg:
		m.prLoaded = true
		return m, nil

	case stageDoneMsg:
		m.stagingInFlight--
		if m.stagingInFlight < 0 {
			m.stagingInFlight = 0
		}
		// If all staging ops are done, do a single clean reload.
		if m.stagingInFlight == 0 {
			return m, m.loadDiff()
		}
		return m, nil

	case watcher.FileChangedMsg:
		// Suppress reloads while staging is in flight to avoid lock contention.
		if m.stagingInFlight > 0 {
			if m.watcher != nil {
				return m, m.watcher.WaitCmd()
			}
			return m, nil
		}
		cmds := []tea.Cmd{m.loadDiff()}
		if m.watcher != nil {
			cmds = append(cmds, m.watcher.WaitCmd())
		}
		return m, tea.Batch(cmds...)

	case fileHighlightedMsg:
		if msg.index >= len(m.dv.HighlightedFiles) {
			return m, nil
		}
		m.dv.HighlightedFiles[msg.index] = msg.highlight
		m.dv.FilesHighlighted++
		// Only render the current file synchronously. Others render on access.
		if msg.index == m.dv.CurrentFileIdx || m.dv.CurrentFileIdx == -1 {
			m.formatFile(msg.index)
			m.rebuildContent()
		}
		// Find the next file that needs highlighting.
		for next := msg.index + 1; next < len(m.dv.Files); next++ {
			if m.dv.HighlightedFiles[next].File.Filename == "" {
				return m, m.highlightFileCmd(next)
			}
		}
		m.dv.FilesLoading = false
		return m, nil

	case copilot.ReplyMsg:
		m.dv.CopilotReplyBuf[msg.CommentID] += msg.Content
		if msg.Done {
			body := m.dv.CopilotReplyBuf[msg.CommentID]
			delete(m.dv.CopilotReplyBuf, msg.CommentID)
			pendingPath := m.dv.CopilotPendingPath(msg.CommentID)
			m.dv.ClearCopilotPending(msg.CommentID)
			if body != "" {
				for _, c := range m.commentStore.Comments {
					if c.ID == msg.CommentID {
						reply := comments.LocalComment{
							ID:          uuid.New().String(),
							Body:        strings.TrimSpace(body),
							Path:        c.Path,
							Line:        c.Line,
							Side:        c.Side,
							InReplyToID: c.ID,
							Author:      "copilot",
							CreatedAt:   time.Now(),
						}
						m.commentStore.Add(reply)
						break
					}
				}
			}
			// Update the affected file's rendered cache.
			if fileIdx := m.fileIndexForPath(pendingPath); fileIdx >= 0 {
				m.formatFile(fileIdx)
				// Only rebuild viewport if we're looking at this file.
				if fileIdx == m.dv.CurrentFileIdx {
					m.rebuildContent()
				}
			}
		} else if fileIdx := m.fileIndexForPath(m.dv.CopilotPendingPath(msg.CommentID)); fileIdx >= 0 && fileIdx == m.dv.CurrentFileIdx {
			// Streaming delta — splice only the affected thread (O(thread), not O(n)).
			m.spliceThreadForComment(fileIdx, msg.CommentID)
			m.rebuildContentIfChanged()
		}
		cmds := []tea.Cmd{m.dv.Copilot.ListenCmd()}
		if m.dv.HasCopilotPending() {
			cmds = append(cmds, tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg {
				return copilotTickMsg{}
			}))
		}
		return m, tea.Batch(cmds...)

	case copilot.ErrorMsg:
		pendingPath := m.dv.CopilotPendingPath(msg.CommentID)
		m.dv.ClearCopilotPending(msg.CommentID)
		// Only re-render the affected file.
		if fileIdx := m.fileIndexForPath(pendingPath); fileIdx >= 0 {
			m.formatFile(fileIdx)
		}
		m.rebuildContent()
		cmds := []tea.Cmd{}
		if m.dv.Copilot != nil {
			cmds = append(cmds, m.dv.Copilot.ListenCmd())
		}
		return m, tea.Batch(cmds...)

	case copilotTickMsg:
		if !m.dv.HasCopilotPending() {
			return m, nil
		}
		m.dv.CopilotDots = (m.dv.CopilotDots + 1) % 4
		// Splice pending threads in the current file (O(thread), not O(n)).
		for commentID, info := range m.dv.CopilotPending {
			if fileIdx := m.fileIndexForPath(info.Path); fileIdx >= 0 && fileIdx == m.dv.CurrentFileIdx {
				m.spliceThreadForComment(fileIdx, commentID)
			}
		}
		m.rebuildContentIfChanged()
		return m, tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg {
			return copilotTickMsg{}
		})

	case GoToLineMsg:
		if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			m.goToSourceLine(msg.Line)
		}
		return m, nil

	case tea.KeyPressMsg:
		var cmd tea.Cmd
		var handled bool
		m, cmd, handled = m.handleKey(msg)
		if handled {
			return m, cmd
		}
	}

	// When composing, delegate non-key messages to textarea.
	if m.dv.Composing {
		var cmd tea.Cmd
		m.dv.CommentInput, cmd = m.dv.CommentInput.Update(msg)
		return m, cmd
	}

	// Viewport updates.
	if m.dv.VPReady {
		prevOffset := m.dv.VP.YOffset()
		var cmd tea.Cmd
		m.dv.VP, cmd = m.dv.VP.Update(msg)
		if m.dv.VP.YOffset() != prevOffset && m.dv.CurrentFileIdx >= 0 {
			m.syncDiffCursorToViewport()
		}
		return m, cmd
	}
	return m, nil
}

func (m Model) HandleKey(msg tea.KeyPressMsg) (uictx.View, tea.Cmd, bool) {
	return m.handleKey(msg)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	// When composing a comment, handle textarea keys.
	if m.dv.Composing {
		return m.handleCommentKey(msg)
	}

	// Thread navigation mode.
	if m.dv.ThreadCursor > 0 {
		switch msg.String() {
		case "j", "down":
			// Scroll within a long comment before moving to the next one.
			if m.commentExtendsBelow() {
				m.dv.VP.SetYOffset(m.dv.VP.YOffset() + 1)
				return m, nil, true
			}
			count := m.threadCommentCount()
			if m.dv.ThreadCursor < count {
				m.dv.ThreadCursor++
				m.formatFile(m.dv.CurrentFileIdx)
				m.rebuildContent()
				m.scrollToThreadCursor()
			}
			return m, nil, true
		case "k", "up":
			// Scroll within a long comment before moving to the previous one.
			if m.commentExtendsAbove() {
				m.dv.VP.SetYOffset(m.dv.VP.YOffset() - 1)
				return m, nil, true
			}
			if m.dv.ThreadCursor > 1 {
				m.dv.ThreadCursor--
				m.formatFile(m.dv.CurrentFileIdx)
				m.rebuildContent()
				m.scrollToThreadCursorBottom()
			} else {
				m.dv.ThreadCursor = 0
				m.formatFile(m.dv.CurrentFileIdx)
				m.rebuildContent()
				m.scrollToDiffCursor()
			}
			return m, nil, true
		case "ctrl+d":
			m.dv.VP.SetYOffset(m.dv.VP.YOffset() + m.dv.Height/2)
			return m, nil, true
		case "ctrl+u":
			m.dv.VP.SetYOffset(m.dv.VP.YOffset() - m.dv.Height/2)
			return m, nil, true
		case "esc":
			m.dv.ThreadCursor = 0
			m.formatFile(m.dv.CurrentFileIdx)
			m.rebuildContent()
			m.scrollToDiffCursor()
			return m, nil, true
		case "r":
			if cmd := m.replyToThreadAtCursor(); cmd != nil {
				m.dv.ThreadCursor = 0
				return m, cmd, true
			}
			return m, nil, true
		case "x":
			m.toggleResolveAtCursor()
			m.dv.ThreadCursor = 0
			return m, nil, true
		case "enter":
			m.dv.ThreadCursor = 0
			m.formatFile(m.dv.CurrentFileIdx)
			m.rebuildContent()
			m.scrollToDiffCursor()
			return m, nil, true
		}
		return m, nil, true
	}

	// Clear selection on esc.
	if msg.String() == "esc" && m.dv.SelectionAnchor >= 0 {
		m.dv.SelectionAnchor = -1
		return m, nil, true
	}

	switch msg.String() {
	case "f":
		m.dv.Tree.Focused = !m.dv.Tree.Focused
		return m, nil, true
	case "m":
		// Cycle diff mode: Working → Staged → Branch (skip Branch on default branch).
		m.saveViewState()
		defaultBranch, _ := git.DefaultBranch(m.repoRoot)
		if m.branch == defaultBranch {
			if m.mode == git.DiffWorking {
				m.mode = git.DiffStaged
			} else {
				m.mode = git.DiffWorking
			}
		} else {
			m.mode = (m.mode + 1) % 3
		}
		m.dv.FilesListLoaded = false
		m.dv.FilesHighlighted = 0
		m.dv.FilesLoading = true
		m.dv.CurrentFileIdx = -1
		m.dv.Tree.Cursor = 0
		vs := comments.LoadViewState(m.repoRoot, m.branch, m.mode)
		m.savedFilename = vs.Filename
		m.savedLineNo = vs.LineNo
		m.savedSide = vs.Side
		return m, m.loadDiff(), true
	case "h", "left":
		m.dv.Tree.Focused = true
		return m, nil, true
	case "l", "right":
		m.dv.Tree.Focused = false
		return m, nil, true
	case "ctrl+k":
		m.dv.Tree.MoveSelection(-1)
		m.selectTreeEntry()
		return m, nil, true
	case "ctrl+j":
		m.dv.Tree.MoveSelection(1)
		m.selectTreeEntry()
		return m, nil, true
	case "j", "down":
		if m.dv.Tree.Focused {
			m.dv.Tree.MoveCursorBy(1)
			return m, nil, true
		}
		if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			m.dv.SelectionAnchor = -1
			m.moveDiffCursor(1)
			return m, nil, true
		}
	case "k", "up":
		if m.dv.Tree.Focused {
			m.dv.Tree.MoveCursorBy(-1)
			return m, nil, true
		}
		if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			m.dv.SelectionAnchor = -1
			m.moveDiffCursor(-1)
			return m, nil, true
		}
	case "J", "shift+down":
		if !m.dv.Tree.Focused && m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			if m.dv.SelectionAnchor < 0 {
				m.dv.SelectionAnchor = m.dv.DiffCursor
			}
			m.moveDiffCursor(1)
			return m, nil, true
		}
	case "K", "shift+up":
		if !m.dv.Tree.Focused && m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			if m.dv.SelectionAnchor < 0 {
				m.dv.SelectionAnchor = m.dv.DiffCursor
			}
			m.moveDiffCursor(-1)
			return m, nil, true
		}
	case "enter":
		if m.dv.Tree.Focused {
			m.selectTreeEntry()
			m.dv.Tree.Focused = false
			return m, nil, true
		}
		// If inside a thread, exit thread mode.
		if m.dv.ThreadCursor > 0 {
			m.dv.ThreadCursor = 0
			m.formatFile(m.dv.CurrentFileIdx)
			m.rebuildContent()
			m.scrollToDiffCursor()
			return m, nil, true
		}
		// If on a line with comments, enter thread navigation.
		if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() && m.cursorHasThread() {
			m.dv.ThreadCursor = 1
			m.formatFile(m.dv.CurrentFileIdx)
			m.rebuildContent()
			m.scrollToThreadCursor()
			return m, nil, true
		}
		// Otherwise open comment input.
		if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			return m.openCommentInput()
		}
	case "a":
		if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			return m.openCommentInput()
		}
	case "r":
		// Reply to comment thread on current line.
		if !m.dv.Tree.Focused && m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			if cmd := m.replyToThreadAtCursor(); cmd != nil {
				return m, cmd, true
			}
		}
	case "x":
		// Resolve/unresolve comment thread on current line.
		if !m.dv.Tree.Focused && m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			if m.toggleResolveAtCursor() {
				return m, nil, true
			}
		}
	case "s":
		if m.mode == git.DiffWorking {
			if m.dv.Tree.Focused {
				// Stage the whole file under the tree cursor.
				if fileIdx := m.dv.Tree.FileIndex(); fileIdx >= 0 {
					m.dv.CurrentFileIdx = fileIdx
					return m.stageWholeFile(false)
				}
			} else if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
				status := m.dv.Files[m.dv.CurrentFileIdx].Status
				if status == "removed" || status == "renamed" {
					return m.stageWholeFile(false)
				}
				return m.stageSelection(false)
			}
		}
	case "u":
		if m.mode == git.DiffStaged {
			if m.dv.Tree.Focused {
				if fileIdx := m.dv.Tree.FileIndex(); fileIdx >= 0 {
					m.dv.CurrentFileIdx = fileIdx
					return m.stageWholeFile(true)
				}
			} else if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
				return m.stageSelection(true)
			}
		}
	case "S":
		if m.mode == git.DiffWorking {
			if m.dv.Tree.Focused {
				if fileIdx := m.dv.Tree.FileIndex(); fileIdx >= 0 {
					m.dv.CurrentFileIdx = fileIdx
					return m.stageWholeFile(false)
				}
			} else if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
				status := m.dv.Files[m.dv.CurrentFileIdx].Status
				if status == "removed" || status == "renamed" {
					return m.stageWholeFile(false)
				}
				return m.stageHunk(false)
			}
		}
	case "U":
		if m.mode == git.DiffStaged {
			if m.dv.Tree.Focused {
				if fileIdx := m.dv.Tree.FileIndex(); fileIdx >= 0 {
					m.dv.CurrentFileIdx = fileIdx
					return m.stageWholeFile(true)
				}
			} else if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
				return m.stageHunk(true)
			}
		}
	case "ctrl+d":
		m.dv.SelectionAnchor = -1
		if m.dv.Tree.Focused {
			m.dv.Tree.MoveCursorBy(m.dv.Height / 2)
		} else if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			m.scrollAndSyncCursor(m.dv.Height / 2)
		} else {
			m.dv.VP.SetYOffset(m.dv.VP.YOffset() + m.dv.Height/2)
		}
		return m, nil, true
	case "ctrl+u":
		m.dv.SelectionAnchor = -1
		if m.dv.Tree.Focused {
			m.dv.Tree.MoveCursorBy(-m.dv.Height / 2)
		} else if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			m.scrollAndSyncCursor(-m.dv.Height / 2)
		} else {
			m.dv.VP.SetYOffset(m.dv.VP.YOffset() - m.dv.Height/2)
		}
		return m, nil, true
	case "ctrl+f":
		m.dv.SelectionAnchor = -1
		if m.dv.Tree.Focused {
			m.dv.Tree.MoveCursorBy(m.dv.Height)
		} else if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			m.scrollAndSyncCursor(m.dv.Height)
		} else {
			m.dv.VP.SetYOffset(m.dv.VP.YOffset() + m.dv.Height)
		}
		return m, nil, true
	case "ctrl+b":
		m.dv.SelectionAnchor = -1
		if m.dv.Tree.Focused {
			m.dv.Tree.MoveCursorBy(-m.dv.Height)
		} else if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
			m.scrollAndSyncCursor(-m.dv.Height)
		} else {
			m.dv.VP.SetYOffset(m.dv.VP.YOffset() - m.dv.Height)
		}
		return m, nil, true
	case "G":
		m.dv.WaitingG = false
		if m.dv.Tree.Focused {
			totalEntries := 2 + len(m.dv.Tree.Entries)
			m.dv.Tree.MoveCursorBy(totalEntries)
		} else {
			m.dv.VP.GotoBottom()
			if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
				m.syncDiffCursorToViewport()
			}
		}
		return m, nil, true
	case "g":
		if m.dv.WaitingG {
			m.dv.WaitingG = false
			if m.dv.Tree.Focused {
				m.dv.Tree.MoveCursorBy(-2 - len(m.dv.Tree.Entries))
			} else {
				m.dv.VP.GotoTop()
				if m.dv.CurrentFileIdx >= 0 && m.dv.HasDiffLines() {
					m.syncDiffCursorToViewport()
				}
			}
			return m, nil, true
		}
		m.dv.WaitingG = true
		return m, nil, true
	default:
		m.dv.WaitingG = false
	}
	return m, nil, false
}

// StatusHints returns left and right hint groups for the status bar.
func (m Model) KeyBindings() []uictx.KeyBinding {
	return []uictx.KeyBinding{
		{Key: "j / k", Description: "Move cursor down / up", Keywords: []string{"navigate"}},
		{Key: "J / K", Description: "Extend selection range"},
		{Key: "h / l", Description: "Focus left / right pane"},
		{Key: "f", Description: "Toggle tree focus"},
		{Key: "ctrl+j / k", Description: "Previous / next file"},
		{Key: "ctrl+d / u", Description: "Scroll half page down / up"},
		{Key: "ctrl+f / b", Description: "Scroll full page down / up"},
		{Key: "g g", Description: "Go to top"},
		{Key: "G", Description: "Go to bottom"},
		{Key: "m", Description: "Cycle diff mode (working/staged/branch)", Keywords: []string{"toggle"}},
		{Key: "a", Description: "Add comment on current line"},
		{Key: "enter", Description: "Select file / enter comment thread"},
		{Key: "r", Description: "Reply to comment thread"},
		{Key: "x", Description: "Resolve / unresolve thread"},
		{Key: "s", Description: "Stage line/selection (Working mode)"},
		{Key: "u", Description: "Unstage line/selection (Staged mode)"},
		{Key: "S", Description: "Stage entire hunk"},
		{Key: "U", Description: "Unstage entire hunk"},
		{Key: ":N", Description: "Jump to line number N"},
		{Key: "esc", Description: "Cancel / exit thread"},
	}
}

func (m Model) StatusHints() (left, right []string) {
	if m.dv.Composing {
		left = append(left, styles.StatusBarKey.Render("esc")+" "+styles.StatusBarHint.Render("cancel"))
		right = append(right, styles.StatusBarKey.Render("enter")+" "+styles.StatusBarHint.Render("submit"))
		return
	}
	if m.dv.Tree.Focused {
		left = append(left, styles.StatusBarKey.Render("f")+" "+styles.StatusBarHint.Render("unfocus tree"))
	} else {
		left = append(left, styles.StatusBarKey.Render("f")+" "+styles.StatusBarHint.Render("focus tree"))
	}
	left = append(left, styles.StatusBarKey.Render("h/l")+" "+styles.StatusBarHint.Render("panes"))
	left = append(left, styles.StatusBarKey.Render("ctrl+j/k")+" "+styles.StatusBarHint.Render("files"))
	if !m.dv.Tree.Focused && m.dv.CurrentFileIdx >= 0 {
		left = append(left, styles.StatusBarKey.Render("J/K")+" "+styles.StatusBarHint.Render("select range"))
		if m.dv.ThreadCursor > 0 {
			count := m.threadCommentCount()
			left = append(left, styles.StatusBarHint.Render(fmt.Sprintf("comment %d/%d", m.dv.ThreadCursor, count)))
			left = append(left, styles.StatusBarKey.Render("r")+" "+styles.StatusBarHint.Render("reply"))
			left = append(left, styles.StatusBarKey.Render("x")+" "+styles.StatusBarHint.Render("resolve"))
		} else if m.cursorHasThread() {
			left = append(left, styles.StatusBarKey.Render("r")+" "+styles.StatusBarHint.Render("reply"))
			left = append(left, styles.StatusBarKey.Render("x")+" "+styles.StatusBarHint.Render("resolve"))
		}
	}
	modeStr := m.mode.String()
	if m.pr != nil {
		modeStr += fmt.Sprintf(" · PR #%d", m.pr.Number)
	}
	right = append(right, styles.StatusBarKey.Render("m")+" "+styles.StatusBarHint.Render(modeStr))
	return
}

// cursorHasThread returns true if the cursor is on a line with a comment thread.
func (m Model) cursorHasThread() bool {
	path, line, side, ok := m.cursorThreadInfo()
	if !ok {
		return false
	}
	return m.commentStore.FindThreadRoot(path, line, side) != ""
}

// --- View ---

func (m Model) View() string {
	if !m.dv.VPReady {
		return ""
	}

	rightView := m.dv.VP.View()
	if m.dv.CurrentFileIdx >= 0 {
		rightView = m.dv.OverlayDiffCursor(rightView)
	}

	return m.renderLayout(rightView)
}

func (m Model) renderLayout(rightView string) string {
	var rightTitle string
	if m.dv.CurrentFileIdx >= 0 && m.dv.CurrentFileIdx < len(m.dv.Files) {
		rightTitle = m.dv.Files[m.dv.CurrentFileIdx].Filename
	} else {
		rightTitle = "Overview"
	}
	return m.dv.RenderLayout(rightView, rightTitle)
}

// --- Content building ---

func (m *Model) rebuildContent() {
	innerW := m.dv.RightPanelInnerWidth()
	innerH := m.dv.Height - 2

	if !m.dv.VPReady {
		m.dv.VP = viewport.New()
		m.dv.VPReady = true
	}
	m.dv.VP.SetWidth(innerW)
	m.dv.VP.SetHeight(innerH)

	var newContent string
	if m.dv.CurrentFileIdx == -1 {
		newContent = m.buildOverviewContent(innerW)
	} else {
		newContent = m.buildFileContent(innerW)
	}
	m.dv.VP.SetContent(newContent)
}

// rebuildContentIfChanged only updates the viewport if the content actually changed.
// Use this for paths where the content might not have changed (timer ticks, etc.)
func (m *Model) rebuildContentIfChanged() {
	innerW := m.dv.RightPanelInnerWidth()
	innerH := m.dv.Height - 2

	if !m.dv.VPReady {
		m.dv.VP = viewport.New()
		m.dv.VPReady = true
	}
	m.dv.VP.SetWidth(innerW)
	m.dv.VP.SetHeight(innerH)

	var newContent string
	if m.dv.CurrentFileIdx == -1 {
		newContent = m.buildOverviewContent(innerW)
	} else {
		newContent = m.buildFileContent(innerW)
	}
	if newContent != m.dv.LastContent {
		m.dv.LastContent = newContent
		m.dv.VP.SetContent(newContent)
	}
}

func (m Model) buildOverviewContent(w int) string {
	var content strings.Builder

	// Branch + mode info.
	branchStr := lipgloss.NewStyle().Bold(true).Render(m.branch)
	modeStr := dimStyle.Render("(" + m.mode.String() + ")")
	content.WriteString("\n  " + branchStr + " " + modeStr + "\n")

	if len(m.dv.Files) == 0 {
		if m.dv.FilesListLoaded {
			content.WriteString("\n  " + dimStyle.Render("No changes") + "\n")
		} else {
			content.WriteString("\n  " + dimStyle.Render("Loading...") + "\n")
		}
		return content.String()
	}

	// Stats summary.
	adds, dels := git.FilesAddedDeletedStats(m.dv.Files)
	statsStr := fmt.Sprintf("%d files changed", len(m.dv.Files))
	if adds > 0 {
		statsStr += fmt.Sprintf(", %d insertions(+)", adds)
	}
	if dels > 0 {
		statsStr += fmt.Sprintf(", %d deletions(-)", dels)
	}
	content.WriteString("\n  " + dimStyle.Render(statsStr) + "\n")

	// File list.
	content.WriteString("\n")
	for _, f := range m.dv.Files {
		icon := "≈"
		switch f.Status {
		case "added":
			icon = lipgloss.NewStyle().Foreground(lipgloss.Green).Render("+")
		case "removed":
			icon = lipgloss.NewStyle().Foreground(lipgloss.Red).Render("-")
		case "renamed":
			icon = lipgloss.NewStyle().Foreground(lipgloss.Yellow).Render("→")
		default:
			icon = lipgloss.NewStyle().Foreground(lipgloss.Blue).Render("≈")
		}
		content.WriteString("  " + icon + " " + f.Filename + "\n")
	}

	content.WriteString("\n  " + dimStyle.Render("Press m to toggle diff mode") + "\n")
	content.WriteString("\n")

	return content.String()
}

func (m *Model) buildFileContent(w int) string {
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.Files) {
		return ""
	}

	// Use virtualized rendering if layout is available.
	if idx < len(m.fileLayouts) && idx < len(m.dv.HighlightedFiles) &&
		m.fileLayouts[idx].TotalRenderedLines > 0 && m.dv.HighlightedFiles[idx].File.Filename != "" {
		return m.buildVirtualFileContent(idx, w)
	}

	// Fallback: full render or skeleton.
	var content strings.Builder
	if m.dv.RenderedFiles[idx] != "" {
		rendered := m.dv.RenderedFiles[idx]
		if m.dv.Composing && m.dv.HasDiffLines() {
			rendered = m.insertCommentBox(rendered, idx)
		}
		content.WriteString(rendered)
	} else {
		for i := 0; i < 20; i++ {
			gutter := dimStyle.Render(strings.Repeat("─", components.TotalGutterWidth(components.DefaultGutterColWidth)))
			lineW := 15 + (i*7)%25
			if lineW > w-12 {
				lineW = w - 12
			}
			code := dimStyle.Render(strings.Repeat("─", lineW))
			content.WriteString(gutter + " " + code + "\n")
		}
	}
	content.WriteString("\n" + strings.Repeat("\n", m.dv.Height/2))
	return content.String()
}

func (m *Model) buildVirtualFileContent(idx, w int) string {
	// Use cached rendered content. Recomputed by formatFile on comment/width changes.
	rendered := m.dv.RenderedFiles[idx]
	if rendered == "" {
		// Not yet rendered — render now and cache.
		m.renderAndCacheFile(idx, w)
		rendered = m.dv.RenderedFiles[idx]
	}

	if m.dv.Composing && m.dv.HasDiffLines() {
		rendered = m.insertCommentBox(rendered, idx)
	}

	return rendered + "\n" + strings.Repeat("\n", m.dv.Height/2)
}

// spliceThreadForComment re-renders a single comment thread and splices it
// into the cached render. O(thread) instead of O(n) for the whole file.
func (m *Model) spliceThreadForComment(fileIdx int, commentID string) {
	if fileIdx < 0 || fileIdx >= len(m.fileRenderCache) || m.fileRenderCache[fileIdx] == nil {
		// No cache — fall back to full render.
		m.formatFile(fileIdx)
		return
	}
	rc := m.fileRenderCache[fileIdx]
	if len(rc.ThreadRanges) == 0 {
		m.formatFile(fileIdx)
		return
	}

	// Find which thread this comment belongs to.
	// The pending copilot comment is a reply, so find the thread by its anchor line.
	info, ok := m.dv.CopilotPending[commentID]
	if !ok {
		m.formatFile(fileIdx)
		return
	}
	threadIdx := -1
	for i, tr := range rc.ThreadRanges {
		if tr.Side == info.Side && tr.Line == info.Line {
			threadIdx = i
			break
		}
	}
	if threadIdx < 0 {
		// Thread not in cache (new thread since last full render) — full render.
		m.formatFile(fileIdx)
		return
	}

	// Get the diff line type for this thread's anchor.
	diffLineIdx := rc.ThreadRanges[threadIdx].DiffLineIdx
	lt := components.LineAdd
	if diffLineIdx < len(m.dv.FileDiffs[fileIdx]) {
		lt = m.dv.FileDiffs[fileIdx][diffLineIdx].Type
	}

	// Gather thread comments for the affected line.
	fileComments := m.commentsForFile(fileIdx)
	threadComments := components.CommentsForThread(fileComments, info.Side, info.Line)

	gutterW := components.TotalGutterWidth(components.GutterColWidth(m.dv.FileDiffs[fileIdx]))
	newContent := components.RenderSingleThread(threadComments, m.dv.ContentWidth(), lt, m.ctx.DiffColors, false, 0, func(body string, width int, bg string) string {
		return renderMarkdownBody(body, width, bg)
	}, gutterW)

	components.SpliceThread(rc, threadIdx, newContent)
	m.dv.RenderedFiles[fileIdx] = rc.Content
	if fileIdx < len(m.dv.FileDiffOffsets) {
		m.dv.FileDiffOffsets[fileIdx] = rc.DiffLineOffsets
	}
}

// renderAndCacheFile runs FormatDiffFile and caches the result.
func (m *Model) renderAndCacheFile(idx, w int) {
	hl := m.dv.HighlightedFiles[idx]
	colors := m.ctx.DiffColors
	fileComments := m.commentsForFile(idx)

	var opts components.DiffFormatOptions
	opts.RenderBody = func(body string, width int, bg string) string {
		return renderMarkdownBody(body, width, bg)
	}

	result := components.FormatDiffFile(hl, w, colors, fileComments, opts)
	m.dv.RenderedFiles[idx] = result.Content
	if idx < len(m.dv.FileDiffOffsets) {
		m.dv.FileDiffOffsets[idx] = result.DiffLineOffsets
	}
	if idx < len(m.dv.FileCommentPositions) {
		m.dv.FileCommentPositions[idx] = result.CommentPositions
	}
	if idx < len(m.fileRenderCache) {
		m.fileRenderCache[idx] = &result
	}
}

// --- File rendering pipeline ---

func (m Model) highlightFileCmd(index int) tea.Cmd {
	f := m.dv.Files[index]
	repoRoot := m.repoRoot
	chromaStyle := m.ctx.DiffColors.ChromaStyle

	return func() tea.Msg {
		var fileContent string
		if f.Status != "removed" && f.Patch != "" {
			if content, err := git.FileContent(repoRoot, f.Filename); err == nil {
				fileContent = content
			}
		}
		hl := components.HighlightDiffFile(f, fileContent, chromaStyle)
		return fileHighlightedMsg{highlight: hl, index: index}
	}
}

func (m *Model) formatFile(index int) {
	if index >= len(m.dv.HighlightedFiles) {
		return
	}
	// Re-render and cache.
	m.renderAndCacheFile(index, m.dv.ContentWidth())
}

func (m Model) commentsForFile(fileIdx int) []github.ReviewComment {
	if m.commentStore == nil || fileIdx < 0 || fileIdx >= len(m.dv.Files) {
		return nil
	}
	filename := m.dv.Files[fileIdx].Filename
	fileComments := m.commentStore.ForFile(filename)

	// Wrap width for comment bodies inside the thread box.
	var gutterW int
	if fileIdx < len(m.dv.FileDiffs) {
		gutterW = components.TotalGutterWidth(components.GutterColWidth(m.dv.FileDiffs[fileIdx]))
	} else {
		gutterW = components.TotalGutterWidth(components.DefaultGutterColWidth)
	}
	wrapW := m.dv.ContentWidth() - gutterW - 4 // gutter + "│ " + " │"
	if wrapW < 20 {
		wrapW = 20
	}

	// Don't pre-render markdown here — it's done in commentsForFileWithBg
	// at render time when we know the diff line's background color.

	// Add pending copilot replies as temporary comments so they render inline.
	fileComments = m.dv.AppendCopilotPending(filename, fileComments)

	return fileComments
}

// renderMarkdownBody does lightweight inline markdown rendering suitable
// for comment thread boxes. Wraps text to width, applies bold, italic,
// code, and code block formatting. Uses reset+bg instead of bare \033[0m
// so the diff background color survives through formatting resets.
func renderMarkdownBody(body string, width int, bg string) string {
	reset := "\033[0m" + bg

	var out strings.Builder
	inCodeBlock := false

	for _, line := range strings.Split(body, "\n") {
		// Fenced code blocks — don't wrap, just indent.
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
			if inCodeBlock {
				out.WriteString("\033[90m" + bg)
			} else {
				out.WriteString(reset)
			}
			continue
		}
		if inCodeBlock {
			out.WriteString("  " + line + "\n")
			continue
		}

		for _, wrapped := range wordWrap(line, width) {
			out.WriteString(renderInlineMarkdown(wrapped, reset) + "\n")
		}
	}

	if inCodeBlock {
		out.WriteString(reset)
	}

	return strings.TrimRight(out.String(), "\n")
}

// wordWrap splits a line into multiple lines at word boundaries to fit width.
// Uses visible width (not byte length) so ANSI codes don't break wrapping.
func wordWrap(line string, width int) []string {
	if width <= 0 || lipgloss.Width(line) <= width {
		return []string{line}
	}
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := words[0]
	for _, w := range words[1:] {
		// Use len here since we're working with plain text before ANSI is applied.
		if len(cur)+1+len(w) > width {
			lines = append(lines, cur)
			cur = w
		} else {
			cur += " " + w
		}
	}
	lines = append(lines, cur)
	return lines
}

// renderInlineMarkdown handles bold, italic, and inline code.
// reset should be "\033[0m" + bg to preserve the diff background.
func renderInlineMarkdown(line string, reset string) string {
	// Inline code: `code`
	for {
		start := strings.Index(line, "`")
		if start < 0 {
			break
		}
		end := strings.Index(line[start+1:], "`")
		if end < 0 {
			break
		}
		end += start + 1
		code := line[start+1 : end]
		line = line[:start] + "\033[36m" + code + reset + line[end+1:]
	}

	// Bold: **text** or __text__
	line = replaceMarkdownPair(line, "**", "\033[1m", reset)
	line = replaceMarkdownPair(line, "__", "\033[1m", reset)

	// Italic: *text* or _text_
	line = replaceMarkdownPair(line, "*", "\033[3m", reset)

	return line
}

func replaceMarkdownPair(s, delim, open, close string) string {
	for {
		start := strings.Index(s, delim)
		if start < 0 {
			break
		}
		end := strings.Index(s[start+len(delim):], delim)
		if end < 0 {
			break
		}
		end += start + len(delim)
		inner := s[start+len(delim) : end]
		s = s[:start] + open + inner + close + s[end+len(delim):]
	}
	return s
}

// reformatAllFiles invalidates all file layouts so they get recomputed
// on next access. Only the current file is reformatted immediately.
func (m *Model) reformatAllFiles() {
	for i := range m.fileLayouts {
		m.fileLayouts[i] = components.DiffLayout{}
	}
	if m.dv.CurrentFileIdx >= 0 {
		m.formatFile(m.dv.CurrentFileIdx)
	}
}

// needsRenderBufferUpdate returns true if the viewport scrolled outside
// the pre-rendered buffer and needs a fresh render.

func (m *Model) selectTreeEntry() {
	m.dv.SelectionAnchor = -1
	// Save cursor position for the file we're leaving.
	if m.dv.CurrentFileIdx >= 0 && m.dv.CurrentFileIdx < len(m.dv.Files) {
		m.fileCursors[m.dv.Files[m.dv.CurrentFileIdx].Filename] = m.dv.DiffCursor
	}
	m.dv.ThreadCursor = 0
	if m.dv.Tree.Cursor == 0 {
		m.dv.CurrentFileIdx = -1
		m.rebuildContent()
		m.dv.VP.GotoTop()
		m.saveViewState()
		return
	}
	eIdx := m.dv.Tree.Cursor - 2
	if eIdx >= 0 && eIdx < len(m.dv.Tree.Entries) {
		e := m.dv.Tree.Entries[eIdx]
		if !e.IsDir && e.FileIndex >= 0 && e.FileIndex < len(m.dv.Files) && e.FileIndex < len(m.dv.FileDiffs) {
			m.dv.CurrentFileIdx = e.FileIndex
			if saved, ok := m.fileCursors[m.dv.Files[e.FileIndex].Filename]; ok && saved < len(m.dv.FileDiffs[e.FileIndex]) {
				m.dv.DiffCursor = saved
			} else {
				m.dv.DiffCursor = m.dv.FirstNonHunkLine(e.FileIndex)
			}
			// Use cached render if available (instant). Otherwise render now.
			if m.dv.RenderedFiles[e.FileIndex] == "" {
				m.formatFile(e.FileIndex)
			}
			m.rebuildContent()
			m.scrollToDiffCursor()
			m.saveViewState()
		}
	}
}

// --- Diff cursor ---

func (m *Model) moveDiffCursor(delta int) {
	if m.dv.CurrentFileIdx < 0 || m.dv.CurrentFileIdx >= len(m.dv.FileDiffs) {
		return
	}
	m.dv.ThreadCursor = 0
	lines := m.dv.FileDiffs[m.dv.CurrentFileIdx]
	newPos := m.dv.DiffCursor + delta

	for newPos >= 0 && newPos < len(lines) && lines[newPos].Type == components.LineHunk {
		newPos += delta
	}

	if newPos < 0 || newPos >= len(lines) {
		return
	}
	m.dv.DiffCursor = newPos
	m.scrollToDiffCursor()
}

// lineHasComments returns true if the diff line at the given index has a comment thread.
func (m Model) lineHasComments(diffIdx int) bool {
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.FileDiffs) || diffIdx < 0 || diffIdx >= len(m.dv.FileDiffs[idx]) {
		return false
	}
	dl := m.dv.FileDiffs[idx][diffIdx]
	if dl.Type == components.LineHunk {
		return false
	}
	path := m.dv.Files[idx].Filename
	var line int
	var side string
	if dl.Type == components.LineDel {
		line = dl.OldLineNo
		side = "LEFT"
	} else {
		line = dl.NewLineNo
		side = "RIGHT"
	}
	return m.commentStore != nil && m.commentStore.FindThreadRoot(path, line, side) != ""
}

func (m *Model) moveDiffCursorBy(delta int) {
	if m.dv.CurrentFileIdx < 0 || m.dv.CurrentFileIdx >= len(m.dv.FileDiffs) {
		return
	}
	m.dv.ThreadCursor = 0
	lines := m.dv.FileDiffs[m.dv.CurrentFileIdx]
	newPos := m.dv.DiffCursor + delta

	if newPos < 0 {
		newPos = 0
	}
	if newPos >= len(lines) {
		newPos = len(lines) - 1
	}

	if newPos >= 0 && newPos < len(lines) && lines[newPos].Type == components.LineHunk {
		dir := 1
		if delta < 0 {
			dir = -1
		}
		found := false
		for p := newPos + dir; p >= 0 && p < len(lines); p += dir {
			if lines[p].Type != components.LineHunk {
				newPos = p
				found = true
				break
			}
		}
		if !found {
			for p := newPos - dir; p >= 0 && p < len(lines); p -= dir {
				if lines[p].Type != components.LineHunk {
					newPos = p
					found = true
					break
				}
			}
		}
		if !found {
			return
		}
	}

	m.dv.DiffCursor = newPos
	m.scrollToDiffCursor()
}

// goToSourceLine jumps the diff cursor to the line closest to the given
// source line number (new side preferred, falls back to old side).
func (m *Model) goToSourceLine(lineNo int) {
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.FileDiffs) {
		return
	}
	best := m.findDiffLineBySourceLine(idx, lineNo, "RIGHT")
	m.dv.DiffCursor = best
	m.dv.SelectionAnchor = -1
	m.formatFile(idx)
	m.rebuildContent()
	m.scrollToDiffCursor()
}

// threadCommentCount returns the number of comments in the thread on the
// current cursor line, consistent with what's actually rendered.
func (m Model) threadCommentCount() int {
	path, line, side, ok := m.cursorThreadInfo()
	if !ok {
		return 0
	}
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.FileCommentPositions) {
		return 0
	}
	// Count from the rendered comment positions — this is the source of truth.
	count := 0
	for _, cp := range m.dv.FileCommentPositions[idx] {
		if cp.Line == line && cp.Side == side {
			count++
		}
	}
	_ = path
	return count
}

// scrollToThreadCursor scrolls the viewport to show the selected comment
// using the exact rendered positions tracked by CommentPositions.
func (m *Model) scrollToThreadCursor() {
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.FileCommentPositions) {
		return
	}

	// Find the comment position matching the current cursor line and threadCursor.
	path, line, side, ok := m.cursorThreadInfo()
	if !ok {
		return
	}

	targetLine := -1
	for _, cp := range m.dv.FileCommentPositions[idx] {
		if cp.Line == line && cp.Side == side && cp.Idx == m.dv.ThreadCursor-1 {
			targetLine = cp.Offset
			break
		}
	}
	if targetLine < 0 {
		_ = path // suppress unused
		return
	}

	vpH := m.dv.ViewportHeight()
	top := m.dv.VP.YOffset()
	bottom := top + vpH - 1

	if targetLine < top+scrollMargin {
		target := targetLine - scrollMargin
		if target < 0 {
			target = 0
		}
		m.dv.VP.SetYOffset(target)
	} else if targetLine > bottom-scrollMargin {
		m.dv.VP.SetYOffset(targetLine - vpH + scrollMargin + 1)
	}
}

// currentCommentRange returns the start and end rendered line offsets for
// the currently selected comment (threadCursor). Returns (-1,-1) if unknown.
func (m Model) currentCommentRange() (start, end int) {
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.FileCommentPositions) {
		return -1, -1
	}
	_, line, side, ok := m.cursorThreadInfo()
	if !ok {
		return -1, -1
	}

	// Find all positions for this thread.
	var threadPositions []components.CommentPosition
	for _, cp := range m.dv.FileCommentPositions[idx] {
		if cp.Line == line && cp.Side == side {
			threadPositions = append(threadPositions, cp)
		}
	}

	ci := m.dv.ThreadCursor - 1
	if ci < 0 || ci >= len(threadPositions) {
		return -1, -1
	}

	start = threadPositions[ci].Offset
	// End is the next comment's start, or estimate from the next diff line.
	if ci+1 < len(threadPositions) {
		end = threadPositions[ci+1].Offset - 1
	} else {
		// Last comment in thread — find where the thread ends.
		// Use the next diff line's offset as the boundary.
		if m.dv.DiffCursor+1 < len(m.dv.FileDiffOffsets[idx]) {
			end = m.dv.FileDiffOffsets[idx][m.dv.DiffCursor+1] - 1
		} else {
			// Last diff line — estimate generously.
			end = start + 50
		}
	}
	return start, end
}

// commentExtendsBelow returns true if the selected comment's body extends
// below the viewport (needs scrolling down to see the rest).
func (m Model) commentExtendsBelow() bool {
	_, end := m.currentCommentRange()
	if end < 0 {
		return false
	}
	vpH := m.dv.ViewportHeight()
	bottom := m.dv.VP.YOffset() + vpH - 1
	return end > bottom
}

// commentExtendsAbove returns true if the selected comment's header is above
// the viewport (needs scrolling up to see the top).
func (m Model) commentExtendsAbove() bool {
	start, _ := m.currentCommentRange()
	if start < 0 {
		return false
	}
	return start < m.dv.VP.YOffset()
}

// scrollToThreadCursorBottom scrolls so the bottom of the comment is visible
// (used when navigating up into a long comment).
func (m *Model) scrollToThreadCursorBottom() {
	_, end := m.currentCommentRange()
	if end < 0 {
		m.scrollToThreadCursor()
		return
	}
	vpH := m.dv.ViewportHeight()
	bottom := m.dv.VP.YOffset() + vpH - 1
	if end > bottom {
		m.dv.VP.SetYOffset(end - vpH + scrollMargin + 1)
	}
	// Also make sure the header is visible if the comment fits.
	start, _ := m.currentCommentRange()
	if start >= 0 && start < m.dv.VP.YOffset() {
		commentH := end - start + 1
		if commentH <= vpH-scrollMargin*2 {
			m.scrollToThreadCursor()
		}
	}
}

// scrollCommentBoxIntoView scrolls the viewport so the comment input box
// (which is inserted after the cursor line) is fully visible.
func (m *Model) scrollCommentBoxIntoView() {
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.FileDiffOffsets) || m.dv.DiffCursor >= len(m.dv.FileDiffOffsets[idx]) {
		return
	}
	vpH := m.dv.ViewportHeight()
	cursorLine := m.dv.FileDiffOffsets[idx][m.dv.DiffCursor]
	// The comment box is ~8 lines (border + textarea + hints) inserted after the cursor line.
	boxBottom := cursorLine + 10
	bottom := m.dv.VP.YOffset() + vpH - 1
	if boxBottom > bottom {
		m.dv.VP.SetYOffset(boxBottom - vpH + 1)
	}
}

const scrollMargin = 5

func (m *Model) scrollToDiffCursor() {
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.FileDiffOffsets) {
		return
	}
	if m.dv.DiffCursor >= len(m.dv.FileDiffOffsets[idx]) {
		return
	}
	vpH := m.dv.ViewportHeight()
	absLine := m.dv.FileDiffOffsets[idx][m.dv.DiffCursor]
	top := m.dv.VP.YOffset()
	bottom := top + vpH - 1

	if absLine < top+scrollMargin {
		target := absLine - scrollMargin
		if target < 0 {
			target = 0
		}
		m.dv.VP.SetYOffset(target)
	} else if absLine > bottom-scrollMargin {
		m.dv.VP.SetYOffset(absLine - vpH + scrollMargin + 1)
	}
}

// scrollAndSyncCursor scrolls the viewport by delta lines, then moves
// the diff cursor to the diff line at the same screen-relative position.
// This keeps the cursor visually stable (vim ctrl+d/u behavior).
func (m *Model) scrollAndSyncCursor(delta int) {
	m.dv.ThreadCursor = 0
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.FileDiffOffsets) {
		return
	}

	prevOffset := m.dv.VP.YOffset()

	// Remember cursor's screen-relative position.
	cursorAbs := 0
	if m.dv.DiffCursor < len(m.dv.FileDiffOffsets[idx]) {
		cursorAbs = m.dv.FileDiffOffsets[idx][m.dv.DiffCursor]
	}
	relPos := cursorAbs - prevOffset

	// Scroll viewport.
	m.dv.VP.SetYOffset(prevOffset + delta)

	// Skip if viewport didn't actually move.
	if m.dv.VP.YOffset() == prevOffset {
		return
	}

	// Binary search for the closest non-hunk diff line to the target position.
	targetAbs := m.dv.VP.YOffset() + relPos
	offsets := m.dv.FileDiffOffsets[idx]
	diffs := m.dv.FileDiffs[idx]

	// Binary search: find the first offset >= targetAbs.
	lo, hi := 0, len(offsets)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if offsets[mid] < targetAbs {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	// Check lo and neighbors for closest non-hunk line.
	best := -1
	bestDist := 0
	for _, i := range []int{lo - 1, lo, lo + 1} {
		if i < 0 || i >= len(offsets) {
			continue
		}
		if i < len(diffs) && diffs[i].Type == components.LineHunk {
			continue
		}
		dist := offsets[i] - targetAbs
		if dist < 0 {
			dist = -dist
		}
		if best == -1 || dist < bestDist {
			best = i
			bestDist = dist
		}
	}
	if best >= 0 {
		m.dv.DiffCursor = best
	}
}

// findClosestDiffLine binary-searches for the non-hunk diff line closest to targetOffset.
func findClosestDiffLine(offsets []int, diffs []components.DiffLine, targetOffset int) int {
	if len(offsets) == 0 {
		return -1
	}
	lo, hi := 0, len(offsets)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if offsets[mid] < targetOffset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	best := -1
	bestDist := 0
	for _, i := range []int{lo - 1, lo, lo + 1} {
		if i < 0 || i >= len(offsets) {
			continue
		}
		if i < len(diffs) && diffs[i].Type == components.LineHunk {
			continue
		}
		dist := offsets[i] - targetOffset
		if dist < 0 {
			dist = -dist
		}
		if best == -1 || dist < bestDist {
			best = i
			bestDist = dist
		}
	}
	return best
}

func (m *Model) syncDiffCursorToViewport() {
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.FileDiffOffsets) || len(m.dv.FileDiffOffsets[idx]) == 0 {
		return
	}
	target := m.dv.VP.YOffset() + m.dv.ViewportHeight()/2
	best := findClosestDiffLine(m.dv.FileDiffOffsets[idx], m.dv.FileDiffs[idx], target)
	if best >= 0 {
		m.dv.DiffCursor = best
	}
}

// --- Comment composition ---

func (m Model) handleCommentKey(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		m.dv.Composing = false
		m.dv.SelectionAnchor = -1
		m.rebuildContent()
		return m, nil, true
	case "shift+enter":
		// Insert newline.
		m.dv.CommentInput.InsertString("\n")
		m.rebuildContent()
		return m, nil, true
	case "enter":
		body := strings.TrimSpace(m.dv.CommentInput.Value())
		if body == "" {
			m.dv.Composing = false
			m.dv.SelectionAnchor = -1
			m.rebuildContent()
			return m, nil, true
		}
		m.dv.Composing = false

		comment := comments.LocalComment{
			ID:        uuid.New().String(),
			Body:      body,
			Path:      m.dv.CommentFile,
			Line:      m.dv.CommentLine,
			Side:      m.dv.CommentSide,
			StartLine: m.dv.CommentStartLine,
			StartSide: m.dv.CommentStartSide,
			Author:    m.dv.AuthorName(),
			CreatedAt: time.Now(),
		}
		if m.replyToID != "" {
			comment.InReplyToID = m.replyToID
		}

		m.commentStore.Add(comment)
		m.dv.SelectionAnchor = -1

		// Set copilot pending state BEFORE rebuild so "Thinking..." shows immediately.
		if m.dv.Copilot != nil {
			m.dv.SetCopilotPending(comment.ID, comment.Path, comment.Line, comment.Side)
			m.dv.CopilotDots = 0
		}

		m.formatFile(m.dv.CurrentFileIdx)
		m.rebuildContent()

		if m.dv.Copilot != nil {
			// Gather copilot context before returning — these are cheap in-memory lookups.
			diffHunk := m.getDiffHunkForComment(comment)
			threadHistory := m.getThreadHistory(comment)
			fileContent, _ := git.FileContent(m.repoRoot, comment.Path)
			fullDiff := m.getFullFileDiff(comment.Path)
			return m, tea.Batch(
				m.dv.Copilot.SendComment(comment.ID, body, comment.Path, fileContent, fullDiff, diffHunk, threadHistory),
				tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return copilotTickMsg{} }),
			), true
		}
		return m, nil, true
	}

	// Delegate to textarea.
	var cmd tea.Cmd
	m.dv.CommentInput, cmd = m.dv.CommentInput.Update(msg)
	m.rebuildContent()
	return m, cmd, true
}

func (m Model) openCommentInput() (Model, tea.Cmd, bool) {
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.FileDiffs) || idx >= len(m.dv.Files) {
		return m, nil, false
	}
	lines := m.dv.FileDiffs[idx]
	if m.dv.DiffCursor >= len(lines) {
		return m, nil, false
	}
	dl := lines[m.dv.DiffCursor]
	if dl.Type == components.LineHunk {
		return m, nil, false
	}

	m.dv.CommentFile = m.dv.Files[idx].Filename
	m.dv.CommentStartLine = 0
	m.dv.CommentStartSide = ""

	if m.dv.SelectionAnchor >= 0 && m.dv.SelectionAnchor != m.dv.DiffCursor {
		selStart, selEnd := m.dv.SelectionAnchor, m.dv.DiffCursor
		if selStart > selEnd {
			selStart, selEnd = selEnd, selStart
		}
		startDL := lines[selStart]
		endDL := lines[selEnd]
		if startDL.Type == components.LineHunk || endDL.Type == components.LineHunk {
			return m, nil, false
		}
		if endDL.Type == components.LineDel {
			m.dv.CommentLine = endDL.OldLineNo
			m.dv.CommentSide = "LEFT"
		} else {
			m.dv.CommentLine = endDL.NewLineNo
			m.dv.CommentSide = "RIGHT"
		}
		if startDL.Type == components.LineDel {
			m.dv.CommentStartLine = startDL.OldLineNo
			m.dv.CommentStartSide = "LEFT"
		} else {
			m.dv.CommentStartLine = startDL.NewLineNo
			m.dv.CommentStartSide = "RIGHT"
		}
	} else {
		if dl.Type == components.LineDel {
			m.dv.CommentLine = dl.OldLineNo
			m.dv.CommentSide = "LEFT"
		} else {
			m.dv.CommentLine = dl.NewLineNo
			m.dv.CommentSide = "RIGHT"
		}
	}

	// Check for existing thread to reply to.
	if m.dv.CommentStartLine > 0 {
		m.replyToID = ""
	} else {
		m.replyToID = m.commentStore.FindThreadRoot(m.dv.CommentFile, m.dv.CommentLine, m.dv.CommentSide)
	}

	ta := textarea.New()
	ta.SetWidth(m.dv.ContentWidth() - 10 - 6)
	ta.SetHeight(5)
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.Focus()
	if m.replyToID != "" {
		ta.Placeholder = "Reply to thread..."
	} else {
		ta.Placeholder = "Add a comment..."
	}
	m.dv.CommentInput = ta
	m.dv.Composing = true
	m.rebuildContent()
	m.scrollCommentBoxIntoView()
	return m, ta.Focus(), true
}

func (m Model) insertCommentBox(rendered string, fileIdx int) string {
	lines := strings.Split(rendered, "\n")
	cursorRenderedLine := -1
	if fileIdx < len(m.dv.FileDiffOffsets) && m.dv.DiffCursor < len(m.dv.FileDiffOffsets[fileIdx]) {
		cursorRenderedLine = m.dv.FileDiffOffsets[fileIdx][m.dv.DiffCursor]
	}
	if cursorRenderedLine < 0 || cursorRenderedLine >= len(lines) {
		return rendered
	}

	// When replying to a thread, find the end of the existing thread block
	// (the last line with a ╰ border character) so we insert right before
	// the thread's closing border.
	insertAt := cursorRenderedLine
	if m.replyToID != "" {
		// Scan forward from cursor line to find the thread's bottom border (╰).
		for i := cursorRenderedLine + 1; i < len(lines); i++ {
			if strings.Contains(lines[i], "╰") {
				insertAt = i
				break
			}
			// Stop if we hit another diff line (no gutter indent = not a comment).
			if i > cursorRenderedLine+200 {
				break
			}
		}
	}

	inputBox := m.renderCommentBox()
	inputLines := strings.Split(inputBox, "\n")
	after := make([]string, len(lines)-insertAt-1)
	copy(after, lines[insertAt+1:])
	lines = append(lines[:insertAt+1], inputLines...)
	lines = append(lines, after...)
	return strings.Join(lines, "\n")
}

func (m Model) renderCommentBox() string {
	gutter := components.TotalGutterWidth(components.GutterColWidth(m.dv.FileDiffs[m.dv.CurrentFileIdx]))
	indent := strings.Repeat(" ", gutter)
	boxW := m.dv.ContentWidth() - gutter - 2

	taView := m.dv.CommentInput.View()

	isReply := m.replyToID != ""
	bc := m.dv.BorderStyle()
	// Use highlighted border color for the reply box.
	hlStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.ctx.DiffColors.HighlightBorderFg))
	borderRender := bc.Render
	if isReply {
		borderRender = hlStyle.Render
	}

	var topLeft, bottomLeft string
	if isReply {
		topLeft = "├" // connects to thread above
		bottomLeft = "╰"
	} else {
		topLeft = "╭"
		bottomLeft = "╰"
	}

	topRule := borderRender(topLeft + strings.Repeat("─", boxW) + "╮")
	bottomRule := borderRender(bottomLeft + strings.Repeat("─", boxW) + "╯")
	side := borderRender("│")

	var boxLines []string

	// Label for reply.
	if isReply {
		label := dimStyle.Render(" replying...")
		fillW := boxW - lipgloss.Width(label)
		if fillW < 0 {
			fillW = 0
		}
		boxLines = append(boxLines, indent+topRule)
		boxLines = append(boxLines, indent+side+label+strings.Repeat(" ", fillW)+side)
	} else {
		boxLines = append(boxLines, indent+topRule)
	}

	for _, line := range strings.Split(taView, "\n") {
		visW := lipgloss.Width(line)
		padW := boxW - 2 - visW
		if padW < 0 {
			padW = 0
		}
		boxLines = append(boxLines, indent+side+" "+line+strings.Repeat(" ", padW)+" "+side)
	}
	boxLines = append(boxLines, indent+bottomRule)

	colors := m.ctx.DiffColors
	cancelBtn := lipgloss.NewStyle().
		Foreground(colors.PaletteDim).
		Padding(0, 1).
		Render("Cancel")
	submitBtn := lipgloss.NewStyle().
		Background(colors.PaletteGreen).
		Foreground(colors.PaletteBg).
		Bold(true).
		Padding(0, 1).
		Render("Submit")

	buttons := cancelBtn + " " + submitBtn
	hintGap := boxW - lipgloss.Width(buttons)
	if hintGap < 1 {
		hintGap = 1
	}
	boxLines = append(boxLines, indent+" "+strings.Repeat(" ", hintGap)+buttons)

	return strings.Join(boxLines, "\n")
}

// cursorThreadInfo returns the path/line/side for the comment thread at the cursor.
func (m Model) cursorThreadInfo() (path string, line int, side string, ok bool) {
	idx := m.dv.CurrentFileIdx
	if idx >= len(m.dv.FileDiffs) || m.dv.DiffCursor >= len(m.dv.FileDiffs[idx]) {
		return
	}
	dl := m.dv.FileDiffs[idx][m.dv.DiffCursor]
	if dl.Type == components.LineHunk {
		return
	}
	path = m.dv.Files[idx].Filename
	if dl.Type == components.LineDel {
		line = dl.OldLineNo
		side = "LEFT"
	} else {
		line = dl.NewLineNo
		side = "RIGHT"
	}
	ok = true
	return
}

// replyToThreadAtCursor opens a reply input for the thread at the cursor.
// Returns a tea.Cmd if a thread was found, nil otherwise.
func (m *Model) replyToThreadAtCursor() tea.Cmd {
	path, line, side, ok := m.cursorThreadInfo()
	if !ok {
		return nil
	}
	rootID := m.commentStore.FindThreadRoot(path, line, side)
	if rootID == "" {
		return nil
	}
	m.replyToID = rootID
	m.dv.CommentFile = path
	m.dv.CommentLine = line
	m.dv.CommentSide = side
	m.dv.CommentStartLine = 0
	m.dv.CommentStartSide = ""
	ta := textarea.New()
	ta.SetWidth(m.dv.ContentWidth() - 10 - 6)
	ta.SetHeight(5)
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.Focus()
	ta.Placeholder = "Reply to thread..."
	m.dv.CommentInput = ta
	m.dv.Composing = true
	m.rebuildContent()
	// Scroll to ensure the comment box is visible.
	m.scrollCommentBoxIntoView()
	return ta.Focus()
}

// toggleResolveAtCursor resolves/unresolves the thread at the cursor.
// Returns true if a thread was found.
func (m *Model) toggleResolveAtCursor() bool {
	path, line, side, ok := m.cursorThreadInfo()
	if !ok {
		return false
	}
	rootID := m.commentStore.FindThreadRoot(path, line, side)
	if rootID == "" {
		return false
	}
	for _, c := range m.commentStore.Comments {
		if c.ID == rootID {
			m.commentStore.Resolve(rootID, !c.Resolved)
			break
		}
	}
	m.formatFile(m.dv.CurrentFileIdx)
	m.rebuildContent()
	return true
}

// getDiffHunkForComment extracts the diff hunk around the commented line.
func (m Model) getDiffHunkForComment(c comments.LocalComment) string {
	// Find the file index.
	fileIdx := -1
	for i, f := range m.dv.Files {
		if f.Filename == c.Path {
			fileIdx = i
			break
		}
	}
	if fileIdx < 0 || fileIdx >= len(m.dv.FileDiffs) {
		return ""
	}

	lines := m.dv.FileDiffs[fileIdx]
	// Find the diff line that matches the comment.
	targetIdx := -1
	for i, dl := range lines {
		if c.Side == "LEFT" && dl.OldLineNo == c.Line {
			targetIdx = i
			break
		}
		if c.Side == "RIGHT" && dl.NewLineNo == c.Line {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return ""
	}

	// Extract surrounding context (up to 10 lines each direction).
	start := targetIdx - 10
	if start < 0 {
		start = 0
	}
	end := targetIdx + 10
	if end >= len(lines) {
		end = len(lines) - 1
	}

	var b strings.Builder
	for i := start; i <= end; i++ {
		dl := lines[i]
		switch dl.Type {
		case components.LineHunk:
			b.WriteString(dl.Content + "\n")
		case components.LineAdd:
			b.WriteString("+" + dl.Content + "\n")
		case components.LineDel:
			b.WriteString("-" + dl.Content + "\n")
		default:
			b.WriteString(" " + dl.Content + "\n")
		}
	}
	return b.String()
}

// getThreadHistory returns the bodies of all comments in a thread for context.
func (m Model) getThreadHistory(c comments.LocalComment) []string {
	if c.InReplyToID == "" {
		return nil
	}
	var history []string
	for _, existing := range m.commentStore.Comments {
		if existing.ID == c.InReplyToID || existing.InReplyToID == c.InReplyToID {
			if existing.ID != c.ID { // don't include the current comment
				prefix := existing.Author + ": "
				history = append(history, prefix+existing.Body)
			}
		}
	}
	return history
}

// getFullFileDiff returns the complete patch for a file.
// fileIndexForPath returns the index of the file with the given path, or -1.
func (m Model) buildViewPickerItems() []picker.Item {
	items := []picker.Item{
		{
			Label:       "Working Tree",
			Description: "Uncommitted changes vs HEAD",
			Value:       "working",
			Keywords:    []string{"local", "unstaged"},
		},
		{
			Label:       "Staged",
			Description: "Staged changes (git add)",
			Value:       "staged",
			Keywords:    []string{"cached", "index"},
		},
	}

	defaultBranch, _ := git.DefaultBranch(m.repoRoot)
	if m.branch != defaultBranch {
		items = append(items, picker.Item{
			Label:       "Branch Diff",
			Description: "vs " + defaultBranch,
			Value:       "branch",
			Keywords:    []string{"compare", "base"},
		})
	}

	if m.pr != nil {
		items = append(items, picker.Item{
			Label:       fmt.Sprintf("PR #%d", m.pr.Number),
			Description: m.pr.Title,
			Value:       "pr",
			Keywords:    []string{"pull request", "review"},
		})
	}

	return items
}

func (m Model) fileIndexForPath(path string) int {
	if idx, ok := m.filePathIndex[path]; ok {
		return idx
	}
	return -1
}

func (m *Model) rebuildFilePathIndex() {
	m.filePathIndex = make(map[string]int, len(m.dv.Files))
	for i, f := range m.dv.Files {
		m.filePathIndex[f.Filename] = i
	}
}

type stageDoneMsg struct{}

// stageWholeFile stages an entire file using the appropriate git command.
func (m Model) stageWholeFile(unstage bool) (Model, tea.Cmd, bool) {
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.Files) || idx >= len(m.dv.FileDiffs) {
		return m, nil, false
	}
	filename := m.dv.Files[idx].Filename
	fileStatus := m.dv.Files[idx].Status
	repoRoot := m.repoRoot

	// Optimistically remove the file from the view.
	m.removeDiffLines(idx, 0, len(m.dv.FileDiffs[idx])-1)
	m.stagingInFlight++

	return m, func() tea.Msg {
		if unstage {
			exec.Command("git", "-C", repoRoot, "reset", "HEAD", "--", filename).Run()
		} else if fileStatus == "removed" {
			// Stage a deletion.
			exec.Command("git", "-C", repoRoot, "rm", "--cached", "--", filename).Run()
		} else {
			exec.Command("git", "-C", repoRoot, "add", "--", filename).Run()
		}
		return stageDoneMsg{}
	}, true
}

// stageSelection stages or unstages the current line or J/K selection range.
func (m Model) stageSelection(unstage bool) (Model, tea.Cmd, bool) {
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.FileDiffs) || idx >= len(m.dv.Files) {
		return m, nil, false
	}

	lines := m.dv.FileDiffs[idx]
	filename := m.dv.Files[idx].Filename
	fileStatus := m.dv.Files[idx].Status
	patch := m.dv.Files[idx].Patch
	repoRoot := m.repoRoot

	// Determine selection range.
	selStart, selEnd := m.dv.DiffCursor, m.dv.DiffCursor
	if m.dv.SelectionAnchor >= 0 && m.dv.SelectionAnchor != m.dv.DiffCursor {
		selStart, selEnd = m.dv.SelectionAnchor, m.dv.DiffCursor
		if selStart > selEnd {
			selStart, selEnd = selEnd, selStart
		}
	}

	// Collect line numbers to stage.
	var newLineNos, oldLineNos []int
	for i := selStart; i <= selEnd; i++ {
		if i >= len(lines) {
			continue
		}
		dl := lines[i]
		switch dl.Type {
		case components.LineAdd:
			newLineNos = append(newLineNos, dl.NewLineNo)
		case components.LineDel:
			oldLineNos = append(oldLineNos, dl.OldLineNo)
		}
	}

	if len(newLineNos) == 0 && len(oldLineNos) == 0 {
		return m, nil, true
	}

	m.dv.SelectionAnchor = -1

	// Optimistically remove staged lines from the current diff view.
	m.removeDiffLines(idx, selStart, selEnd)
	m.stagingInFlight++

	return m, func() tea.Msg {
		git.StageLines(repoRoot, filename, fileStatus, patch, newLineNos, oldLineNos, unstage)
		return stageDoneMsg{}
	}, true
}

// stageHunk stages or unstages the entire hunk the cursor is in.
func (m Model) stageHunk(unstage bool) (Model, tea.Cmd, bool) {
	idx := m.dv.CurrentFileIdx
	if idx < 0 || idx >= len(m.dv.FileDiffs) || idx >= len(m.dv.Files) {
		return m, nil, false
	}

	lines := m.dv.FileDiffs[idx]
	if m.dv.DiffCursor >= len(lines) {
		return m, nil, false
	}

	dl := lines[m.dv.DiffCursor]
	if dl.Type == components.LineHunk {
		return m, nil, false
	}

	filename := m.dv.Files[idx].Filename
	fileStatus := m.dv.Files[idx].Status
	patch := m.dv.Files[idx].Patch
	repoRoot := m.repoRoot

	var lineNo int
	var side string
	if dl.Type == components.LineDel {
		lineNo = dl.OldLineNo
		side = "LEFT"
	} else {
		lineNo = dl.NewLineNo
		side = "RIGHT"
	}

	// Optimistically remove the entire hunk from the view.
	hunkStart, hunkEnd := m.findHunkRange(idx, m.dv.DiffCursor)
	m.removeDiffLines(idx, hunkStart, hunkEnd)
	m.stagingInFlight++

	return m, func() tea.Msg {
		git.StageHunk(repoRoot, filename, fileStatus, patch, lineNo, side, unstage)
		return stageDoneMsg{}
	}, true
}

// removeDiffLines removes diff lines from the view optimistically.
// Additions are removed entirely; deletions become context lines.
// If no diff lines remain, the file is removed from the file list.
func (m *Model) removeDiffLines(fileIdx, start, end int) {
	if fileIdx >= len(m.dv.FileDiffs) {
		return
	}
	lines := m.dv.FileDiffs[fileIdx]
	var newLines []components.DiffLine
	for i, dl := range lines {
		if i >= start && i <= end {
			if dl.Type == components.LineAdd || dl.Type == components.LineDel {
				// Remove staged lines from the view entirely.
				continue
			}
		}
		newLines = append(newLines, dl)
	}

	// Check if any actual changes remain in this file.
	hasChanges := false
	for _, dl := range newLines {
		if dl.Type == components.LineAdd || dl.Type == components.LineDel {
			hasChanges = true
			break
		}
	}

	if !hasChanges {
		// Remove the file entirely from the view.
		m.dv.Files = append(m.dv.Files[:fileIdx], m.dv.Files[fileIdx+1:]...)
		m.dv.FileDiffs = append(m.dv.FileDiffs[:fileIdx], m.dv.FileDiffs[fileIdx+1:]...)
		m.dv.HighlightedFiles = append(m.dv.HighlightedFiles[:fileIdx], m.dv.HighlightedFiles[fileIdx+1:]...)
		m.dv.RenderedFiles = append(m.dv.RenderedFiles[:fileIdx], m.dv.RenderedFiles[fileIdx+1:]...)
		m.dv.FileDiffOffsets = append(m.dv.FileDiffOffsets[:fileIdx], m.dv.FileDiffOffsets[fileIdx+1:]...)
		m.dv.FileCommentPositions = append(m.dv.FileCommentPositions[:fileIdx], m.dv.FileCommentPositions[fileIdx+1:]...)
		m.dv.Tree.Files = m.dv.Files
		m.dv.Tree.Entries = components.BuildFileTree(m.dv.Files)
		m.dv.SelectionAnchor = -1
		m.rebuildFilePathIndex()

		// Navigate away from the removed file.
		if len(m.dv.Files) == 0 {
			m.dv.CurrentFileIdx = -1
			m.dv.Tree.Cursor = 0
			m.dv.DiffCursor = 0
		} else if m.dv.CurrentFileIdx >= len(m.dv.Files) {
			m.dv.CurrentFileIdx = len(m.dv.Files) - 1
			m.dv.Tree.Cursor = m.dv.Tree.IndexForFile(m.dv.CurrentFileIdx)
			m.dv.DiffCursor = m.dv.FirstNonHunkLine(m.dv.CurrentFileIdx)
			m.formatFile(m.dv.CurrentFileIdx)
		} else {
			m.dv.Tree.Cursor = m.dv.Tree.IndexForFile(m.dv.CurrentFileIdx)
			m.dv.DiffCursor = m.dv.FirstNonHunkLine(m.dv.CurrentFileIdx)
			m.formatFile(m.dv.CurrentFileIdx)
		}
		m.rebuildContent()
		return
	}

	m.dv.FileDiffs[fileIdx] = newLines

	// Clamp cursor and skip hunk lines.
	if m.dv.DiffCursor >= len(newLines) {
		m.dv.DiffCursor = len(newLines) - 1
	}
	if m.dv.DiffCursor < 0 {
		m.dv.DiffCursor = 0
	}
	if len(newLines) > 0 && newLines[m.dv.DiffCursor].Type == components.LineHunk {
		m.dv.DiffCursor = m.dv.FirstNonHunkLine(fileIdx)
	}

	// Re-format to get correct rendered content and offsets.
	m.formatFile(fileIdx)
	m.rebuildContent()
}

// findHunkRange returns the start and end diff line indices for the hunk
// containing the given line index.
func (m Model) findHunkRange(fileIdx, lineIdx int) (start, end int) {
	lines := m.dv.FileDiffs[fileIdx]

	// Find hunk start — scan backward for the @@ header.
	start = lineIdx
	for start > 0 && lines[start].Type != components.LineHunk {
		start--
	}
	// Skip the hunk header itself.
	if lines[start].Type == components.LineHunk {
		start++
	}

	// Find hunk end — scan forward to next @@ or end of file.
	end = lineIdx
	for end < len(lines)-1 {
		if lines[end+1].Type == components.LineHunk {
			break
		}
		end++
	}

	return start, end
}

func (m Model) getFullFileDiff(path string) string {
	for _, f := range m.dv.Files {
		if f.Filename == path {
			return f.Patch
		}
	}
	return ""
}

