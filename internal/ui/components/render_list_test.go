package components

import (
	"strings"
	"testing"
	"time"

	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/ui/styles"
)

func testColors() styles.DiffColors {
	return styles.DiffColors{
		BorderFg:          "\033[90m",
		HighlightBorderFg: "\033[33m",
	}
}

func intPtr(i int) *int { return &i }

func makeComment(id int, body, path string, line int, side string, replyTo *int) github.ReviewComment {
	return github.ReviewComment{
		ID:           id,
		Body:         body,
		Path:         path,
		Line:         &line,
		OriginalLine: &line,
		Side:         side,
		InReplyToID:  replyTo,
		User:         github.User{Login: "testuser"},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

func makeDiffLineItem(idx int, lt LineType, content string) *DiffLineItem {
	dl := &DiffLine{
		Type:      lt,
		NewLineNo: idx + 1,
		OldLineNo: idx + 1,
		Content:   content,
		Rendered:  content, // simplified: no ANSI for tests
	}
	return NewDiffLineItem(idx, dl)
}

func makeThreadItem(diffLineIdx int, side string, line int, bodies ...string) *CommentThreadItem {
	var comments []github.ReviewComment
	for _, body := range bodies {
		comments = append(comments, github.ReviewComment{
			User: github.User{Login: "testuser"},
			Body: body,
		})
	}
	return NewCommentThreadItem(diffLineIdx, side, line, comments, LineAdd)
}

func testRC() RenderContext {
	return RenderContext{
		Width:  80,
		Colors: testColors(),
		ColW:   4,
	}
}

func TestFileRenderList_InsertAfterDiffLine(t *testing.T) {
	list := &FileRenderList{
		Items: []Renderable{
			makeDiffLineItem(0, LineAdd, "line 0"),
			makeDiffLineItem(1, LineAdd, "line 1"),
			makeDiffLineItem(2, LineAdd, "line 2"),
		},
	}

	thread := makeThreadItem(1, "RIGHT", 2, "nice code")
	list.InsertAfterDiffLine(1, thread)

	if len(list.Items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(list.Items))
	}
	s, l := list.Items[2].ThreadKey()
	if s != "RIGHT" || l != 2 {
		t.Errorf("thread has wrong position: side=%s line=%d", s, l)
	}
	// Diff line 2 should now be at index 3
	if !list.Items[3].IsDiffLine() || list.Items[3].DiffIdx() != 2 {
		t.Errorf("expected item 3 to be diff line 2")
	}
	if !list.dirty {
		t.Error("expected dirty flag to be set")
	}
}

func TestFileRenderList_InsertSkipsExistingThreads(t *testing.T) {
	existing := makeThreadItem(1, "RIGHT", 2, "existing comment")
	list := &FileRenderList{
		Items: []Renderable{
			makeDiffLineItem(0, LineAdd, "line 0"),
			makeDiffLineItem(1, LineAdd, "line 1"),
			existing,
			makeDiffLineItem(2, LineAdd, "line 2"),
		},
	}

	newThread := makeThreadItem(1, "LEFT", 2, "left side comment")
	list.InsertAfterDiffLine(1, newThread)

	if len(list.Items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(list.Items))
	}
	s, _ := list.Items[3].ThreadKey()
	if s != "LEFT" {
		t.Errorf("new thread at wrong position: side=%s", s)
	}
}

func TestFileRenderList_ReplaceThread(t *testing.T) {
	list := &FileRenderList{
		Items: []Renderable{
			makeDiffLineItem(0, LineAdd, "line 0"),
			makeThreadItem(0, "RIGHT", 1, "original"),
			makeDiffLineItem(1, LineAdd, "line 1"),
		},
	}

	replacement := makeThreadItem(0, "RIGHT", 1, "original", "reply")
	ok := list.ReplaceThread("RIGHT", 1, replacement)

	if !ok {
		t.Fatal("ReplaceThread returned false")
	}
	ct := list.Items[1].(*CommentThreadItem)
	if len(ct.Comments) != 2 {
		t.Errorf("expected 2 comments, got %d", len(ct.Comments))
	}
	if !list.dirty {
		t.Error("expected dirty flag")
	}
}

func TestFileRenderList_ReplaceThread_NotFound(t *testing.T) {
	list := &FileRenderList{
		Items: []Renderable{
			makeDiffLineItem(0, LineAdd, "line 0"),
		},
	}

	ok := list.ReplaceThread("RIGHT", 99, makeThreadItem(0, "RIGHT", 99, "hello"))
	if ok {
		t.Error("expected ReplaceThread to return false for missing thread")
	}
}

func TestFileRenderList_RemoveThread(t *testing.T) {
	list := &FileRenderList{
		Items: []Renderable{
			makeDiffLineItem(0, LineAdd, "line 0"),
			makeThreadItem(0, "RIGHT", 1, "to remove"),
			makeDiffLineItem(1, LineAdd, "line 1"),
		},
	}

	ok := list.RemoveThread("RIGHT", 1)
	if !ok {
		t.Fatal("RemoveThread returned false")
	}
	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list.Items))
	}
	for _, item := range list.Items {
		if !item.IsDiffLine() {
			t.Error("expected all remaining items to be diff lines")
		}
	}
}

func TestFileRenderList_DiffLineOffset(t *testing.T) {
	list := &FileRenderList{
		Items: []Renderable{
			makeDiffLineItem(0, LineAdd, "line 0"),
			makeDiffLineItem(1, LineAdd, "line 1"),
			makeDiffLineItem(2, LineAdd, "line 2"),
		},
	}

	rc := testRC()

	if got := list.DiffLineOffset(0, rc); got != 0 {
		t.Errorf("offset for line 0: got %d, want 0", got)
	}
	if got := list.DiffLineOffset(1, rc); got != 1 {
		t.Errorf("offset for line 1: got %d, want 1", got)
	}
	if got := list.DiffLineOffset(2, rc); got != 2 {
		t.Errorf("offset for line 2: got %d, want 2", got)
	}
}

func TestFileRenderList_DiffLineOffsets_Slice(t *testing.T) {
	list := &FileRenderList{
		Items: []Renderable{
			makeDiffLineItem(0, LineAdd, "line 0"),
			makeDiffLineItem(1, LineAdd, "line 1"),
			makeDiffLineItem(2, LineAdd, "line 2"),
		},
	}

	rc := testRC()
	offsets := list.DiffLineOffsets(3, rc)
	if len(offsets) != 3 {
		t.Fatalf("expected 3 offsets, got %d", len(offsets))
	}
	for i, want := range []int{0, 1, 2} {
		if offsets[i] != want {
			t.Errorf("offsets[%d] = %d, want %d", i, offsets[i], want)
		}
	}
}

func TestFileRenderList_String(t *testing.T) {
	list := &FileRenderList{
		Items: []Renderable{
			makeDiffLineItem(0, LineAdd, "hello"),
			makeDiffLineItem(1, LineAdd, "world"),
		},
	}

	rc := testRC()
	result := list.String(rc)

	if !strings.Contains(result, "hello") {
		t.Error("result missing 'hello'")
	}
	if !strings.Contains(result, "world") {
		t.Error("result missing 'world'")
	}

	// Second call should be cached
	result2 := list.String(rc)
	if result != result2 {
		t.Error("second call returned different result (caching broken)")
	}
}

func TestFileRenderList_StringCacheInvalidation(t *testing.T) {
	list := &FileRenderList{
		Items: []Renderable{
			makeDiffLineItem(0, LineAdd, "hello"),
		},
	}

	rc := testRC()
	result1 := list.String(rc)

	list.InsertAfterDiffLine(0, makeDiffLineItem(1, LineAdd, "world"))
	result2 := list.String(rc)

	if result1 == result2 {
		t.Error("string cache not invalidated after insert")
	}
}

func TestFileRenderList_FindThread(t *testing.T) {
	list := &FileRenderList{
		Items: []Renderable{
			makeDiffLineItem(0, LineAdd, "line 0"),
			makeThreadItem(0, "RIGHT", 1, "comment"),
			makeDiffLineItem(1, LineAdd, "line 1"),
		},
	}

	if idx := list.FindThread("RIGHT", 1); idx != 1 {
		t.Errorf("FindThread RIGHT/1: got %d, want 1", idx)
	}
	if idx := list.FindThread("LEFT", 1); idx != -1 {
		t.Errorf("FindThread LEFT/1: got %d, want -1", idx)
	}
}

func TestFileRenderList_InvalidateAll(t *testing.T) {
	list := &FileRenderList{
		Items: []Renderable{
			makeDiffLineItem(0, LineAdd, "hello"),
		},
	}

	rc := testRC()
	list.String(rc)

	list.InvalidateAll()

	if !list.dirty {
		t.Error("expected dirty after InvalidateAll")
	}
}

func TestBuildRenderList_OutputStructure(t *testing.T) {
	// Build a realistic highlighted diff with comments and verify the render
	// list produces structurally valid output (offsets, comment positions, etc.).
	patch := `@@ -1,3 +1,4 @@
 context line 1
+added line 2
 context line 3
 context line 4`

	diffLines := ParsePatchLines(patch)
	colW := GutterColWidth(diffLines)

	// Pre-format lines.
	formattedLines := make([]DiffLine, len(diffLines))
	copy(formattedLines, diffLines)
	colors := testColors()
	formatDiffLinesFromHL(formattedLines, nil, nil, "test.go", 80, colors, colW)

	comments := []github.ReviewComment{
		{
			ID:   1,
			Body: "looks good",
			User: github.User{Login: "alice"},
			Side: "RIGHT",
			Line: intPtr(2),
			OriginalLine: intPtr(2),
			Path: "test.go",
		},
	}

	// Build render list.
	list := BuildRenderList(formattedLines, comments)
	rc := RenderContext{Width: 80, Colors: colors, ColW: colW}
	result := list.String(rc)

	// Verify output is non-empty and has expected number of lines.
	if result == "" {
		t.Fatal("render list produced empty output")
	}

	resultLines := strings.Split(result, "\n")
	// Should have 4 diff lines + comment thread lines.
	if len(resultLines) < 4 {
		t.Errorf("expected at least 4 lines, got %d", len(resultLines))
	}

	// Verify DiffLineOffsets: should have one entry per diff line.
	offsets := list.DiffLineOffsets(len(diffLines), rc)
	if len(offsets) != len(diffLines) {
		t.Fatalf("offset count mismatch: got %d, want %d", len(offsets), len(diffLines))
	}

	// Offsets should be non-decreasing.
	for i := 1; i < len(offsets); i++ {
		if offsets[i] < offsets[i-1] {
			t.Errorf("offsets not non-decreasing: [%d]=%d [%d]=%d", i-1, offsets[i-1], i, offsets[i])
		}
	}

	// Comment positions should exist and point to valid lines.
	positions := list.CommentPositions(rc)
	if len(positions) != 1 {
		t.Fatalf("expected 1 comment position, got %d", len(positions))
	}
	if positions[0].Line != 2 || positions[0].Side != "RIGHT" {
		t.Errorf("unexpected position: line=%d side=%s", positions[0].Line, positions[0].Side)
	}
	if positions[0].Offset <= 0 || positions[0].Offset >= len(resultLines) {
		t.Errorf("comment offset %d out of range (content has %d lines)", positions[0].Offset, len(resultLines))
	}
}
