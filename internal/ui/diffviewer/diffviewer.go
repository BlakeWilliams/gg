package diffviewer

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/review/comments"
	"github.com/blakewilliams/ghq/internal/review/copilot"
	"github.com/blakewilliams/ghq/internal/ui/components"
	"github.com/blakewilliams/ghq/internal/ui/styles"
	"github.com/blakewilliams/ghq/internal/ui/uictx"
)

const scrollMargin = 5

// CopilotPendingInfo tracks a single pending Copilot reply.
type CopilotPendingInfo struct {
	Path string
	Line int
	Side string
}

// DiffViewer holds the shared state for a file-tree + diff-viewport layout.
// Both localdiff and prdetail embed this struct.
type DiffViewer struct {
	Ctx    *uictx.Context
	Width  int
	Height int

	// Viewport
	VP      viewport.Model
	VPReady bool

	// Files
	Files                []github.PullRequestFile
	HighlightedFiles     []components.HighlightedDiff
	RenderedFiles        []string
	FileDiffs            [][]components.DiffLine
	FileDiffOffsets      [][]int
	FileCommentPositions [][]components.CommentPosition
	FilesHighlighted     int
	FilesLoading         bool
	FilesListLoaded      bool
	CurrentFileIdx       int // -1 = overview

	// File tree
	Tree components.FileTree

	// Diff cursor
	DiffCursor      int
	SelectionAnchor int
	ThreadCursor    int

	// Comment composing
	Composing        bool
	CommentInput     textarea.Model
	CommentFile      string
	CommentLine      int
	CommentSide      string
	CommentStartLine int
	CommentStartSide string

	// Copilot state
	Copilot         *copilot.Client
	CopilotReplyBuf map[string]string        // commentID -> accumulated reply content
	CopilotPending  map[string]CopilotPendingInfo // commentID -> pending info
	CopilotDots     int                      // shared animation frame (0-3)

	// Internal
	WaitingG    bool
	LastContent string
}

// --- Layout helpers ---

func (d DiffViewer) RightPanelWidth() int {
	return d.Width - d.Tree.Width
}

func (d DiffViewer) RightPanelInnerWidth() int {
	return d.RightPanelWidth() - 2
}

func (d DiffViewer) ContentWidth() int {
	return d.RightPanelInnerWidth()
}

func (d DiffViewer) ViewportHeight() int {
	return d.Height - 2
}

func (d DiffViewer) BorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(d.Ctx.DiffColors.BorderColor)
}

func (d DiffViewer) AuthorName() string {
	if d.Ctx.Username != "" {
		return d.Ctx.Username
	}
	return "you"
}

// --- Diff cursor ---

func (d DiffViewer) HasDiffLines() bool {
	idx := d.CurrentFileIdx
	return idx >= 0 && idx < len(d.FileDiffs) && len(d.FileDiffs[idx]) > 0
}

// MoveDiffCursor moves the cursor by delta, skipping hunk lines.
func (d *DiffViewer) MoveDiffCursor(delta int) {
	if d.CurrentFileIdx < 0 || d.CurrentFileIdx >= len(d.FileDiffs) {
		return
	}
	d.ThreadCursor = 0
	lines := d.FileDiffs[d.CurrentFileIdx]
	newPos := d.DiffCursor + delta

	for newPos >= 0 && newPos < len(lines) && lines[newPos].Type == components.LineHunk {
		newPos += delta
	}

	if newPos < 0 || newPos >= len(lines) {
		return
	}
	d.DiffCursor = newPos
	d.ScrollToDiffCursor()
}

// MoveDiffCursorBy jumps the cursor by delta lines, clamped, skipping hunks.
func (d *DiffViewer) MoveDiffCursorBy(delta int) {
	if d.CurrentFileIdx < 0 || d.CurrentFileIdx >= len(d.FileDiffs) {
		return
	}
	d.ThreadCursor = 0
	lines := d.FileDiffs[d.CurrentFileIdx]
	newPos := d.DiffCursor + delta

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

	d.DiffCursor = newPos
	d.ScrollToDiffCursor()
}

func (d DiffViewer) FirstNonHunkLine(fileIdx int) int {
	if fileIdx < 0 || fileIdx >= len(d.FileDiffs) {
		return 0
	}
	for i, dl := range d.FileDiffs[fileIdx] {
		if dl.Type != components.LineHunk {
			return i
		}
	}
	return 0
}

func (d DiffViewer) LastNonHunkLine(fileIdx int) int {
	if fileIdx < 0 || fileIdx >= len(d.FileDiffs) {
		return 0
	}
	lines := d.FileDiffs[fileIdx]
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].Type != components.LineHunk {
			return i
		}
	}
	return len(lines) - 1
}

// ScrollToDiffCursor adjusts the viewport so the cursor line is visible.
func (d *DiffViewer) ScrollToDiffCursor() {
	idx := d.CurrentFileIdx
	if idx < 0 || idx >= len(d.FileDiffOffsets) {
		return
	}
	if d.DiffCursor >= len(d.FileDiffOffsets[idx]) {
		return
	}
	vpH := d.ViewportHeight()
	absLine := d.FileDiffOffsets[idx][d.DiffCursor]
	top := d.VP.YOffset()
	bottom := top + vpH - 1

	if absLine < top+scrollMargin {
		target := absLine - scrollMargin
		if target < 0 {
			target = 0
		}
		d.VP.SetYOffset(target)
	} else if absLine > bottom-scrollMargin {
		d.VP.SetYOffset(absLine - vpH + scrollMargin + 1)
	}
}

// ScrollAndSyncCursor scrolls the viewport by delta, keeping the cursor
// at the same screen-relative position (vim ctrl+d/u behavior).
func (d *DiffViewer) ScrollAndSyncCursor(delta int) {
	d.ThreadCursor = 0
	idx := d.CurrentFileIdx
	if idx < 0 || idx >= len(d.FileDiffOffsets) {
		return
	}

	cursorAbs := 0
	if d.DiffCursor < len(d.FileDiffOffsets[idx]) {
		cursorAbs = d.FileDiffOffsets[idx][d.DiffCursor]
	}
	relPos := cursorAbs - d.VP.YOffset()

	d.VP.SetYOffset(d.VP.YOffset() + delta)

	targetAbs := d.VP.YOffset() + relPos
	offsets := d.FileDiffOffsets[idx]
	diffs := d.FileDiffs[idx]
	best := -1
	bestDist := 0
	for i := 0; i < len(offsets); i++ {
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
		d.DiffCursor = best
	}
}

// SyncDiffCursorToViewport moves the diff cursor to the line closest to
// the center of the viewport. Used after viewport-only scrolling.
func (d *DiffViewer) SyncDiffCursorToViewport() {
	idx := d.CurrentFileIdx
	if idx < 0 || idx >= len(d.FileDiffOffsets) || len(d.FileDiffOffsets[idx]) == 0 {
		return
	}
	center := d.VP.YOffset() + d.ViewportHeight()/2
	offsets := d.FileDiffOffsets[idx]
	diffs := d.FileDiffs[idx]
	best := -1
	bestDist := 0
	for i := 0; i < len(offsets); i++ {
		if i < len(diffs) && diffs[i].Type == components.LineHunk {
			continue
		}
		dist := offsets[i] - center
		if dist < 0 {
			dist = -dist
		}
		if best == -1 || dist < bestDist {
			best = i
			bestDist = dist
		}
	}
	if best >= 0 {
		d.DiffCursor = best
	}
}

// CursorViewportLine returns the cursor's Y position in the viewport, or -1 if not visible.
func (d DiffViewer) CursorViewportLine() int {
	idx := d.CurrentFileIdx
	if idx < 0 || idx >= len(d.FileDiffOffsets) {
		return -1
	}
	if d.DiffCursor >= len(d.FileDiffOffsets[idx]) {
		return -1
	}
	absLine := d.FileDiffOffsets[idx][d.DiffCursor]
	rel := absLine - d.VP.YOffset()
	if rel < 0 || rel >= d.ViewportHeight() {
		return -1
	}
	return rel
}

// --- Cursor overlay rendering ---

// OverlayDiffCursor applies cursor or selection highlighting to the viewport content.
func (d DiffViewer) OverlayDiffCursor(view string) string {
	if !d.FilesListLoaded || !d.HasDiffLines() {
		return view
	}

	if d.SelectionAnchor >= 0 && d.SelectionAnchor != d.DiffCursor {
		return d.overlaySelectionRange(view)
	}

	vLine := d.CursorViewportLine()
	if vLine < 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	if vLine < len(lines) {
		lines[vLine] = d.ApplyCursorHighlight(lines[vLine])
	}
	return strings.Join(lines, "\n")
}

// ApplyCursorHighlight applies the cursor background to a single rendered line.
func (d DiffViewer) ApplyCursorHighlight(line string) string {
	idx := d.CurrentFileIdx
	if idx >= len(d.FileDiffs) || d.DiffCursor >= len(d.FileDiffs[idx]) {
		return line
	}
	dl := d.FileDiffs[idx][d.DiffCursor]
	if dl.Type == components.LineHunk {
		return line
	}

	prefix, inner, suffix := SplitDiffBorders(line)

	inner = strings.Replace(inner, "\033[1m+\033[0m", "\033[1m>\033[0m", 1)
	inner = strings.Replace(inner, "\033[1m-\033[0m", "\033[1m>\033[0m", 1)

	colors := d.Ctx.DiffColors
	var selBg string
	switch dl.Type {
	case components.LineAdd:
		selBg = colors.SelectedAddBg
	case components.LineDel:
		selBg = colors.SelectedDelBg
	default:
		selBg = colors.SelectedCtxBg
	}

	if selBg != "" {
		inner = replaceBackground(inner, colors, selBg)
	}

	return prefix + inner + suffix
}

func (d DiffViewer) overlaySelectionRange(view string) string {
	idx := d.CurrentFileIdx
	if idx < 0 || idx >= len(d.FileDiffOffsets) {
		return view
	}

	selStart, selEnd := d.SelectionAnchor, d.DiffCursor
	if selStart > selEnd {
		selStart, selEnd = selEnd, selStart
	}

	offsets := d.FileDiffOffsets[idx]
	diffs := d.FileDiffs[idx]
	vpTop := d.VP.YOffset()

	lines := strings.Split(view, "\n")

	for i := selStart; i <= selEnd; i++ {
		if i >= len(offsets) || i >= len(diffs) {
			continue
		}
		if diffs[i].Type == components.LineHunk {
			continue
		}
		absLine := offsets[i]
		rel := absLine - vpTop
		if rel < 0 || rel >= len(lines) {
			continue
		}
		lines[rel] = d.ApplySelectionHighlight(lines[rel], diffs[i])
	}

	return strings.Join(lines, "\n")
}

// ApplySelectionHighlight applies selection background to a diff line.
func (d DiffViewer) ApplySelectionHighlight(line string, dl components.DiffLine) string {
	if dl.Type == components.LineHunk {
		return line
	}

	prefix, inner, suffix := SplitDiffBorders(line)

	inner = strings.Replace(inner, "\033[1m+\033[0m", "\033[1m>\033[0m", 1)
	inner = strings.Replace(inner, "\033[1m-\033[0m", "\033[1m>\033[0m", 1)

	colors := d.Ctx.DiffColors
	var selBg string
	switch dl.Type {
	case components.LineAdd:
		selBg = colors.SelectedAddBg
	case components.LineDel:
		selBg = colors.SelectedDelBg
	default:
		selBg = colors.SelectedCtxBg
	}

	if selBg != "" {
		inner = replaceBackground(inner, colors, selBg)
	}

	return prefix + inner + suffix
}

// replaceBackground swaps diff bg colors for selected bg in an ANSI string.
func replaceBackground(inner string, colors styles.DiffColors, selBg string) string {
	if colors.AddBg != "" {
		inner = strings.ReplaceAll(inner, colors.AddBg, selBg)
	}
	if colors.DelBg != "" {
		inner = strings.ReplaceAll(inner, colors.DelBg, selBg)
	}
	inner = strings.ReplaceAll(inner, "\033[0m", "\033[0m"+selBg)
	inner = strings.ReplaceAll(inner, "\033[m", "\033[m"+selBg)
	inner = selBg + inner + "\033[0m"
	return inner
}

// --- Layout rendering ---

// RenderLayout composes the file tree (left) and diff view (right) into the final output.
func (d DiffViewer) RenderLayout(rightView string, rightTitle string) string {
	treeW := d.Tree.Width
	innerTreeW := treeW - 2
	innerTreeH := d.Height - 2

	bc := d.BorderStyle()
	var treeBorderStyle lipgloss.Style
	if d.Tree.Focused {
		treeBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	} else {
		treeBorderStyle = bc
	}

	// Tree border.
	titleStr := " " + lipgloss.NewStyle().Bold(true).Render("Files") + " "
	titleW := lipgloss.Width(titleStr)
	fillW := treeW - 3 - titleW
	if fillW < 0 {
		fillW = 0
	}
	topBorder := treeBorderStyle.Render("╭─") + titleStr + treeBorderStyle.Render(strings.Repeat("─", fillW)+"╮")
	bw := treeW - 2
	if bw < 0 {
		bw = 0
	}
	bottomBorder := treeBorderStyle.Render("╰" + strings.Repeat("─", bw) + "╯")
	sideBorderL := treeBorderStyle.Render("│")
	sideBorderR := treeBorderStyle.Render("│")

	// Temporarily set tree dimensions for rendering.
	tree := d.Tree
	tree.Width = innerTreeW
	tree.Height = innerTreeH
	tree.CurrentFileIdx = d.CurrentFileIdx
	treeContentLines := tree.View()
	rightLines := strings.Split(rightView, "\n")

	// Right panel border.
	rightW := d.RightPanelWidth()
	innerRightW := rightW - 2
	var rightBorderStyle lipgloss.Style
	if !d.Tree.Focused {
		rightBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	} else {
		rightBorderStyle = bc
	}

	rtTitle := " " + lipgloss.NewStyle().Bold(true).Render(rightTitle) + " "
	rtW := lipgloss.Width(rtTitle)
	rtFill := rightW - 3 - rtW
	if rtFill < 0 {
		rtFill = 0
	}
	rightTop := rightBorderStyle.Render("╭─") + rtTitle + rightBorderStyle.Render(strings.Repeat("─", rtFill)+"╮")
	rbw := rightW - 2
	if rbw < 0 {
		rbw = 0
	}
	rightBottom := rightBorderStyle.Render("╰" + strings.Repeat("─", rbw) + "╯")
	rightSideL := rightBorderStyle.Render("│")
	rightSideR := rightBorderStyle.Render("│")

	var b strings.Builder
	for i := 0; i < d.Height; i++ {
		var treeLine string
		if i == 0 {
			treeLine = topBorder
		} else if i == d.Height-1 {
			treeLine = bottomBorder
		} else {
			tIdx := i - 1
			cl := ""
			if tIdx < len(treeContentLines) {
				cl = treeContentLines[tIdx]
			}
			treeLine = sideBorderL + cl + sideBorderR
		}

		var rightLine string
		if i == 0 {
			rightLine = rightTop
		} else if i == d.Height-1 {
			rightLine = rightBottom
		} else {
			rIdx := i - 1
			rl := ""
			if rIdx < len(rightLines) {
				rl = rightLines[rIdx]
			}
			rlW := lipgloss.Width(rl)
			if rlW < innerRightW {
				rl += strings.Repeat(" ", innerRightW-rlW)
			}
			rightLine = rightSideL + rl + rightSideR
		}

		b.WriteString(treeLine + rightLine)
		if i < d.Height-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// --- File formatting ---

// FormatFileWithComments recomputes the rendered diff + offsets for a single file
// using the provided comments. The outer model is responsible for gathering comments.
func (d *DiffViewer) FormatFileWithComments(index int, fileComments []github.ReviewComment) {
	if index >= len(d.HighlightedFiles) {
		return
	}
	hl := d.HighlightedFiles[index]
	if hl.File.Filename == "" {
		return
	}
	width := d.ContentWidth()

	result := components.FormatDiffFile(hl, width, d.Ctx.DiffColors, fileComments)
	if index < len(d.RenderedFiles) {
		d.RenderedFiles[index] = result.Content
	}
	if index < len(d.FileDiffOffsets) {
		d.FileDiffOffsets[index] = result.DiffLineOffsets
	}
	if index < len(d.FileCommentPositions) {
		d.FileCommentPositions[index] = result.CommentPositions
	}
}

// AppendCopilotPending appends all pending Copilot "Thinking..." comments
// for the given file to the comment slice.
func (d DiffViewer) AppendCopilotPending(filename string, fileComments []github.ReviewComment) []github.ReviewComment {
	dots := strings.Repeat(".", d.CopilotDots+1)
	for commentID, info := range d.CopilotPending {
		if info.Path != filename {
			continue
		}
		body := d.CopilotReplyBuf[commentID]
		if body == "" {
			body = "Thinking" + dots
		} else {
			body = body + dots
		}
		line := info.Line
		replyToInt := comments.IDToInt(commentID)
		pending := github.ReviewComment{
			ID:           0,
			Body:         body,
			Path:         filename,
			Line:         &line,
			OriginalLine: &line,
			Side:         info.Side,
			InReplyToID:  &replyToInt,
			User:         github.User{Login: "copilot"},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		fileComments = append(fileComments, pending)
	}
	return fileComments
}

// FileIndexForPath returns the index of the file with the given path, or -1.
func (d DiffViewer) FileIndexForPath(path string) int {
	for i, f := range d.Files {
		if f.Filename == path {
			return i
		}
	}
	return -1
}

// --- Viewport helpers ---

// RebuildContent sets the viewport content using the provided builders.
// buildOverview is called when CurrentFileIdx == -1, buildFile otherwise.
func (d *DiffViewer) RebuildContent(buildOverview func(w int) string, buildFile func(w int) string) {
	innerW := d.RightPanelInnerWidth()
	innerH := d.Height - 2

	if !d.VPReady {
		d.VP = viewport.New()
		d.VPReady = true
	}
	d.VP.SetWidth(innerW)
	d.VP.SetHeight(innerH)

	var content string
	if d.CurrentFileIdx == -1 {
		content = buildOverview(innerW)
	} else {
		content = buildFile(innerW)
	}
	d.VP.SetContent(content)
}

// RebuildContentIfChanged only updates the viewport if the content changed.
func (d *DiffViewer) RebuildContentIfChanged(buildOverview func(w int) string, buildFile func(w int) string) {
	innerW := d.RightPanelInnerWidth()
	innerH := d.Height - 2

	if !d.VPReady {
		d.VP = viewport.New()
		d.VPReady = true
	}
	d.VP.SetWidth(innerW)
	d.VP.SetHeight(innerH)

	var content string
	if d.CurrentFileIdx == -1 {
		content = buildOverview(innerW)
	} else {
		content = buildFile(innerW)
	}
	if content != d.LastContent {
		d.LastContent = content
		d.VP.SetContent(content)
	}
}

// InitFileSlices allocates the per-file slices for a new set of files.
func (d *DiffViewer) InitFileSlices(n int) {
	d.HighlightedFiles = make([]components.HighlightedDiff, n)
	d.RenderedFiles = make([]string, n)
	d.FileDiffs = make([][]components.DiffLine, n)
	d.FileDiffOffsets = make([][]int, n)
	d.FileCommentPositions = make([][]components.CommentPosition, n)
}

// --- Copilot helpers ---

// SetCopilotPending marks a comment as awaiting Copilot reply.
func (d *DiffViewer) SetCopilotPending(commentID, path string, line int, side string) {
	if d.CopilotPending == nil {
		d.CopilotPending = make(map[string]CopilotPendingInfo)
	}
	d.CopilotPending[commentID] = CopilotPendingInfo{Path: path, Line: line, Side: side}
}

// ClearCopilotPending removes a single pending Copilot session.
func (d *DiffViewer) ClearCopilotPending(commentID string) {
	delete(d.CopilotPending, commentID)
}

// HasCopilotPending returns true if there are any pending Copilot sessions.
func (d DiffViewer) HasCopilotPending() bool {
	return len(d.CopilotPending) > 0
}

// IsCopilotPending returns true if the given comment is pending.
func (d DiffViewer) IsCopilotPending(commentID string) bool {
	_, ok := d.CopilotPending[commentID]
	return ok
}

// CopilotPendingPath returns the path of a pending Copilot session, or "".
func (d DiffViewer) CopilotPendingPath(commentID string) string {
	if info, ok := d.CopilotPending[commentID]; ok {
		return info.Path
	}
	return ""
}

// --- Standalone helpers ---

// SplitDiffBorders splits a rendered diff line into border prefix, inner content, and border suffix.
func SplitDiffBorders(line string) (prefix, inner, suffix string) {
	const borderChar = "│"

	firstIdx := strings.Index(line, borderChar)
	if firstIdx < 0 {
		return "", line, ""
	}

	lastIdx := strings.LastIndex(line, borderChar)
	if lastIdx == firstIdx {
		return "", line, ""
	}

	prefixEnd := firstIdx + len(borderChar)
	if prefixEnd < len(line) && line[prefixEnd] == '\033' {
		if i := strings.IndexByte(line[prefixEnd:], 'm'); i >= 0 {
			prefixEnd += i + 1
		}
	}

	suffixStart := lastIdx
	for i := lastIdx - 1; i >= prefixEnd; i-- {
		if line[i] == '\033' {
			suffixStart = i
			break
		}
	}

	return line[:prefixEnd], line[prefixEnd:suffixStart], line[suffixStart:]
}
