package main

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

func TestAppScenarioLiveBufferPrunesToMaxEntries(t *testing.T) {
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

	if len(m.logEntries) != 3 {
		t.Fatalf("logEntries len = %d, want 3", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 3 || m.logEntries[1].Seq != 4 || m.logEntries[2].Seq != 5 {
		t.Fatalf("remaining seqs = [%d %d %d], want [3 4 5]", m.logEntries[0].Seq, m.logEntries[1].Seq, m.logEntries[2].Seq)
	}
	if len(m.seenLogSeqs) != 3 {
		t.Fatalf("seenLogSeqs len = %d, want 3", len(m.seenLogSeqs))
	}
	if _, exists := m.seenLogSeqs[2]; exists {
		t.Fatal("seenLogSeqs should not keep trimmed seq=2")
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

	if len(m.logEntries) != 3 {
		t.Fatalf("logEntries len after live append = %d, want 3", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 20 || m.logEntries[1].Seq != 31 || m.logEntries[2].Seq != 32 {
		t.Fatalf("seqs after bounded append = [%d %d %d], want [20 31 32]", m.logEntries[0].Seq, m.logEntries[1].Seq, m.logEntries[2].Seq)
	}
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
