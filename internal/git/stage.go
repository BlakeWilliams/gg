package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// StageLines stages specific lines from a file by generating a partial patch
// and applying it with `git apply --cached`.
// For new (untracked) files, uses `git add --intent-to-add` first.
func StageLines(dir string, filename string, fileStatus string, fullPatch string, lineNos []int, oldLineNos []int, unstage bool) error {
	if fileStatus == "added" && !unstage {
		// Ensure the file is known to git's index before applying patches.
		ensureTracked(dir, filename)
	}
	patch := buildPartialPatch(filename, fileStatus, fullPatch, lineNos, oldLineNos)
	if patch == "" {
		return nil
	}
	return applyPatch(dir, patch, unstage)
}

// StageHunk stages the entire hunk that contains the given line number.
func StageHunk(dir string, filename string, fileStatus string, fullPatch string, lineNo int, side string, unstage bool) error {
	if fileStatus == "added" && !unstage {
		ensureTracked(dir, filename)
	}
	patch := buildHunkPatch(filename, fileStatus, fullPatch, lineNo, side)
	if patch == "" {
		return nil
	}
	return applyPatch(dir, patch, unstage)
}

// ensureTracked makes sure git knows about a file (for new files).
func ensureTracked(dir, filename string) {
	// Check if file is already tracked.
	cmd := exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", filename)
	if cmd.Run() == nil {
		return // already tracked
	}
	// Add with --intent-to-add so the index knows about it.
	exec.Command("git", "-C", dir, "add", "--intent-to-add", filename).Run()
}

func applyPatch(dir string, patch string, unstage bool) error {
	args := []string{"-C", dir, "apply", "--cached", "--allow-empty"}
	if unstage {
		args = append(args, "--reverse")
	}
	args = append(args, "-")

	cmd := exec.Command("git", args...)
	cmd.Stdin = strings.NewReader(patch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git apply: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// buildPartialPatch creates a valid unified diff that only includes the specified lines.
// Lines not selected are converted to context lines.
func buildPartialPatch(filename string, fileStatus string, fullPatch string, newLineNos []int, oldLineNos []int) string {
	newSet := map[int]bool{}
	for _, n := range newLineNos {
		newSet[n] = true
	}
	oldSet := map[int]bool{}
	for _, n := range oldLineNos {
		oldSet[n] = true
	}

	lines := strings.Split(fullPatch, "\n")
	var hunks []string
	var currentHunk []string
	oldStart, newStart := 0, 0
	oldCount, newCount := 0, 0
	oldNum, newNum := 0, 0
	inHunk := false

	flushHunk := func() {
		if len(currentHunk) == 0 {
			return
		}
		// Check if hunk has any actual changes.
		hasChange := false
		for _, l := range currentHunk {
			if strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
				hasChange = true
				break
			}
		}
		if !hasChange {
			currentHunk = nil
			return
		}
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldCount, newStart, newCount)
		hunks = append(hunks, header)
		hunks = append(hunks, currentHunk...)
		currentHunk = nil
	}

	for _, line := range lines {
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "@@") {
			flushHunk()
			// Parse hunk header.
			oldNum, newNum = parseHunkNums(line)
			oldStart = oldNum
			newStart = newNum
			oldCount = 0
			newCount = 0
			inHunk = true
			continue
		}

		if !inHunk {
			continue
		}

		switch {
		case strings.HasPrefix(line, "+"):
			if newSet[newNum] {
				// Keep as addition.
				currentHunk = append(currentHunk, line)
				newCount++
			} else {
				// Convert to nothing — skip this addition.
				// Don't add as context, don't increment old count.
			}
			newNum++
		case strings.HasPrefix(line, "-"):
			if oldSet[oldNum] {
				// Keep as deletion.
				currentHunk = append(currentHunk, line)
				oldCount++
			} else {
				// Convert to context — keep the line but as unchanged.
				currentHunk = append(currentHunk, " "+line[1:])
				oldCount++
				newCount++
			}
			oldNum++
		default:
			// Context line.
			content := line
			if len(line) > 0 && line[0] == ' ' {
				content = line
			} else {
				content = " " + line
			}
			currentHunk = append(currentHunk, content)
			oldCount++
			newCount++
			oldNum++
			newNum++
		}
	}
	flushHunk()

	if len(hunks) == 0 {
		return ""
	}

	// Build full patch with header.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", filename, filename))
	if fileStatus == "added" {
		b.WriteString("new file mode 100644\n")
		b.WriteString("--- /dev/null\n")
	} else {
		b.WriteString(fmt.Sprintf("--- a/%s\n", filename))
	}
	b.WriteString(fmt.Sprintf("+++ b/%s\n", filename))
	for _, h := range hunks {
		b.WriteString(h + "\n")
	}
	return b.String()
}

// buildHunkPatch extracts the hunk containing the given line and builds a patch from it.
func buildHunkPatch(filename string, fileStatus string, fullPatch string, lineNo int, side string) string {
	lines := strings.Split(fullPatch, "\n")
	var hunkHeader string
	var hunkLines []string
	found := false
	oldNum, newNum := 0, 0

	for _, line := range lines {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "@@") {
			// If we already found the target hunk, stop.
			if found {
				break
			}
			hunkHeader = line
			hunkLines = nil
			oldNum, newNum = parseHunkNums(line)
			continue
		}
		if hunkHeader == "" {
			continue
		}

		hunkLines = append(hunkLines, line)

		// Check if this line matches the target.
		switch {
		case strings.HasPrefix(line, "+"):
			if side == "RIGHT" && newNum == lineNo {
				found = true
			}
			newNum++
		case strings.HasPrefix(line, "-"):
			if side == "LEFT" && oldNum == lineNo {
				found = true
			}
			oldNum++
		default:
			if side == "RIGHT" && newNum == lineNo {
				found = true
			}
			if side == "LEFT" && oldNum == lineNo {
				found = true
			}
			oldNum++
			newNum++
		}
	}

	if !found || hunkHeader == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", filename, filename))
	if fileStatus == "added" {
		b.WriteString("new file mode 100644\n")
		b.WriteString("--- /dev/null\n")
	} else {
		b.WriteString(fmt.Sprintf("--- a/%s\n", filename))
	}
	b.WriteString(fmt.Sprintf("+++ b/%s\n", filename))
	b.WriteString(hunkHeader + "\n")
	for _, hl := range hunkLines {
		b.WriteString(hl + "\n")
	}
	return b.String()
}

func parseHunkNums(header string) (oldStart, newStart int) {
	parts := strings.Fields(header)
	if len(parts) < 3 {
		return 0, 0
	}
	old := strings.TrimPrefix(parts[1], "-")
	if comma := strings.IndexByte(old, ','); comma >= 0 {
		old = old[:comma]
	}
	new_ := strings.TrimPrefix(parts[2], "+")
	if comma := strings.IndexByte(new_, ','); comma >= 0 {
		new_ = new_[:comma]
	}
	for _, c := range old {
		if c >= '0' && c <= '9' {
			oldStart = oldStart*10 + int(c-'0')
		}
	}
	for _, c := range new_ {
		if c >= '0' && c <= '9' {
			newStart = newStart*10 + int(c-'0')
		}
	}
	return
}
