package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func stripANSISequences(value string) string {
	var out strings.Builder

	for index := 0; index < len(value); {
		next, ok := ansiEscapeEnd(value, index)
		if ok {
			index = next
			continue
		}

		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 0 {
			break
		}

		out.WriteRune(r)
		index += size
	}

	return out.String()
}

func slicePlainByWidth(line string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end <= start {
		return ""
	}

	var out strings.Builder
	visible := 0

	for _, r := range line {
		runeWidth := lipgloss.Width(string(r))
		nextVisible := visible + runeWidth

		if nextVisible > start && visible < end {
			out.WriteRune(r)
		}

		visible = nextVisible
		if visible >= end {
			break
		}
	}

	return out.String()
}

func TestConnectionInfoModalToggleAndEsc(t *testing.T) {
	current := newModel("127.0.0.1:6999", "dev-token", nil)
	if current.showConnectionInfo {
		t.Fatal("expected modal hidden by default")
	}

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !current.showConnectionInfo {
		t.Fatal("expected i to open connection info modal")
	}

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyEsc})
	if current.showConnectionInfo {
		t.Fatal("expected esc to close connection info modal")
	}

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !current.showConnectionInfo {
		t.Fatal("expected i to open connection info modal")
	}

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if current.showConnectionInfo {
		t.Fatal("expected second i to close connection info modal")
	}
}

func TestConnectionInfoModalBlocksOtherShortcuts(t *testing.T) {
	current := newModel("127.0.0.1:6999", "dev-token", nil)
	current.serviceItems = []serviceItem{
		{name: "billing", status: "running"},
		{name: "users", status: "running"},
	}
	current.focus = focusLeft
	current.serviceSelection = 0

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !current.showConnectionInfo {
		t.Fatal("expected modal open")
	}

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyDown})
	if current.serviceSelection != 0 {
		t.Fatalf("expected selection unchanged while modal open, got %d", current.serviceSelection)
	}

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyTab})
	if current.focus != focusLeft {
		t.Fatal("expected tab to be ignored while modal open")
	}

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !current.showConnectionInfo {
		t.Fatal("expected q to be ignored while modal open")
	}

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyEsc})
	if current.showConnectionInfo {
		t.Fatal("expected esc to close modal")
	}

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyTab})
	if current.focus != focusRight {
		t.Fatal("expected tab to work again once modal is closed")
	}
}

func TestConnectionInfoModalBlocksCtrlCWhileOpen(t *testing.T) {
	current := newModel("127.0.0.1:6999", "dev-token", nil)
	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !current.showConnectionInfo {
		t.Fatal("expected modal open")
	}

	updated, command := current.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	next, ok := updated.(model)
	if !ok {
		t.Fatalf("updated model type = %T, want main.model", updated)
	}
	if !next.showConnectionInfo {
		t.Fatal("expected ctrl+c to be ignored while modal open")
	}
	if command != nil {
		t.Fatal("expected no quit command while modal is open")
	}
}

func TestConnectionInfoModalStaysOpenOnWindowResize(t *testing.T) {
	current := newModel("127.0.0.1:6999", "dev-token", nil)
	current.width = 80
	current.height = 24
	current.showConnectionInfo = true

	next := applyUpdate(t, current, tea.WindowSizeMsg{Width: 120, Height: 40})
	if !next.showConnectionInfo {
		t.Fatal("expected modal to remain open after resize")
	}
	if next.width != 120 || next.height != 40 {
		t.Fatalf("expected resized dimensions 120x40, got %dx%d", next.width, next.height)
	}
}

func TestConnectionInfoModalKeepsServiceFilterStateConsistent(t *testing.T) {
	current := newModel("127.0.0.1:6999", "dev-token", nil)
	current.serviceFilterEdit = true
	current.serviceFilter = "bill"

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !current.showConnectionInfo {
		t.Fatal("expected modal open")
	}
	if !current.serviceFilterEdit || current.serviceFilter != "bill" {
		t.Fatalf("expected existing service filter state unchanged on modal open, edit=%v filter=%q", current.serviceFilterEdit, current.serviceFilter)
	}

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyBackspace})
	if current.serviceFilter != "bill" {
		t.Fatalf("expected filter unchanged while modal open, got %q", current.serviceFilter)
	}

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyEsc})
	if current.showConnectionInfo {
		t.Fatal("expected modal closed")
	}
	if !current.serviceFilterEdit {
		t.Fatal("expected service filter edit mode preserved after closing modal")
	}

	current = applyUpdate(t, current, tea.KeyMsg{Type: tea.KeyBackspace})
	if current.serviceFilter != "bil" {
		t.Fatalf("expected service filter editing to resume after closing modal, got %q", current.serviceFilter)
	}
}

func TestConnectionInfoModalViewUsesLocalValues(t *testing.T) {
	current := newModel("127.0.0.1:6999", "", nil)
	current.width = 100
	current.height = 24
	current.showConnectionInfo = true

	rendered := current.View()

	checks := []string{
		"FlowLayer TUI",
		"Connection Info",
		"Address",
		"127.0.0.1:6999",
		"Token",
		"(none)",
		"Status",
		current.connectionLabel,
		"i / esc  close",
	}
	for _, expected := range checks {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected rendered view to contain %q", expected)
		}
	}
}

func TestHeaderShortcutsIncludeConnectionInfo(t *testing.T) {
	current := newModel("127.0.0.1:6999", "dev-token", nil)
	foundBinding := false
	for _, binding := range current.headerKeys.ShortHelp() {
		keys := binding.Keys()
		help := binding.Help()
		if help.Desc != "conn info" {
			continue
		}

		if help.Key != "i" {
			t.Fatalf("expected conn info help key i, got %q", help.Key)
		}
		if len(keys) != 1 || keys[0] != "i" {
			t.Fatalf("expected conn info binding keys [i], got %v", keys)
		}

		foundBinding = true
		break
	}
	if !foundBinding {
		t.Fatalf("expected short help bindings to include i/conn info, got %+v", current.headerKeys.ShortHelp())
	}

	rendered := current.renderHeaderShortcuts(120)
	normalized := strings.Join(strings.Fields(stripANSISequences(rendered)), " ")

	if !strings.Contains(" "+normalized+" ", " i conn info ") {
		t.Fatalf("expected rendered shortcuts to include i conn info, got %q", normalized)
	}
}

func TestSanitizeConnectionInfoValueStripsCSISequences(t *testing.T) {
	raw := "prefix \x1b[31mTOKEN\x1b[0m suffix\x1b[2K"
	got := sanitizeConnectionInfoValue(raw)
	want := "prefix TOKEN suffix"

	if got != want {
		t.Fatalf("sanitizeConnectionInfoValue() = %q, want %q", got, want)
	}
}

func TestSanitizeConnectionInfoValueKeepsNonCSISequences(t *testing.T) {
	// This sanitizer intentionally strips CSI sequences only (ESC [ ... final byte).
	raw := "prefix \x1b]0;title\x07 suffix"
	got := sanitizeConnectionInfoValue(raw)

	if got != raw {
		t.Fatalf("sanitizeConnectionInfoValue() changed non-CSI sequence: got %q, want %q", got, raw)
	}
}

func TestConnectionInfoModalViewHasNoCarriageReturn(t *testing.T) {
	current := newModel("127.0.0.1:6999", "dev-token", nil)
	current.width = 96
	current.height = 24
	current.showConnectionInfo = true

	rendered := current.View()
	if strings.Contains(rendered, "\r") {
		t.Fatal("expected modal view to avoid carriage return characters")
	}
}

func TestConnectionInfoModalViewLineWidthStaysStable(t *testing.T) {
	current := newModel("127.0.0.1:6999", "dev-token", nil)
	current.width = 92
	current.height = 24
	current.showConnectionInfo = true

	rendered := current.View()
	lines := strings.Split(rendered, "\n")

	if len(lines) != current.height {
		t.Fatalf("line count = %d, want %d", len(lines), current.height)
	}

	for idx, line := range lines {
		if got := lipgloss.Width(line); got != current.width {
			t.Fatalf("line %d width = %d, want %d", idx+1, got, current.width)
		}
	}
}

func TestOverlayPlacedCoversCenteredRectangleIncludingSpaceRows(t *testing.T) {
	const (
		width         = 40
		height        = 12
		overlayWidth  = 14
		overlayHeight = 5
	)

	baseLine := strings.Repeat("Z", width)
	baseLines := make([]string, height)
	for i := range baseLines {
		baseLines[i] = baseLine
	}
	base := strings.Join(baseLines, "\n")

	overlayLines := []string{
		strings.Repeat("#", overlayWidth),
		strings.Repeat(" ", overlayWidth),
		fitLine("Connection", overlayWidth),
		strings.Repeat(" ", overlayWidth),
		strings.Repeat("#", overlayWidth),
	}
	overlay := strings.Join(overlayLines, "\n")

	rendered := overlayPlaced(base, overlay, width, height)
	lines := strings.Split(rendered, "\n")
	if len(lines) != height {
		t.Fatalf("line count = %d, want %d", len(lines), height)
	}

	xOffset := (width - overlayWidth) / 2
	yOffset := (height - overlayHeight) / 2

	for row := yOffset; row < yOffset+overlayHeight; row++ {
		segment := sliceStyledByWidth(lines[row], xOffset, xOffset+overlayWidth)
		if strings.Contains(segment, "Z") {
			t.Fatalf("found background bleed-through on row %d inside overlay rectangle", row+1)
		}
	}

	prefix := sliceStyledByWidth(lines[yOffset+2], 0, xOffset)
	suffix := sliceStyledByWidth(lines[yOffset+2], xOffset+overlayWidth, width)
	if !strings.Contains(prefix, "Z") || !strings.Contains(suffix, "Z") {
		t.Fatal("expected background to remain visible around the overlay")
	}
}

func TestConnectionInfoModalOverlayMatchesRenderedRectangleAcrossWidths(t *testing.T) {
	for width := 24; width <= 120; width++ {
		current := newModel("127.0.0.1:6999", "dev-token", nil)
		current.width = width
		current.height = 24
		current.showConnectionInfo = true

		overlay := current.renderConnectionInfoModal(width)
		overlayWidth := lipgloss.Width(overlay)
		overlayHeight := lipgloss.Height(overlay)
		if overlayWidth > current.width {
			overlayWidth = current.width
		}
		if overlayHeight > current.height {
			overlayHeight = current.height
		}

		xOffset := 0
		if current.width > overlayWidth {
			xOffset = (current.width - overlayWidth) / 2
		}
		yOffset := 0
		if current.height > overlayHeight {
			yOffset = (current.height - overlayHeight) / 2
		}

		expectedOverlay := fillBg(overlay, overlayWidth, overlayHeight, colorPanelBackground)
		expectedLines := strings.Split(stripANSISequences(expectedOverlay), "\n")
		renderedLines := strings.Split(stripANSISequences(current.View()), "\n")

		for row := 0; row < overlayHeight; row++ {
			got := slicePlainByWidth(renderedLines[yOffset+row], xOffset, xOffset+overlayWidth)
			want := expectedLines[row]
			if got != want {
				t.Fatalf("width %d row %d mismatch: got %q want %q", width, row+1, got, want)
			}
		}
	}
}

func TestConnectionInfoModalRightBorderIsVisible(t *testing.T) {
	current := newModel("127.0.0.1:6999", "dev-token", nil)
	current.width = 96
	current.height = 24
	current.showConnectionInfo = true

	overlay := current.renderConnectionInfoModal(current.width)
	overlayWidth := lipgloss.Width(overlay)
	overlayHeight := lipgloss.Height(overlay)
	xOffset := (current.width - overlayWidth) / 2
	yOffset := (current.height - overlayHeight) / 2

	lines := strings.Split(stripANSISequences(current.View()), "\n")
	expectedOverlayLines := strings.Split(stripANSISequences(fillBg(overlay, overlayWidth, overlayHeight, colorPanelBackground)), "\n")

	for row := yOffset + 1; row < yOffset+overlayHeight-1; row++ {
		rightBorder := slicePlainByWidth(lines[row], xOffset+overlayWidth-1, xOffset+overlayWidth)
		if rightBorder != "│" {
			segment := slicePlainByWidth(lines[row], xOffset, xOffset+overlayWidth)
			expectedSegment := expectedOverlayLines[row-yOffset]
			expectedRight := slicePlainByWidth(expectedSegment, overlayWidth-1, overlayWidth)
			t.Fatalf("row %d right border = %q, want %q; modal segment=%q; expected right=%q expected segment=%q", row+1, rightBorder, "│", segment, expectedRight, expectedSegment)
		}
	}
}
