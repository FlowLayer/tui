package tui

import (
	"fmt"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FlowLayer/tui/internal/wsclient"
	tea "github.com/charmbracelet/bubbletea"
)

func applyUpdate(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()

	updated, _ := m.Update(msg)
	casted, ok := updated.(model)
	if !ok {
		t.Fatalf("updated model type = %T, want model", updated)
	}

	return casted
}

func TestAppScenarioWSHelloSnapshotServiceStatusLog(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "hello",
		Data: json.RawMessage(`{"protocol_version":1,"server":"flowlayer","capabilities":["get_snapshot"]}`),
	}})
	if m.connectionLabel != "Connected" {
		t.Fatalf("connection label = %q, want %q", m.connectionLabel, "Connected")
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"stopped"}]}`),
	}})
	if len(m.serviceItems) != 2 {
		t.Fatalf("services len = %d, want 2", len(m.serviceItems))
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "service_status",
		Data: json.RawMessage(`{"service":"billing","status":"running","timestamp":"2026-04-14T12:00:00Z"}`),
	}})
	if m.serviceItems[0].name != "billing" || m.serviceItems[0].status != "running" {
		t.Fatalf("billing state = %+v, want billing/running", m.serviceItems[0])
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":7,"service":"billing","phase":"start","stream":"stdout","message":"service booted","timestamp":"2026-04-14T12:00:01Z"}`),
	}})

	if m.lastSeq != 7 {
		t.Fatalf("lastSeq = %d, want 7", m.lastSeq)
	}
	if len(m.logEntries) != 1 {
		t.Fatalf("logEntries len = %d, want 1", len(m.logEntries))
	}
	if m.logEntries[0].Message != "service booted" {
		t.Fatalf("log message = %q, want %q", m.logEntries[0].Message, "service booted")
	}
}

func TestAppScenarioWSLogSelectionFilterAndLastSeq(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})

	m.serviceSelection = 1
	selected, ok := m.selectedService()
	if !ok || selected.name != "billing" {
		t.Fatalf("selected service = %+v, want billing", selected)
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":10,"service":"users","phase":"start","stream":"stdout","message":"users booted","timestamp":"2026-04-14T12:00:01Z"}`),
	}})
	if m.lastSeq != 10 {
		t.Fatalf("lastSeq after users log = %d, want 10", m.lastSeq)
	}
	if len(m.logEntries) != 0 {
		t.Fatalf("logEntries len after users log = %d, want 0", len(m.logEntries))
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":11,"service":"billing","phase":"start","stream":"stdout","message":"billing booted","timestamp":"2026-04-14T12:00:02Z"}`),
	}})
	if m.lastSeq != 11 {
		t.Fatalf("lastSeq after billing log = %d, want 11", m.lastSeq)
	}
	if len(m.logEntries) != 1 {
		t.Fatalf("logEntries len after billing log = %d, want 1", len(m.logEntries))
	}
	if m.logEntries[0].Service != "billing" {
		t.Fatalf("log service = %q, want %q", m.logEntries[0].Service, "billing")
	}
}

func TestAppScenarioWSMalformedSnapshotIgnored(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m.serviceItems = []serviceItem{{name: "billing", status: "ready"}}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[`),
	}})

	if len(m.serviceItems) != 1 {
		t.Fatalf("services len = %d, want 1", len(m.serviceItems))
	}
	if m.serviceItems[0].name != "billing" || m.serviceItems[0].status != "ready" {
		t.Fatalf("service = %+v, want billing/ready", m.serviceItems[0])
	}
}

func TestAppScenarioHistoricalLogsUpdateLastSeq(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{
		Status:    ServiceLogsFetchOK,
		Truncated: true,
		Logs: []ServiceLog{
			{Seq: 3, Service: "billing", Stream: "stdout", Message: "old log"},
			{Seq: 7, Service: "billing", Stream: "stdout", Message: "new log"},
		},
	}})

	if m.lastSeq != 7 {
		t.Fatalf("lastSeq = %d, want 7", m.lastSeq)
	}
	if len(m.logEntries) != 2 {
		t.Fatalf("logEntries len = %d, want 2", len(m.logEntries))
	}
	if !m.logsTruncated {
		t.Fatal("expected logsTruncated=true")
	}
}

func TestAppScenarioLogsTruncatedResetsOnSelectionChange(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{
		Status:    ServiceLogsFetchOK,
		Truncated: true,
		Logs: []ServiceLog{
			{Seq: 2, Service: "billing", Stream: "stdout", Message: "historical"},
		},
	}})
	if !m.logsTruncated {
		t.Fatal("expected logsTruncated=true on all logs after fetch")
	}

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "billing" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "billing")
	}
	if m.logsTruncated {
		t.Fatal("expected logsTruncated=false after selection change")
	}
}

func TestAppScenarioLogsTruncatedTracksCurrentSelectionOnly(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "billing" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "billing")
	}

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{
		Status:    ServiceLogsFetchOK,
		Truncated: true,
		Logs:      []ServiceLog{{Seq: 1, Service: "billing", Stream: "stdout", Message: "other selection"}},
	}})
	if m.logsTruncated {
		t.Fatal("logsTruncated should not update for non-selected fetch response")
	}

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "billing", result: ServiceLogsFetchResult{
		Status:    ServiceLogsFetchOK,
		Truncated: true,
		Logs:      []ServiceLog{{Seq: 2, Service: "billing", Stream: "stdout", Message: "selected selection"}},
	}})
	if !m.logsTruncated {
		t.Fatal("expected logsTruncated=true for selected fetch response")
	}
}

func TestAppScenarioSelectionChangeClearsLogsImmediatelyBeforeFetchResponse(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "billing" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "billing")
	}

	limit := 5
	m.logEntries = []ServiceLog{
		{Seq: 1, Service: "billing", Stream: "stdout", Message: "billing-1"},
		{Seq: 2, Service: "billing", Stream: "stdout", Message: "billing-2"},
	}
	m.rebuildSeenLogSeqs()
	m.logsTruncated = true
	m.loadingOlderLogs = true
	m.noOlderLogsAvailable = true
	m.setEffectiveLogLimit(&limit)

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "users" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "users")
	}
	if len(m.logEntries) != 0 {
		t.Fatalf("logEntries len after selection change = %d, want 0", len(m.logEntries))
	}
	if len(m.seenLogSeqs) != 0 {
		t.Fatalf("seenLogSeqs len after selection change = %d, want 0", len(m.seenLogSeqs))
	}
	if m.logsTruncated {
		t.Fatal("logsTruncated should reset after selection change")
	}
	if m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should reset after selection change")
	}
	if m.noOlderLogsAvailable {
		t.Fatal("noOlderLogsAvailable should reset after selection change")
	}
	if m.effectiveLogLimit != nil {
		t.Fatalf("effectiveLogLimit after selection change = %v, want nil", m.effectiveLogLimit)
	}
	if !strings.Contains(m.logsViewport.View(), "No logs") {
		t.Fatalf("logs viewport should reflect cleared state, got %q", m.logsViewport.View())
	}
}

func TestAppScenarioSelectionChangeKeepsOnlyNewServiceLiveLogsDuringFetch(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "billing" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "billing")
	}

	m.logEntries = []ServiceLog{
		{Seq: 7, Service: "billing", Stream: "stdout", Message: "billing stale 1"},
		{Seq: 8, Service: "billing", Stream: "stdout", Message: "billing stale 2"},
	}
	m.rebuildSeenLogSeqs()
	m.lastSeq = 20

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "users" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "users")
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":21,"service":"users","phase":"run","stream":"stdout","message":"users live while fetching","timestamp":"2026-04-14T12:00:03Z"}`),
	}})

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "users", requestSeq: 20, result: ServiceLogsFetchResult{
		Status: ServiceLogsFetchOK,
		Logs: []ServiceLog{
			{Seq: 19, Service: "users", Stream: "stdout", Message: "users historical 19"},
			{Seq: 20, Service: "users", Stream: "stdout", Message: "users historical 20"},
		},
	}})

	if len(m.logEntries) != 3 {
		t.Fatalf("logEntries len after users fetch merge = %d, want 3", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 19 || m.logEntries[1].Seq != 20 || m.logEntries[2].Seq != 21 {
		t.Fatalf("seqs after users fetch merge = [%d %d %d], want [19 20 21]", m.logEntries[0].Seq, m.logEntries[1].Seq, m.logEntries[2].Seq)
	}
	for _, entry := range m.logEntries {
		if entry.Service != "users" {
			t.Fatalf("entry service = %q, want only users entries", entry.Service)
		}
	}
}

func TestAppScenarioStaleServiceLogsResponseIgnoredAfterSelectionChange(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "users" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "users")
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":50,"service":"users","phase":"run","stream":"stdout","message":"users baseline","timestamp":"2026-04-14T12:00:04Z"}`),
	}})

	if len(m.logEntries) != 1 {
		t.Fatalf("baseline logEntries len = %d, want 1", len(m.logEntries))
	}
	m.logsFollow = true
	m.setPersistentFooter("connected")
	footerBefore := m.footerMessage
	footerTransientBefore := m.footerTransient

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "billing", requestSeq: 10, result: ServiceLogsFetchResult{
		Status:    ServiceLogsFetchOK,
		Truncated: true,
		Logs: []ServiceLog{
			{Seq: 9, Service: "billing", Stream: "stdout", Message: "billing stale historical"},
		},
	}})

	if len(m.logEntries) != 1 {
		t.Fatalf("logEntries len after stale billing response = %d, want 1", len(m.logEntries))
	}
	if m.logEntries[0].Service != "users" || m.logEntries[0].Seq != 50 {
		t.Fatalf("log entry after stale billing response = %+v, want users seq 50", m.logEntries[0])
	}
	if m.logsTruncated {
		t.Fatal("logsTruncated should not be updated by stale service response")
	}
	if !m.logsFollow {
		t.Fatal("stale service response should not disable follow mode")
	}
	if m.footerMessage != footerBefore {
		t.Fatalf("footer message after stale response = %q, want %q", m.footerMessage, footerBefore)
	}
	if m.footerTransient != footerTransientBefore {
		t.Fatalf("footer transient after stale response = %v, want %v", m.footerTransient, footerTransientBefore)
	}
	if strings.Contains(strings.ToLower(m.footerMessage), "loaded") {
		t.Fatalf("stale service response should not set loaded footer, got %q", m.footerMessage)
	}
}

func TestAppScenarioStaleReplayLogsResponseIgnoredAfterSelectionChange(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "billing" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "billing")
	}

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "users" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "users")
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":60,"service":"users","phase":"run","stream":"stdout","message":"users baseline","timestamp":"2026-04-14T12:00:05Z"}`),
	}})

	if m.lastSeq != 60 {
		t.Fatalf("lastSeq baseline = %d, want 60", m.lastSeq)
	}

	staleLimit := 7
	m = applyUpdate(t, m, replayLogsLoadedMsg{serviceName: "billing", result: ServiceLogsFetchResult{
		Status:         ServiceLogsFetchOK,
		EffectiveLimit: &staleLimit,
		Logs: []ServiceLog{
			{Seq: 61, Service: "billing", Stream: "stdout", Message: "billing stale replay"},
		},
	}})

	if m.effectiveLogLimit != nil {
		t.Fatalf("effectiveLogLimit after stale replay = %v, want nil", m.effectiveLogLimit)
	}
	if m.lastSeq != 60 {
		t.Fatalf("lastSeq after stale replay = %d, want 60", m.lastSeq)
	}
	if len(m.logEntries) != 1 {
		t.Fatalf("logEntries len after stale replay = %d, want 1", len(m.logEntries))
	}
	if m.logEntries[0].Service != "users" || m.logEntries[0].Seq != 60 {
		t.Fatalf("log entry after stale replay = %+v, want users seq 60", m.logEntries[0])
	}
}

func TestAppScenarioServiceLogsLoadedKeepsLiveLogsAfterRequestStart(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})

	m.logEntries = []ServiceLog{
		{Seq: 8, Service: "billing", Stream: "stdout", Message: "stale before request"},
		{Seq: 31, Service: "billing", Stream: "stdout", Message: "live after request"},
	}
	m.rebuildSeenLogSeqs()
	m.lastSeq = 31

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, requestSeq: 20, result: ServiceLogsFetchResult{
		Status: ServiceLogsFetchOK,
		Logs: []ServiceLog{
			{Seq: 10, Service: "billing", Stream: "stdout", Message: "historical log"},
			{Seq: 20, Service: "billing", Stream: "stdout", Message: "historical newest at request time"},
		},
	}})

	if len(m.logEntries) != 3 {
		t.Fatalf("logEntries len = %d, want 3", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 10 {
		t.Fatalf("first seq = %d, want 10", m.logEntries[0].Seq)
	}
	if m.logEntries[1].Seq != 20 {
		t.Fatalf("second seq = %d, want 20", m.logEntries[1].Seq)
	}
	if m.logEntries[2].Seq != 31 {
		t.Fatalf("third seq = %d, want 31", m.logEntries[2].Seq)
	}
	if m.logsFollow {
		t.Fatal("expected follow mode disabled after historical load")
	}
	if m.footerMessage != "loaded 2 historical entries" {
		t.Fatalf("footer message = %q, want %q", m.footerMessage, "loaded 2 historical entries")
	}
	if !m.footerTransient {
		t.Fatal("historical loaded footer should be transient")
	}
}

func TestAppScenarioReplayAfterReconnectAndNoSeqDuplicate(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{
		Status: ServiceLogsFetchOK,
		Logs:   []ServiceLog{{Seq: 20, Service: "billing", Stream: "stdout", Message: "already shown"}},
	}})

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "hello",
		Data: json.RawMessage(`{"protocol_version":1,"server":"flowlayer","capabilities":["get_snapshot"]}`),
	}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, replayLogsLoadedMsg{serviceName: "", result: ServiceLogsFetchResult{
		Status: ServiceLogsFetchOK,
		Logs: []ServiceLog{
			{Seq: 20, Service: "billing", Stream: "stdout", Message: "duplicate replay"},
			{Seq: 21, Service: "billing", Stream: "stdout", Message: "missed while reconnecting"},
		},
	}})

	if m.lastSeq != 21 {
		t.Fatalf("lastSeq after replay = %d, want 21", m.lastSeq)
	}
	if len(m.logEntries) != 2 {
		t.Fatalf("logEntries len after replay = %d, want 2", len(m.logEntries))
	}
	if m.logEntries[1].Seq != 21 {
		t.Fatalf("last replayed seq = %d, want 21", m.logEntries[1].Seq)
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":21,"service":"billing","phase":"run","stream":"stdout","message":"duplicate live","timestamp":"2026-04-14T12:00:03Z"}`),
	}})

	if len(m.logEntries) != 2 {
		t.Fatalf("logEntries len after duplicate live = %d, want 2", len(m.logEntries))
	}
}

func TestAppScenarioOlderLogsLoadedPrependsAndFlagsExhaustion(t *testing.T) {
m := newModel("127.0.0.1:3000", "", nil)
m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
Name: "snapshot",
Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
}})

m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
if m.selectedServiceName() != "billing" {
t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "billing")
}

m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "billing", result: ServiceLogsFetchResult{
Status: ServiceLogsFetchOK,
Logs: []ServiceLog{
{Seq: 10, Service: "billing", Stream: "stdout", Message: "tenth"},
{Seq: 11, Service: "billing", Stream: "stdout", Message: "eleventh"},
},
}})
if len(m.logEntries) != 2 {
t.Fatalf("logEntries after initial load = %d, want 2", len(m.logEntries))
}

m.loadingOlderLogs = true
m = applyUpdate(t, m, olderLogsLoadedMsg{serviceName: "billing", beforeSeq: 10, result: ServiceLogsFetchResult{
Status: ServiceLogsFetchOK,
Logs: []ServiceLog{
{Seq: 8, Service: "billing", Stream: "stdout", Message: "eighth"},
{Seq: 9, Service: "billing", Stream: "stdout", Message: "ninth"},
},
}})
if m.loadingOlderLogs {
t.Fatal("loadingOlderLogs should clear after response")
}
if len(m.logEntries) != 4 {
t.Fatalf("logEntries after prepend = %d, want 4", len(m.logEntries))
}
if m.logEntries[0].Seq != 8 || m.logEntries[3].Seq != 11 {
t.Fatalf("ordering wrong after prepend: seqs = [%d..%d]", m.logEntries[0].Seq, m.logEntries[3].Seq)
}
if m.logsFollow {
t.Fatal("follow mode should disengage when prepending older history")
}

// A second response yielding only already-seen entries should mark the
// stream as exhausted so the TUI stops re-issuing requests.
m.loadingOlderLogs = true
m = applyUpdate(t, m, olderLogsLoadedMsg{serviceName: "billing", beforeSeq: 8, result: ServiceLogsFetchResult{
Status: ServiceLogsFetchOK,
Logs:   []ServiceLog{{Seq: 9, Service: "billing", Stream: "stdout", Message: "ninth"}},
}})
if m.loadingOlderLogs {
t.Fatal("loadingOlderLogs should clear even when no new entries")
}
if !m.noOlderLogsAvailable {
t.Fatal("noOlderLogsAvailable should latch when the response brings nothing new")
}
if len(m.logEntries) != 4 {
t.Fatalf("logEntries should be unchanged when older response is empty; got %d", len(m.logEntries))
}
}

func TestAppScenarioOlderLogsResetOnSelectionChange(t *testing.T) {
m := newModel("127.0.0.1:3000", "", nil)
m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
Name: "snapshot",
Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
}})

m.noOlderLogsAvailable = true
m.loadingOlderLogs = true
m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
if m.noOlderLogsAvailable {
t.Fatal("noOlderLogsAvailable should reset when selection changes")
}
if m.loadingOlderLogs {
t.Fatal("loadingOlderLogs should reset when selection changes")
}
}

func TestAppScenarioSelectionChangeLiveLogsDoNotEvictHistory(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "billing" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "billing")
	}

	m.logEntries = []ServiceLog{{Seq: 88, Service: "billing", Stream: "stdout", Message: "billing stale before switch"}}
	m.rebuildSeenLogSeqs()
	m.lastSeq = 100

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "users" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "users")
	}
	if len(m.logEntries) != 0 {
		t.Fatalf("logEntries len after switch = %d, want 0", len(m.logEntries))
	}

	for seq := int64(101); seq <= 130; seq++ {
		m.updateLastSeq(seq)
		if !m.appendLogEntryIfVisible(ServiceLog{Seq: seq, Service: "users", Stream: "stdout", Message: "users live while loading"}) {
			t.Fatalf("expected live log seq=%d to be visible", seq)
		}
	}

	limit := 10
	loaded := make([]ServiceLog, 0, 10)
	for seq := int64(91); seq <= 100; seq++ {
		loaded = append(loaded, ServiceLog{Seq: seq, Service: "users", Stream: "stdout", Message: "users historical"})
	}

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "users", requestSeq: 100, result: ServiceLogsFetchResult{
		Status:         ServiceLogsFetchOK,
		EffectiveLimit: &limit,
		Logs:           loaded,
	}})

	if len(m.logEntries) > limit {
		t.Fatalf("logEntries len after merge = %d, want <= %d", len(m.logEntries), limit)
	}

	hasHistorical := false
	hasLive := false
	for _, entry := range m.logEntries {
		if entry.Service != "users" {
			t.Fatalf("entry service = %q, want users only", entry.Service)
		}
		if entry.Seq <= 100 {
			hasHistorical = true
		}
		if entry.Seq > 100 {
			hasLive = true
		}
	}

	if !hasHistorical {
		t.Fatal("expected at least one historical entry after merge")
	}
	if !hasLive {
		t.Fatal("expected at least one live entry after merge")
	}
	if m.logsFollow {
		t.Fatal("expected follow mode disabled after non-empty historical response")
	}
	if m.footerMessage != "loaded 10 historical entries" {
		t.Fatalf("footer message = %q, want %q", m.footerMessage, "loaded 10 historical entries")
	}
}

func TestAppScenarioHistoricalLoadKeepsViewportAtTopAndShowsLoadedFooter(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})

	loaded := make([]ServiceLog, 0, 120)
	for seq := int64(1); seq <= 120; seq++ {
		loaded = append(loaded, ServiceLog{
			Seq:     seq,
			Service: "billing",
			Stream:  "stdout",
			Message: fmt.Sprintf("historical-%d", seq),
		})
	}

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{
		Status: ServiceLogsFetchOK,
		Logs:   loaded,
	}})

	if m.logsFollow {
		t.Fatal("expected follow mode disabled after non-empty historical all-logs response")
	}
	if m.logsViewport.YOffset != 0 {
		t.Fatalf("viewport yOffset after historical load = %d, want 0", m.logsViewport.YOffset)
	}
	if m.logsViewport.AtBottom() {
		t.Fatal("viewport should not jump to bottom right after historical load")
	}
	if m.footerMessage != "loaded 120 historical entries" {
		t.Fatalf("footer message = %q, want %q", m.footerMessage, "loaded 120 historical entries")
	}
	if !m.footerTransient {
		t.Fatal("expected historical loaded footer to be transient")
	}
}

func TestAppScenarioSelectionChangeManyLiveLogsKeepsHistoricalAndLive(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "billing" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "billing")
	}

	m.lastSeq = 200
	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "users" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "users")
	}

	for seq := int64(201); seq <= 420; seq++ {
		m.updateLastSeq(seq)
		if !m.appendLogEntryIfVisible(ServiceLog{Seq: seq, Service: "users", Stream: "stdout", Message: "users live burst"}) {
			t.Fatalf("expected live log seq=%d to be visible", seq)
		}
	}

	loaded := make([]ServiceLog, 0, 200)
	for seq := int64(1); seq <= 200; seq++ {
		loaded = append(loaded, ServiceLog{Seq: seq, Service: "users", Stream: "stdout", Message: "users historical"})
	}
	limit := 200

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "users", requestSeq: 200, result: ServiceLogsFetchResult{
		Status:         ServiceLogsFetchOK,
		EffectiveLimit: &limit,
		Logs:           loaded,
	}})

	if len(m.logEntries) > limit {
		t.Fatalf("logEntries len after merge = %d, want <= %d", len(m.logEntries), limit)
	}

	hasHistorical := false
	hasLive := false
	for _, entry := range m.logEntries {
		if entry.Service != "users" {
			t.Fatalf("entry service = %q, want users only", entry.Service)
		}
		if entry.Seq <= 200 {
			hasHistorical = true
		}
		if entry.Seq > 200 {
			hasLive = true
		}
	}

	if !hasHistorical {
		t.Fatal("expected at least one historical entry when live volume exceeds limit")
	}
	if !hasLive {
		t.Fatal("expected at least one recent live entry when live volume exceeds limit")
	}
}

func TestAppScenarioBackwardPaginationKeepsOlderEntriesAfterLiveAppend(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "billing" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "billing")
	}

	effectiveLimit := 200
	tail := make([]ServiceLog, 0, 200)
	for seq := int64(801); seq <= 1000; seq++ {
		tail = append(tail, ServiceLog{Seq: seq, Service: "billing", Stream: "stdout", Message: fmt.Sprintf("tail-%d", seq)})
	}
	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "billing", result: ServiceLogsFetchResult{
		Status:         ServiceLogsFetchOK,
		EffectiveLimit: &effectiveLimit,
		Logs:           tail,
	}})

	olderPage := make([]ServiceLog, 0, 200)
	for seq := int64(601); seq <= 800; seq++ {
		olderPage = append(olderPage, ServiceLog{Seq: seq, Service: "billing", Stream: "stdout", Message: fmt.Sprintf("older-%d", seq)})
	}
	m = applyUpdate(t, m, olderLogsLoadedMsg{serviceName: "billing", beforeSeq: 801, result: ServiceLogsFetchResult{
		Status: ServiceLogsFetchOK,
		Logs:   olderPage,
	}})

	if len(m.logEntries) != 400 {
		t.Fatalf("logEntries len after older page = %d, want 400", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 601 || m.logEntries[len(m.logEntries)-1].Seq != 1000 {
		t.Fatalf("seq bounds after older page = [%d ... %d], want [601 ... 1000]", m.logEntries[0].Seq, m.logEntries[len(m.logEntries)-1].Seq)
	}
	if m.footerMessage != "loaded 200 older entries" {
		t.Fatalf("footer message after older page = %q, want %q", m.footerMessage, "loaded 200 older entries")
	}
	if !m.footerTransient {
		t.Fatal("older loaded footer should be transient")
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":1001,"service":"billing","phase":"run","stream":"stdout","message":"live-1001","timestamp":"2026-05-02T10:00:01Z"}`),
	}})

	if len(m.logEntries) != 401 {
		t.Fatalf("logEntries len after one live append = %d, want 401", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 601 {
		t.Fatalf("oldest seq after one live append = %d, want 601", m.logEntries[0].Seq)
	}
	if lowestSeq(m.logEntries) != 601 {
		t.Fatalf("lowest seq after one live append = %d, want 601", lowestSeq(m.logEntries))
	}
	foundOlder := false
	for _, entry := range m.logEntries {
		if entry.Seq == 650 {
			foundOlder = true
			break
		}
	}
	if !foundOlder {
		t.Fatal("older page entries should remain present after one live append")
	}
	if len(m.logEntries) > computeLocalLogCacheLimit(&effectiveLimit) {
		t.Fatalf("logEntries len = %d, want <= local limit %d", len(m.logEntries), computeLocalLogCacheLimit(&effectiveLimit))
	}
}

func TestAppScenarioBackwardPaginationAccumulatesMultiplePages(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "billing" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "billing")
	}

	effectiveLimit := 200
	tail := make([]ServiceLog, 0, 200)
	for seq := int64(801); seq <= 1000; seq++ {
		tail = append(tail, ServiceLog{Seq: seq, Service: "billing", Stream: "stdout", Message: fmt.Sprintf("tail-%d", seq)})
	}
	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "billing", result: ServiceLogsFetchResult{
		Status:         ServiceLogsFetchOK,
		EffectiveLimit: &effectiveLimit,
		Logs:           tail,
	}})
	if got := lowestSeq(m.logEntries); got != 801 {
		t.Fatalf("lowest seq after initial tail = %d, want 801", got)
	}

	olderPageOne := make([]ServiceLog, 0, 200)
	for seq := int64(601); seq <= 800; seq++ {
		olderPageOne = append(olderPageOne, ServiceLog{Seq: seq, Service: "billing", Stream: "stdout", Message: fmt.Sprintf("older-1-%d", seq)})
	}
	m = applyUpdate(t, m, olderLogsLoadedMsg{serviceName: "billing", beforeSeq: 801, result: ServiceLogsFetchResult{
		Status: ServiceLogsFetchOK,
		Logs:   olderPageOne,
	}})
	if got := lowestSeq(m.logEntries); got != 601 {
		t.Fatalf("lowest seq after first older page = %d, want 601", got)
	}

	olderPageTwo := make([]ServiceLog, 0, 200)
	for seq := int64(401); seq <= 600; seq++ {
		olderPageTwo = append(olderPageTwo, ServiceLog{Seq: seq, Service: "billing", Stream: "stdout", Message: fmt.Sprintf("older-2-%d", seq)})
	}
	m = applyUpdate(t, m, olderLogsLoadedMsg{serviceName: "billing", beforeSeq: 601, result: ServiceLogsFetchResult{
		Status: ServiceLogsFetchOK,
		Logs:   olderPageTwo,
	}})

	if len(m.logEntries) != 600 {
		t.Fatalf("logEntries len after two older pages = %d, want 600", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 401 || m.logEntries[len(m.logEntries)-1].Seq != 1000 {
		t.Fatalf("seq bounds after two older pages = [%d ... %d], want [401 ... 1000]", m.logEntries[0].Seq, m.logEntries[len(m.logEntries)-1].Seq)
	}
	if got := lowestSeq(m.logEntries); got != 401 {
		t.Fatalf("lowest seq after second older page = %d, want 401", got)
	}
	if len(m.logEntries) > computeLocalLogCacheLimit(&effectiveLimit) {
		t.Fatalf("logEntries len = %d, want <= local limit %d", len(m.logEntries), computeLocalLogCacheLimit(&effectiveLimit))
	}
}

func TestAppScenarioInitialFetchKeepsSinglePageUntilBackwardPagination(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "billing" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "billing")
	}

	effectiveLimit := 200
	tail := make([]ServiceLog, 0, 200)
	for seq := int64(801); seq <= 1000; seq++ {
		tail = append(tail, ServiceLog{Seq: seq, Service: "billing", Stream: "stdout", Message: fmt.Sprintf("tail-%d", seq)})
	}
	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "billing", result: ServiceLogsFetchResult{
		Status:         ServiceLogsFetchOK,
		EffectiveLimit: &effectiveLimit,
		Logs:           tail,
	}})

	if len(m.logEntries) != 200 {
		t.Fatalf("logEntries len after initial fetch = %d, want 200", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 801 || m.logEntries[len(m.logEntries)-1].Seq != 1000 {
		t.Fatalf("seq bounds after initial fetch = [%d ... %d], want [801 ... 1000]", m.logEntries[0].Seq, m.logEntries[len(m.logEntries)-1].Seq)
	}
	if got := lowestSeq(m.logEntries); got != 801 {
		t.Fatalf("lowest seq after initial fetch = %d, want 801", got)
	}
	if m.noOlderLogsAvailable {
		t.Fatal("initial fetch should not imply older history is exhausted")
	}
}

func TestAppScenarioAllLogsBackwardPaginationKeepsOlderAfterLiveAppend(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})

	if m.selectedServiceName() != allLogsName {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), allLogsName)
	}

	effectiveLimit := 200
	tail := make([]ServiceLog, 0, 200)
	for seq := int64(801); seq <= 1000; seq++ {
		serviceName := "billing"
		if seq%2 == 0 {
			serviceName = "users"
		}
		tail = append(tail, ServiceLog{Seq: seq, Service: serviceName, Stream: "stdout", Message: fmt.Sprintf("tail-%d", seq)})
	}
	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{
		Status:         ServiceLogsFetchOK,
		EffectiveLimit: &effectiveLimit,
		Logs:           tail,
	}})

	olderPage := make([]ServiceLog, 0, 200)
	for seq := int64(601); seq <= 800; seq++ {
		serviceName := "users"
		if seq%2 == 0 {
			serviceName = "billing"
		}
		olderPage = append(olderPage, ServiceLog{Seq: seq, Service: serviceName, Stream: "stdout", Message: fmt.Sprintf("older-%d", seq)})
	}
	m = applyUpdate(t, m, olderLogsLoadedMsg{serviceName: allLogsName, beforeSeq: 801, result: ServiceLogsFetchResult{
		Status: ServiceLogsFetchOK,
		Logs:   olderPage,
	}})

	if len(m.logEntries) != 400 {
		t.Fatalf("logEntries len after all-logs older page = %d, want 400", len(m.logEntries))
	}
	if lowestSeq(m.logEntries) != 601 {
		t.Fatalf("lowest seq after all-logs older page = %d, want 601", lowestSeq(m.logEntries))
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":1001,"service":"users","phase":"run","stream":"stdout","message":"live-1001","timestamp":"2026-05-02T10:00:01Z"}`),
	}})

	if len(m.logEntries) != 401 {
		t.Fatalf("logEntries len after all-logs live append = %d, want 401", len(m.logEntries))
	}
	if lowestSeq(m.logEntries) != 601 {
		t.Fatalf("lowest seq after all-logs live append = %d, want 601", lowestSeq(m.logEntries))
	}
	foundOlder := false
	for _, entry := range m.logEntries {
		if entry.Seq == 650 {
			foundOlder = true
			break
		}
	}
	if !foundOlder {
		t.Fatal("all-logs older entries should remain present after one live append")
	}
	if m.footerMessage != "loaded 200 older entries" {
		t.Fatalf("footer message after all-logs older page = %q, want %q", m.footerMessage, "loaded 200 older entries")
	}
	if len(m.logEntries) > computeLocalLogCacheLimit(&effectiveLimit) {
		t.Fatalf("logEntries len = %d, want <= local limit %d", len(m.logEntries), computeLocalLogCacheLimit(&effectiveLimit))
	}
}
