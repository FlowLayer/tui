package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	protocol "github.com/FlowLayer/tui/internal/protocol"
	"github.com/FlowLayer/tui/internal/wsclient"
	tea "github.com/charmbracelet/bubbletea"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
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

func applyUpdateWithCmd(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()

	updated, cmd := m.Update(msg)
	casted, ok := updated.(model)
	if !ok {
		t.Fatalf("updated model type = %T, want model", updated)
	}

	return casted, cmd
}

func makeSequentialLogs(service string, startSeq, endSeq int64, prefix string) []ServiceLog {
	if endSeq < startSeq {
		return nil
	}

	logs := make([]ServiceLog, 0, endSeq-startSeq+1)
	for seq := startSeq; seq <= endSeq; seq++ {
		logs = append(logs, ServiceLog{
			Seq:     seq,
			Service: service,
			Stream:  "stdout",
			Message: fmt.Sprintf("%s-%d", prefix, seq),
		})
	}

	return logs
}

func newCommandReadyClient(t *testing.T) *wsclient.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		hello := protocol.Envelope{
			Type:    protocol.MessageTypeEvent,
			Name:    "hello",
			Payload: json.RawMessage(`{"protocol_version":1,"server":"flowlayer"}`),
		}
		if err := wsjson.Write(r.Context(), conn, hello); err != nil {
			return
		}

		snapshot := protocol.Envelope{
			Type:    protocol.MessageTypeEvent,
			Name:    "snapshot",
			Payload: json.RawMessage(`{"services":[]}`),
		}
		if err := wsjson.Write(r.Context(), conn, snapshot); err != nil {
			return
		}

		for {
			var envelope protocol.Envelope
			if err := wsjson.Read(r.Context(), conn, &envelope); err != nil {
				return
			}
			if envelope.Type != protocol.MessageTypeCommand {
				continue
			}
			if err := wsjson.Write(r.Context(), conn, protocol.NewAckEnvelope(envelope.ID, true, nil)); err != nil {
				return
			}
			if err := wsjson.Write(r.Context(), conn, protocol.NewResultEnvelope(envelope.ID, true, nil, nil, json.RawMessage(`{"logs":[]}`))); err != nil {
				return
			}
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	client := wsclient.New(url)

	connectCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("connect command-ready client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if client.IsCommandReady() {
			return client
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("timed out waiting for command-ready wsclient")
	return nil
}

func newScrollableLogsFocusModel(t *testing.T) model {
	t.Helper()

	m := newModel("127.0.0.1:3000", "", newCommandReadyClient(t))
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})

	effectiveLimit := 200
	m.setEffectiveLogLimit(&effectiveLimit)
	m.focus = focusRight
	m.logsFollow = false
	m.logEntries = makeSequentialLogs("billing", 801, 1000, "tail")
	m.rebuildSeenLogSeqs()
	m.setLogsViewportContent()
	m.initialLogsLoading = false

	return m
}

func TestAppScenarioWSHelloSnapshotServiceStatusLog(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "hello",
		Data: json.RawMessage(`{"protocol_version":1,"server":"flowlayer","capabilities":["get_snapshot"]}`),
	}})
	if !m.replayPending {
		t.Fatal("hello should mark replayPending=true")
	}

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"stopped"}]}`),
	}})
	if m.connectionLabel != "Connected" {
		t.Fatalf("connection label = %q, want %q", m.connectionLabel, "Connected")
	}
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

func TestAppScenarioHelloAloneDoesNotSetConnected(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "hello",
		Data: json.RawMessage(`{"protocol_version":1,"server":"flowlayer","capabilities":["get_snapshot"]}`),
	}})

	if m.connectionLabel == "Connected" {
		t.Fatalf("connection label after hello = %q, want not %q before snapshot", m.connectionLabel, "Connected")
	}
	if !m.replayPending {
		t.Fatal("hello should keep replayPending=true")
	}
}

func TestAppScenarioSnapshotSetsConnected(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)

	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})

	if m.connectionLabel != "Connected" {
		t.Fatalf("connection label after snapshot = %q, want %q", m.connectionLabel, "Connected")
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

func TestAppScenarioServiceSwitchArmsInitialLogsLoading(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{Status: ServiceLogsFetchOK}})
	if m.initialLogsLoading {
		t.Fatal("initialLogsLoading should be false after current all-logs load completes")
	}

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "billing" {
		t.Fatalf("selected service after switch = %q, want %q", m.selectedServiceName(), "billing")
	}
	if !m.initialLogsLoading {
		t.Fatal("initialLogsLoading should become true on service switch fetch")
	}
}

func TestAppScenarioCurrentServiceLogsLoadedClearsInitialLogsLoading(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})
	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{Status: ServiceLogsFetchOK}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if !m.initialLogsLoading {
		t.Fatal("expected initialLogsLoading=true after switching to billing")
	}

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "billing", result: ServiceLogsFetchResult{Status: ServiceLogsFetchOK}})
	if m.initialLogsLoading {
		t.Fatal("initialLogsLoading should clear when current selection load resolves")
	}
}

func TestAppScenarioStaleServiceLogsLoadedDoesNotClearInitialLogsLoading(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})
	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{Status: ServiceLogsFetchOK}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown}) // billing
	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown}) // users
	if m.selectedServiceName() != "users" {
		t.Fatalf("selected service after second switch = %q, want %q", m.selectedServiceName(), "users")
	}
	if !m.initialLogsLoading {
		t.Fatal("initialLogsLoading should remain true while users initial fetch is pending")
	}

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "billing", result: ServiceLogsFetchResult{Status: ServiceLogsFetchOK}})
	if !m.initialLogsLoading {
		t.Fatal("stale billing response should not clear users initialLogsLoading")
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
	if !m.logsFollow {
		t.Fatal("expected follow mode enabled after historical load")
	}
	if !m.logsViewport.AtBottom() {
		t.Fatal("viewport should remain at bottom after historical load")
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
	if m.footerMessage != "loaded 2 older entries" {
		t.Fatalf("footer message after successful older fetch = %q, want %q", m.footerMessage, "loaded 2 older entries")
	}
	if !m.footerTransient {
		t.Fatal("successful older fetch footer should be transient")
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
	if m.footerMessage != "no older logs available" {
		t.Fatalf("footer message when older response is empty = %q, want %q", m.footerMessage, "no older logs available")
	}
	if !m.footerTransient {
		t.Fatal("no-older footer should be transient")
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

func TestAppScenarioInitialFetchFailureFooterIsExplicit(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})
	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "billing", result: ServiceLogsFetchResult{Status: ServiceLogsFetchRequestFailed}})

	if !strings.HasPrefix(m.footerMessage, "initial") {
		t.Fatalf("initial fetch failure footer = %q, want prefix %q", m.footerMessage, "initial")
	}
	if !m.footerTransient {
		t.Fatal("initial fetch failure footer should be transient")
	}
}

func TestAppScenarioOlderLogsRequestFailedFooterIsExplicit(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})
	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})

	m.logEntries = []ServiceLog{{Seq: 10, Service: "billing", Stream: "stdout", Message: "existing"}}
	m.rebuildSeenLogSeqs()
	m.loadingOlderLogs = true

	m = applyUpdate(t, m, olderLogsLoadedMsg{serviceName: "billing", beforeSeq: 10, result: ServiceLogsFetchResult{Status: ServiceLogsFetchRequestFailed, FailureReason: runCommandFailureContextTimeout}})

	if m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should clear after failed older response")
	}
	if m.noOlderLogsAvailable {
		t.Fatal("noOlderLogsAvailable should remain false on older fetch failure")
	}
	if !strings.HasPrefix(m.footerMessage, "older") {
		t.Fatalf("older fetch failure footer = %q, want prefix %q", m.footerMessage, "older")
	}
	if !strings.Contains(strings.ToLower(m.footerMessage), "timeout") {
		t.Fatalf("older fetch failure footer = %q, want to contain %q", m.footerMessage, "timeout")
	}
	if !m.footerTransient {
		t.Fatal("older fetch failure footer should be transient")
	}
}

func TestAppScenarioOlderFetchCmdCarriesDiagnostics(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}})
	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})

	effectiveLimit := 75
	m.setEffectiveLogLimit(&effectiveLimit)
	m.logEntries = []ServiceLog{
		{Seq: 10, Service: "billing", Stream: "stdout", Message: "ten"},
		{Seq: 11, Service: "billing", Stream: "stdout", Message: "eleven"},
	}
	m.rebuildSeenLogSeqs()

	msg := m.fetchOlderLogsCmd("billing", 10)()
	olderMsg, ok := msg.(olderLogsLoadedMsg)
	if !ok {
		t.Fatalf("message type = %T, want olderLogsLoadedMsg", msg)
	}

	diagnostics := olderMsg.result.Diagnostics
	if diagnostics.Kind != logsFetchKindOlder {
		t.Fatalf("diagnostics kind = %q, want %q", diagnostics.Kind, logsFetchKindOlder)
	}
	if diagnostics.RequestedServiceName != "billing" {
		t.Fatalf("diagnostics requested service = %q, want %q", diagnostics.RequestedServiceName, "billing")
	}
	if diagnostics.PayloadServiceName != "billing" {
		t.Fatalf("diagnostics payload service = %q, want %q", diagnostics.PayloadServiceName, "billing")
	}
	if diagnostics.BeforeSeq != 10 {
		t.Fatalf("diagnostics before_seq = %d, want 10", diagnostics.BeforeSeq)
	}
	if diagnostics.Limit != effectiveLimit {
		t.Fatalf("diagnostics limit = %d, want %d", diagnostics.Limit, effectiveLimit)
	}
	if diagnostics.LowestSeqAtFetch != 10 {
		t.Fatalf("diagnostics lowest seq = %d, want 10", diagnostics.LowestSeqAtFetch)
	}
	if diagnostics.LogEntriesLenAtFetch != 2 {
		t.Fatalf("diagnostics log entries len = %d, want 2", diagnostics.LogEntriesLenAtFetch)
	}
	if diagnostics.ClientReady {
		t.Fatal("diagnostics client ready should be false with nil client")
	}
	if diagnostics.SelectedServiceAtFetch != "billing" {
		t.Fatalf("diagnostics selected at fetch = %q, want %q", diagnostics.SelectedServiceAtFetch, "billing")
	}
	if diagnostics.EffectiveLogLimitAtFetch == nil || *diagnostics.EffectiveLogLimitAtFetch != effectiveLimit {
		t.Fatalf("diagnostics effective log limit = %v, want %d", diagnostics.EffectiveLogLimitAtFetch, effectiveLimit)
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
	m.logsFollow = false

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selectedServiceName() != "users" {
		t.Fatalf("selected service = %q, want %q", m.selectedServiceName(), "users")
	}
	if len(m.logEntries) != 0 {
		t.Fatalf("logEntries len after switch = %d, want 0", len(m.logEntries))
	}
	if !m.logsFollow {
		t.Fatal("selection change should re-enable follow mode")
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
	if !m.logsFollow {
		t.Fatal("expected follow mode enabled after non-empty historical response")
	}
	if !m.logsViewport.AtBottom() {
		t.Fatal("viewport should be at bottom after service historical load")
	}
	if m.footerMessage != "loaded 10 historical entries" {
		t.Fatalf("footer message = %q, want %q", m.footerMessage, "loaded 10 historical entries")
	}
}

func TestAppScenarioHistoricalLoadFollowsTailAndShowsLoadedFooter(t *testing.T) {
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

	if !m.logsFollow {
		t.Fatal("expected follow mode enabled after non-empty historical all-logs response")
	}
	if m.logsViewport.YOffset <= 0 {
		t.Fatalf("viewport yOffset after historical load = %d, want > 0", m.logsViewport.YOffset)
	}
	if !m.logsViewport.AtBottom() {
		t.Fatal("viewport should jump to bottom after historical load")
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

func TestAppScenarioMaybeRequestOlderLogsClientNilDoesNotTrigger(t *testing.T) {
	m := newScrollableLogsFocusModel(t)
	m.client = nil
	m.logsViewport.SetYOffset(olderLogsPrefetchThreshold)

	olderCmd, triggered := m.maybeRequestOlderLogs()
	if triggered {
		t.Fatal("maybeRequestOlderLogs should not trigger when client is nil")
	}
	if olderCmd != nil {
		t.Fatal("maybeRequestOlderLogs command should be nil when client is nil")
	}
	if m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should remain false when client is nil")
	}
	if m.noOlderLogsAvailable {
		t.Fatal("noOlderLogsAvailable should remain false when client is nil")
	}
}

func TestAppScenarioMaybeRequestOlderLogsClientNotReadyDoesNotTrigger(t *testing.T) {
	m := newScrollableLogsFocusModel(t)
	m.client = wsclient.New("ws://127.0.0.1:65535/ws")
	m.logsViewport.SetYOffset(olderLogsPrefetchThreshold)

	olderCmd, triggered := m.maybeRequestOlderLogs()
	if triggered {
		t.Fatal("maybeRequestOlderLogs should not trigger when client is not command-ready")
	}
	if olderCmd != nil {
		t.Fatal("maybeRequestOlderLogs command should be nil when client is not command-ready")
	}
	if m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should remain false when client is not command-ready")
	}
	if m.noOlderLogsAvailable {
		t.Fatal("noOlderLogsAvailable should remain false when client is not command-ready")
	}

	m, _ = applyUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if strings.Contains(strings.ToLower(m.footerMessage), "older logs fetch failed") {
		t.Fatalf("footer should not report older fetch failure when command-ready guard blocks request; got %q", m.footerMessage)
	}
}

func TestAppScenarioMaybeRequestOlderLogsCommandReadyTriggersFetch(t *testing.T) {
	m := newScrollableLogsFocusModel(t)
	m.logsViewport.SetYOffset(olderLogsPrefetchThreshold)

	olderCmd, triggered := m.maybeRequestOlderLogs()
	if !triggered {
		t.Fatal("maybeRequestOlderLogs should trigger when client is command-ready")
	}
	if olderCmd == nil {
		t.Fatal("maybeRequestOlderLogs should return a command when client is command-ready")
	}
	if !m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should become true when command-ready older fetch is scheduled")
	}
}

func TestAppScenarioUpNearTopTriggersOlderFetch(t *testing.T) {
	m := newScrollableLogsFocusModel(t)
	m.logsViewport.SetYOffset(olderLogsPrefetchThreshold)

	wantBeforeSeq := lowestSeq(m.logEntries)
	if wantBeforeSeq <= 0 {
		t.Fatalf("lowest seq = %d, want > 0", wantBeforeSeq)
	}

	probe := newScrollableLogsFocusModel(t)
	probe.logsViewport.SetYOffset(olderLogsPrefetchThreshold)
	olderCmd, triggered := probe.maybeRequestOlderLogs()
	if !triggered {
		t.Fatal("maybeRequestOlderLogs should trigger near top")
	}
	if olderCmd == nil {
		t.Fatal("maybeRequestOlderLogs should return a command")
	}
	olderResult := olderCmd()
	olderMsg, ok := olderResult.(olderLogsLoadedMsg)
	if !ok {
		t.Fatalf("older command message type = %T, want olderLogsLoadedMsg", olderResult)
	}
	if olderMsg.beforeSeq != wantBeforeSeq {
		t.Fatalf("beforeSeq = %d, want %d", olderMsg.beforeSeq, wantBeforeSeq)
	}

	m, cmd := applyUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if !m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should become true after near-top up")
	}
	if cmd == nil {
		t.Fatal("update command should not be nil when older fetch is scheduled")
	}
	wantFooter := fmt.Sprintf("loading older logs before seq %d", wantBeforeSeq)
	if m.footerMessage != wantFooter {
		t.Fatalf("footer message = %q, want %q", m.footerMessage, wantFooter)
	}
	if !m.footerTransient {
		t.Fatal("loading older footer should be transient")
	}
}

func TestAppScenarioLiveLogsDuringInitialLoadDoNotTriggerOlderFetch(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})
	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{Status: ServiceLogsFetchOK}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown}) // billing, starts initial fetch
	if !m.initialLogsLoading {
		t.Fatal("expected initialLogsLoading=true after switching to billing")
	}

	m.focus = focusRight
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":101,"service":"billing","phase":"run","stream":"stdout","message":"live-before-initial","timestamp":"2026-04-14T12:00:03Z"}`),
	}})
	if len(m.logEntries) == 0 {
		t.Fatal("expected live billing log to be visible before initial response")
	}
	m.logsViewport.SetYOffset(0)

	footerBefore := m.footerMessage
	m, _ = applyUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should stay false while initialLogsLoading=true")
	}
	if m.noOlderLogsAvailable {
		t.Fatal("noOlderLogsAvailable should not change while older fetch is blocked")
	}
	if strings.HasPrefix(strings.ToLower(m.footerMessage), "loading older logs") {
		t.Fatalf("footer should not advertise older fetch while blocked; got %q", m.footerMessage)
	}
	if m.footerMessage != footerBefore {
		t.Fatalf("footer message changed unexpectedly while older fetch is blocked: got %q, want %q", m.footerMessage, footerBefore)
	}
}

func TestAppScenarioOlderFetchReenabledAfterCurrentInitialLoad(t *testing.T) {
	m := newModel("127.0.0.1:3000", "", nil)
	m = applyUpdate(t, m, connectDoneMsg{result: ConnectResult{Status: StatusConnected}})
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"},{"name":"users","status":"ready"}]}`),
	}})
	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: allLogsName, result: ServiceLogsFetchResult{Status: ServiceLogsFetchOK}})

	m = applyUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown}) // billing
	m.focus = focusRight
	m = applyUpdate(t, m, wsEventMsg{ok: true, event: wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":101,"service":"billing","phase":"run","stream":"stdout","message":"live-before-initial","timestamp":"2026-04-14T12:00:03Z"}`),
	}})

	m = applyUpdate(t, m, serviceLogsLoadedMsg{serviceName: "billing", requestSeq: 100, result: ServiceLogsFetchResult{
		Status: ServiceLogsFetchOK,
		Logs: []ServiceLog{
			{Seq: 100, Service: "billing", Stream: "stdout", Message: "historical-before-live"},
		},
	}})
	if m.initialLogsLoading {
		t.Fatal("initialLogsLoading should be false after current service load")
	}
	m.client = newCommandReadyClient(t)

	m.logsViewport.SetYOffset(0)
	m, _ = applyUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if !m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should become true once initialLogsLoading is cleared")
	}
}

func TestAppScenarioUpFarFromTopDoesNotTriggerOlderFetch(t *testing.T) {
	m := newScrollableLogsFocusModel(t)
	m.logsViewport.SetYOffset(olderLogsPrefetchThreshold + 8)
	footerBefore := m.footerMessage

	m, _ = applyUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should stay false when far from top")
	}
	if m.footerMessage != footerBefore {
		t.Fatalf("footer message = %q, want %q when no older fetch is started", m.footerMessage, footerBefore)
	}
}

func TestAppScenarioUpAtTopStillTriggersOlderFetch(t *testing.T) {
	m := newScrollableLogsFocusModel(t)
	m.logsViewport.SetYOffset(0)

	m, _ = applyUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if !m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should become true when already at top")
	}
}

func TestAppScenarioUpWithLeftFocusDoesNotTriggerOlderFetch(t *testing.T) {
	m := newScrollableLogsFocusModel(t)
	m.focus = focusLeft
	m.serviceSelection = 2 // users
	m.logsViewport.SetYOffset(olderLogsPrefetchThreshold)

	m, _ = applyUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyUp})

	if m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should remain false with left panel focus")
	}
	if m.selectedServiceName() != "billing" {
		t.Fatalf("selected service after up on left panel = %q, want %q", m.selectedServiceName(), "billing")
	}
}

func TestAppScenarioScrollUpKeysTriggerOlderFetchNearTop(t *testing.T) {
	testCases := []struct {
		name string
		msg  tea.KeyMsg
	}{
		{name: "page up", msg: tea.KeyMsg{Type: tea.KeyPgUp}},
		{name: "k", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}},
		{name: "b", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}},
		{name: "u", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}}},
		{name: "ctrl+u", msg: tea.KeyMsg{Type: tea.KeyCtrlU}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			m := newScrollableLogsFocusModel(t)
			m.logsViewport.SetYOffset(olderLogsPrefetchThreshold)

			m, _ = applyUpdateWithCmd(t, m, testCase.msg)
			if !m.loadingOlderLogs {
				t.Fatalf("loadingOlderLogs should become true for key %q near top", testCase.name)
			}
		})
	}
}

func TestAppScenarioAfterPrependOlderPageBecomesReachableWithUp(t *testing.T) {
	m := newScrollableLogsFocusModel(t)
	m.logsViewport.SetYOffset(5)
	beforePrependYOffset := m.logsViewport.YOffset

	m.loadingOlderLogs = true
	m = applyUpdate(t, m, olderLogsLoadedMsg{serviceName: allLogsName, beforeSeq: 801, result: ServiceLogsFetchResult{
		Status: ServiceLogsFetchOK,
		Logs:   makeSequentialLogs("billing", 601, 800, "older"),
	}})

	if len(m.logEntries) != 400 {
		t.Fatalf("logEntries len after prepend = %d, want 400", len(m.logEntries))
	}
	if m.logEntries[0].Seq != 601 || m.logEntries[len(m.logEntries)-1].Seq != 1000 {
		t.Fatalf("seq bounds after prepend = [%d ... %d], want [601 ... 1000]", m.logEntries[0].Seq, m.logEntries[len(m.logEntries)-1].Seq)
	}
	if m.logsViewport.YOffset <= beforePrependYOffset {
		t.Fatalf("yOffset after prepend = %d, want > %d to preserve anchor", m.logsViewport.YOffset, beforePrependYOffset)
	}
	if m.logsFollow {
		t.Fatal("follow mode should stay disabled after prepending older logs")
	}
	if m.logsViewport.AtBottom() {
		t.Fatal("older logs prepend should preserve anchor, not force viewport to bottom")
	}

	yOffsetAfterPrepend := m.logsViewport.YOffset
	for i := 0; i < 8; i++ {
		m, _ = applyUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyUp})
	}

	if m.logsViewport.YOffset >= yOffsetAfterPrepend {
		t.Fatalf("yOffset after repeated up = %d, want < %d", m.logsViewport.YOffset, yOffsetAfterPrepend)
	}
}

func TestAppScenarioNoDuplicateOlderFetchWhileLoading(t *testing.T) {
	m := newScrollableLogsFocusModel(t)
	m.logsViewport.SetYOffset(olderLogsPrefetchThreshold)
	m.loadingOlderLogs = true
	m.setPersistentFooter("steady")

	m, _ = applyUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyUp})

	if !m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should remain true while an older fetch is in flight")
	}
	if m.footerMessage != "steady" {
		t.Fatalf("footer message = %q, want %q", m.footerMessage, "steady")
	}
}

func TestAppScenarioUpWithEmptyLogEntriesDoesNotStartOlderFetch(t *testing.T) {
	m := newScrollableLogsFocusModel(t)
	m.logEntries = nil
	m.resetSeenLogSeqs()
	m.setLogsViewportContent()
	m.logsViewport.SetYOffset(0)

	probe := m
	olderCmd, triggered := probe.maybeRequestOlderLogs()
	if triggered {
		t.Fatal("maybeRequestOlderLogs should not trigger when logEntries is empty")
	}
	if olderCmd != nil {
		t.Fatal("maybeRequestOlderLogs command should be nil when logEntries is empty")
	}

	footerBefore := m.footerMessage
	m, _ = applyUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.loadingOlderLogs {
		t.Fatal("loadingOlderLogs should remain false with empty logEntries")
	}
	if m.footerMessage != footerBefore {
		t.Fatalf("footer message changed unexpectedly: got %q, want %q", m.footerMessage, footerBefore)
	}
}
