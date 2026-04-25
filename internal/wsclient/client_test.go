package wsclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/FlowLayer/tui/internal/protocol"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func TestConnectHandshakeTransitionsToConnected(t *testing.T) {
	url, closeServer := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		defer conn.Close(websocket.StatusNormalClosure, "")
		if err := writeHelloSnapshot(ctx, conn); err != nil {
			return
		}
		<-ctx.Done()
	})
	defer closeServer()

	client := New(url)
	connectCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	waitForState(t, client, stateConnected, 2*time.Second)

	eventHello := expectEvent(t, client.Events(), 2*time.Second)
	if eventHello.Name != "hello" {
		t.Fatalf("first event name = %q, want %q", eventHello.Name, "hello")
	}

	eventSnapshot := expectEvent(t, client.Events(), 2*time.Second)
	if eventSnapshot.Name != "snapshot" {
		t.Fatalf("second event name = %q, want %q", eventSnapshot.Name, "snapshot")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestConnectInjectsAuthorizationHeader(t *testing.T) {
	const bearerToken = "fl_test_wsclient_token"

	authorizationHeader := make(chan string, 1)
	url, closeServer := newWSTestServerWithRequest(t, func(ctx context.Context, conn *websocket.Conn, request *http.Request, _ int) {
		defer conn.Close(websocket.StatusNormalClosure, "")

		authorizationHeader <- request.Header.Get("Authorization")
		if err := writeHelloSnapshot(ctx, conn); err != nil {
			return
		}

		<-ctx.Done()
	})
	defer closeServer()

	client := NewWithOptions(url, Options{BearerToken: bearerToken})
	connectCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	waitForState(t, client, stateConnected, 2*time.Second)

	select {
	case got := <-authorizationHeader:
		want := "Bearer " + bearerToken
		if got != want {
			t.Fatalf("Authorization header = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for observed Authorization header")
	}
}

func TestForceReconnectReestablishesSession(t *testing.T) {
	var lastConnectionIndex int32

	url, closeServer := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn, connectionIndex int) {
		defer conn.Close(websocket.StatusNormalClosure, "")

		atomic.StoreInt32(&lastConnectionIndex, int32(connectionIndex))
		if err := writeHelloSnapshot(ctx, conn); err != nil {
			return
		}

		<-ctx.Done()
	})
	defer closeServer()

	client := New(url)
	connectCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	if !client.ForceReconnect() {
		t.Fatal("ForceReconnect returned false with an active session")
	}

	waitForState(t, client, stateConnected, 3*time.Second)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&lastConnectionIndex) >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected at least 2 websocket sessions, observed %d", atomic.LoadInt32(&lastConnectionIndex))
}

func TestSendCommandAckThenResult(t *testing.T) {
	url, closeServer := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		defer conn.Close(websocket.StatusNormalClosure, "")
		if err := writeHelloSnapshot(ctx, conn); err != nil {
			return
		}

		envelope, err := readEnvelope(ctx, conn)
		if err != nil {
			return
		}
		if envelope.Type != protocol.MessageTypeCommand {
			return
		}

		_ = wsjson.Write(ctx, conn, protocol.NewAckEnvelope(envelope.ID, true, nil))
		_ = wsjson.Write(ctx, conn, protocol.NewResultEnvelope(envelope.ID, true, nil, nil, json.RawMessage(`{"ok":true}`)))

		<-ctx.Done()
	})
	defer closeServer()

	client := New(url)
	connectCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	resultCh, err := client.SendCommand(context.Background(), "start_service", map[string]string{"service": "billing"})
	if err != nil {
		t.Fatalf("send command: %v", err)
	}

	result, ok := recvResult(t, resultCh, 2*time.Second)
	if !ok {
		t.Fatal("expected one command result value")
	}
	if !result.Accepted {
		t.Fatal("expected Accepted=true")
	}
	if result.Result == nil {
		t.Fatal("expected non-nil result payload")
	}

	_, ok = recvResult(t, resultCh, 2*time.Second)
	if ok {
		t.Fatal("expected command result channel to be closed after one value")
	}
}

func TestSendCommandAckFalse(t *testing.T) {
	url, closeServer := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		defer conn.Close(websocket.StatusNormalClosure, "")
		if err := writeHelloSnapshot(ctx, conn); err != nil {
			return
		}

		envelope, err := readEnvelope(ctx, conn)
		if err != nil {
			return
		}
		if envelope.Type != protocol.MessageTypeCommand {
			return
		}

		_ = wsjson.Write(ctx, conn, protocol.NewAckEnvelope(envelope.ID, false, &protocol.ErrorPayload{
			Code:    string(protocol.ErrorCodeUnknownService),
			Message: "service unknown",
		}))

		<-ctx.Done()
	})
	defer closeServer()

	client := New(url)
	connectCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	resultCh, err := client.SendCommand(context.Background(), "start_service", map[string]string{"service": "ghost"})
	if err != nil {
		t.Fatalf("send command: %v", err)
	}

	result, ok := recvResult(t, resultCh, 2*time.Second)
	if !ok {
		t.Fatal("expected one command result value")
	}
	if result.Accepted {
		t.Fatal("expected Accepted=false")
	}
	if result.Result != nil {
		t.Fatal("expected nil Result on ack(false)")
	}
	if result.AckError == nil {
		t.Fatal("expected ack error payload")
	}

	_, ok = recvResult(t, resultCh, 2*time.Second)
	if ok {
		t.Fatal("expected channel to be closed after terminal ack(false)")
	}
}

func TestReconnectInvalidatesPendingWithoutRetry(t *testing.T) {
	var connectionCount int32
	var startServiceCommandCount int32
	var secondSessionCommandCount int32
	secondConnected := make(chan struct{})
	var secondConnectedOnce sync.Once

	url, closeServer := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		defer conn.Close(websocket.StatusNormalClosure, "")

		index := int(atomic.AddInt32(&connectionCount, 1))
		if err := writeHelloSnapshot(ctx, conn); err != nil {
			return
		}

		if index == 1 {
			for {
				envelope, err := readEnvelope(ctx, conn)
				if err != nil {
					return
				}
				if envelope.Type != protocol.MessageTypeCommand {
					continue
				}

				if envelope.Name == "start_service" {
					atomic.AddInt32(&startServiceCommandCount, 1)
					_ = wsjson.Write(ctx, conn, protocol.NewAckEnvelope(envelope.ID, true, nil))
					_ = conn.Close(websocket.StatusGoingAway, "force reconnect")
					return
				}
			}
		}

		secondConnectedOnce.Do(func() { close(secondConnected) })
		for {
			envelope, err := readEnvelope(ctx, conn)
			if err != nil {
				return
			}
			if envelope.Type == protocol.MessageTypeCommand {
				atomic.AddInt32(&secondSessionCommandCount, 1)
			}
		}
	})
	defer closeServer()

	client := New(url)
	connectCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	resultCh, err := client.SendCommand(context.Background(), "start_service", map[string]string{"service": "billing"})
	if err != nil {
		t.Fatalf("send command: %v", err)
	}

	result, ok := recvResult(t, resultCh, 3*time.Second)
	if !ok {
		t.Fatal("expected one invalidated command result value")
	}
	if !result.Invalidated {
		t.Fatalf("expected Invalidated=true, got %+v", result)
	}
	if result.Accepted {
		t.Fatalf("expected Accepted=false on invalidation, got %+v", result)
	}
	if result.Result != nil {
		t.Fatalf("expected nil Result on invalidation, got %+v", result)
	}
	if result.AckError != nil {
		t.Fatalf("expected nil AckError on invalidation, got %+v", result)
	}

	_, ok = recvResult(t, resultCh, 3*time.Second)
	if ok {
		t.Fatal("expected command result channel to be closed after invalidation")
	}

	select {
	case <-secondConnected:
	case <-time.After(3 * time.Second):
		t.Fatal("expected a second websocket connection")
	}

	waitForState(t, client, stateConnected, 3*time.Second)
	time.Sleep(300 * time.Millisecond)

	if got := atomic.LoadInt32(&startServiceCommandCount); got != 1 {
		t.Fatalf("start_service command count = %d, want %d (no automatic retry)", got, 1)
	}

	if got := atomic.LoadInt32(&secondSessionCommandCount); got != 0 {
		t.Fatalf("second session command count = %d, want 0 (no automatic commands)", got)
	}
}

func TestReconnectDoesNotSendAutomaticCommands(t *testing.T) {
	var connectionCount int32
	var secondSessionCommandCount int32
	secondConnected := make(chan struct{})
	var secondConnectedOnce sync.Once

	url, closeServer := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		defer conn.Close(websocket.StatusNormalClosure, "")

		index := int(atomic.AddInt32(&connectionCount, 1))
		if err := writeHelloSnapshot(ctx, conn); err != nil {
			return
		}

		if index == 1 {
			<-ctx.Done()
			return
		}

		secondConnectedOnce.Do(func() { close(secondConnected) })
		for {
			envelope, err := readEnvelope(ctx, conn)
			if err != nil {
				return
			}
			if envelope.Type == protocol.MessageTypeCommand {
				atomic.AddInt32(&secondSessionCommandCount, 1)
			}
		}
	})
	defer closeServer()

	client := New(url)
	connectCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	waitForState(t, client, stateConnected, 3*time.Second)

	if !client.ForceReconnect() {
		t.Fatal("ForceReconnect returned false with an active session")
	}

	select {
	case <-secondConnected:
	case <-time.After(3 * time.Second):
		t.Fatal("expected a second websocket connection")
	}

	waitForState(t, client, stateConnected, 3*time.Second)
	time.Sleep(300 * time.Millisecond)

	if got := atomic.LoadInt32(&secondSessionCommandCount); got != 0 {
		t.Fatalf("second session command count = %d, want 0 (no automatic commands)", got)
	}
}

func TestSendCommandDuringReconnectReturnsErrNotReady(t *testing.T) {
	var connectionCount int32
	secondDialed := make(chan struct{})
	allowSecondHandshake := make(chan struct{})

	url, closeServer := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		defer conn.Close(websocket.StatusNormalClosure, "")

		index := int(atomic.AddInt32(&connectionCount, 1))
		if index == 1 {
			if err := writeHelloSnapshot(ctx, conn); err != nil {
				return
			}
			_ = conn.Close(websocket.StatusGoingAway, "drop first session")
			return
		}

		close(secondDialed)
		select {
		case <-allowSecondHandshake:
		case <-ctx.Done():
			return
		}

		if err := writeHelloSnapshot(ctx, conn); err != nil {
			return
		}

		<-ctx.Done()
	})
	defer closeServer()

	client := New(url)
	connectCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	select {
	case <-secondDialed:
	case <-time.After(3 * time.Second):
		t.Fatal("expected reconnect dial")
	}

	if _, err := client.SendCommand(context.Background(), "start_service", map[string]string{"service": "users"}); !errorsIs(err, ErrNotReady) {
		t.Fatalf("send command error = %v, want %v", err, ErrNotReady)
	}

	close(allowSecondHandshake)
	waitForState(t, client, stateConnected, 3*time.Second)
}

func TestEventsFlowIsDelivered(t *testing.T) {
	url, closeServer := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		defer conn.Close(websocket.StatusNormalClosure, "")
		if err := writeHelloSnapshot(ctx, conn); err != nil {
			return
		}

		_ = wsjson.Write(ctx, conn, protocol.Envelope{
			Type: protocol.MessageTypeEvent,
			Name: "service_status",
			Payload: json.RawMessage(`{
				"service":"billing",
				"status":"running",
				"timestamp":"2026-04-12T10:30:45Z"
			}`),
		})

		_ = wsjson.Write(ctx, conn, protocol.Envelope{
			Type: protocol.MessageTypeEvent,
			Name: "log",
			Payload: json.RawMessage(`{
				"service":"users",
				"phase":"start",
				"stream":"stdout",
				"message":"boot",
				"timestamp":"2026-04-12T10:31:15Z"
			}`),
		})

		<-ctx.Done()
	})
	defer closeServer()

	client := New(url)
	connectCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for !(seen["service_status"] && seen["log"]) {
		select {
		case event := <-client.Events():
			seen[event.Name] = true
		case <-deadline:
			t.Fatalf("missing events, seen=%v", seen)
		}
	}
}

func TestErrorMessagesArePropagatedToEvents(t *testing.T) {
	url, closeServer := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		defer conn.Close(websocket.StatusNormalClosure, "")
		if err := writeHelloSnapshot(ctx, conn); err != nil {
			return
		}

		_ = wsjson.Write(ctx, conn, protocol.NewErrorEnvelope(protocol.NewErrorMessage("", protocol.ErrorCodeInternal, "boom")))
		<-ctx.Done()
	})
	defer closeServer()

	client := New(url)
	connectCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-client.Events():
			if event.Name != "error" {
				continue
			}
			if !strings.Contains(string(event.Data), `"code":"internal_error"`) {
				t.Fatalf("error event payload = %s, missing code", string(event.Data))
			}
			if !strings.Contains(string(event.Data), `"message":"boom"`) {
				t.Fatalf("error event payload = %s, missing message", string(event.Data))
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for error event")
		}
	}
}

func TestCloseInvalidatesPendingAndShutsDown(t *testing.T) {
	url, closeServer := newWSTestServer(t, func(ctx context.Context, conn *websocket.Conn, _ int) {
		defer conn.Close(websocket.StatusNormalClosure, "")
		if err := writeHelloSnapshot(ctx, conn); err != nil {
			return
		}

		for {
			if _, err := readEnvelope(ctx, conn); err != nil {
				return
			}
		}
	})
	defer closeServer()

	client := New(url)
	connectCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(connectCtx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	resultCh, err := client.SendCommand(context.Background(), "start_service", map[string]string{"service": "users"})
	if err != nil {
		t.Fatalf("send command: %v", err)
	}

	closeDone := make(chan struct{})
	go func() {
		_ = client.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("client.Close blocked")
	}

	result, ok := recvResult(t, resultCh, 2*time.Second)
	if !ok {
		t.Fatal("expected invalidated pending command result on Close")
	}
	if !result.Invalidated {
		t.Fatalf("expected Invalidated=true on Close, got %+v", result)
	}

	_, ok = recvResult(t, resultCh, 2*time.Second)
	if ok {
		t.Fatal("expected pending command channel to be closed after invalidation on Close")
	}

	drainUntilClosed(t, client.Events(), 2*time.Second)
	if state := currentState(client); state != stateClosed {
		t.Fatalf("state = %q, want %q", state, stateClosed)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func newWSTestServer(
	t *testing.T,
	handler func(ctx context.Context, conn *websocket.Conn, connectionIndex int),
) (string, func()) {
	t.Helper()

	return newWSTestServerWithRequest(t, func(ctx context.Context, conn *websocket.Conn, _ *http.Request, connectionIndex int) {
		handler(ctx, conn, connectionIndex)
	})
}

func newWSTestServerWithRequest(
	t *testing.T,
	handler func(ctx context.Context, conn *websocket.Conn, request *http.Request, connectionIndex int),
) (string, func()) {
	t.Helper()

	var connectionIndex int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}

		index := int(atomic.AddInt32(&connectionIndex, 1))
		handler(r.Context(), conn, r, index)
	})

	server := httptest.NewServer(mux)
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	return url, server.Close
}

func writeHelloSnapshot(ctx context.Context, conn *websocket.Conn) error {
	hello := protocol.Envelope{
		Type:    protocol.MessageTypeEvent,
		Name:    "hello",
		Payload: json.RawMessage(`{"protocol_version":1,"server":"flowlayer"}`),
	}
	if err := wsjson.Write(ctx, conn, hello); err != nil {
		return err
	}

	snapshot := protocol.Envelope{
		Type:    protocol.MessageTypeEvent,
		Name:    "snapshot",
		Payload: json.RawMessage(`{"services":[]}`),
	}
	if err := wsjson.Write(ctx, conn, snapshot); err != nil {
		return err
	}

	return nil
}

func readEnvelope(ctx context.Context, conn *websocket.Conn) (protocol.Envelope, error) {
	var envelope protocol.Envelope
	err := wsjson.Read(ctx, conn, &envelope)
	return envelope, err
}

func expectEvent(t *testing.T, eventCh <-chan Event, timeout time.Duration) Event {
	t.Helper()

	select {
	case event := <-eventCh:
		return event
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for event")
		return Event{}
	}
}

func recvResult(t *testing.T, resultCh <-chan CommandResult, timeout time.Duration) (CommandResult, bool) {
	t.Helper()

	select {
	case result, ok := <-resultCh:
		return result, ok
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for command result")
		return CommandResult{}, false
	}
}

func waitForState(t *testing.T, client *Client, expected clientState, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if currentState(client) == expected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("state = %q, want %q", currentState(client), expected)
}

func currentState(client *Client) clientState {
	client.mu.Lock()
	defer client.mu.Unlock()

	return client.state
}

func drainUntilClosed[T any](t *testing.T, ch <-chan T, timeout time.Duration) {
	t.Helper()

	deadline := time.After(timeout)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("channel did not close")
		}
	}
}

func errorsIs(err, target error) bool {
	if target == nil {
		return err == nil
	}
	if err == nil {
		return false
	}
	return err.Error() == target.Error()
}

func TestEnqueueMessageFullQueueReturnsFalse(t *testing.T) {
	queue := make(chan protocol.Envelope, 1)
	queue <- protocol.NewAckEnvelope("filled", true, nil)

	if enqueueMessage(queue, protocol.NewAckEnvelope("next", true, nil)) {
		t.Fatal("expected enqueueMessage to return false when queue is full")
	}

	if got := len(queue); got != 1 {
		t.Fatalf("queue length = %d, want 1", got)
	}
}
