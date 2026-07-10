package ui

import "charm.land/lipgloss/v2"

var (
	ColorPrimary   = lipgloss.Color("#64D8CB")
	ColorSecondary = lipgloss.Color("#8C7AE6")
	ColorSuccess   = lipgloss.Color("#73D08A")
	ColorError     = lipgloss.Color("#E0636F")
	ColorWarn      = lipgloss.Color("#E0C064")
	ColorDim       = lipgloss.Color("#777777")

	StyleBold    = lipgloss.NewStyle().Bold(true)
	StylePrimary = lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	StyleSuccess = lipgloss.NewStyle().Foreground(ColorSuccess)
	StyleError   = lipgloss.NewStyle().Foreground(ColorError)
	StyleWarn    = lipgloss.NewStyle().Foreground(ColorWarn)
	StyleDim     = lipgloss.NewStyle().Foreground(ColorDim)
	StyleLabel   = lipgloss.NewStyle().Foreground(ColorDim).Width(14)
)
