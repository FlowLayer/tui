package main

import (
	"strings"
	"testing"
)

func TestRenderLogRow_StdoutNotRendered(t *testing.T) {
	entry := ServiceLog{
		Service:   "billing",
		Stream:    "stdout",
		Message:   "server started",
		Timestamp: "2026-04-05T12:00:00Z",
	}
	sw := computeServiceWidth([]ServiceLog{entry})
	row := renderLogRow(entry, 80, true, sw)
	if strings.Contains(row, "stdout") {
		t.Fatalf("stdout should not appear in rendered row, got %q", row)
	}
	if !strings.Contains(row, "server started") {
		t.Fatalf("message missing, got %q", row)
	}
}

func TestRenderLogRow_ServiceInOwnColumn(t *testing.T) {
	entry := ServiceLog{
		Service:   "billing",
		Stream:    "stdout",
		Message:   "hello",
		Timestamp: "2026-04-05T12:00:00Z",
	}
	sw := computeServiceWidth([]ServiceLog{entry})
	row := renderLogRow(entry, 80, true, sw)
	if !strings.Contains(row, "billing") {
		t.Fatalf("service name should appear, got %q", row)
	}
	// Must NOT have bracket-padded service like "[billing ]"
	if strings.Contains(row, "[billing") {
		t.Fatalf("service should not be bracket-wrapped, got %q", row)
	}
}

func TestRenderLogRow_ServiceHiddenInSingleMode(t *testing.T) {
	row := renderLogRow(ServiceLog{
		Service:   "billing",
		Stream:    "stdout",
		Message:   "hello",
		Timestamp: "2026-04-05T12:00:00Z",
	}, 80, false, 0)
	if strings.Contains(row, "billing") {
		t.Fatalf("service should not appear in single-service mode, got %q", row)
	}
	if !strings.Contains(row, "hello") {
		t.Fatalf("message missing, got %q", row)
	}
}

func TestRenderLogRow_StderrRendered(t *testing.T) {
	entry := ServiceLog{
		Service:   "billing",
		Stream:    "stderr",
		Message:   "crash",
		Timestamp: "2026-04-05T12:00:00Z",
	}
	sw := computeServiceWidth([]ServiceLog{entry})
	row := renderLogRow(entry, 80, true, sw)
	if !strings.Contains(row, "[stderr]") {
		t.Fatalf("stderr marker should be visible, got %q", row)
	}
	if !strings.Contains(row, "crash") {
		t.Fatalf("message missing, got %q", row)
	}
}

func TestRenderLogRow_TimestampShort(t *testing.T) {
	entry := ServiceLog{
		Service:   "users",
		Stream:    "stdout",
		Message:   "ready",
		Timestamp: "2026-04-05T09:36:25.038Z",
	}
	sw := computeServiceWidth([]ServiceLog{entry})
	row := renderLogRow(entry, 80, true, sw)
	if !strings.Contains(row, "09:36:25.038") {
		t.Fatalf("expected short timestamp HH:MM:SS.mmm, got %q", row)
	}
	if strings.Contains(row, "2026") {
		t.Fatalf("ISO date prefix should not appear, got %q", row)
	}
}

func TestRenderLogRow_ColumnsAreFixedWidth(t *testing.T) {
	// Two rows with different service names should have messages starting at the same offset
	// because serviceWidth is derived from the BATCH (max of all labels), not per-row.
	entries := []ServiceLog{
		{Service: "a", Stream: "stdout", Message: "msg1", Timestamp: "2026-04-05T12:00:00Z"},
		{Service: "longname", Stream: "stdout", Message: "msg2", Timestamp: "2026-04-05T12:00:00Z"},
	}
	sw := computeServiceWidth(entries)
	row1 := renderLogRow(entries[0], 80, true, sw)
	row2 := renderLogRow(entries[1], 80, true, sw)
	idx1 := strings.Index(row1, "msg1")
	idx2 := strings.Index(row2, "msg2")
	if idx1 != idx2 {
		t.Fatalf("messages should start at same column; row1 msg at %d, row2 msg at %d\nrow1=%q\nrow2=%q", idx1, idx2, row1, row2)
	}
}

func TestRenderLogRow_LongMessageWraps(t *testing.T) {
	// serviceWidth is derived from "billing" (7 chars)
	// innerWidth=50, usedWidth=logTimeWidth(12)+gap(1)+sw(7)+gap(1)=21, msgWidth=29
	// message "aaa bbb ccc ddd eee fff ggg hhh iii" (35 chars) must wrap
	entries := []ServiceLog{{
		Service:   "billing",
		Stream:    "stdout",
		Message:   "aaa bbb ccc ddd eee fff ggg hhh iii",
		Timestamp: "2026-04-05T12:00:00Z",
	}}
	sw := computeServiceWidth(entries)
	row := renderLogRow(entries[0], 50, true, sw)
	if !strings.Contains(row, "\n") {
		t.Fatalf("expected multi-line wrapped output, got %q", row)
	}
	lines := strings.Split(row, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d: %v", len(lines), lines)
	}
	// Continuation lines must start with exactly (logTimeWidth+logColumnGap+sw+logColumnGap) spaces.
	indent := strings.Repeat(" ", logTimeWidth+logColumnGap+sw+logColumnGap)
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, indent) {
			t.Fatalf("continuation line must start with %d-space indent, got %q", len(indent), l)
		}
	}
	// All message words must still be present across all lines.
	joined := strings.Join(lines, " ")
	for _, word := range []string{"aaa", "bbb", "ccc", "ddd", "eee", "fff", "ggg", "hhh", "iii"} {
		if !strings.Contains(joined, word) {
			t.Fatalf("word %q missing from wrapped output: %q", word, row)
		}
	}
}

func TestRenderLogRow_ShortMessageNoWrap(t *testing.T) {
	entry := ServiceLog{
		Service:   "billing",
		Stream:    "stdout",
		Message:   "ok",
		Timestamp: "2026-04-05T12:00:00Z",
	}
	sw := computeServiceWidth([]ServiceLog{entry})
	row := renderLogRow(entry, 80, true, sw)
	if strings.Contains(row, "\n") {
		t.Fatalf("short message should not wrap, got %q", row)
	}
}

func TestWrapWords_SoftBreak(t *testing.T) {
	chunks := wrapWords("hello world foo bar", 12)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %v", chunks)
	}
	for _, c := range chunks {
		if len([]rune(c)) > 12 {
			t.Fatalf("chunk %q exceeds width 12", c)
		}
	}
	joined := strings.Join(chunks, " ")
	if !strings.Contains(joined, "hello") || !strings.Contains(joined, "bar") {
		t.Fatalf("all words should be preserved, got %v", chunks)
	}
}

func TestWrapWords_HardBreakNoSpaces(t *testing.T) {
	chunks := wrapWords("abcdefghij", 4)
	for _, c := range chunks {
		if len([]rune(c)) > 4 {
			t.Fatalf("chunk %q exceeds hard-break width 4", c)
		}
	}
	joined := strings.Join(chunks, "")
	if joined != "abcdefghij" {
		t.Fatalf("all runes should be preserved, got %q", joined)
	}
}

func TestRenderLogRow_SingleServiceIndentNoService(t *testing.T) {
	// In single-service mode, continuation lines indent by logTimeWidth+logColumnGap only.
	row := renderLogRow(ServiceLog{
		Service:   "billing",
		Stream:    "stdout",
		Message:   "aaa bbb ccc ddd eee fff ggg hhh iii",
		Timestamp: "2026-04-05T12:00:00Z",
	}, 40, false, 0)
	lines := strings.Split(row, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapped output, got %q", row)
	}
	indent := strings.Repeat(" ", logTimeWidth+logColumnGap)
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, indent) {
			t.Fatalf("continuation line must start with %d-space indent, got %q", logTimeWidth+logColumnGap, l)
		}
	}
}

func TestFilterEmptyLogs(t *testing.T) {
	logs := []ServiceLog{
		{Service: "billing", Stream: "stdout", Message: "first", Timestamp: "T1"},
		{Service: "billing", Stream: "stdout", Message: "", Timestamp: "T2"},
		{Service: "billing", Stream: "stdout", Message: "   ", Timestamp: "T3"},
		{Service: "billing", Stream: "stdout", Message: "second", Timestamp: "T4"},
	}
	filtered := filterEmptyLogs(logs)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 non-empty entries, got %d: %v", len(filtered), filtered)
	}
	if filtered[0].Message != "first" {
		t.Fatalf("first entry should be 'first', got %q", filtered[0].Message)
	}
	if filtered[1].Message != "second" {
		t.Fatalf("second entry should be 'second', got %q", filtered[1].Message)
	}
}

func TestIsEmptyLogEntry(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"", true},
		{"   ", true},
		{"\t\n", true},
		{"hello", false},
		{" x ", false},
	}
	for _, tt := range tests {
		got := isEmptyLogEntry(ServiceLog{Message: tt.msg})
		if got != tt.want {
			t.Errorf("isEmptyLogEntry(Message=%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

// --- Layout-logic tests ---

// TestComputeServiceWidth_DerivedFromLabels verifies that the service column width
// is derived from the longest actual label in the batch, not a fixed constant.
func TestComputeServiceWidth_DerivedFromLabels(t *testing.T) {
	entries := []ServiceLog{
		{Service: "users"},
		{Service: "billing"},
		{Service: "api"},
	}
	sw := computeServiceWidth(entries)
	// "billing" is the longest label (7 chars)
	expected := len("billing")
	if sw != expected {
		t.Fatalf("service width should be %d (len of longest label 'billing'), got %d", expected, sw)
	}
}

// TestComputeServiceWidth_Empty verifies that an empty or nil batch yields width 0.
func TestComputeServiceWidth_Empty(t *testing.T) {
	if sw := computeServiceWidth(nil); sw != 0 {
		t.Fatalf("nil entries should yield service width 0, got %d", sw)
	}
	if sw := computeServiceWidth([]ServiceLog{}); sw != 0 {
		t.Fatalf("empty entries should yield service width 0, got %d", sw)
	}
}

// TestMessageWidth_UsesRemainingInnerWidth verifies that MESSAGE occupies exactly
// the space left after TIME and SERVICE columns are accounted for.
func TestMessageWidth_UsesRemainingInnerWidth(t *testing.T) {
	// service "billing" = 7 chars; innerWidth = 40
	// usedWidth = logTimeWidth(12) + gap(1) + sw(7) = 20
	// msgWidth  = 40 - 20 = 20
	entry := ServiceLog{Service: "billing", Stream: "stdout", Timestamp: "2026-04-05T12:00:00Z"}
	sw := computeServiceWidth([]ServiceLog{entry})
	innerWidth := 40
	msgWidth := innerWidth - logTimeWidth - logColumnGap - sw - logColumnGap

	// A message of exactly msgWidth chars fits on one line.
	entry.Message = strings.Repeat("x", msgWidth)
	rowFit := renderLogRow(entry, innerWidth, true, sw)
	if strings.Contains(rowFit, "\n") {
		t.Fatalf("message of exactly msgWidth=%d chars should not wrap, got %q", msgWidth, rowFit)
	}

	// A message of msgWidth+1 chars (no spaces) must hard-break onto a second line.
	entry.Message = strings.Repeat("x", msgWidth+1)
	rowWrap := renderLogRow(entry, innerWidth, true, sw)
	if !strings.Contains(rowWrap, "\n") {
		t.Fatalf("message of msgWidth+1=%d chars should wrap, got %q", msgWidth+1, rowWrap)
	}
}

// TestAllLogsMode_ServiceWidthFromActualLabels verifies that in all-logs mode the
// service column is only as wide as the longest label actually present.
func TestAllLogsMode_ServiceWidthFromActualLabels(t *testing.T) {
	entries := []ServiceLog{
		{Service: "ab", Stream: "stdout", Message: "hello", Timestamp: "2026-04-05T12:00:00Z"},
		{Service: "abcdefgh", Stream: "stdout", Message: "world", Timestamp: "2026-04-05T12:00:00Z"},
	}
	sw := computeServiceWidth(entries)
	if sw != len("abcdefgh") {
		t.Fatalf("expected sw=%d, got %d", len("abcdefgh"), sw)
	}
	// Both rows rendered with the same sw must have their messages at equal byte offsets.
	row1 := renderLogRow(entries[0], 80, true, sw)
	row2 := renderLogRow(entries[1], 80, true, sw)
	idx1 := strings.Index(row1, "hello")
	idx2 := strings.Index(row2, "world")
	if idx1 != idx2 {
		t.Fatalf("message start offset: row1=%d, row2=%d\nrow1=%q\nrow2=%q", idx1, idx2, row1, row2)
	}
}

// TestSingleServiceMode_NoServiceWidthWasted verifies that hiding the service column
// in single-service mode shifts MESSAGE further left (more message space).
func TestSingleServiceMode_NoServiceWidthWasted(t *testing.T) {
	entry := ServiceLog{
		Service:   "billing",
		Stream:    "stdout",
		Message:   strings.Repeat("w", 40),
		Timestamp: "2026-04-05T12:00:00Z",
	}
	innerWidth := 60
	sw := computeServiceWidth([]ServiceLog{entry})

	rowAll := renderLogRow(entry, innerWidth, true, sw)
	rowSingle := renderLogRow(entry, innerWidth, false, 0)

	// In all-logs mode the message wraps (usedWidth > logTimeWidth+logColumnGap).
	// In single-service mode the message starts earlier so it may not wrap.
	idxAll := strings.Index(rowAll, "w")
	idxSingle := strings.Index(rowSingle, "w")
	if idxSingle >= idxAll {
		t.Fatalf("message should start earlier in single-service mode (idxSingle=%d, idxAll=%d)", idxSingle, idxAll)
	}
}

// TestContinuationLinesAlignUnderMessage verifies hanging-indent in both modes.
func TestContinuationLinesAlignUnderMessage(t *testing.T) {
	// All-logs mode
	entry := ServiceLog{
		Service:   "billing",
		Stream:    "stdout",
		Message:   "aaa bbb ccc ddd eee fff ggg hhh iii jjj kkk",
		Timestamp: "2026-04-05T12:00:00Z",
	}
	sw := computeServiceWidth([]ServiceLog{entry})
	indentWidthAll := logTimeWidth + logColumnGap + sw + logColumnGap
	rowAll := renderLogRow(entry, 40, true, sw)
	linesAll := strings.Split(rowAll, "\n")
	if len(linesAll) < 2 {
		t.Fatalf("expected wrapped output in all-logs mode, got %q", rowAll)
	}
	for i, l := range linesAll[1:] {
		if !hasVisualIndent(l, indentWidthAll) {
			t.Fatalf("all-logs continuation line %d does not have %d-col indent, got %q", i+1, indentWidthAll, l)
		}
	}

	// Single-service mode
	indentWidthSingle := logTimeWidth + logColumnGap
	rowSingle := renderLogRow(entry, 30, false, 0)
	linesSingle := strings.Split(rowSingle, "\n")
	if len(linesSingle) < 2 {
		t.Fatalf("expected wrapped output in single-service mode, got %q", rowSingle)
	}
	for i, l := range linesSingle[1:] {
		if !hasVisualIndent(l, indentWidthSingle) {
			t.Fatalf("single-service continuation line %d does not have %d-col indent, got %q", i+1, indentWidthSingle, l)
		}
	}
}

// hasVisualIndent checks that a line starts with at least n visual columns of
// whitespace (plain or ANSI-styled spaces).
func hasVisualIndent(line string, n int) bool {
	// Walk runes, skipping ANSI escapes, counting space characters.
	spaceCount := 0
	inEsc := false
	for _, r := range line {
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if r == ' ' {
			spaceCount++
		} else {
			break
		}
	}
	return spaceCount >= n
}

// --- Responsive rendering tests ---

// TestWrapWords_RealNewlinesPreserved verifies that embedded newlines are
// handled by the caller (renderLogRow), not collapsed by wrapWords.
func TestWrapWords_RealNewlinesPreserved(t *testing.T) {
	// wrapWords no longer replaces \n; the caller splits beforehand.
	// A single call with no newlines should still work normally.
	chunks := wrapWords("hello world", 100)
	if len(chunks) != 1 || chunks[0] != "hello world" {
		t.Fatalf("expected single chunk 'hello world', got %v", chunks)
	}
}

// TestRenderLogRow_EmbeddedNewlines verifies that real \n in a message produces
// separate visual lines, each wrapped independently.
func TestRenderLogRow_EmbeddedNewlines(t *testing.T) {
	entry := ServiceLog{
		Service:   "api",
		Stream:    "stdout",
		Message:   "line one\nline two\nline three",
		Timestamp: "2026-04-05T12:00:00Z",
	}
	sw := computeServiceWidth([]ServiceLog{entry})
	// Wide enough that none of the sub-lines need wrapping.
	row := renderLogRow(entry, 80, true, sw)
	lines := strings.Split(row, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (one per embedded newline), got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "line one") {
		t.Fatalf("first line should contain 'line one', got %q", lines[0])
	}
	if !strings.Contains(lines[1], "line two") {
		t.Fatalf("second line should contain 'line two', got %q", lines[1])
	}
	if !strings.Contains(lines[2], "line three") {
		t.Fatalf("third line should contain 'line three', got %q", lines[2])
	}
}

// TestRenderLogRow_ResponsiveWiderUnwraps checks that a message that wrapped at
// a narrow width renders on one line when the width increases.
func TestRenderLogRow_ResponsiveWiderUnwraps(t *testing.T) {
	entry := ServiceLog{
		Service:   "billing",
		Stream:    "stdout",
		Message:   "aaa bbb ccc ddd eee",
		Timestamp: "2026-04-05T12:00:00Z",
	}
	sw := computeServiceWidth([]ServiceLog{entry})
	// usedWidth = 12+1+7+1 = 21

	// Narrow: force wrap (innerWidth=30 → msgWidth=9, message=19 chars → wraps)
	narrow := renderLogRow(entry, 30, true, sw)
	if !strings.Contains(narrow, "\n") {
		t.Fatalf("narrow render should wrap, got %q", narrow)
	}

	// Wide: no wrap needed (innerWidth=80 → msgWidth=59, message=19 chars → fits)
	wide := renderLogRow(entry, 80, true, sw)
	if strings.Contains(wide, "\n") {
		t.Fatalf("wide render should NOT wrap, got %q", wide)
	}
}

// TestRenderLogRow_NoSpuriousWrap verifies that a message fitting exactly in the
// available width is NOT wrapped.
func TestRenderLogRow_NoSpuriousWrap(t *testing.T) {
	entry := ServiceLog{
		Service:   "billing",
		Stream:    "stdout",
		Timestamp: "2026-04-05T12:00:00Z",
	}
	sw := computeServiceWidth([]ServiceLog{entry})
	// usedWidth = 12+1+7+1 = 21
	innerWidth := 50
	msgWidth := innerWidth - logTimeWidth - logColumnGap - sw - logColumnGap // 29

	// Message of exactly msgWidth chars.
	entry.Message = strings.Repeat("a", msgWidth)
	row := renderLogRow(entry, innerWidth, true, sw)
	if strings.Contains(row, "\n") {
		t.Fatalf("message of exactly msgWidth=%d should NOT wrap, got %q", msgWidth, row)
	}

	// Message of msgWidth-1 chars (shorter than available).
	entry.Message = strings.Repeat("a", msgWidth-1)
	row = renderLogRow(entry, innerWidth, true, sw)
	if strings.Contains(row, "\n") {
		t.Fatalf("message shorter than msgWidth should NOT wrap, got %q", row)
	}
}

// TestFitLine_VisualWidthPadding verifies that fitLine pads/truncates based on
// visual width, not rune count.
func TestFitLine_VisualWidthPadding(t *testing.T) {
	// ASCII: visual width == rune count
	padded := fitLine("hi", 10)
	if len(padded) != 10 {
		t.Fatalf("expected padded length 10, got %d: %q", len(padded), padded)
	}
	truncated := fitLine("hello world!", 5)
	if !strings.HasSuffix(truncated, "...") {
		t.Fatalf("expected ellipsis in truncated line, got %q", truncated)
	}
}
