package components

import (
	"path"
	"sort"
	"strings"

	"github.com/blakewilliams/ghq/internal/github"
	"github.com/charmbracelet/x/ansi"
	"charm.land/lipgloss/v2"
)

const (
	iconFolder  = "\U000f024b" // 󰉋 nf-md-folder
	iconPointer = "\U000f0142" // 󰅂 nf-md-chevron_right
	iconPlus    = "\U000f0415" // 󰐕 nf-md-plus
	iconMinus   = "\U000f0374" // 󰍴 nf-md-minus
	iconRename  = "\U000f0453" // 󰑓 nf-md-rename_box
)

var (
	treeDir      = lipgloss.NewStyle().Bold(true)
	treeFile     = lipgloss.NewStyle()
	treeSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Magenta)
	treeDim      = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	treeAdd      = lipgloss.NewStyle().Foreground(lipgloss.Green)
	treeDel      = lipgloss.NewStyle().Foreground(lipgloss.Red)
)

// FileTreeEntry is a flat entry in the rendered tree.
type FileTreeEntry struct {
	FileIndex int    // index into the files slice, -1 for directories
	Display   string // the display name (just filename, not full path)
	Depth     int    // nesting depth
	IsDir     bool
}

// BuildFileTree converts a flat list of changed files into a tree structure.
func BuildFileTree(files []github.PullRequestFile) []FileTreeEntry {
	type dirEntry struct {
		name      string
		fileIndex int
	}
	dirs := make(map[string][]dirEntry)
	var dirNames []string

	for i, f := range files {
		dir := path.Dir(f.Filename)
		base := path.Base(f.Filename)
		if _, ok := dirs[dir]; !ok {
			dirNames = append(dirNames, dir)
		}
		dirs[dir] = append(dirs[dir], dirEntry{name: base, fileIndex: i})
	}

	sort.Strings(dirNames)

	var entries []FileTreeEntry
	for _, dir := range dirNames {
		if dir != "." {
			depth := strings.Count(dir, "/")
			entries = append(entries, FileTreeEntry{
				FileIndex: -1,
				Display:   dir + "/",
				Depth:     depth,
				IsDir:     true,
			})
		}

		for _, de := range dirs[dir] {
			depth := 0
			if dir != "." {
				depth = strings.Count(dir, "/") + 1
			}
			entries = append(entries, FileTreeEntry{
				FileIndex: de.fileIndex,
				Display:   de.name,
				Depth:     depth,
			})
		}
	}

	return entries
}

// RenderFileTree renders the file tree as exactly `height` lines.
// Each line is padded to `width`. The cursor is kept visible.
func RenderFileTree(entries []FileTreeEntry, files []github.PullRequestFile, cursor int, currentFileIdx int, width, height int) []string {
	lines := make([]string, height)

	if len(entries) == 0 {
		lines[0] = padTo(treeDim.Render("  No files"), width)
		for i := 1; i < height; i++ {
			lines[i] = strings.Repeat(" ", width)
		}
		return lines
	}

	// Scroll window: keep cursor visible, centered when possible.
	start := cursor - height/2
	if start < 0 {
		start = 0
	}
	if start+height > len(entries) {
		start = len(entries) - height
	}
	if start < 0 {
		start = 0
	}

	for row := 0; row < height; row++ {
		idx := start + row
		if idx >= len(entries) {
			lines[row] = strings.Repeat(" ", width)
			continue
		}

		e := entries[idx]
		indent := strings.Repeat(" ", e.Depth)

		var line string
		if e.IsDir {
			line = indent + treeDir.Render(iconFolder+" "+e.Display)
		} else {
			f := files[e.FileIndex]
			isCurrent := e.FileIndex == currentFileIdx
			isCursor := idx == cursor

			name := e.Display
			var stats string
			switch f.Status {
			case "added":
				stats = treeAdd.Render(iconPlus)
			case "removed":
				stats = treeDel.Render(iconMinus)
			case "renamed":
				stats = treeDim.Render(iconRename)
			default:
				stats = treeAdd.Render(iconPlus) + treeDel.Render(iconMinus)
			}

			if isCursor {
				name = treeSelected.Render(iconPointer + " " + name)
			} else if isCurrent {
				name = treeSelected.Render("  " + name)
			} else {
				name = treeFile.Render("  " + name)
			}

			line = indent + name + " " + stats
		}

		lines[row] = padTo(line, width)
	}

	return lines
}

func padTo(s string, width int) string {
	w := lipgloss.Width(s)
	if w > width {
		return ansi.Truncate(s, width, "")
	}
	if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}
