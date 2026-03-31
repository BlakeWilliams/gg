package components

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
)

// NickColors is the palette used for weechat-style username coloring.
// These are chosen to be distinct and readable on dark backgrounds.
var NickColors = []color.Color{
	lipgloss.Color("1"),   // red
	lipgloss.Color("2"),   // green
	lipgloss.Color("3"),   // yellow
	lipgloss.Color("4"),   // blue
	lipgloss.Color("5"),   // magenta
	lipgloss.Color("6"),   // cyan
	lipgloss.Color("9"),   // bright red
	lipgloss.Color("10"),  // bright green
	lipgloss.Color("11"),  // bright yellow
	lipgloss.Color("12"),  // bright blue
	lipgloss.Color("13"),  // bright magenta
	lipgloss.Color("14"),  // bright cyan
	lipgloss.Color("208"), // orange
	lipgloss.Color("172"), // dark orange
	lipgloss.Color("141"), // purple
	lipgloss.Color("167"), // salmon
	lipgloss.Color("109"), // steel blue
	lipgloss.Color("150"), // sage
}

// NickColor returns a consistent color for a given username using
// a djb2-style hash, similar to weechat's nick coloring algorithm.
func NickColor(name string) color.Color {
	var hash uint32 = 5381
	for _, c := range name {
		hash = hash*33 + uint32(c)
	}
	return NickColors[hash%uint32(len(NickColors))]
}

// ColoredAuthor renders a username with a consistent hash-based color.
func ColoredAuthor(login string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(NickColor(login)).
		UnderlineStyle(lipgloss.UnderlineDotted).
		Hyperlink(fmt.Sprintf("https://github.com/%s", login)).
		Render("@" + login)
}
