package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func scrollIndicatorModel(lines int, height int, yOffset int) model {
	m := newModel("127.0.0.1:3000", "", nil)
	m.width = 80
	m.height = 24
	m.logsViewport.Width = 40
	m.logsViewport.Height = height
	if lines > 0 {
		content := make([]string, lines)
		for i := range content {
			content[i] = "log line"
		}
		m.logEntries = make([]ServiceLog, lines)
		for i := range m.logEntries {
			m.logEntries[i] = ServiceLog{Message: "log line"}
		}
		m.logsViewport.SetContent(strings.Join(content, "\n"))
		m.logsViewport.SetYOffset(yOffset)
	}
	return m
}

func TestLogsScrollIndicatorEmpty(t *testing.T) {
	m := scrollIndicatorModel(0, 10, 0)
	got := m.logsScrollIndicator()
	if got != "" {
		t.Fatalf("empty logs indicator = %q, want empty", got)
	}
}

func TestLogsScrollIndicatorFitsOnScreen(t *testing.T) {
	// 5 lines, viewport height 10 → everything visible
	m := scrollIndicatorModel(5, 10, 0)
	got := m.logsScrollIndicator()
	want := "1-5 / 5  100%"
	if got != want {
		t.Fatalf("fits-on-screen indicator = %q, want %q", got, want)
	}
}

func TestLogsScrollIndicatorTopPosition(t *testing.T) {
	// 100 lines, viewport height 20, offset 0 → top
	m := scrollIndicatorModel(100, 20, 0)
	got := m.logsScrollIndicator()
	want := "1-20 / 100  0%"
	if got != want {
		t.Fatalf("top position indicator = %q, want %q", got, want)
	}
}

func TestLogsScrollIndicatorMiddlePosition(t *testing.T) {
	// 100 lines, viewport height 20, offset 40 → middle
	m := scrollIndicatorModel(100, 20, 40)
	got := m.logsScrollIndicator()
	// percent = 40 / (100-20) = 0.5 → 50%
	want := "41-60 / 100  50%"
	if got != want {
		t.Fatalf("middle position indicator = %q, want %q", got, want)
	}
}

func TestLogsScrollIndicatorBottomPosition(t *testing.T) {
	// 100 lines, viewport height 20, offset 80 → bottom
	m := scrollIndicatorModel(100, 20, 80)
	got := m.logsScrollIndicator()
	want := "81-100 / 100  100%"
	if got != want {
		t.Fatalf("bottom position indicator = %q, want %q", got, want)
	}
}

func TestLogsScrollIndicatorAfterResize(t *testing.T) {
	m := scrollIndicatorModel(100, 20, 40)
	// Simulate resize: height changes to 50
	m.logsViewport.Height = 50
	// YOffset stays at 40, but viewport is now larger
	got := m.logsScrollIndicator()
	// percent = 40 / (100-50) = 0.8 → 80%
	want := "41-90 / 100  80%"
	// ScrollPercent uses float math; allow 81% as well.
	alt := "41-90 / 100  81%"
	if got != want && got != alt {
		t.Fatalf("after resize indicator = %q, want %q or %q", got, want, alt)
	}
}

func TestRenderFooterContainsIndicator(t *testing.T) {
	m := scrollIndicatorModel(100, 20, 0)
	m.footerMessage = "connected"
	footer := m.renderFooter(80)
	if !strings.Contains(footer, "1-20 / 100") {
		t.Fatalf("footer should contain scroll indicator, got %q", footer)
	}
	if !strings.Contains(footer, "connected") {
		t.Fatalf("footer should contain status message, got %q", footer)
	}
	if !strings.Contains(footer, "flowlayer.tech • since 2026") {
		t.Fatalf("footer should contain center text, got %q", footer)
	}
}

func TestRenderFooterEmptyLogs(t *testing.T) {
	m := scrollIndicatorModel(0, 10, 0)
	m.footerMessage = "OK"
	footer := m.renderFooter(80)
	// No indicator for empty logs — just the status message
	if !strings.Contains(footer, "OK") {
		t.Fatalf("footer should contain OK for empty logs, got %q", footer)
	}
}

func TestRenderFooterReflectsTruncatedState(t *testing.T) {
	m := scrollIndicatorModel(100, 20, 0)
	m.footerMessage = "connected"

	m.logsTruncated = true
	footer := m.renderFooter(80)
	if !strings.Contains(footer, "history truncated") {
		t.Fatalf("footer should show truncation marker when truncated=true, got %q", footer)
	}

	m.logsTruncated = false
	footer = m.renderFooter(80)
	if strings.Contains(footer, "history truncated") {
		t.Fatalf("footer should hide truncation marker when truncated=false, got %q", footer)
	}
}

func TestRenderFooterNarrowWidthPrioritizesEdges(t *testing.T) {
	m := scrollIndicatorModel(100, 20, 0)
	m.footerMessage = "connected"
	footer := m.renderFooter(32)

	if !strings.Contains(footer, "connected") {
		t.Fatalf("footer should contain status message, got %q", footer)
	}
	if !strings.Contains(footer, "1-20 / 100") {
		t.Fatalf("footer should contain scroll indicator, got %q", footer)
	}
	if strings.Contains(footer, "flowlayer.tech • since 2026") {
		t.Fatalf("footer center text should truncate on narrow width, got %q", footer)
	}
	if strings.Contains(footer, "\n") {
		t.Fatalf("footer should remain on one line, got %q", footer)
	}
	if gotWidth := lipgloss.Width(footer); gotWidth != 32 {
		t.Fatalf("footer width = %d, want %d", gotWidth, 32)
	}
}
