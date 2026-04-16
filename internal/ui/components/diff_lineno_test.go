package components

import (
"strings"
"testing"

"github.com/blakewilliams/ghq/internal/github"
"github.com/blakewilliams/ghq/internal/ui/styles"
)

// TestHighlightLineNumberMismatch reproduces a bug where the rendered content
// doesn't match the diff when the highlighted file has different line numbers
// than what the diff expects.
func TestHighlightLineNumberMismatch(t *testing.T) {
// Patch that expects:
// - old line 11 = "internal/git" (context)
// - new line 11 = "internal/git" (context)  
// - new line 14 = "diffviewer" (added)
// - new line 19 = "spinner" (added)
// - old line 18, new line 20 = "textarea" (context)
// - old line 19 = "viewport" (deleted)
patch := `@@ -11,12 +11,13 @@ import (
 "internal/git"
 "internal/github"
 "internal/components"
+"internal/diffviewer"
 "internal/picker"
 "internal/styles"
 "internal/uictx"
 "internal/watcher"
+"charm.land/spinner"
 "charm.land/textarea"
-"charm.land/viewport"
 "charm.land/bubbletea"
 "charm.land/lipgloss"`

// New file content that MATCHES the diff's new line numbers
correctNewContent := `package main

import (
"fmt"
"os"
"strings"
"time"

)

import (
"internal/git"
"internal/github"
"internal/components"
"internal/diffviewer"
"internal/picker"
"internal/styles"
"internal/uictx"
"internal/watcher"
"charm.land/spinner"
"charm.land/textarea"
"charm.land/bubbletea"
"charm.land/lipgloss"
)
`

// New file content with EXTRA lines, causing line number mismatch
// This simulates uncommitted debug code shifting everything
wrongNewContent := `package main

import (
"fmt"
"os"
"strings"
"time"

)

import (
"internal/git"
"internal/github"
"internal/components"
"internal/diffviewer"
"internal/picker"
"internal/styles"
"internal/uictx"
"internal/watcher"
"charm.land/spinner"
"charm.land/viewport"
"charm.land/textarea"
"charm.land/bubbletea"
"charm.land/lipgloss"
)
`

// Old file content (merge-base)
oldContent := `package main

import (
"fmt"
"os"
"strings"
"time"

)

import (
"internal/git"
"internal/github"
"internal/components"
"internal/picker"
"internal/styles"
"internal/uictx"
"internal/watcher"
"charm.land/textarea"
"charm.land/viewport"
"charm.land/bubbletea"
"charm.land/lipgloss"
)
`

file := github.PullRequestFile{
Filename: "test.go",
Patch:    patch,
Status:   "modified",
}

t.Run("correct line numbers", func(t *testing.T) {
hl := HighlightDiffFile(file, correctNewContent, oldContent, nil)
result := FormatDiffFile(hl, 120, styles.DiffColors{}, nil)

// Find the context line that should be "textarea"
// It's old=18, new=20 in the diff
for i, dl := range hl.DiffLines {
if dl.Type == LineContext && dl.OldLineNo == 18 && dl.NewLineNo == 20 {
if !strings.Contains(dl.Content, "textarea") {
t.Errorf("DiffLine[%d] content should be 'textarea', got %q", i, dl.Content)
}
// Check the rendered line contains textarea
lines := strings.Split(result.Content, "\n")
found := false
for _, line := range lines {
if strings.Contains(line, "18") && strings.Contains(line, "20") {
if !strings.Contains(line, "textarea") {
t.Errorf("Rendered line for old=18 new=20 should contain 'textarea', got: %s", line)
}
found = true
break
}
}
if !found {
t.Error("Could not find rendered line with old=18 new=20")
}
}
}
})

t.Run("wrong line numbers shows bug", func(t *testing.T) {
hl := HighlightDiffFile(file, wrongNewContent, oldContent, nil)
result := FormatDiffFile(hl, 120, styles.DiffColors{}, nil)

// The bug: when wrongNewContent is used, line 20 in that file is "viewport"
// but the diff expects line 20 to be "textarea"
lines := strings.Split(result.Content, "\n")
for _, line := range lines {
// Find the line with old=18 new=20 (should be textarea context line)
if strings.Contains(line, "18") && strings.Contains(line, "20") && !strings.Contains(line, "@@") {
// This line should show "textarea" not "viewport"
if strings.Contains(line, "viewport") {
t.Errorf("BUG REPRODUCED: Line old=18 new=20 shows 'viewport' instead of 'textarea': %s", line)
}
if !strings.Contains(line, "textarea") {
t.Errorf("Line old=18 new=20 should show 'textarea', got: %s", line)
}
break
}
}
})
}

func TestDetectUseLineIndex(t *testing.T) {
patch := `@@ -11,5 +11,6 @@ import (
 "internal/git"
 "internal/github"
+"internal/diffviewer"
 "internal/picker"
 "internal/styles"`

// 25 line file - all diff line numbers (11-16) fit
content25 := strings.Repeat("line\n", 25)

// 10 line file - diff line numbers exceed it
content10 := strings.Repeat("line\n", 10)

file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}

t.Run("content covers diff lines", func(t *testing.T) {
hl := HighlightDiffFile(file, content25, content25, nil)
use := detectUseLineIndex(hl.DiffLines, hl.HlLines)
t.Logf("detectUseLineIndex = %v, len(HlLines) = %d", use, len(hl.HlLines))
if !use {
t.Error("Expected useLineIndex=true when content covers all diff line numbers")
}
})

t.Run("content too short", func(t *testing.T) {
hl := HighlightDiffFile(file, content10, content10, nil)
use := detectUseLineIndex(hl.DiffLines, hl.HlLines)
t.Logf("detectUseLineIndex = %v, len(HlLines) = %d", use, len(hl.HlLines))
if use {
t.Error("Expected useLineIndex=false when content doesn't cover diff line numbers")
}
})
}

func TestHighlightedLineContent(t *testing.T) {
// New file where line 20 is "textarea"
newContent := `line1
line2
line3
line4
line5
line6
line7
line8
line9
line10
line11 internal/git
line12 internal/github
line13 internal/components
line14 internal/diffviewer
line15 internal/picker
line16 internal/styles
line17 internal/uictx
line18 internal/watcher
line19 charm.land/spinner
line20 charm.land/textarea
line21 charm.land/bubbletea
line22 charm.land/lipgloss`

patch := `@@ -11,5 +11,6 @@ import (
 line11 internal/git
 line12 internal/github
+line14 internal/diffviewer
 line15 internal/picker
 line20 charm.land/textarea`

file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
hl := HighlightDiffFile(file, newContent, "", nil)

t.Logf("HlLines has %d lines", len(hl.HlLines))
for i, line := range hl.HlLines {
if i >= 18 && i <= 22 {
t.Logf("HlLines[%d] = %q", i, line)
}
}

// Line 20 (index 19) should be "textarea"
if len(hl.HlLines) >= 20 {
line20 := hl.HlLines[19] // 0-indexed
t.Logf("Line 20 (HlLines[19]) = %q", line20)
if !strings.Contains(line20, "textarea") {
t.Errorf("Line 20 should contain 'textarea', got %q", line20)
}
}

result := FormatDiffFile(hl, 120, styles.DiffColors{}, nil)
t.Logf("Rendered:\n%s", result.Content)
}

func TestParsedVsRendered(t *testing.T) {
patch := `@@ -11,5 +11,6 @@ import (
 "internal/git"
 "internal/github"
+"internal/diffviewer"
 "internal/picker"
 "internal/textarea"`

newContent := `line1
line2
line3
line4
line5
line6
line7
line8
line9
line10
"internal/git"
"internal/github"
"internal/components"
"internal/diffviewer"
"internal/picker"
"internal/styles"
"internal/textarea"`

file := github.PullRequestFile{Filename: "test.go", Patch: patch, Status: "modified"}
hl := HighlightDiffFile(file, newContent, "", nil)

t.Log("Parsed DiffLines:")
for i, dl := range hl.DiffLines {
t.Logf("  [%d] type=%d old=%d new=%d content=%q", i, dl.Type, dl.OldLineNo, dl.NewLineNo, dl.Content)
}

t.Log("\nHlLines (line index -> content):")
for i := 10; i < len(hl.HlLines) && i < 18; i++ {
clean := strings.ReplaceAll(hl.HlLines[i], "\x1b", "")
t.Logf("  HlLines[%d] (line %d) = ...%s", i, i+1, clean[max(0,len(clean)-30):])
}

result := FormatDiffFile(hl, 120, styles.DiffColors{}, nil)
t.Logf("\nRendered:\n%s", result.Content)
}

func max2(a, b int) int {
if a > b { return a }
return b
}
