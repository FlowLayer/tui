package main

import "github.com/charmbracelet/lipgloss"

// FlowLayer TUI palette.
//
// Log message content can contain ANSI colors from service output and must
// remain untouched by theme styling.

var (
	// Foundations.
	colorBackground              = lipgloss.Color("#242a39")
	colorPanelBackground         = lipgloss.Color("#242a39")
	colorPanelSelectedBackground = lipgloss.Color("#2f3544")
	colorTextDefault             = lipgloss.Color("#dde2f6")
	colorTextMuted               = lipgloss.Color("#c2c6d6")
	colorStatusSuccess           = lipgloss.Color("#4edea3")
	colorStatusError             = lipgloss.Color("#ffb4ab")

	// Explicit UI usage roles.
	colorSelectedPanelBorder   = lipgloss.Color("#8fb4ff")
	colorUnselectedPanelBorder = lipgloss.Color("#424754")
	colorSelectedPanelTitle    = lipgloss.Color("#adc6ff")
	colorUnselectedPanelTitle  = lipgloss.Color("#dde2f6")
	colorTimestamp             = lipgloss.Color("#9ea9c7")
	colorServiceName           = lipgloss.Color("#7db0ff")
	colorHeaderIdentity        = lipgloss.Color("#adc6ff")
)
