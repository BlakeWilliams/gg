package terminal

import (
	"fmt"
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	tea "charm.land/bubbletea/v2"
)

// Color indices for the 16 ANSI colors.
const (
	Black         = 0
	Red           = 1
	Green         = 2
	Yellow        = 3
	Blue          = 4
	Magenta       = 5
	Cyan          = 6
	White         = 7
	BrightBlack   = 8
	BrightRed     = 9
	BrightGreen   = 10
	BrightYellow  = 11
	BrightBlue    = 12
	BrightMagenta = 13
	BrightCyan    = 14
	BrightWhite   = 15
)

// Palette holds the resolved RGB values for the terminal's 16 ANSI colors.
type Palette struct {
	Colors [16]color.Color
}

// Get returns the color at the given ANSI index (0-15).
func (p *Palette) Get(index int) color.Color {
	if index < 0 || index >= 16 {
		return nil
	}
	return p.Colors[index]
}

// Set sets the color at the given ANSI index.
func (p *Palette) Set(index int, c color.Color) {
	if index >= 0 && index < 16 {
		p.Colors[index] = c
	}
}

// Ready returns true if at least one color has been resolved.
func (p *Palette) Ready() bool {
	for _, c := range p.Colors {
		if c != nil {
			return true
		}
	}
	return false
}

// Complete returns true if all 16 colors have been resolved.
func (p *Palette) Complete() bool {
	for _, c := range p.Colors {
		if c == nil {
			return false
		}
	}
	return true
}

// PaletteCompleteMsg is sent when all 16 colors have been resolved.
type PaletteCompleteMsg struct {
	Palette Palette
}

// HandleMessage processes palette-related messages. Call this from your
// app's Update function. Returns the updated palette, an optional Cmd,
// and whether the message was handled.
func HandleMessage(msg tea.Msg, p *Palette) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case uv.UnknownOscEvent:
		if cmd, ok := parseOSC4Response(string(msg), p); ok {
			return cmd, true
		}
	}

	return nil, false
}

// parseOSC4Response parses an OSC 4 response from the terminal.
// Format: \x1b]4;<index>;rgb:<rr>/<gg>/<bb>\x07 (or ST terminator)
// The raw event string may include the ESC ] prefix and terminator.
func parseOSC4Response(raw string, p *Palette) (tea.Cmd, bool) {
	// The UnknownOscEvent contains the full sequence bytes.
	// Strip OSC prefix and terminator to get: "4;<index>;<color>"
	s := raw
	// Remove ESC ] prefix
	s = strings.TrimPrefix(s, "\x1b]")
	s = strings.TrimPrefix(s, "\x9c")
	// Remove terminators
	s = strings.TrimSuffix(s, "\x07")
	s = strings.TrimSuffix(s, "\x1b\\")

	if !strings.HasPrefix(s, "4;") {
		return nil, false
	}
	s = s[2:] // strip "4;"

	// Split into index and color
	parts := strings.SplitN(s, ";", 2)
	if len(parts) != 2 {
		return nil, false
	}

	var index int
	if _, err := fmt.Sscanf(parts[0], "%d", &index); err != nil || index < 0 || index > 15 {
		return nil, false
	}

	c := ansi.XParseColor(parts[1])
	if c == nil {
		return nil, false
	}

	p.Set(index, c)

	var cmd tea.Cmd
	if p.Complete() {
		palette := *p
		cmd = func() tea.Msg {
			return PaletteCompleteMsg{Palette: palette}
		}
	}

	return cmd, true
}
