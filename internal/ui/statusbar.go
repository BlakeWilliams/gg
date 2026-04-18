package ui

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/blakewilliams/ghq/internal/ui/localdiff"
	"github.com/blakewilliams/ghq/internal/ui/styles"
	"charm.land/lipgloss/v2"
)

// Powerline separator characters (require nerd font).
const (
	plRight = "\ue0b0" // right-pointing solid arrow
	plLeft  = "\ue0b2" // left-pointing solid arrow
)

func (m Model) renderStatusBar() string {
	ld, isLocal := m.activeView.(localdiff.Model)

	if !isLocal {
		return strings.Repeat(" ", m.width)
	}

	branch := ld.BranchName()
	mode := ld.DiffMode()
	modeBg := styles.ModeColor(mode)
	treeW := ld.TreeWidth()
	rightW := m.width - treeW

	modeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Black).Background(modeBg)

	barBg := m.barBackground()
	barFg := m.barForeground()
	barStyle := lipgloss.NewStyle().Foreground(barFg).Background(barBg)

	// === Left panel (under file tree): MODE ▶ branch ===
	modeText := modeStyle.Render(" " + strings.ToUpper(mode.String()) + " ")
	modeToBar := lipgloss.NewStyle().Foreground(modeBg).Background(barBg).Render(plRight)
	branchText := lipgloss.NewStyle().Foreground(lipgloss.BrightBlack).Background(barBg).Render(" " + branch)

	leftContent := modeText + modeToBar + branchText
	leftContentW := lipgloss.Width(leftContent)
	leftPad := treeW - leftContentW - 1 // -1 for separator
	if leftPad < 0 {
		leftPad = 0
	}
	leftPanel := leftContent + barStyle.Render(strings.Repeat(" ", leftPad))

	// Footer separator: slightly lighter than bar bg, but darker than panel chrome.
	// Midpoint between barBg and chromeColor.
	var sepColor color.Color = lipgloss.BrightBlack
	if m.ctx.ChromeColor != nil && barBg != nil {
		cr, cg, cb, _ := m.ctx.ChromeColor.RGBA()
		br, bg2, bb, _ := barBg.RGBA()
		mr := (int(cr>>8) + int(br>>8)) / 2
		mg := (int(cg>>8) + int(bg2>>8)) / 2
		mb := (int(cb>>8) + int(bb>>8)) / 2
		sepColor = lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", mr, mg, mb))
	}
	sep := lipgloss.NewStyle().Foreground(sepColor).Background(barBg).Render("│")

	// === Right panel (under diff): scroll position ===

	// Scroll position badge on far right (powerline style).
	scrollText := ld.ScrollPercent()
	scrollBg := modeBg
	scrollStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Black).Background(scrollBg)

	// PR badge (between stats and scroll).
	var prBadge string
	var scrollBadge string
	if pr := ld.PR(); pr != nil {
		prBg := lipgloss.Cyan
		prStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Black).Background(prBg)
		barToPr := lipgloss.NewStyle().Foreground(prBg).Background(barBg).Render(plLeft)
		prToScroll := lipgloss.NewStyle().Foreground(scrollBg).Background(prBg).Render(plLeft)
		prBadge = barToPr + prStyle.Render(fmt.Sprintf(" PR #%d ", pr.Number))
		scrollBadge = prToScroll + scrollStyle.Render(" "+scrollText+" ")
	} else {
		barToScroll := lipgloss.NewStyle().Foreground(scrollBg).Background(barBg).Render(plLeft)
		scrollBadge = barToScroll + scrollStyle.Render(" "+scrollText+" ")
	}

	scrollBadgeW := lipgloss.Width(scrollBadge)
	prBadgeW := lipgloss.Width(prBadge)
	rightGap := rightW - 1 - prBadgeW - scrollBadgeW // -1 for separator
	if rightGap < 0 {
		rightGap = 0
	}
	rightPanel := barStyle.Render(strings.Repeat(" ", rightGap)) + prBadge + scrollBadge

	return leftPanel + sep + rightPanel
}

// brightnessModify applies lualine's brightness_modifier formula:
//
//	channel = clamp(channel + channel * pct / 100, 0, 255)
//
// Positive pct lightens, negative darkens. This is proportional — brighter
// channels shift more than darker ones, preserving the color's hue/warmth.
func brightnessModify(r, g, b float64, pct float64) (int, int, int) {
	return clampByte(int(r + r*pct/100)),
		clampByte(int(g + g*pct/100)),
		clampByte(int(b + b*pct/100))
}

// barBackground computes the status bar background matching lualine's auto
// theme: brightness_modifier(Normal bg, ±10%).
func (m Model) barBackground() color.Color {
	if m.termBg == nil {
		if m.hasDarkBg {
			return lipgloss.Black
		}
		return lipgloss.White
	}
	r, g, b, _ := m.termBg.RGBA()
	pct := 30.0
	if !m.hasDarkBg {
		pct = -15.0
	}
	rr, gg, bb := brightnessModify(float64(r>>8), float64(g>>8), float64(b>>8), pct)
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", rr, gg, bb))
}

// barForeground computes the status bar text color. Lualine's auto theme
// uses Normal fg, then iteratively adjusts contrast until
// |avg(fg) - avg(bg)| >= 0.3. We approximate with a large brightness shift.
func (m Model) barForeground() color.Color {
	if m.termBg == nil {
		if m.hasDarkBg {
			return lipgloss.BrightBlack
		}
		return lipgloss.BrightBlack
	}
	r, g, b, _ := m.termBg.RGBA()
	pct := 500.0
	if !m.hasDarkBg {
		pct = -60.0
	}
	rr, gg, bb := brightnessModify(float64(r>>8), float64(g>>8), float64(b>>8), pct)
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", rr, gg, bb))
}

// chromeColor computes the border/separator color: slightly lighter than
// barBackground so the separator is visible against the status bar.
func (m Model) chromeColor() color.Color {
	bg := m.barBackground()
	r, g, b, _ := bg.RGBA()
	pct := 40.0
	if !m.hasDarkBg {
		pct = -20.0
	}
	rr, gg, bb := brightnessModify(float64(r>>8), float64(g>>8), float64(b>>8), pct)
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", rr, gg, bb))
}

func clampByte(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
