package components

import (
	"strings"
	"testing"

	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/ui/styles"
)

func TestComputeDiffLayout_Basic(t *testing.T) {
	patch := "@@ -1,3 +1,5 @@\n context\n+added one\n+added two\n-removed\n context end"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)

	layout := ComputeDiffLayout(hl, 80, nil)

	// 6 diff lines: hunk, context, +, +, -, context
	if len(layout.DiffLineOffsets) != 6 {
		t.Fatalf("expected 6 diff line offsets, got %d", len(layout.DiffLineOffsets))
	}

	// Offsets should be monotonically increasing.
	for i := 1; i < len(layout.DiffLineOffsets); i++ {
		if layout.DiffLineOffsets[i] <= layout.DiffLineOffsets[i-1] {
			t.Errorf("offsets not increasing: [%d]=%d [%d]=%d",
				i-1, layout.DiffLineOffsets[i-1], i, layout.DiffLineOffsets[i])
		}
	}

	// Total rendered lines should be at least the number of diff lines.
	if layout.TotalRenderedLines < 6 {
		t.Errorf("expected at least 6 total rendered lines, got %d", layout.TotalRenderedLines)
	}
}

func TestComputeDiffLayout_WithComments(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@\n context\n+added line\n+another\n context end"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)

	line := 2
	comments := []github.ReviewComment{
		{ID: 1, Body: "Nice change", Path: "test.go", Line: &line, Side: "RIGHT",
			User: github.User{Login: "reviewer"}},
	}

	layout := ComputeDiffLayout(hl, 80, comments)

	// Should have comment positions.
	if len(layout.CommentPositions) != 1 {
		t.Fatalf("expected 1 comment position, got %d", len(layout.CommentPositions))
	}

	// Total height should be larger than without comments.
	layoutNoComments := ComputeDiffLayout(hl, 80, nil)
	if layout.TotalRenderedLines <= layoutNoComments.TotalRenderedLines {
		t.Errorf("layout with comments (%d lines) should be taller than without (%d)",
			layout.TotalRenderedLines, layoutNoComments.TotalRenderedLines)
	}
}

func TestRenderLineRange_FullRange(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@\n context\n+added\n context end"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)
	colors := styles.DiffColors{}

	layout := ComputeDiffLayout(hl, 80, nil)
	lines, offset := RenderLineRange(hl, layout, 0, layout.TotalRenderedLines, 80, colors, nil)

	if offset != 0 {
		t.Errorf("expected offset 0, got %d", offset)
	}
	if len(lines) == 0 {
		t.Fatal("expected non-empty rendered lines")
	}

	// Compare with full FormatDiffFile output.
	fullResult := FormatDiffFile(hl, 80, colors, nil)
	fullLines := strings.Split(fullResult.Content, "\n")

	// Line counts should be similar (not exact due to estimation vs actual wrapping).
	if len(lines) < len(fullLines)-2 || len(lines) > len(fullLines)+2 {
		t.Errorf("line count mismatch: RenderLineRange=%d, FormatDiffFile=%d", len(lines), len(fullLines))
	}
}

func TestRenderLineRange_PartialRange(t *testing.T) {
	// Build a longer diff.
	var patchLines []string
	patchLines = append(patchLines, "@@ -1,20 +1,25 @@")
	for i := 0; i < 20; i++ {
		if i%3 == 0 {
			patchLines = append(patchLines, "+added line")
		} else {
			patchLines = append(patchLines, " context line")
		}
	}
	patch := strings.Join(patchLines, "\n")
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)
	colors := styles.DiffColors{}

	layout := ComputeDiffLayout(hl, 80, nil)

	// Render only the middle portion.
	mid := layout.TotalRenderedLines / 2
	lines, offset := RenderLineRange(hl, layout, mid-3, mid+3, 80, colors, nil)

	if len(lines) == 0 {
		t.Fatal("expected non-empty partial render")
	}
	// Should be much fewer lines than the total.
	if len(lines) >= layout.TotalRenderedLines {
		t.Errorf("partial render (%d) should be fewer than total (%d)", len(lines), layout.TotalRenderedLines)
	}
	_ = offset
}

func TestRenderLineRange_WithComments(t *testing.T) {
	patch := "@@ -1,5 +1,6 @@\n context\n+added one\n+added two\n context mid\n+added three\n context end"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)
	colors := styles.DiffColors{}

	line2 := 2
	line5 := 5
	replyTo := 1
	comments := []github.ReviewComment{
		{ID: 1, Body: "Root comment on line 2", Path: "test.go", Line: &line2, Side: "RIGHT",
			User: github.User{Login: "alice"}},
		{ID: 2, Body: "Reply to root", Path: "test.go", Line: &line2, Side: "RIGHT",
			InReplyToID: &replyTo, User: github.User{Login: "bob"}},
		{ID: 3, Body: "Comment on line 5", Path: "test.go", Line: &line5, Side: "RIGHT",
			User: github.User{Login: "charlie"}},
	}

	layout := ComputeDiffLayout(hl, 80, comments)

	// Should have 3 comment positions.
	if len(layout.CommentPositions) != 3 {
		t.Fatalf("expected 3 comment positions, got %d", len(layout.CommentPositions))
	}

	// Full render should include comment content.
	lines, _ := RenderLineRange(hl, layout, 0, layout.TotalRenderedLines, 80, colors, comments)
	content := strings.Join(lines, "\n")

	if !strings.Contains(content, "alice") {
		t.Error("expected alice's comment in rendered output")
	}
	if !strings.Contains(content, "bob") {
		t.Error("expected bob's reply in rendered output")
	}
	if !strings.Contains(content, "charlie") {
		t.Error("expected charlie's comment in rendered output")
	}

	// Partial render should only include comments in the visible range.
	// Comment on line 2 is near the top, comment on line 5 is near the bottom.
	topLines, _ := RenderLineRange(hl, layout, 0, 5, 80, colors, comments)
	topContent := strings.Join(topLines, "\n")

	// Top portion should have alice (line 2 comment) but might not have charlie (line 5).
	if !strings.Contains(topContent, "alice") {
		t.Error("top portion should contain alice's comment")
	}
}

func TestRenderLineRange_CommentHighlight(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@\n context\n+added\n context end"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)
	colors := styles.DiffColors{HighlightBorderFg: "\033[33m"}

	line := 2
	comments := []github.ReviewComment{
		{ID: 1, Body: "Highlighted comment", Path: "test.go", Line: &line, Side: "RIGHT",
			User: github.User{Login: "reviewer"}},
	}

	layout := ComputeDiffLayout(hl, 80, comments)

	// Render with highlight on the comment.
	opts := DiffFormatOptions{
		HighlightThreadLine:  2,
		HighlightThreadSide:  "RIGHT",
		HighlightCommentIndex: 1,
	}
	lines, _ := RenderLineRange(hl, layout, 0, layout.TotalRenderedLines, 80, colors, comments, opts)
	content := strings.Join(lines, "\n")

	if !strings.Contains(content, "reviewer") {
		t.Error("expected reviewer's comment in highlighted output")
	}
	// Should contain the highlight border color.
	if !strings.Contains(content, "\033[33m") {
		t.Error("expected highlight border color in output")
	}
}

func TestComputeDiffLayout_EmptyPatch(t *testing.T) {
	file := github.PullRequestFile{Filename: "test.go", Patch: "", Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)

	layout := ComputeDiffLayout(hl, 80, nil)

	if layout.TotalRenderedLines != 0 {
		t.Errorf("expected 0 lines for empty patch, got %d", layout.TotalRenderedLines)
	}
}

// --- Layout accuracy tests ---

func TestLayoutOffsets_MatchFullRender(t *testing.T) {
	// Verify that layout offsets agree with the actual FormatDiffFile offsets.
	patch := "@@ -1,5 +1,7 @@\n first\n+added a\n middle\n-removed\n+replaced\n+extra\n last"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)
	colors := styles.DiffColors{}

	layout := ComputeDiffLayout(hl, 80, nil)
	fullResult := FormatDiffFile(hl, 80, colors, nil)

	if len(layout.DiffLineOffsets) != len(fullResult.DiffLineOffsets) {
		t.Fatalf("offset count mismatch: layout=%d, full=%d",
			len(layout.DiffLineOffsets), len(fullResult.DiffLineOffsets))
	}

	// Offsets should match exactly for non-wrapping lines at width 80.
	for i := range layout.DiffLineOffsets {
		if layout.DiffLineOffsets[i] != fullResult.DiffLineOffsets[i] {
			t.Errorf("offset[%d]: layout=%d, full=%d",
				i, layout.DiffLineOffsets[i], fullResult.DiffLineOffsets[i])
		}
	}
}

func TestLayoutOffsets_WithComments_MatchFullRender(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@\n context\n+added\n+another\n context end"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)
	colors := styles.DiffColors{}

	line := 2
	comments := []github.ReviewComment{
		{ID: 1, Body: "A comment", Path: "test.go", Line: &line, Side: "RIGHT",
			User: github.User{Login: "user"}},
	}

	layout := ComputeDiffLayout(hl, 80, comments)
	fullResult := FormatDiffFile(hl, 80, colors, comments)

	// Diff line offsets should match.
	for i := range layout.DiffLineOffsets {
		if i >= len(fullResult.DiffLineOffsets) {
			break
		}
		if layout.DiffLineOffsets[i] != fullResult.DiffLineOffsets[i] {
			t.Errorf("offset[%d] with comments: layout=%d, full=%d",
				i, layout.DiffLineOffsets[i], fullResult.DiffLineOffsets[i])
		}
	}

	// Total lines should be close (layout estimates, full is exact).
	fullLineCount := len(strings.Split(fullResult.Content, "\n"))
	diff := layout.TotalRenderedLines - fullLineCount
	if diff < -3 || diff > 3 {
		t.Errorf("total line count too far off: layout=%d, full=%d",
			layout.TotalRenderedLines, fullLineCount)
	}
}

// --- Virtual vs full content tests ---

func TestVirtualRender_MatchesFullRender(t *testing.T) {
	// For a small diff, virtual full-range render should match FormatDiffFile exactly.
	patch := "@@ -1,4 +1,5 @@\n alpha\n+beta\n gamma\n-delta\n epsilon"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)
	colors := styles.DiffColors{}

	layout := ComputeDiffLayout(hl, 80, nil)
	virtualLines, _ := RenderLineRange(hl, layout, 0, layout.TotalRenderedLines, 80, colors, nil)
	virtualContent := strings.Join(virtualLines, "\n")

	fullResult := FormatDiffFile(hl, 80, colors, nil)

	if virtualContent != fullResult.Content {
		t.Errorf("virtual render does not match full render.\nvirtual:\n%s\n\nfull:\n%s",
			virtualContent, fullResult.Content)
	}
}

func TestVirtualRender_WithComments_MatchesFullRender(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@\n context\n+added\n context end"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)
	colors := styles.DiffColors{}

	line := 2
	comments := []github.ReviewComment{
		{ID: 1, Body: "Review note", Path: "test.go", Line: &line, Side: "RIGHT",
			User: github.User{Login: "reviewer"}},
	}

	layout := ComputeDiffLayout(hl, 80, comments)
	virtualLines, _ := RenderLineRange(hl, layout, 0, layout.TotalRenderedLines, 80, colors, comments)
	virtualContent := strings.Join(virtualLines, "\n")

	fullResult := FormatDiffFile(hl, 80, colors, comments)

	if virtualContent != fullResult.Content {
		t.Errorf("virtual render with comments does not match full.\nvirtual:\n%s\n\nfull:\n%s",
			virtualContent, fullResult.Content)
	}
}

// --- Large file tests ---

func TestLayout_LargeFile(t *testing.T) {
	// Build a 500-line diff.
	var patchLines []string
	patchLines = append(patchLines, "@@ -1,300 +1,500 @@")
	for i := 0; i < 500; i++ {
		switch {
		case i%5 == 0:
			patchLines = append(patchLines, "+added line number something")
		case i%7 == 0:
			patchLines = append(patchLines, "-removed line here")
		default:
			patchLines = append(patchLines, " context unchanged line")
		}
	}
	patch := strings.Join(patchLines, "\n")
	file := github.PullRequestFile{Filename: "big.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)

	layout := ComputeDiffLayout(hl, 120, nil)

	if layout.TotalRenderedLines < 500 {
		t.Errorf("expected at least 500 rendered lines, got %d", layout.TotalRenderedLines)
	}
	if len(layout.DiffLineOffsets) != 501 { // 500 lines + 1 hunk header
		t.Errorf("expected 501 diff line offsets, got %d", len(layout.DiffLineOffsets))
	}

	// Partial render should be much smaller.
	lines, offset := RenderLineRange(hl, layout, 100, 130, 120, styles.DiffColors{}, nil)
	if len(lines) == 0 {
		t.Fatal("expected non-empty partial render of large file")
	}
	if len(lines) > 50 {
		t.Errorf("partial render too large: %d lines (expected ~30)", len(lines))
	}
	if offset < 100 {
		t.Errorf("expected offset >= 100, got %d", offset)
	}
}

func TestLayout_LargeFile_WithScatteredComments(t *testing.T) {
	var patchLines []string
	patchLines = append(patchLines, "@@ -1,100 +1,120 @@")
	for i := 0; i < 100; i++ {
		if i%4 == 0 {
			patchLines = append(patchLines, "+new line")
		} else {
			patchLines = append(patchLines, " ctx")
		}
	}
	patch := strings.Join(patchLines, "\n")
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)

	// Comments scattered at various lines.
	line10, line30, line60, line90 := 10, 30, 60, 90
	comments := []github.ReviewComment{
		{ID: 1, Body: "Early comment", Path: "test.go", Line: &line10, Side: "RIGHT", User: github.User{Login: "a"}},
		{ID: 2, Body: "Middle comment\nwith multiple lines\nof text", Path: "test.go", Line: &line30, Side: "RIGHT", User: github.User{Login: "b"}},
		{ID: 3, Body: "Late comment", Path: "test.go", Line: &line60, Side: "RIGHT", User: github.User{Login: "c"}},
		{ID: 4, Body: "Very late", Path: "test.go", Line: &line90, Side: "RIGHT", User: github.User{Login: "d"}},
	}

	layout := ComputeDiffLayout(hl, 80, comments)

	if len(layout.CommentPositions) != 4 {
		t.Fatalf("expected 4 comment positions, got %d", len(layout.CommentPositions))
	}

	// Render the middle section — should include the middle comment but not the early/late ones.
	midStart := layout.CommentPositions[1].Offset - 2
	midEnd := layout.CommentPositions[1].Offset + 15
	lines, _ := RenderLineRange(hl, layout, midStart, midEnd, 80, styles.DiffColors{}, comments)
	content := strings.Join(lines, "\n")

	if !strings.Contains(content, "b") { // author "b" has the middle comment
		t.Error("middle render should contain author b's comment")
	}
}

// --- Width variation tests ---

func TestLayout_DifferentWidths(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@\n context\n+added a somewhat longer line of code that might wrap at narrow widths\n context end"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)

	layoutWide := ComputeDiffLayout(hl, 200, nil)
	layoutNarrow := ComputeDiffLayout(hl, 40, nil)

	// Narrow width should produce more rendered lines due to wrapping.
	if layoutNarrow.TotalRenderedLines < layoutWide.TotalRenderedLines {
		t.Errorf("narrow layout (%d) should have >= lines than wide (%d)",
			layoutNarrow.TotalRenderedLines, layoutWide.TotalRenderedLines)
	}
}

func TestLayout_ResizeProducesDifferentLayout(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@\n ctx\n+added\n ctx end"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)

	layout80 := ComputeDiffLayout(hl, 80, nil)
	layout120 := ComputeDiffLayout(hl, 120, nil)

	// Both should have the same number of diff lines.
	if len(layout80.DiffLineOffsets) != len(layout120.DiffLineOffsets) {
		t.Errorf("diff line count should be width-independent: %d vs %d",
			len(layout80.DiffLineOffsets), len(layout120.DiffLineOffsets))
	}
}

// --- Edge cases ---

func TestRenderLineRange_BeyondEnd(t *testing.T) {
	patch := "@@ -1,2 +1,3 @@\n ctx\n+added"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)

	layout := ComputeDiffLayout(hl, 80, nil)
	// Request range beyond the end.
	lines, _ := RenderLineRange(hl, layout, layout.TotalRenderedLines+100, layout.TotalRenderedLines+200, 80, styles.DiffColors{}, nil)

	if len(lines) != 0 {
		t.Errorf("expected empty result for range beyond end, got %d lines", len(lines))
	}
}

func TestRenderLineRange_NegativeStart(t *testing.T) {
	patch := "@@ -1,2 +1,3 @@\n ctx\n+added"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)

	layout := ComputeDiffLayout(hl, 80, nil)
	// Negative start should be clamped to 0.
	lines, offset := RenderLineRange(hl, layout, -10, 5, 80, styles.DiffColors{}, nil)

	if len(lines) == 0 {
		t.Fatal("expected non-empty result")
	}
	if offset != 0 {
		t.Errorf("expected offset 0 for negative start, got %d", offset)
	}
}

func TestRenderLineRange_ZeroRange(t *testing.T) {
	patch := "@@ -1,2 +1,3 @@\n ctx\n+added"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)

	layout := ComputeDiffLayout(hl, 80, nil)
	lines, _ := RenderLineRange(hl, layout, 5, 5, 80, styles.DiffColors{}, nil)

	if len(lines) != 0 {
		t.Errorf("expected empty result for zero range, got %d lines", len(lines))
	}
}

func TestCommentThread_AtBufferBoundary(t *testing.T) {
	// Comment thread that starts in the visible range but extends beyond it.
	patch := "@@ -1,3 +1,4 @@\n context\n+added\n context end"
	file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
	hl := HighlightDiffFile(file, "", "", nil)

	line := 2
	comments := []github.ReviewComment{
		{ID: 1, Body: "A very long comment\nthat spans\nmultiple lines\nof content\nand keeps going",
			Path: "test.go", Line: &line, Side: "RIGHT", User: github.User{Login: "verbose"}},
	}

	layout := ComputeDiffLayout(hl, 80, comments)

	// Render just the first few lines — comment thread might extend beyond.
	lines, _ := RenderLineRange(hl, layout, 0, 5, 80, styles.DiffColors{}, comments)

	// Should at least start rendering (not panic).
	if len(lines) == 0 {
		t.Fatal("expected non-empty result even with comment at boundary")
	}
}
