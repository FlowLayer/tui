package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestBackgroundOwnership verifies that every visible space character in the
// TUI chrome has an explicit ANSI background set, preventing the terminal's
// default background from leaking through.
//
// It forces truecolor output, renders a full frame, then walks each line
// checking that spaces only appear while an explicit background escape (48;…)
// is active.  ANSI \033[0m resets clear the active bg.
func TestBackgroundOwnership(t *testing.T) {
	origProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(origProfile)

	m := newModel("127.0.0.1:3000", "", nil)
	m.width = 80
	m.height = 24
	m.connectionLabel = "Connected"
	m.serviceItems = []serviceItem{
		{name: "billing", status: "ready"},
		{name: "users", status: "stopped"},
	}
	m.resizeLogsViewport()
	m.setLogsViewportContent()

	frame := m.View()
	lines := strings.Split(frame, "\n")

	if len(lines) < m.height {
		t.Fatalf("frame has %d lines, expected at least %d", len(lines), m.height)
	}

	// ANSI sequences: any SGR escape.
	ansiEsc := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	// Matches any explicit background: 48;2;… (truecolor) or 48;5;… (256) or 40-47/100-107.
	bgSetPat := regexp.MustCompile(`(?:48;[25];|4[0-7]|10[0-7])`)
	resetPat := regexp.MustCompile(`\x1b\[0m`)

	type leak struct {
		line int
		col  int
	}
	var leaks []leak

	for lineIdx, line := range lines[:m.height] {
		bgActive := false
		col := 0
		remaining := line

		for len(remaining) > 0 {
			loc := ansiEsc.FindStringIndex(remaining)

			var textChunk string
			if loc == nil {
				textChunk = remaining
				remaining = ""
			} else {
				textChunk = remaining[:loc[0]]
				escSeq := remaining[loc[0]:loc[1]]
				remaining = remaining[loc[1]:]

				// Check text that appeared BEFORE this escape with the
				// CURRENT bg state (prior to the escape's effect).
				for _, ch := range textChunk {
					if ch == ' ' && !bgActive {
						leaks = append(leaks, leak{line: lineIdx + 1, col: col + 1})
					}
					col++
				}

				// Now update state from the escape sequence.
				if resetPat.MatchString(escSeq) {
					bgActive = false
				}
				if bgSetPat.MatchString(escSeq) {
					bgActive = true
				}
				continue
			}

			for _, ch := range textChunk {
				if ch == ' ' && !bgActive {
					leaks = append(leaks, leak{line: lineIdx + 1, col: col + 1})
				}
				col++
			}
		}
	}

	if len(leaks) > 0 {
		// Show per-line breakdown.
		lineCounts := map[int]int{}
		for _, lk := range leaks {
			lineCounts[lk.line]++
		}
		for line := 1; line <= m.height; line++ {
			if n, ok := lineCounts[line]; ok {
				t.Logf("line %2d: %d bare spaces", line, n)
			}
		}
		t.Fatalf("found %d space characters without explicit background across %d×%d frame",
			len(leaks), m.width, m.height)
	}
}
