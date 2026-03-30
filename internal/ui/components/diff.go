package components

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"charm.land/lipgloss/v2"

	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/ui/styles"
)

// LineType identifies the kind of diff line.
type LineType int

const (
	LineContext LineType = iota
	LineAdd
	LineDel
	LineHunk
)

// DiffLine represents a single parsed + rendered line in a diff.
type DiffLine struct {
	Type      LineType
	OldLineNo int
	NewLineNo int
	Content   string // raw code text (no ANSI)
	Rendered  string // fully rendered with gutter + syntax highlighting + bg
}

const gutterWidth = 4

var (
	fileNameStyle = lipgloss.NewStyle().Bold(true)
	borderStyle   = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
)

// RenderDiffFile renders a single file's diff with full-width colored backgrounds.
func RenderDiffFile(f github.PullRequestFile, fileContent string, width int, colors styles.DiffColors, comments []github.ReviewComment) string {
	name := f.Filename
	if f.Status == "renamed" && f.PreviousFilename != "" {
		name = f.PreviousFilename + " → " + f.Filename
	}

	adds := lipgloss.NewStyle().Foreground(lipgloss.Green).Render(fmt.Sprintf("+%d", f.Additions))
	dels := lipgloss.NewStyle().Foreground(lipgloss.Red).Render(fmt.Sprintf("-%d", f.Deletions))
	statsPlain := fmt.Sprintf("+%d -%d", f.Additions, f.Deletions)

	nameMax := width - len(statsPlain) - 2
	if nameMax < 0 {
		nameMax = 0
	}
	if lipgloss.Width(name) > nameMax {
		name = ansi.Truncate(name, nameMax-1, "…")
	}
	gap := width - lipgloss.Width(name) - len(statsPlain)
	if gap < 1 {
		gap = 1
	}

	// Embed title + stats in the top border: ── filename ── +N -N ──
	title := " " + fileNameStyle.Render(name) + " "
	stats := " " + adds + " " + dels + " "
	titleW := lipgloss.Width(title)
	statsW := lipgloss.Width(stats)
	leadW := 2 // "──"
	fillW := width - leadW - titleW - statsW
	if fillW < 0 {
		fillW = 0
	}
	topBorder := borderStyle.Render("──") + title + borderStyle.Render(strings.Repeat("─", fillW)) + stats
	// Trim or pad to exact width if stats pushed it over
	bottomBorder := borderStyle.Render(strings.Repeat("─", width))

	if f.Patch == "" {
		return topBorder + "\n" + styles.SubtitleStyle.Render("(binary or empty)") + "\n" + bottomBorder
	}

	diffLines := parsePatchLines(f.Patch)

	if fileContent != "" {
		hlLines := highlightFileLines(fileContent, f.Filename, colors.ChromaStyle)
		renderDiffLines(diffLines, hlLines, f.Filename, width, colors)
	} else {
		renderDiffLinesFallback(diffLines, f.Filename, width, colors)
	}

	// Index comments by the line they attach to (new-side line number).
	commentsByLine := buildCommentThreads(comments)

	var b strings.Builder
	b.WriteString(topBorder)
	b.WriteString("\n")
	for _, dl := range diffLines {
		b.WriteString(dl.Rendered)
		b.WriteString("\n")

		// Check if there are comments on this line.
		lineNo := dl.NewLineNo
		if dl.Type == LineDel {
			lineNo = dl.OldLineNo
		}
		if lineNo > 0 {
			if threads, ok := commentsByLine[lineNo]; ok {
				b.WriteString(renderCommentThread(threads, width, dl.Type, colors))
			}
		}
	}
	b.WriteString(bottomBorder)
	return b.String()
}

// parsePatchLines parses a unified diff patch into structured DiffLines.
func parsePatchLines(patch string) []DiffLine {
	lines := strings.Split(patch, "\n")
	result := make([]DiffLine, 0, len(lines))
	oldNum, newNum := 0, 0

	for _, line := range lines {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "@@"):
			dl := DiffLine{Type: LineHunk, Content: line}
			if h, ok := parseHunkHeader(line); ok {
				oldNum = h.oldStart
				newNum = h.newStart
			}
			result = append(result, dl)
		case strings.HasPrefix(line, "+"):
			result = append(result, DiffLine{
				Type: LineAdd, NewLineNo: newNum, Content: line[1:],
			})
			newNum++
		case strings.HasPrefix(line, "-"):
			result = append(result, DiffLine{
				Type: LineDel, OldLineNo: oldNum, Content: line[1:],
			})
			oldNum++
		default:
			content := line
			if len(line) > 0 && line[0] == ' ' {
				content = line[1:]
			}
			result = append(result, DiffLine{
				Type: LineContext, OldLineNo: oldNum, NewLineNo: newNum,
				Content: content,
			})
			oldNum++
			newNum++
		}
	}
	return result
}

// renderDiffLines renders each DiffLine using pre-highlighted full file lines.
func renderDiffLines(diffLines []DiffLine, hlLines []string, filename string, width int, colors styles.DiffColors) {
	for i := range diffLines {
		dl := &diffLines[i]
		switch dl.Type {
		case LineHunk:
			dl.Rendered = renderHunkLine(dl.Content, width, colors)

		case LineAdd:
			hl := getHighlightedLine(hlLines, dl.NewLineNo)
			hl = injectBackground(hl, colors.AddBg)
			gutter := colors.AddBg + colors.AddFg +
				padNum(gutterWidth) + padNum(gutterWidth, dl.NewLineNo) +
				" " + "\033[1m" + "+" + "\033[0m" + colors.AddBg
			dl.Rendered = padWithBg(truncateLine(gutter+hl, width), width, colors.AddBg)

		case LineDel:
			hl := highlightSnippet(dl.Content, filename, colors.ChromaStyle)
			hl = injectBackground(hl, colors.DelBg)
			gutter := colors.DelBg + colors.DelFg +
				padNum(gutterWidth, dl.OldLineNo) + padNum(gutterWidth) +
				" " + "\033[1m" + "-" + "\033[0m" + colors.DelBg
			dl.Rendered = padWithBg(truncateLine(gutter+hl, width), width, colors.DelBg)

		case LineContext:
			hl := getHighlightedLine(hlLines, dl.NewLineNo)
			gutter := styles.DiffLineNum.Render(
				padNum(gutterWidth, dl.OldLineNo) + padNum(gutterWidth, dl.NewLineNo) + " ",
			)
			dl.Rendered = truncateLine(gutter+" "+hl, width)
		}
	}
}

// renderDiffLinesFallback renders when no file content is available.
func renderDiffLinesFallback(diffLines []DiffLine, filename string, width int, colors styles.DiffColors) {
	// Build a single code block from all non-hunk lines for batch highlighting.
	var codeBuilder strings.Builder
	for _, dl := range diffLines {
		if dl.Type == LineHunk {
			codeBuilder.WriteString("\n")
		} else {
			codeBuilder.WriteString(dl.Content)
			codeBuilder.WriteString("\n")
		}
	}
	highlighted := highlightBlock(codeBuilder.String(), filename, colors.ChromaStyle)
	hlLines := strings.Split(highlighted, "\n")

	gutterTotal := gutterWidth*2 + 1
	codeWidth := width - gutterTotal - 1
	if codeWidth < 1 {
		codeWidth = 1
	}

	hlIdx := 0
	for i := range diffLines {
		dl := &diffLines[i]
		switch dl.Type {
		case LineHunk:
			dl.Rendered = renderHunkLine(dl.Content, width, colors)
			hlIdx++

		case LineAdd:
			hl := ""
			if hlIdx < len(hlLines) {
				hl = hlLines[hlIdx]
			}
			hlIdx++
			hl = injectBackground(hl, colors.AddBg)
			gutter := colors.AddBg + colors.AddFg +
				padNum(gutterWidth) + padNum(gutterWidth, dl.NewLineNo) +
				" " + "\033[1m" + "+" + "\033[0m" + colors.AddBg
			dl.Rendered = padWithBg(truncateLine(gutter+hl, width), width, colors.AddBg)

		case LineDel:
			hl := ""
			if hlIdx < len(hlLines) {
				hl = hlLines[hlIdx]
			}
			hlIdx++
			hl = injectBackground(hl, colors.DelBg)
			gutter := colors.DelBg + colors.DelFg +
				padNum(gutterWidth, dl.OldLineNo) + padNum(gutterWidth) +
				" " + "\033[1m" + "-" + "\033[0m" + colors.DelBg
			dl.Rendered = padWithBg(truncateLine(gutter+hl, width), width, colors.DelBg)

		case LineContext:
			hl := ""
			if hlIdx < len(hlLines) {
				hl = hlLines[hlIdx]
			}
			hlIdx++
			gutter := styles.DiffLineNum.Render(
				padNum(gutterWidth, dl.OldLineNo) + padNum(gutterWidth, dl.NewLineNo) + " ",
			)
			dl.Rendered = truncateLine(gutter+" "+hl, width)
		}
	}
}

// renderHunkLine renders a @@ hunk header with full-width background.
func renderHunkLine(content string, width int, colors styles.DiffColors) string {
	line := colors.HunkBg + colors.HunkFg + content
	return padWithBg(truncateLine(line, width), width, colors.HunkBg)
}

// injectBackground replaces every SGR reset in chroma output with a reset
// followed by the given background code, so the bg survives per-token resets.
func injectBackground(highlighted string, bgCode string) string {
	if bgCode == "" {
		return highlighted
	}
	// Chroma uses \033[0m, lipgloss uses \033[m — catch both.
	s := strings.ReplaceAll(highlighted, "\033[0m", "\033[0m"+bgCode)
	s = strings.ReplaceAll(s, "\033[m", "\033[m"+bgCode)
	return s
}

// padWithBg pads a line to targetWidth with the background color, then resets.
func padWithBg(s string, targetWidth int, bgCode string) string {
	currentWidth := lipgloss.Width(s)
	pad := ""
	if currentWidth < targetWidth {
		pad = strings.Repeat(" ", targetWidth-currentWidth)
	}
	return s + pad + "\033[0m"
}

// --- Helpers ---

type hunk struct {
	oldStart int
	newStart int
}

func parseHunkHeader(line string) (hunk, bool) {
	parts := strings.SplitN(line, "@@", 3)
	if len(parts) < 3 {
		return hunk{}, false
	}
	ranges := strings.TrimSpace(parts[1])
	fields := strings.Fields(ranges)
	if len(fields) < 2 {
		return hunk{}, false
	}

	h := hunk{}
	old := strings.TrimPrefix(fields[0], "-")
	if comma := strings.IndexByte(old, ','); comma >= 0 {
		old = old[:comma]
	}
	if n, err := strconv.Atoi(old); err == nil {
		h.oldStart = n
	}

	new_ := strings.TrimPrefix(fields[1], "+")
	if comma := strings.IndexByte(new_, ','); comma >= 0 {
		new_ = new_[:comma]
	}
	if n, err := strconv.Atoi(new_); err == nil {
		h.newStart = n
	}

	return h, true
}

func padNum(w int, nums ...int) string {
	if len(nums) == 0 || nums[0] == 0 {
		return strings.Repeat(" ", w)
	}
	s := strconv.Itoa(nums[0])
	if len(s) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(s)) + s
}

func getHighlightedLine(lines []string, lineNum int) string {
	idx := lineNum - 1
	if idx >= 0 && idx < len(lines) {
		return lines[idx]
	}
	return ""
}

func highlightFileLines(content, filename string, chromaStyle *chroma.Style) []string {
	highlighted := highlightBlock(content, filename, chromaStyle)
	return strings.Split(highlighted, "\n")
}

func highlightSnippet(code, filename string, chromaStyle *chroma.Style) string {
	result := highlightBlock(code, filename, chromaStyle)
	return strings.TrimRight(result, "\n")
}

func truncateLine(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Truncate(s, width, "")
}

// buildCommentThreads groups comments by the line they're attached to,
// threading replies under their parent.
func buildCommentThreads(comments []github.ReviewComment) map[int][]github.ReviewComment {
	if len(comments) == 0 {
		return nil
	}

	// First, collect root comments (no InReplyToID) keyed by their line.
	// Then append replies after their root.
	byID := make(map[int]*github.ReviewComment, len(comments))
	for i := range comments {
		byID[comments[i].ID] = &comments[i]
	}

	result := make(map[int][]github.ReviewComment)
	// Add root comments first.
	for _, c := range comments {
		if c.InReplyToID != nil {
			continue
		}
		line := 0
		if c.Line != nil {
			line = *c.Line
		} else if c.OriginalLine != nil {
			line = *c.OriginalLine
		}
		if line > 0 {
			result[line] = append(result[line], c)
		}
	}
	// Add replies after their root.
	for _, c := range comments {
		if c.InReplyToID == nil {
			continue
		}
		// Find the root of the thread to get the line.
		root := byID[*c.InReplyToID]
		if root == nil {
			continue
		}
		// Walk up to the actual root.
		for root.InReplyToID != nil {
			if parent, ok := byID[*root.InReplyToID]; ok {
				root = parent
			} else {
				break
			}
		}
		line := 0
		if root.Line != nil {
			line = *root.Line
		} else if root.OriginalLine != nil {
			line = *root.OriginalLine
		}
		if line > 0 {
			result[line] = append(result[line], c)
		}
	}
	return result
}

// dimCode is the raw ANSI escape for BrightBlack foreground (used for comment borders).
const dimCode = "\033[90m"

// bgForLineType returns the raw ANSI bg code for the given diff line type.
func bgForLineType(lt LineType, colors styles.DiffColors) string {
	switch lt {
	case LineAdd:
		return colors.AddBg
	case LineDel:
		return colors.DelBg
	default:
		return ""
	}
}

// fgForLineType returns the raw ANSI fg code for the gutter on the given line type.
func fgForLineType(lt LineType, colors styles.DiffColors) string {
	switch lt {
	case LineAdd:
		return colors.AddFg
	case LineDel:
		return colors.DelFg
	default:
		return ""
	}
}

// commentGutterWidth is the visible width of the gutter area in diff lines.
// padNum(4) + padNum(4) + " " + marker(1) = 10 visible chars.
const commentGutterWidth = gutterWidth*2 + 2

// commentGutter renders an empty gutter matching the diff line style.
func commentGutter(bg string) string {
	return bg + strings.Repeat(" ", commentGutterWidth)
}

// emptyLine renders a full-width blank line with the given bg.
func emptyLine(bg string, width int) string {
	return padWithBg(bg, width, bg) + "\n"
}

// renderCommentThread renders a thread of review comments as a block below a diff line.
// It inherits the background color from the line type (add/del/context) and uses
// the line's fg color for borders to make the comment box stand out.
func renderCommentThread(comments []github.ReviewComment, width int, lt LineType, colors styles.DiffColors) string {
	bg := bgForLineType(lt, colors)
	fg := fgForLineType(lt, colors)
	gutterStr := commentGutter(bg)
	// Content area is everything after the gutter.
	contentW := width - commentGutterWidth
	if contentW < 20 {
		contentW = 20
	}

	var b strings.Builder

	// Blank line above.
	b.WriteString(emptyLine(bg, width))

	for i, c := range comments {
		author := " \033[1m@" + c.User.Login + "\033[0m" + bg + " "
		ageStr := relativeTime(c.CreatedAt)
		age := dimCode + ageStr + "\033[0m" + bg + " "
		authorW := 1 + 1 + len(c.User.Login) + 1 // " @user "
		ageW := len(ageStr) + 1

		var left, right string
		if i == 0 {
			left = "╭"
			right = "╮"
		} else {
			left = "├"
			right = "┤"
		}

		// ╭ + author + age + ─fill─ + ╮ = contentW
		fillW := contentW - 1 - authorW - ageW - 1
		if fillW < 0 {
			fillW = 0
		}
		topLine := gutterStr + fg + left + "\033[0m" + bg +
			author + age +
			fg + strings.Repeat("─", fillW) + right + "\033[0m"
		b.WriteString(padWithBg(topLine, width, bg))
		b.WriteString("\n")

		// Body lines: │ text                      │
		innerW := contentW - 4 // "│ " + " │"
		if innerW < 10 {
			innerW = 10
		}
		bodyLines := wrapText(c.Body, innerW)
		for _, line := range bodyLines {
			visW := lipgloss.Width(line)
			pad := innerW - visW
			if pad < 0 {
				pad = 0
			}
			content := gutterStr + fg + "│" + "\033[0m" + bg +
				" " + line + strings.Repeat(" ", pad) + " " +
				fg + "│" + "\033[0m"
			b.WriteString(padWithBg(content, width, bg))
			b.WriteString("\n")
		}
	}

	// Bottom border: ╰──────╯
	fillW := contentW - 2
	if fillW < 0 {
		fillW = 0
	}
	bottomLine := gutterStr + fg + "╰" + strings.Repeat("─", fillW) + "╯" + "\033[0m"
	b.WriteString(padWithBg(bottomLine, width, bg))
	b.WriteString("\n")

	// Blank line below.
	b.WriteString(emptyLine(bg, width))

	return b.String()
}

// wrapText wraps text to the given width, splitting on whitespace.
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		current := words[0]
		for _, w := range words[1:] {
			if len(current)+1+len(w) > width {
				lines = append(lines, current)
				current = w
			} else {
				current += " " + w
			}
		}
		lines = append(lines, current)
	}
	return lines
}

// relativeTime formats a time as a human-readable relative string.
func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	case d < 30*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	default:
		months := int(d.Hours() / 24 / 30)
		if months <= 1 {
			return "1mo ago"
		}
		return fmt.Sprintf("%dmo ago", months)
	}
}

func highlightBlock(code, filename string, chromaStyle *chroma.Style) string {
	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	formatter := formatters.Get("terminal16m")
	style := chromaStyle
	if style == nil {
		style = chromastyles.Get("monokai")
	}

	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}

	var b strings.Builder
	err = formatter.Format(&b, style, iterator)
	if err != nil {
		return code
	}

	return strings.TrimRight(b.String(), "\n")
}
