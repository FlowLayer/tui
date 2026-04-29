package tui

import (
	"encoding/json"
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

	m = applyUpdate(t, m, replayLogsLoadedMsg{result: ServiceLogsFetchResult{
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
