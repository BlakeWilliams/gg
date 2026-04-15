package components

import (
	"strings"
	"testing"

	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/ui/styles"
)

func spliceTestColors() styles.DiffColors {
	return styles.DiffColors{
		AddBg:              "\033[42m",
		AddFg:              "\033[32m",
		DelBg:              "\033[41m",
		DelFg:              "\033[31m",
		HunkBg:             "\033[44m",
		HunkFg:             "\033[34m",
		HighlightBorderFg:  "#ffff00",
		BorderFg:           "#888888",
	}
}

func makeTestFile(adds int) github.PullRequestFile {
	var patchLines []string
	patchLines = append(patchLines, "@@ -0,0 +1,"+strings.Repeat("0", len(string(rune('0'+adds))))+" @@")
	for i := 1; i <= adds; i++ {
		patchLines = append(patchLines, "+line "+string(rune('a'-1+i)))
	}
	return github.PullRequestFile{
		Filename: "test.go",
		Status:   "added",
		Patch:    strings.Join(patchLines, "\n"),
	}
}

func makeTestPatch(lines int) string {
	var b strings.Builder
	b.WriteString("@@ -0,0 +1," + strings.Repeat("0", 1) + " @@\n")
	for i := 1; i <= lines; i++ {
		b.WriteString("+line " + string(rune('a'-1+(i%26)+1)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderWithComments(patch string, comments []github.ReviewComment, width int) DiffRenderResult {
	f := github.PullRequestFile{
		Filename: "test.go",
		Status:   "added",
		Patch:    patch,
	}
	hl := HighlightDiffFile(f, "", nil)
	return FormatDiffFile(hl, width, spliceTestColors(), comments)
}

func comment(id int, line int, body string) github.ReviewComment {
	l := line
	return github.ReviewComment{
		ID:   id,
		Body: body,
		Path: "test.go",
		Line: &l,
		OriginalLine: &l,
		Side: "RIGHT",
		User: github.User{Login: "testuser"},
	}
}

func replyComment(id int, replyTo int, body string, line int) github.ReviewComment {
	l := line
	return github.ReviewComment{
		ID:          id,
		Body:        body,
		Path:        "test.go",
		Line:        &l,
		OriginalLine: &l,
		Side:        "RIGHT",
		InReplyToID: &replyTo,
		User:        github.User{Login: "bot"},
	}
}

// TestFormatDiffFile_ThreadRanges verifies that ThreadRanges are populated.
func TestFormatDiffFile_ThreadRanges(t *testing.T) {
	patch := makeTestPatch(10)
	comments := []github.ReviewComment{comment(1, 3, "hello")}
	result := renderWithComments(patch, comments, 80)

	if len(result.ThreadRanges) != 1 {
		t.Fatalf("expected 1 thread range, got %d", len(result.ThreadRanges))
	}

	tr := result.ThreadRanges[0]
	if tr.ByteStart >= tr.ByteEnd {
		t.Errorf("invalid byte range: start=%d end=%d", tr.ByteStart, tr.ByteEnd)
	}
	if tr.LineCount <= 0 {
		t.Errorf("expected positive line count, got %d", tr.LineCount)
	}

	// The byte range should contain the rendered thread content.
	threadContent := result.Content[tr.ByteStart:tr.ByteEnd]
	if !strings.Contains(threadContent, "hello") {
		t.Errorf("thread content should contain comment body, got: %s", threadContent)
	}
	if !strings.Contains(threadContent, "testuser") {
		t.Errorf("thread content should contain author, got: %s", threadContent)
	}
}

// TestFormatDiffFile_ByteOffsets verifies byte offsets point to valid positions.
func TestFormatDiffFile_ByteOffsets(t *testing.T) {
	patch := makeTestPatch(20)
	comments := []github.ReviewComment{
		comment(1, 5, "first comment"),
		comment(2, 15, "second comment"),
	}
	result := renderWithComments(patch, comments, 80)

	if len(result.ThreadRanges) != 2 {
		t.Fatalf("expected 2 thread ranges, got %d", len(result.ThreadRanges))
	}

	contentLen := len(result.Content)
	for i, tr := range result.ThreadRanges {
		if tr.ByteStart < 0 || tr.ByteStart > contentLen {
			t.Errorf("range %d: ByteStart %d out of bounds (content len %d)", i, tr.ByteStart, contentLen)
		}
		if tr.ByteEnd < tr.ByteStart || tr.ByteEnd > contentLen+1 {
			t.Errorf("range %d: ByteEnd %d invalid (start=%d, content len %d)", i, tr.ByteEnd, tr.ByteStart, contentLen)
		}
	}

	// Ranges should not overlap.
	if result.ThreadRanges[0].ByteEnd > result.ThreadRanges[1].ByteStart {
		t.Errorf("thread ranges overlap: first ends at %d, second starts at %d",
			result.ThreadRanges[0].ByteEnd, result.ThreadRanges[1].ByteStart)
	}
}

// TestSplice_SingleThread tests splicing a single thread with updated content.
func TestSplice_SingleThread(t *testing.T) {
	patch := makeTestPatch(10)
	comments := []github.ReviewComment{comment(1, 5, "original")}
	result := renderWithComments(patch, comments, 80)

	// Verify original content.
	if !strings.Contains(result.Content, "original") {
		t.Fatal("expected 'original' in content")
	}

	// Splice with updated comment.
	updatedComments := []github.ReviewComment{comment(1, 5, "updated body")}
	newThread := RenderSingleThread(updatedComments, 80, LineAdd, spliceTestColors(), false, 0, nil, result.ThreadRanges[0].LineCount)
	// Actually we need the gutterW. Let me compute it properly.
	hl := HighlightDiffFile(github.PullRequestFile{Filename: "test.go", Status: "added", Patch: patch}, "", nil)
	gutterW := TotalGutterWidth(GutterColWidth(hl.DiffLines))
	newThread = RenderSingleThread(updatedComments, 80, LineAdd, spliceTestColors(), false, 0, nil, gutterW)

	SpliceThread(&result, 0, newThread)

	if strings.Contains(result.Content, "original") {
		t.Error("spliced content should not contain 'original'")
	}
	if !strings.Contains(result.Content, "updated body") {
		t.Error("spliced content should contain 'updated body'")
	}
}

// TestSplice_MatchesFullRender verifies splice result matches a fresh full render.
func TestSplice_MatchesFullRender(t *testing.T) {
	patch := makeTestPatch(10)
	originalComments := []github.ReviewComment{comment(1, 5, "original")}
	result := renderWithComments(patch, originalComments, 80)

	// Splice to "updated".
	updatedComments := []github.ReviewComment{comment(1, 5, "updated")}
	hl := HighlightDiffFile(github.PullRequestFile{Filename: "test.go", Status: "added", Patch: patch}, "", nil)
	gutterW := TotalGutterWidth(GutterColWidth(hl.DiffLines))
	newThread := RenderSingleThread(updatedComments, 80, LineAdd, spliceTestColors(), false, 0, nil, gutterW)
	SpliceThread(&result, 0, newThread)

	// Fresh full render with the updated comment.
	fresh := renderWithComments(patch, updatedComments, 80)

	if result.Content != fresh.Content {
		// Find first difference for debugging.
		rLines := strings.Split(result.Content, "\n")
		fLines := strings.Split(fresh.Content, "\n")
		for i := 0; i < len(rLines) && i < len(fLines); i++ {
			if rLines[i] != fLines[i] {
				t.Errorf("line %d differs:\n  splice: %q\n  fresh:  %q", i, rLines[i], fLines[i])
				break
			}
		}
		if len(rLines) != len(fLines) {
			t.Errorf("line count differs: splice=%d fresh=%d", len(rLines), len(fLines))
		}
	}
}

// TestSplice_MultipleThreads tests splicing the middle thread of three.
func TestSplice_MultipleThreads(t *testing.T) {
	patch := makeTestPatch(20)
	comments := []github.ReviewComment{
		comment(1, 3, "first"),
		comment(2, 10, "second"),
		comment(3, 18, "third"),
	}
	result := renderWithComments(patch, comments, 80)

	if len(result.ThreadRanges) != 3 {
		t.Fatalf("expected 3 ranges, got %d", len(result.ThreadRanges))
	}

	// Save first and third thread content for comparison.
	firstBefore := result.Content[result.ThreadRanges[0].ByteStart:result.ThreadRanges[0].ByteEnd]
	thirdBefore := result.Content[result.ThreadRanges[2].ByteStart:result.ThreadRanges[2].ByteEnd]

	// Splice the middle thread.
	updatedComments := []github.ReviewComment{comment(2, 10, "REPLACED")}
	hl := HighlightDiffFile(github.PullRequestFile{Filename: "test.go", Status: "added", Patch: patch}, "", nil)
	gutterW := TotalGutterWidth(GutterColWidth(hl.DiffLines))
	newThread := RenderSingleThread(updatedComments, 80, LineAdd, spliceTestColors(), false, 0, nil, gutterW)
	SpliceThread(&result, 1, newThread)

	// First thread should be unchanged.
	firstAfter := result.Content[result.ThreadRanges[0].ByteStart:result.ThreadRanges[0].ByteEnd]
	if firstAfter != firstBefore {
		t.Error("first thread should be unchanged after splicing middle")
	}

	// Third thread content should be the same (shifted).
	thirdAfter := result.Content[result.ThreadRanges[2].ByteStart:result.ThreadRanges[2].ByteEnd]
	if thirdAfter != thirdBefore {
		t.Error("third thread content should be unchanged after splicing middle")
	}

	// Middle should contain updated text.
	middleContent := result.Content[result.ThreadRanges[1].ByteStart:result.ThreadRanges[1].ByteEnd]
	if !strings.Contains(middleContent, "REPLACED") {
		t.Error("middle thread should contain 'REPLACED'")
	}
}

// TestSplice_GrowingComment simulates copilot streaming — comment grows on each splice.
func TestSplice_GrowingComment(t *testing.T) {
	patch := makeTestPatch(10)
	hl := HighlightDiffFile(github.PullRequestFile{Filename: "test.go", Status: "added", Patch: patch}, "", nil)
	gutterW := TotalGutterWidth(GutterColWidth(hl.DiffLines))

	// Start with short comment.
	comments := []github.ReviewComment{comment(1, 5, "Thinking...")}
	result := renderWithComments(patch, comments, 80)

	// Simulate 5 streaming deltas.
	bodies := []string{
		"Short reply",
		"A bit longer reply now",
		"Getting longer with more detail",
		"Even more text\nwith a newline",
		"Final version\nwith multiple\nnewlines here",
	}

	for i, body := range bodies {
		updated := []github.ReviewComment{comment(1, 5, body)}
		newThread := RenderSingleThread(updated, 80, LineAdd, spliceTestColors(), false, 0, nil, gutterW)
		SpliceThread(&result, 0, newThread)

		// Verify byte ranges are still valid.
		tr := result.ThreadRanges[0]
		if tr.ByteStart < 0 || tr.ByteEnd > len(result.Content)+1 {
			t.Errorf("step %d: invalid byte range [%d, %d) in content of len %d", i, tr.ByteStart, tr.ByteEnd, len(result.Content))
		}
		// Check the first line of the body survives in the rendered content.
		firstLine := strings.SplitN(body, "\n", 2)[0]
		if !strings.Contains(result.Content, firstLine) {
			t.Errorf("step %d: content should contain %q", i, firstLine)
		}
	}
}

// TestSplice_DiffLineOffsets verifies offsets are correct after splice.
func TestSplice_DiffLineOffsets(t *testing.T) {
	patch := makeTestPatch(10)
	comments := []github.ReviewComment{comment(1, 3, "short")}
	result := renderWithComments(patch, comments, 80)

	// Remember offset of line after comment.
	offsetBefore := result.DiffLineOffsets[4] // line 5 (0-indexed: hunk=0, line1=1, line2=2, line3=3, line4=4)

	// Splice with longer comment.
	hl := HighlightDiffFile(github.PullRequestFile{Filename: "test.go", Status: "added", Patch: patch}, "", nil)
	gutterW := TotalGutterWidth(GutterColWidth(hl.DiffLines))
	longComment := []github.ReviewComment{comment(1, 3, "this is a much\nlonger comment\nwith multiple\nlines")}
	newThread := RenderSingleThread(longComment, 80, LineAdd, spliceTestColors(), false, 0, nil, gutterW)

	oldLineCount := result.ThreadRanges[0].LineCount
	SpliceThread(&result, 0, newThread)
	newLineCount := result.ThreadRanges[0].LineCount

	lineDelta := newLineCount - oldLineCount
	offsetAfter := result.DiffLineOffsets[4]

	if offsetAfter != offsetBefore+lineDelta {
		t.Errorf("expected offset to shift by %d (from %d to %d), got %d",
			lineDelta, offsetBefore, offsetBefore+lineDelta, offsetAfter)
	}
}

// TestSplice_MultipleReplies tests a thread with multiple comments.
func TestSplice_MultipleReplies(t *testing.T) {
	patch := makeTestPatch(10)
	comments := []github.ReviewComment{
		comment(1, 5, "original question"),
		replyComment(2, 1, "reply from bot", 5),
	}
	result := renderWithComments(patch, comments, 80)

	if len(result.ThreadRanges) != 1 {
		t.Fatalf("expected 1 thread (replies are grouped), got %d", len(result.ThreadRanges))
	}

	// Splice with 3 comments (added another reply).
	updatedComments := []github.ReviewComment{
		comment(1, 5, "original question"),
		replyComment(2, 1, "reply from bot", 5),
		replyComment(3, 1, "another reply", 5),
	}
	hl := HighlightDiffFile(github.PullRequestFile{Filename: "test.go", Status: "added", Patch: patch}, "", nil)
	gutterW := TotalGutterWidth(GutterColWidth(hl.DiffLines))
	newThread := RenderSingleThread(updatedComments, 80, LineAdd, spliceTestColors(), false, 0, nil, gutterW)
	SpliceThread(&result, 0, newThread)

	if !strings.Contains(result.Content, "another reply") {
		t.Error("spliced content should contain 'another reply'")
	}
	if !strings.Contains(result.Content, "original question") {
		t.Error("spliced content should still contain 'original question'")
	}
}
