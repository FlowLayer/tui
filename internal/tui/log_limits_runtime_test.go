package tui

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/FlowLayer/tui/internal/wsclient"
)

func TestModelFetchRequestTargetForSelectionOmitsLimitByDefault(t *testing.T) {
	m := newModel("127.0.0.1:6999", "", nil)

	serviceName := m.fetchRequestTargetForSelection(allLogsName)
	if serviceName != "" {
		t.Fatalf("all logs request target service = %q, want %q", serviceName, "")
	}

	serviceName = m.fetchRequestTargetForSelection("billing")
	if serviceName != "billing" {
		t.Fatalf("service request target = %q, want %q", serviceName, "billing")
	}
}

func TestModelReplayFetchRequestTargetOmitsLimitByDefault(t *testing.T) {
	m := newModel("127.0.0.1:6999", "", nil)
	m.serviceItems = []serviceItem{{name: "users", status: "ready"}}

	serviceName := m.replayFetchRequestTarget()
	if serviceName != "" {
		t.Fatalf("replay all logs target service = %q, want %q", serviceName, "")
	}

	m.serviceSelection = 1
	serviceName = m.replayFetchRequestTarget()
	if serviceName != "users" {
		t.Fatalf("replay users target service = %q, want %q", serviceName, "users")
	}
}

func TestAppScenarioLiveBufferDoesNotPruneToServerPageSize(t *testing.T) {
	m := newModel("127.0.0.1:6999", "", nil)
	m.serviceItems = []serviceItem{{name: "billing", status: "ready"}}
	limit := 3
	m.setEffectiveLogLimit(&limit)

	for seq := int64(1); seq <= 5; seq++ {
		payload := fmt.Sprintf(`{"seq":%d,"service":"billing","phase":"run","stream":"stdout","message":"line","timestamp":"2026-04-18T12:00:00Z"}`, seq)
		m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
			Name: "log",
			Data: json.RawMessage(payload),
		}})
	}

	if len(m.logEntries) != 5 {
		t.Fatalf("logEntries len = %d, want 5", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 1 || m.logEntries[1].Seq != 2 || m.logEntries[2].Seq != 3 || m.logEntries[3].Seq != 4 || m.logEntries[4].Seq != 5 {
		t.Fatalf("remaining seqs = [%d %d %d %d %d], want [1 2 3 4 5]", m.logEntries[0].Seq, m.logEntries[1].Seq, m.logEntries[2].Seq, m.logEntries[3].Seq, m.logEntries[4].Seq)
	}
	if len(m.seenLogSeqs) != 5 {
		t.Fatalf("seenLogSeqs len = %d, want 5", len(m.seenLogSeqs))
	}
	if _, exists := m.seenLogSeqs[2]; !exists {
		t.Fatal("seenLogSeqs should keep seq=2 when local cache is not exceeded")
	}

	localLimit := computeLocalLogCacheLimit(&limit)
	if localLimit != 1000 {
		t.Fatalf("local cache limit = %d, want 1000", localLimit)
	}
}

func TestAppScenarioMergeHistoricalAndLiveStaysBounded(t *testing.T) {
	m := newModel("127.0.0.1:6999", "", nil)
	m.serviceItems = []serviceItem{{name: "billing", status: "ready"}}
	m.logEntries = []ServiceLog{
		{Seq: 8, Service: "billing", Stream: "stdout", Message: "stale before request"},
		{Seq: 31, Service: "billing", Stream: "stdout", Message: "live after request"},
	}
	m.rebuildSeenLogSeqs()
	m.lastSeq = 31
	limit := 3

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, requestSeq: 20, result: ServiceLogsFetchResult{
		Status:         ServiceLogsFetchOK,
		EffectiveLimit: &limit,
		Logs: []ServiceLog{
			{Seq: 10, Service: "billing", Stream: "stdout", Message: "historical log"},
			{Seq: 20, Service: "billing", Stream: "stdout", Message: "historical newest at request time"},
		},
	}})

	if len(m.logEntries) != 3 {
		t.Fatalf("logEntries len after merge = %d, want 3", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 10 || m.logEntries[1].Seq != 20 || m.logEntries[2].Seq != 31 {
		t.Fatalf("seqs after merge = [%d %d %d], want [10 20 31]", m.logEntries[0].Seq, m.logEntries[1].Seq, m.logEntries[2].Seq)
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":32,"service":"billing","phase":"run","stream":"stdout","message":"new live","timestamp":"2026-04-18T12:00:01Z"}`),
	}})

	if len(m.logEntries) != 4 {
		t.Fatalf("logEntries len after live append = %d, want 4", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 10 || m.logEntries[1].Seq != 20 || m.logEntries[2].Seq != 31 || m.logEntries[3].Seq != 32 {
		t.Fatalf("seqs after bounded append = [%d %d %d %d], want [10 20 31 32]", m.logEntries[0].Seq, m.logEntries[1].Seq, m.logEntries[2].Seq, m.logEntries[3].Seq)
	}
	if len(m.logEntries) > computeLocalLogCacheLimit(&limit) {
		t.Fatalf("logEntries len after live append = %d, want <= %d", len(m.logEntries), computeLocalLogCacheLimit(&limit))
	}
}

func TestComputeLocalLogCacheLimitClamps(t *testing.T) {
	tests := []struct {
		name          string
		effective     *int
		wantLocalSize int
	}{
		{name: "nil fallback", effective: nil, wantLocalSize: 1000},
		{name: "non-positive fallback", effective: intPtr(0), wantLocalSize: 1000},
		{name: "small effective gets min", effective: intPtr(3), wantLocalSize: 1000},
		{name: "200 maps to 2000", effective: intPtr(200), wantLocalSize: 2000},
		{name: "500 maps to 5000", effective: intPtr(500), wantLocalSize: 5000},
		{name: "5000 clamps to max", effective: intPtr(5000), wantLocalSize: 10000},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := computeLocalLogCacheLimit(testCase.effective)
			if got != testCase.wantLocalSize {
				t.Fatalf("computeLocalLogCacheLimit(%v) = %d, want %d", testCase.effective, got, testCase.wantLocalSize)
			}
		})
	}
}

func TestAppScenarioLocalCacheRemainsBoundedUnderLiveTraffic(t *testing.T) {
	m := newModel("127.0.0.1:6999", "", nil)
	m.serviceItems = []serviceItem{{name: "billing", status: "ready"}}
	effectiveLimit := 200
	m.setEffectiveLogLimit(&effectiveLimit)

	localLimit := computeLocalLogCacheLimit(&effectiveLimit)
	totalLive := localLimit + 250

	for seq := int64(1); seq <= int64(totalLive); seq++ {
		m.updateLastSeq(seq)
		if !m.appendLogEntryIfVisible(ServiceLog{Seq: seq, Service: "billing", Stream: "stdout", Message: "live"}) {
			t.Fatalf("expected live log seq=%d to be visible", seq)
		}
		m.enforceVisibleLogBufferLimit()
	}

	if len(m.logEntries) > localLimit {
		t.Fatalf("logEntries len = %d, want <= %d", len(m.logEntries), localLimit)
	}
	if len(m.logEntries) != localLimit {
		t.Fatalf("logEntries len after sustained live traffic = %d, want %d", len(m.logEntries), localLimit)
	}

	wantFirstSeq := int64(totalLive-localLimit+1)
	wantLastSeq := int64(totalLive)
	if m.logEntries[0].Seq != wantFirstSeq {
		t.Fatalf("first seq after trim = %d, want %d", m.logEntries[0].Seq, wantFirstSeq)
	}
	if m.logEntries[len(m.logEntries)-1].Seq != wantLastSeq {
		t.Fatalf("last seq after trim = %d, want %d", m.logEntries[len(m.logEntries)-1].Seq, wantLastSeq)
	}
	if _, exists := m.seenLogSeqs[wantFirstSeq-1]; exists {
		t.Fatalf("seenLogSeqs should not keep trimmed seq=%d", wantFirstSeq-1)
	}
	if _, exists := m.seenLogSeqs[wantLastSeq]; !exists {
		t.Fatalf("seenLogSeqs should keep tail seq=%d", wantLastSeq)
	}
}

func intPtr(value int) *int {
	return &value
}

func TestAppScenarioHistoricalFetchUsesServerEffectiveLimit(t *testing.T) {
	m := newModel("127.0.0.1:6999", "", nil)
	m.serviceItems = []serviceItem{{name: "billing", status: "ready"}}
	serverLimit := 5

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{
		Status:         ServiceLogsFetchOK,
		EffectiveLimit: &serverLimit,
		Logs: []ServiceLog{
			{Seq: 1, Service: "billing", Stream: "stdout", Message: "line-1"},
			{Seq: 2, Service: "billing", Stream: "stdout", Message: "line-2"},
			{Seq: 3, Service: "billing", Stream: "stdout", Message: "line-3"},
			{Seq: 4, Service: "billing", Stream: "stdout", Message: "line-4"},
			{Seq: 5, Service: "billing", Stream: "stdout", Message: "line-5"},
			{Seq: 6, Service: "billing", Stream: "stdout", Message: "line-6"},
		},
	}})

	if len(m.logEntries) != 5 {
		t.Fatalf("logEntries len = %d, want 5", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 2 || m.logEntries[4].Seq != 6 {
		t.Fatalf("historical bounds = [%d ... %d], want [2 ... 6]", m.logEntries[0].Seq, m.logEntries[4].Seq)
	}
	if m.effectiveLogLimit == nil || *m.effectiveLogLimit != 5 {
		t.Fatalf("effectiveLogLimit = %v, want 5", m.effectiveLogLimit)
	}
}
