// Embedded WebSocket client used by the FlowLayer TUI.
// Originally based on the FlowLayer server client implementation.
// Maintained independently in this repository.
package wsclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	protocol "github.com/FlowLayer/tui/internal/protocol"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

const (
	writeQueueSize = 32
	eventsBuffer   = 128
)

var (
	ErrNotReady = errors.New("wsclient: not ready")
	ErrClosed   = errors.New("wsclient: closed")

	errAlreadyStarted = errors.New("wsclient: already started")
)

type clientState string

const (
	stateDisconnected clientState = "Disconnected"
	stateConnecting   clientState = "Connecting"
	stateWaitingHello clientState = "WaitingHello"
	stateWaitingSnap  clientState = "WaitingSnapshot"
	stateConnected    clientState = "Connected"
	stateReconnecting clientState = "Reconnecting"
	stateClosed       clientState = "Closed"
	initialBackoff                = 500 * time.Millisecond
	maxBackoff                    = 5 * time.Second
)

type CommandResult struct {
	Accepted    bool
	Invalidated bool
	AckError    *protocol.ErrorPayload
	Result      *protocol.ResultPayload
}

type Event struct {
	Name string
	Data json.RawMessage
}

type Options struct {
	BearerToken string
	DialHeaders http.Header
}

type pendingCommand struct {
	resultCh  chan CommandResult
	ackSeen   bool
	ackFailed bool
}

type Client struct {
	url         string
	dialHeaders http.Header

	mu            sync.Mutex
	state         clientState
	started       bool
	rootCtx       context.Context
	rootCancel    context.CancelFunc
	conn          *websocket.Conn
	sessionCancel context.CancelFunc
	writeQueue    chan protocol.Envelope
	pending       map[string]*pendingCommand

	events chan Event

	wg        sync.WaitGroup
	closeOnce sync.Once
}

func New(url string) *Client {
	return NewWithOptions(url, Options{})
}

func NewWithOptions(url string, options Options) *Client {
	return &Client{
		url:         url,
		dialHeaders: buildDialHeaders(options),
		state:       stateDisconnected,
		pending:     make(map[string]*pendingCommand),
		events:      make(chan Event, eventsBuffer),
	}
}

func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.state == stateClosed {
		c.mu.Unlock()
		return ErrClosed
	}
	if c.started {
		c.mu.Unlock()
		return errAlreadyStarted
	}

	c.started = true
	c.state = stateConnecting
	c.rootCtx, c.rootCancel = context.WithCancel(context.Background())
	c.mu.Unlock()

	initialReady := make(chan error, 1)
	c.wg.Add(1)
	go c.runReconnectLoop(initialReady)

	select {
	case <-ctx.Done():
		_ = c.Close()
		return ctx.Err()
	case err := <-initialReady:
		return err
	}
}

func (c *Client) Close() error {
	var closeErr error

	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.state = stateClosed
		cancel := c.rootCancel
		conn := c.conn
		c.mu.Unlock()

		if cancel != nil {
			cancel()
		}
		if conn != nil {
			closeErr = conn.Close(websocket.StatusNormalClosure, "")
		}

		c.wg.Wait()
		c.invalidatePending()
		close(c.events)
	})

	return closeErr
}

func (c *Client) ForceReconnect() bool {
	c.mu.Lock()
	if c.state == stateClosed {
		c.mu.Unlock()
		return false
	}
	conn := c.conn
	sessionCancel := c.sessionCancel
	c.mu.Unlock()

	if conn == nil || sessionCancel == nil {
		return false
	}

	_ = conn.Close(websocket.StatusGoingAway, "forced reconnect")
	sessionCancel()
	return true
}

func (c *Client) SendCommand(ctx context.Context, name string, payload any) (<-chan CommandResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	commandID := uuid.NewString()
	resultCh := make(chan CommandResult, 1)

	message := protocol.Envelope{
		Type:    protocol.MessageTypeCommand,
		ID:      commandID,
		Name:    name,
		Payload: encodedPayload,
	}

	c.mu.Lock()
	if c.state != stateConnected || c.writeQueue == nil || c.sessionCancel == nil {
		c.mu.Unlock()
		return nil, ErrNotReady
	}

	queue := c.writeQueue
	c.pending[commandID] = &pendingCommand{resultCh: resultCh}
	c.mu.Unlock()

	if !enqueueMessage(queue, message) {
		pending := c.takePending(commandID)
		if pending != nil {
			close(pending.resultCh)
		}
		return nil, ErrNotReady
	}

	return resultCh, nil
}

func (c *Client) Events() <-chan Event {
	return c.events
}

func (c *Client) runReconnectLoop(initialReady chan<- error) {
	defer c.wg.Done()

	backoff := initialBackoff
	notifiedInitial := false
	notifyInitial := func(err error) {
		if notifiedInitial {
			return
		}
		notifiedInitial = true
		initialReady <- err
	}

	for {
		if c.rootCtx.Err() != nil {
			notifyInitial(ErrClosed)
			return
		}

		if c.currentState() == stateReconnecting {
			c.setState(stateReconnecting)
		} else {
			c.setState(stateConnecting)
		}

		conn, _, err := websocket.Dial(c.rootCtx, c.url, c.dialOptions())
		if err != nil {
			if c.rootCtx.Err() != nil {
				notifyInitial(ErrClosed)
				return
			}
			c.setState(stateReconnecting)
			if !waitWithContext(c.rootCtx, backoff) {
				notifyInitial(ErrClosed)
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		backoff = initialBackoff
		shouldReconnect := c.runSession(conn, func() {
			notifyInitial(nil)
		})
		if !shouldReconnect {
			notifyInitial(ErrClosed)
			return
		}

		if !waitWithContext(c.rootCtx, backoff) {
			notifyInitial(ErrClosed)
			return
		}
		backoff = nextBackoff(backoff)
	}
}

func (c *Client) runSession(conn *websocket.Conn, onConnected func()) bool {
	sessionCtx, sessionCancel := context.WithCancel(c.rootCtx)
	queue := make(chan protocol.Envelope, writeQueueSize)
	readyCh := make(chan struct{}, 1)
	errCh := make(chan error, 2)
	readerDone := make(chan struct{})
	writerDone := make(chan struct{})

	c.attachSession(conn, sessionCancel, queue)

	go c.readerLoop(sessionCtx, conn, readyCh, errCh, readerDone)
	go c.writerLoop(sessionCtx, conn, queue, errCh, writerDone)

	connectedReached := false
	reconnect := false

	for {
		select {
		case <-readyCh:
			if !connectedReached {
				connectedReached = true
				if onConnected != nil {
					onConnected()
				}
			}
		case <-c.rootCtx.Done():
			reconnect = false
			goto cleanup
		case err := <-errCh:
			if err == nil {
				continue
			}
			reconnect = true
			goto cleanup
		}
	}

cleanup:
	select {
	case <-readyCh:
		if !connectedReached {
			connectedReached = true
			if onConnected != nil {
				onConnected()
			}
		}
	default:
	}

	sessionCancel()
	_ = conn.Close(websocket.StatusNormalClosure, "")
	<-readerDone
	<-writerDone

	c.detachSession(conn)
	c.invalidatePending()

	if reconnect && c.rootCtx.Err() == nil {
		c.setState(stateReconnecting)
		return true
	}

	return false
}

func (c *Client) readerLoop(
	ctx context.Context,
	conn *websocket.Conn,
	readyCh chan<- struct{},
	errCh chan<- error,
	done chan<- struct{},
) {
	defer close(done)

	for {
		var envelope protocol.Envelope
		if err := wsjson.Read(ctx, conn, &envelope); err != nil {
			if ctx.Err() == nil {
				reportSessionError(errCh, err)
			}
			return
		}

		c.dispatchEnvelope(envelope, readyCh)
	}
}

func (c *Client) writerLoop(
	ctx context.Context,
	conn *websocket.Conn,
	queue <-chan protocol.Envelope,
	errCh chan<- error,
	done chan<- struct{},
) {
	defer close(done)

	for {
		select {
		case <-ctx.Done():
			return
		case envelope := <-queue:
			if err := wsjson.Write(ctx, conn, envelope); err != nil {
				if ctx.Err() == nil {
					reportSessionError(errCh, err)
				}
				return
			}
		}
	}
}

func (c *Client) dispatchEnvelope(envelope protocol.Envelope, readyCh chan<- struct{}) {
	switch envelope.Type {
	case protocol.MessageTypeEvent:
		c.handleEvent(envelope, readyCh)
	case protocol.MessageTypeAck:
		c.handleAck(envelope)
	case protocol.MessageTypeResult:
		c.handleResult(envelope)
	case protocol.MessageTypeError:
		c.emitEvent(Event{
			Name: "error",
			Data: cloneRawMessage(envelope.Payload),
		})
		return
	default:
		return
	}
}

func (c *Client) handleEvent(envelope protocol.Envelope, readyCh chan<- struct{}) {
	switch envelope.Name {
	case "hello":
		c.onHello()
	case "snapshot":
		if c.onSnapshot() {
			select {
			case readyCh <- struct{}{}:
			default:
			}
		}
	}

	event := Event{
		Name: envelope.Name,
		Data: cloneRawMessage(envelope.Payload),
	}

	c.emitEvent(event)
}

func (c *Client) emitEvent(event Event) {
	select {
	case c.events <- event:
	default:
	}
}

func (c *Client) handleAck(envelope protocol.Envelope) {
	var payload protocol.AckPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return
	}

	if !payload.Accepted {
		pending := c.takePending(envelope.ID)
		if pending == nil {
			return
		}

		pending.ackSeen = true
		pending.ackFailed = true
		pending.resultCh <- CommandResult{
			Accepted: false,
			AckError: cloneErrorPayload(payload.Error),
			Result:   nil,
		}
		close(pending.resultCh)
		return
	}

	c.markAckSeen(envelope.ID)
}

func (c *Client) handleResult(envelope protocol.Envelope) {
	var payload protocol.ResultPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return
	}

	pending := c.takePending(envelope.ID)
	if pending == nil {
		return
	}

	resultPayload := cloneResultPayload(payload)
	pending.resultCh <- CommandResult{
		Accepted: true,
		AckError: nil,
		Result:   &resultPayload,
	}
	close(pending.resultCh)
}

func (c *Client) attachSession(conn *websocket.Conn, sessionCancel context.CancelFunc, queue chan protocol.Envelope) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.conn = conn
	c.sessionCancel = sessionCancel
	c.writeQueue = queue
	if c.state != stateClosed {
		c.state = stateWaitingHello
	}
}

func (c *Client) detachSession(conn *websocket.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == conn {
		c.conn = nil
		c.sessionCancel = nil
		c.writeQueue = nil
	}
}

func (c *Client) setState(next clientState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == stateClosed && next != stateClosed {
		return
	}
	c.state = next
}

func (c *Client) currentState() clientState {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.state
}

func (c *Client) onHello() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == stateWaitingHello {
		c.state = stateWaitingSnap
	}
}

func (c *Client) onSnapshot() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == stateWaitingSnap {
		c.state = stateConnected
		return true
	}

	return false
}

func (c *Client) takePending(commandID string) *pendingCommand {
	c.mu.Lock()
	defer c.mu.Unlock()

	pending := c.pending[commandID]
	if pending != nil {
		delete(c.pending, commandID)
	}

	return pending
}

func (c *Client) markAckSeen(commandID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if pending, ok := c.pending[commandID]; ok {
		pending.ackSeen = true
	}
}

func (c *Client) invalidatePending() {
	c.mu.Lock()
	pending := c.pending
	c.pending = make(map[string]*pendingCommand)
	c.mu.Unlock()

	for _, command := range pending {
		command.resultCh <- CommandResult{
			Invalidated: true,
		}
		close(command.resultCh)
	}
}

func enqueueMessage(queue chan protocol.Envelope, message protocol.Envelope) bool {
	select {
	case queue <- message:
		return true
	default:
		return false
	}
}

func reportSessionError(errCh chan<- error, err error) {
	select {
	case errCh <- err:
	default:
	}
}

func cloneErrorPayload(payload *protocol.ErrorPayload) *protocol.ErrorPayload {
	if payload == nil {
		return nil
	}

	cloned := *payload
	return &cloned
}

func cloneResultPayload(payload protocol.ResultPayload) protocol.ResultPayload {
	cloned := payload
	cloned.Data = cloneRawMessage(payload.Data)
	if len(payload.FailedServices) > 0 {
		cloned.FailedServices = append([]string(nil), payload.FailedServices...)
	}
	cloned.Error = cloneErrorPayload(payload.Error)

	return cloned
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}

	cloned := make(json.RawMessage, len(raw))
	copy(cloned, raw)
	return cloned
}

func waitWithContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(current time.Duration) time.Duration {
	switch current {
	case 500 * time.Millisecond:
		return time.Second
	case time.Second:
		return 2 * time.Second
	case 2 * time.Second:
		return maxBackoff
	default:
		return maxBackoff
	}
}

func buildDialHeaders(options Options) http.Header {
	headers := cloneHTTPHeaders(options.DialHeaders)

	bearerToken := strings.TrimSpace(options.BearerToken)
	if bearerToken != "" {
		headers.Set("Authorization", "Bearer "+bearerToken)
	}

	if len(headers) == 0 {
		return nil
	}

	return headers
}

func cloneHTTPHeaders(headers http.Header) http.Header {
	if len(headers) == 0 {
		return make(http.Header)
	}

	cloned := make(http.Header, len(headers))
	for key, values := range headers {
		cloned[key] = append([]string(nil), values...)
	}

	return cloned
}

func (c *Client) dialOptions() *websocket.DialOptions {
	if len(c.dialHeaders) == 0 {
		return nil
	}

	return &websocket.DialOptions{HTTPHeader: cloneHTTPHeaders(c.dialHeaders)}
}
