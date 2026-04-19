package diffviewer

import (
	"regexp"
	"strings"
	"testing"

	"github.com/blakewilliams/ghq/internal/ui/styles"
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
			got := byteToVisual(tt.s, tt.byteOff)
			if got != tt.want {
				t.Errorf("byteToVisual(%q, %d) = %d, want %d", tt.s, tt.byteOff, got, tt.want)
			}
		})
	}
}

func TestHighlightSearchSpans_PlainText(t *testing.T) {
	// Plain inner string (no ANSI): gutter + code
	// Gutter: "   1    2 +" = 11 chars (colW=4, gutterW=11)
	gutter := "   1    2 +"
	code := "func hello() {"
	inner := gutter + code + strings.Repeat(" ", 20) // padded
	raw := "func hello() {"

	pattern := regexp.MustCompile("(?i)hello")
	bgCode := "\033[43m" // simple yellow bg
	resetCode := "\033[0m"
	gutterW := 11

	result := highlightSearchSpans(inner, raw, pattern, gutterW, bgCode, "", resetCode, styles.DiffColors{})

	// "hello" is at positions 5-10 in raw, so visual 16-21 in inner
	// Result should contain the yellow bg wrapping "hello"
	if !strings.Contains(result, bgCode+"hello"+resetCode) {
		t.Errorf("expected yellow bg around 'hello', got: %q", result)
	}

	// Gutter should be unchanged
	if !strings.HasPrefix(result, gutter) {
		t.Errorf("gutter should be unchanged, got prefix: %q", result[:len(gutter)+10])
	}
}

func TestHighlightSearchSpans_WithTabs(t *testing.T) {
	// Content has a tab, but the rendered inner has it expanded to 4 spaces
	// Gutter: "   1    2 +" = 11 chars
	gutter := "   1    2 +"
	code := "    func hello() {" // tab expanded to 4 spaces
	inner := gutter + code + strings.Repeat(" ", 10)
	raw := "\tfunc hello() {" // raw has tab

	pattern := regexp.MustCompile("(?i)hello")
	bgCode := "\033[43m"
	resetCode := "\033[0m"
	gutterW := 11

	result := highlightSearchSpans(inner, raw, pattern, gutterW, bgCode, "", resetCode, styles.DiffColors{})

	// "hello" is at byte offset 6-11 in raw ("\tfunc " = 6 bytes)
	// Visual: tab=4 + "func " = 9, so visual offset 9-14 in code
	// In inner: gutterW(11) + 9 = 20, end = 25
	if !strings.Contains(result, bgCode+"hello"+resetCode) {
		t.Errorf("expected yellow bg around 'hello' with tab expansion, got: %q", result)
	}

	// The "func " before hello should NOT be highlighted
	if strings.Contains(result, bgCode+"func") {
		t.Errorf("func should not be highlighted, got: %q", result)
	}
}

func TestHighlightSearchSpans_WithANSI(t *testing.T) {
	// Simulate a syntax-highlighted inner with ANSI codes
	gutter := "\033[48;2;30;50;30m\033[38;2;100;200;100m   1    2 \033[1m+\033[0m\033[48;2;30;50;30m"
	// "func" in keyword color, " hello" in normal color
	code := "\033[38;2;200;100;100mfunc\033[0m \033[38;2;200;200;200mhello\033[0m() {"
	inner := gutter + code
	raw := "func hello() {"

	pattern := regexp.MustCompile("(?i)hello")
	bgCode := "\033[43m"
	resetCode := "\033[0m"
	gutterW := 11

	result := highlightSearchSpans(inner, raw, pattern, gutterW, bgCode, "", resetCode, styles.DiffColors{})

	// The result should contain the yellow bg code
	if !strings.Contains(result, bgCode) {
		t.Errorf("expected yellow bg code in result, got: %q", result)
	}

	// The highlighted portion should be 5 visual chars starting at position 16
	// (gutterW=11, "func "=5, so "hello" starts at visual col 16)
	// Verify the "func" part is NOT wrapped in yellow
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

	result := highlightSearchSpans(inner, raw, pattern, gutterW, bgCode, "", resetCode, styles.DiffColors{})

	// Should have 3 highlighted "foo" spans
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

	result := highlightSearchSpans(inner, raw, pattern, gutterW, bgCode, "", resetCode, styles.DiffColors{})

	if result != inner {
		t.Errorf("expected unchanged inner when no match, got: %q", result)
	}
	_ = resetCode
}

func TestHighlightSearchSpans_ReplacesExistingBg(t *testing.T) {
	// Simulate a real add-line with add-bg baked into the ANSI.
	// The rendered line has add-bg after every reset.
	addBg := "\033[48;2;30;50;30m"
	yellowBg := "\033[48;2;215;153;33m"
	gutter := addBg + "\033[38;2;100;200;100m   1    2 \033[1m+\033[0m" + addBg
	code := "\033[38;2;200;100;100mfunc\033[0m" + addBg + " \033[38;2;200;200;200mhello\033[0m" + addBg + "() {"
	inner := gutter + code
	raw := "func hello() {"

	pattern := regexp.MustCompile("(?i)hello")
	colors := styles.DiffColors{AddBg: addBg}
	gutterW := 11

	result := highlightSearchSpans(inner, raw, pattern, gutterW, yellowBg, "", addBg, colors)

	// The yellow bg must appear in the result.
	if !strings.Contains(result, yellowBg) {
		t.Errorf("expected yellow bg in result, got: %q", result)
	}

	// The add-bg must NOT appear inside the highlighted match region.
	// Find the highlighted span: it starts with yellowBg and ends where addBg resumes.
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
