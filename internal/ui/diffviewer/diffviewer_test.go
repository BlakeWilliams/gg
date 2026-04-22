package diffviewer

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/blakewilliams/gg/internal/github"
	"github.com/blakewilliams/gg/internal/ui/components"
	"github.com/blakewilliams/gg/internal/ui/styles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestByteToVisual(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		byteOff int
		want    int
	}{
		{"no tabs", "func hello", 5, 5},
		{"leading tab", "\thello", 1, 4},          // tab = 4 spaces
		{"after tab", "\thello", 2, 5},             // 4 + 1
		{"two tabs", "\t\thello", 2, 8},            // 4 + 4
		{"tab then match", "\tfunc hello", 6, 9},   // 4 + "func " = 9
		{"zero offset", "anything", 0, 0},
		{"end of string", "abc", 3, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := components.ByteToVisual(tt.s, tt.byteOff)
			if got != tt.want {
				t.Errorf("ByteToVisual(%q, %d) = %d, want %d", tt.s, tt.byteOff, got, tt.want)
			}
		})
	}
}

func TestHighlightSearchSpans_PlainText(t *testing.T) {
	gutter := "   1    2 +"
	code := "func hello() {"
	inner := gutter + code + strings.Repeat(" ", 20)
	raw := "func hello() {"

	pattern := regexp.MustCompile("(?i)hello")
	bgCode := "\033[43m"
	resetCode := "\033[0m"
	gutterW := 11

	result := components.HighlightSearchSpans(inner, raw, pattern, gutterW, bgCode, "", resetCode)

	if !strings.Contains(result, bgCode+"hello"+resetCode) {
		t.Errorf("expected yellow bg around 'hello', got: %q", result)
	}

	if !strings.HasPrefix(result, gutter) {
		t.Errorf("gutter should be unchanged, got prefix: %q", result[:len(gutter)+10])
	}
}

func TestHighlightSearchSpans_WithTabs(t *testing.T) {
	gutter := "   1    2 +"
	code := "    func hello() {"
	inner := gutter + code + strings.Repeat(" ", 10)
	raw := "\tfunc hello() {"

	pattern := regexp.MustCompile("(?i)hello")
	bgCode := "\033[43m"
	resetCode := "\033[0m"
	gutterW := 11

	result := components.HighlightSearchSpans(inner, raw, pattern, gutterW, bgCode, "", resetCode)

	if !strings.Contains(result, bgCode+"hello"+resetCode) {
		t.Errorf("expected yellow bg around 'hello' with tab expansion, got: %q", result)
	}

	if strings.Contains(result, bgCode+"func") {
		t.Errorf("func should not be highlighted, got: %q", result)
	}
}

func TestHighlightSearchSpans_WithANSI(t *testing.T) {
	gutter := "\033[48;2;30;50;30m\033[38;2;100;200;100m   1    2 \033[1m+\033[0m\033[48;2;30;50;30m"
	code := "\033[38;2;200;100;100mfunc\033[0m \033[38;2;200;200;200mhello\033[0m() {"
	inner := gutter + code
	raw := "func hello() {"

	pattern := regexp.MustCompile("(?i)hello")
	bgCode := "\033[43m"
	resetCode := "\033[0m"
	gutterW := 11

	result := components.HighlightSearchSpans(inner, raw, pattern, gutterW, bgCode, "", resetCode)

	if !strings.Contains(result, bgCode) {
		t.Errorf("expected yellow bg code in result, got: %q", result)
	}

	beforeMatch := result[:strings.Index(result, bgCode)]
	if strings.Contains(beforeMatch, "hello") {
		t.Errorf("hello should not appear before the bg code")
	}
}

func TestHighlightSearchSpans_MultipleMatches(t *testing.T) {
	gutter := "   1    2 +"
	code := "foo bar foo baz foo"
	inner := gutter + code + strings.Repeat(" ", 10)
	raw := "foo bar foo baz foo"

	pattern := regexp.MustCompile("foo")
	bgCode := "\033[43m"
	resetCode := "\033[0m"
	gutterW := 11

	result := components.HighlightSearchSpans(inner, raw, pattern, gutterW, bgCode, "", resetCode)

	count := strings.Count(result, bgCode+"foo"+resetCode)
	if count != 3 {
		t.Errorf("expected 3 highlighted 'foo' spans, got %d in: %q", count, result)
	}
}

func TestHighlightSearchSpans_NoMatch(t *testing.T) {
	inner := "   1    2 +func hello() {"
	raw := "func hello() {"

	pattern := regexp.MustCompile("zzzzz")
	bgCode := "\033[43m"
	resetCode := "\033[0m"
	gutterW := 11

	result := components.HighlightSearchSpans(inner, raw, pattern, gutterW, bgCode, "", resetCode)

	if result != inner {
		t.Errorf("expected unchanged inner when no match, got: %q", result)
	}
	_ = resetCode
}

func TestExpandHunkInner_LineNumbers(t *testing.T) {
	// Build a scenario with two hunks separated by a gap.
	// First hunk covers lines 5-11, second covers 50-56.
	// Expanding the second hunk should reveal context lines from 12..49
	// (or a subset). Verify the last expanded line has NewLineNo = gapNewEnd
	// and the first original line has NewLineNo = hunkInfo.NewStart.
	patch := "@@ -5,7 +5,7 @@ func init()\n" +
		" line5\n" +
		" line6\n" +
		" line7\n" +
		"-old8\n" +
		"+new8\n" +
		" line9\n" +
		" line10\n" +
		" line11\n" +
		"@@ -50,7 +50,7 @@ func main()\n" +
		" line50\n" +
		" line51\n" +
		" line52\n" +
		"-old53\n" +
		"+new53\n" +
		" line54\n" +
		" line55\n" +
		" line56"

	diffLines := components.ParsePatchLines(patch)

	// Build hlLines (60 lines, simulating a file with 60 lines).
	hlLines := make([]string, 60)
	for i := range hlLines {
		hlLines[i] = fmt.Sprintf("file_line_%d", i+1)
	}

	d := &DiffViewer{
		Files: []github.PullRequestFile{{Filename: "test.go"}},
		HighlightedFiles: []components.HighlightedDiff{{
			File:     github.PullRequestFile{Filename: "test.go"},
			DiffLines: diffLines,
			HlLines:  hlLines,
			Filename: "test.go",
		}},
		FileDiffs: [][]components.DiffLine{diffLines},
	}

	// Find the second hunk header index.
	hunkIdx := -1
	for i, dl := range diffLines {
		if dl.Type == components.LineHunk && strings.Contains(dl.Content, "+50") {
			hunkIdx = i
			break
		}
	}
	if hunkIdx < 0 {
		t.Fatal("could not find second hunk header")
	}

	// Expand 5 lines.
	ok := d.expandHunkInner(0, hunkIdx, 5)
	if !ok {
		t.Fatal("expandHunkInner returned false")
	}

	result := d.HighlightedFiles[0].DiffLines

	// Find where the expanded lines are: right after the updated hunk header.
	// The hunk header should still be at hunkIdx.
	if result[hunkIdx].Type != components.LineHunk {
		t.Fatalf("expected hunk header at %d, got type %d", hunkIdx, result[hunkIdx].Type)
	}

	// The 5 expanded lines should be at hunkIdx+1..hunkIdx+5.
	// They should have NewLineNo = 45,46,47,48,49 (the last 5 lines of gap 12..49).
	for i := 0; i < 5; i++ {
		dl := result[hunkIdx+1+i]
		expectedNew := 45 + i
		if dl.Type != components.LineContext {
			t.Errorf("expanded line %d: expected LineContext, got %d", i, dl.Type)
		}
		if dl.NewLineNo != expectedNew {
			t.Errorf("expanded line %d: expected NewLineNo=%d, got %d", i, expectedNew, dl.NewLineNo)
		}
		// Content should come from hlLines[NewLineNo-1].
		expectedContent := fmt.Sprintf("file_line_%d", expectedNew)
		if dl.Content != expectedContent {
			t.Errorf("expanded line %d: expected Content=%q, got %q", i, expectedContent, dl.Content)
		}
	}

	// The first original content line should be at hunkIdx+6 with NewLineNo=50.
	firstOrig := result[hunkIdx+6]
	if firstOrig.NewLineNo != 50 {
		t.Errorf("first original line: expected NewLineNo=50, got %d", firstOrig.NewLineNo)
	}
	if firstOrig.Content != "line50" {
		t.Errorf("first original line: expected Content=%q, got %q", "line50", firstOrig.Content)
	}

	// Verify no duplication: last expanded and first original should differ.
	lastExpanded := result[hunkIdx+5]
	if lastExpanded.NewLineNo == firstOrig.NewLineNo {
		t.Errorf("DUPLICATION BUG: last expanded NewLineNo=%d equals first original NewLineNo=%d",
			lastExpanded.NewLineNo, firstOrig.NewLineNo)
	}
	if lastExpanded.Content == firstOrig.Content {
		t.Errorf("DUPLICATION BUG: last expanded Content=%q equals first original Content=%q",
			lastExpanded.Content, firstOrig.Content)
	}
}

func TestExpandHunkInner_FirstHunk(t *testing.T) {
	// Test expanding the first hunk in a file (expanding upward from line 10).
	patch := "@@ -10,5 +10,5 @@ func foo()\n" +
		" line10\n" +
		" line11\n" +
		"-old12\n" +
		"+new12\n" +
		" line13\n" +
		" line14"

	diffLines := components.ParsePatchLines(patch)

	hlLines := make([]string, 20)
	for i := range hlLines {
		hlLines[i] = fmt.Sprintf("file_line_%d", i+1)
	}

	d := &DiffViewer{
		Files: []github.PullRequestFile{{Filename: "test.go"}},
		HighlightedFiles: []components.HighlightedDiff{{
			File:     github.PullRequestFile{Filename: "test.go"},
			DiffLines: diffLines,
			HlLines:  hlLines,
			Filename: "test.go",
		}},
		FileDiffs: [][]components.DiffLine{diffLines},
	}

	// Expand the only hunk (index 0) by 5 lines.
	ok := d.expandHunkInner(0, 0, 5)
	if !ok {
		t.Fatal("expandHunkInner returned false")
	}

	result := d.HighlightedFiles[0].DiffLines

	// Gap is 1..9 (9 lines). Reveal 5 from the bottom: 5,6,7,8,9.
	// Hunk header should still exist at index 0 (since we didn't reveal all).
	if result[0].Type != components.LineHunk {
		t.Fatalf("expected hunk header at 0, got type %d", result[0].Type)
	}

	// 5 expanded lines at index 1..5.
	for i := 0; i < 5; i++ {
		dl := result[1+i]
		expectedNew := 5 + i
		if dl.NewLineNo != expectedNew {
			t.Errorf("expanded line %d: expected NewLineNo=%d, got %d", i, expectedNew, dl.NewLineNo)
		}
		expectedContent := fmt.Sprintf("file_line_%d", expectedNew)
		if dl.Content != expectedContent {
			t.Errorf("expanded line %d: expected Content=%q, got %q", i, expectedContent, dl.Content)
		}
	}

	// First original line at index 6: NewLineNo=10.
	firstOrig := result[6]
	if firstOrig.NewLineNo != 10 {
		t.Errorf("first original: expected NewLineNo=10, got %d", firstOrig.NewLineNo)
	}

	// Verify last expanded (NewLineNo=9) != first original (NewLineNo=10).
	lastExpanded := result[5]
	if lastExpanded.NewLineNo == firstOrig.NewLineNo {
		t.Errorf("DUPLICATION BUG: last expanded=%d equals first original=%d",
			lastExpanded.NewLineNo, firstOrig.NewLineNo)
	}
}

func TestExpandHunkInner_RenderNoDuplication(t *testing.T) {
	// Verify that after expansion, FormatDiffLinesFromHL renders different
	// content for the last expanded line and the first original content line.
	patch := "@@ -10,5 +10,5 @@ func foo()\n" +
		" line10_content\n" +
		" line11_content\n" +
		"-old12\n" +
		"+new12\n" +
		" line13_content\n" +
		" line14_content"

	diffLines := components.ParsePatchLines(patch)

	// Build hlLines where each line has distinct content.
	hlLines := make([]string, 20)
	for i := range hlLines {
		hlLines[i] = fmt.Sprintf("highlighted_line_%d", i+1)
	}

	d := &DiffViewer{
		Files: []github.PullRequestFile{{Filename: "test.go"}},
		HighlightedFiles: []components.HighlightedDiff{{
			File:     github.PullRequestFile{Filename: "test.go"},
			DiffLines: diffLines,
			HlLines:  hlLines,
			Filename: "test.go",
		}},
		FileDiffs: [][]components.DiffLine{diffLines},
	}

	ok := d.expandHunkInner(0, 0, 5)
	if !ok {
		t.Fatal("expandHunkInner returned false")
	}

	expanded := d.HighlightedFiles[0].DiffLines

	// Format the expanded DiffLines.
	fmtLines := make([]components.DiffLine, len(expanded))
	copy(fmtLines, expanded)
	colW := components.GutterColWidth(fmtLines)
	components.FormatDiffLinesFromHL(fmtLines, hlLines, nil, "test.go", 120, styles.DiffColors{}, colW)

	// Find the boundary: last expanded context line and first original content line.
	// Hunk was at index 0, expanded 5 lines at 1..5, first original at 6.
	lastExpIdx := 5
	firstOrigIdx := 6

	if fmtLines[lastExpIdx].Rendered == fmtLines[firstOrigIdx].Rendered {
		t.Errorf("RENDERING DUPLICATION: last expanded line rendered same as first original")
		t.Logf("last expanded [%d]: NewLineNo=%d Rendered=%q",
			lastExpIdx, fmtLines[lastExpIdx].NewLineNo, fmtLines[lastExpIdx].Rendered)
		t.Logf("first original [%d]: NewLineNo=%d Rendered=%q",
			firstOrigIdx, fmtLines[firstOrigIdx].NewLineNo, fmtLines[firstOrigIdx].Rendered)
	}

	// Also verify the content is from the expected hlLines index.
	if !strings.Contains(fmtLines[lastExpIdx].Rendered, "highlighted_line_9") {
		t.Errorf("last expanded should contain 'highlighted_line_9', got: %q", fmtLines[lastExpIdx].Rendered)
	}
	if !strings.Contains(fmtLines[firstOrigIdx].Rendered, "highlighted_line_10") {
		t.Errorf("first original should contain 'highlighted_line_10', got: %q", fmtLines[firstOrigIdx].Rendered)
	}
}

func TestExpandHunkInner_RepeatedExpansion(t *testing.T) {
	// Expand the same hunk twice and verify line numbers accumulate correctly.
	patch := "@@ -20,5 +20,5 @@ func bar()\n" +
		" line20\n" +
		" line21\n" +
		"-old22\n" +
		"+new22\n" +
		" line23\n" +
		" line24"

	diffLines := components.ParsePatchLines(patch)

	hlLines := make([]string, 30)
	for i := range hlLines {
		hlLines[i] = fmt.Sprintf("file_line_%d", i+1)
	}

	d := &DiffViewer{
		Files: []github.PullRequestFile{{Filename: "test.go"}},
		HighlightedFiles: []components.HighlightedDiff{{
			File:      github.PullRequestFile{Filename: "test.go"},
			DiffLines: diffLines,
			HlLines:   hlLines,
			Filename:  "test.go",
		}},
		FileDiffs: [][]components.DiffLine{diffLines},
	}

	// First expand: 5 lines from gap 1..19. Reveals 15,16,17,18,19.
	ok := d.expandHunkInner(0, 0, 5)
	require.True(t, ok)

	result := d.HighlightedFiles[0].DiffLines
	require.Equal(t, components.LineHunk, result[0].Type, "hunk header should remain")
	assert.Equal(t, 15, result[1].NewLineNo, "first revealed line should be 15")
	assert.Equal(t, 19, result[5].NewLineNo, "last revealed line should be 19")

	// Second expand: 5 more lines. Remaining gap is 1..14. Reveals 10,11,12,13,14.
	ok = d.expandHunkInner(0, 0, 5)
	require.True(t, ok)

	result = d.HighlightedFiles[0].DiffLines
	require.Equal(t, components.LineHunk, result[0].Type, "hunk header should remain after 2nd expand")
	assert.Equal(t, 10, result[1].NewLineNo, "first revealed line after 2nd expand should be 10")
	assert.Equal(t, 14, result[5].NewLineNo, "last of 2nd batch should be 14")
	assert.Equal(t, 15, result[6].NewLineNo, "first of 1st batch should follow")
	assert.Equal(t, 19, result[10].NewLineNo, "last of 1st batch should be 19")
	assert.Equal(t, 20, result[11].NewLineNo, "original content should start at 20")

	// Verify tracked total.
	assert.Equal(t, 10, d.ExpandedHunks["test.go"][components.HunkInfo{OldStart: 20, NewStart: 20}])
}

func TestExpandHunkInner_FullGapRemoval(t *testing.T) {
	// When n >= totalGap, the hunk header should be removed entirely.
	patch := "@@ -5,3 +5,3 @@ func init()\n" +
		" line5\n" +
		"-old6\n" +
		"+new6\n" +
		" line7\n" +
		"@@ -10,3 +10,3 @@ func main()\n" +
		" line10\n" +
		"-old11\n" +
		"+new11\n" +
		" line12"

	diffLines := components.ParsePatchLines(patch)

	hlLines := make([]string, 15)
	for i := range hlLines {
		hlLines[i] = fmt.Sprintf("file_line_%d", i+1)
	}

	d := &DiffViewer{
		Files: []github.PullRequestFile{{Filename: "test.go"}},
		HighlightedFiles: []components.HighlightedDiff{{
			File:      github.PullRequestFile{Filename: "test.go"},
			DiffLines: diffLines,
			HlLines:   hlLines,
			Filename:  "test.go",
		}},
		FileDiffs: [][]components.DiffLine{diffLines},
	}

	// Find the second hunk header.
	hunkIdx := -1
	for i, dl := range diffLines {
		if dl.Type == components.LineHunk && strings.Contains(dl.Content, "+10") {
			hunkIdx = i
			break
		}
	}
	require.True(t, hunkIdx > 0, "should find second hunk header")

	// Gap between hunks is lines 8..9 (2 lines). Expand by 100 to reveal all.
	ok := d.expandHunkInner(0, hunkIdx, 100)
	require.True(t, ok)

	result := d.HighlightedFiles[0].DiffLines

	// The second hunk header should be gone — no LineHunk after the first hunk's content.
	for i := hunkIdx; i < len(result); i++ {
		assert.NotEqual(t, components.LineHunk, result[i].Type,
			"hunk header at index %d should have been removed", i)
	}

	// Verify the gap lines (8, 9) are now context lines.
	assert.Equal(t, 8, result[hunkIdx].NewLineNo)
	assert.Equal(t, "file_line_8", result[hunkIdx].Content)
	assert.Equal(t, 9, result[hunkIdx+1].NewLineNo)
	assert.Equal(t, "file_line_9", result[hunkIdx+1].Content)
	// Original content follows.
	assert.Equal(t, 10, result[hunkIdx+2].NewLineNo)
}

func TestUpdateHunkHeader_PreservesTrailingContext(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		oldStart int
		newStart int
		extra    int
		want     string
	}{
		{
			name:     "preserves function name",
			header:   "@@ -50,7 +50,7 @@ func main() {",
			oldStart: 45,
			newStart: 45,
			extra:    5,
			want:     "@@ -45,12 +45,12 @@ func main() {",
		},
		{
			name:     "preserves empty trailing",
			header:   "@@ -10,3 +10,3 @@",
			oldStart: 5,
			newStart: 5,
			extra:    5,
			want:     "@@ -5,8 +5,8 @@",
		},
		{
			name:     "preserves trailing with space",
			header:   "@@ -30,4 +30,4 @@ type Foo struct {",
			oldStart: 25,
			newStart: 25,
			extra:    5,
			want:     "@@ -25,9 +25,9 @@ type Foo struct {",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateHunkHeader(tt.header, tt.oldStart, tt.newStart, tt.extra)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRewriteRange(t *testing.T) {
	tests := []struct {
		name   string
		r      string
		prefix string
		start  int
		extra  int
		want   string
	}{
		{
			name:   "with comma",
			r:      "-50,7",
			prefix: "-",
			start:  45,
			extra:  5,
			want:   "-45,12",
		},
		{
			name:   "without comma",
			r:      "+10",
			prefix: "+",
			start:  5,
			extra:  3,
			want:   "+5,4",
		},
		{
			name:   "zero extra",
			r:      "-20,10",
			prefix: "-",
			start:  20,
			extra:  0,
			want:   "-20,10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteRange(tt.r, tt.prefix, tt.start, tt.extra)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHighlightSearchSpans_ReplacesExistingBg(t *testing.T) {
	addBg := "\033[48;2;30;50;30m"
	yellowBg := "\033[48;2;215;153;33m"
	gutter := addBg + "\033[38;2;100;200;100m   1    2 \033[1m+\033[0m" + addBg
	code := "\033[38;2;200;100;100mfunc\033[0m" + addBg + " \033[38;2;200;200;200mhello\033[0m" + addBg + "() {"
	inner := gutter + code
	raw := "func hello() {"

	pattern := regexp.MustCompile("(?i)hello")
	gutterW := 11

	result := components.HighlightSearchSpans(inner, raw, pattern, gutterW, yellowBg, "", addBg)

	if !strings.Contains(result, yellowBg) {
		t.Errorf("expected yellow bg in result, got: %q", result)
	}

	hlStart := strings.Index(result, yellowBg)
	hlEnd := strings.Index(result[hlStart+len(yellowBg):], addBg)
	if hlEnd < 0 {
		t.Fatalf("could not find restore-bg after highlight in: %q", result)
	}
	hlRegion := result[hlStart : hlStart+len(yellowBg)+hlEnd]
	if strings.Contains(hlRegion[len(yellowBg):], addBg) {
		t.Errorf("add-bg should not appear inside highlighted match: %q", hlRegion)
	}
}
