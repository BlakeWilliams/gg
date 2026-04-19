package components

import (
	"regexp"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"charm.land/lipgloss/v2"
)

// HighlightSearchSpans injects bgCode around regex matches in an
// ANSI-styled rendered line. raw is the plain-text content that the pattern
// matches against; gutterW is the number of visual columns at the start of
// the rendered line that precede the code (line numbers + sign character).
// restoreBg is the ANSI code to resume after the match (the line's effective bg).
func HighlightSearchSpans(rendered, raw string, pattern *regexp.Regexp, gutterW int, bgCode, fgCode, restoreBg string) string {
	locs := pattern.FindAllStringIndex(raw, -1)
	if len(locs) == 0 {
		return rendered
	}

	type span struct{ start, end int }
	spans := make([]span, len(locs))
	for i, loc := range locs {
		spans[i] = span{
			start: ByteToVisual(raw, loc[0]) + gutterW,
			end:   ByteToVisual(raw, loc[1]) + gutterW,
		}
	}

	innerW := lipgloss.Width(rendered)

	var b strings.Builder
	cursor := 0
	for _, sp := range spans {
		if sp.start >= innerW || sp.start == sp.end {
			continue
		}
		end := sp.end
		if end > innerW {
			end = innerW
		}

		if sp.start > cursor {
			b.WriteString(xansi.Cut(rendered, cursor, sp.start))
		}

		matchPart := xansi.Cut(rendered, sp.start, end)
		matchPlain := xansi.Strip(matchPart)
		b.WriteString(bgCode)
		b.WriteString(fgCode)
		b.WriteString(matchPlain)
		b.WriteString("\033[0m")
		b.WriteString(restoreBg)

		cursor = end
	}

	if cursor < innerW {
		b.WriteString(xansi.Cut(rendered, cursor, innerW))
	}

	return b.String()
}

// ReplaceBackground swaps diff bg colors for the given target bg in an ANSI string.
func ReplaceBackground(s string, addBg, delBg, targetBg string) string {
	if addBg != "" {
		s = strings.ReplaceAll(s, addBg, targetBg)
	}
	if delBg != "" {
		s = strings.ReplaceAll(s, delBg, targetBg)
	}
	s = strings.ReplaceAll(s, "\033[0m", "\033[0m"+targetBg)
	s = strings.ReplaceAll(s, "\033[m", "\033[m"+targetBg)
	s = targetBg + s + "\033[0m"
	return s
}

// ReplaceCursorSign swaps the bold +/- sign character with > for cursor/selection lines.
func ReplaceCursorSign(s string) string {
	s = strings.Replace(s, "\033[1m+\033[0m", "\033[1m>\033[0m", 1)
	s = strings.Replace(s, "\033[1m-\033[0m", "\033[1m>\033[0m", 1)
	return s
}

// ByteToVisual converts a byte offset in a string to a visual column offset,
// accounting for tab expansion (tabs become 4 spaces).
func ByteToVisual(s string, byteOff int) int {
	vis := 0
	for i, r := range s {
		if i >= byteOff {
			break
		}
		if r == '\t' {
			vis += 4
		} else {
			vis++
		}
	}
	return vis
}
