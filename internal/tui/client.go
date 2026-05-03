package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/FlowLayer/tui/internal/wsclient"
)

type ConnectionStatus string

const (
	StatusConnected          ConnectionStatus = "connected"
	StatusTokenRequired      ConnectionStatus = "token required"
	StatusInvalidToken       ConnectionStatus = "invalid token"
	StatusUnreachable        ConnectionStatus = "unreachable"
	StatusInvalidAddress     ConnectionStatus = "invalid address"
	StatusUnexpectedResponse ConnectionStatus = "unexpected response"
)

type Service struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type ServiceLog struct {
	Seq       int64  `json:"seq,omitempty"`
	Service   string `json:"service"`
	Phase     string `json:"phase"`
	Message   string `json:"message"`
	Stream    string `json:"stream"`
	Timestamp string `json:"timestamp"`
}

type ServiceAction string

const (
	ServiceActionStart   ServiceAction = "start"
	ServiceActionStop    ServiceAction = "stop"
	ServiceActionRestart ServiceAction = "restart"
)

type ServiceActionResult string

const (
	ServiceActionSuccess        ServiceActionResult = "success"
	ServiceActionUnknownService ServiceActionResult = "unknown_service"
	ServiceActionConflict       ServiceActionResult = "conflict"
	ServiceActionServerError    ServiceActionResult = "server_error"
	ServiceActionError          ServiceActionResult = "action_error"
)

type ServiceLogsFetchStatus string

const (
	ServiceLogsFetchOK             ServiceLogsFetchStatus = "ok"
	ServiceLogsFetchBadRequest     ServiceLogsFetchStatus = "bad_request"
	ServiceLogsFetchUnknownService ServiceLogsFetchStatus = "unknown_service"
	ServiceLogsFetchRequestFailed  ServiceLogsFetchStatus = "request_failed"
	ServiceLogsFetchError          ServiceLogsFetchStatus = "error"
)

// getLogsTimeout bounds the time allowed for a get_logs round-trip. It is
// deliberately larger than the generic command timeout: on busy stacks (many
// services with high log volume) the server has to assemble and sort all
// in-memory entries under a global lock, which can take several seconds. A
// short timeout here causes the TUI to surface a transient error and lose the
// historical view, even though logs eventually arrive.
const getLogsTimeout = 30 * time.Second

const (
	logsFetchKindInitial = "initial"
	logsFetchKindOlder   = "older"
	logsFetchKindReplay  = "replay"

	commandIDUnavailable = "unavailable"

	runCommandFailureClientNil           = "client nil"
	runCommandFailureSendCommandError    = "send command error"
	runCommandFailureContextTimeout      = "context timeout"
	runCommandFailureContextCanceled     = "context canceled"
	runCommandFailureContextDone         = "context done"
	runCommandFailureResultChannelClosed = "result channel closed"
	runCommandFailureResultInvalidated   = "result invalidated"
)

type ServiceLogsFetchDiagnostics struct {
	Kind                      string
	CommandID                 string
	RequestedServiceName      string
	PayloadServiceName        string
	BeforeSeq                 int64
	AfterSeq                  int64
	Limit                     int
	EffectiveLogLimitAtFetch  *int
	LowestSeqAtFetch          int64
	LogEntriesLenAtFetch      int
	ClientReady               bool
	SelectedServiceAtFetch    string
	SelectedServiceAtResponse string
}

type ServiceLogsFetchResult struct {
	Status         ServiceLogsFetchStatus
	Logs           []ServiceLog
	Truncated      bool
	EffectiveLimit *int
	FailureReason  string
	Diagnostics    ServiceLogsFetchDiagnostics
}

var runCommandDetailedHook = runCommandDetailed

type ConnectResult struct {
	Status   ConnectionStatus
	Services []Service
}

type helloEventPayload struct {
	ProtocolVersion int      `json:"protocol_version"`
	Server          string   `json:"server"`
	Capabilities    []string `json:"capabilities"`
}

type snapshotEventPayload struct {
	Services []Service `json:"services"`
}

type serviceStatusEventPayload struct {
	Service   string `json:"service"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

type logEventPayload struct {
	Seq       int64  `json:"seq"`
	Service   string `json:"service"`
	Phase     string `json:"phase"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

type logsResultPayload struct {
	Entries        []logEventPayload `json:"entries"`
	Truncated      bool              `json:"truncated"`
	EffectiveLimit *int              `json:"effective_limit"`
}

func validateAddress(addr string) bool {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	if strings.TrimSpace(host) == "" {
		return false
	}
	value, err := strconv.Atoi(port)
	if err != nil {
		return false
	}
	return value >= 1 && value <= 65535
}

func connectWSClient(ctx context.Context, addr, token string) (ConnectResult, *wsclient.Client) {
	if !validateAddress(addr) {
		return ConnectResult{Status: StatusInvalidAddress}, nil
	}

	trimmedAddr := strings.TrimSpace(addr)
	client := wsclient.NewWithOptions("ws://"+trimmedAddr+"/ws", wsclient.Options{BearerToken: strings.TrimSpace(token)})

	connectCtx := ctx
	if connectCtx == nil {
		connectCtx = context.Background()
	}

	cancel := func() {}
	if _, hasDeadline := connectCtx.Deadline(); !hasDeadline {
		connectCtx, cancel = context.WithTimeout(connectCtx, 5*time.Second)
	}
	defer cancel()

	if err := client.Connect(connectCtx); err != nil {
		_ = client.Close()
		if err == context.DeadlineExceeded {
			return ConnectResult{Status: StatusUnreachable}, nil
		}
		return ConnectResult{Status: StatusUnreachable}, nil
	}

	return ConnectResult{Status: StatusConnected, Services: []Service{}}, client
}

func decodeHelloEvent(event wsclient.Event) (helloEventPayload, bool) {
	if strings.TrimSpace(event.Name) != "hello" {
		return helloEventPayload{}, false
	}

	var payload helloEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return helloEventPayload{}, false
	}

	if payload.ProtocolVersion <= 0 {
		return helloEventPayload{}, false
	}

	return payload, true
}

func decodeSnapshotEvent(event wsclient.Event) ([]Service, bool) {
	if strings.TrimSpace(event.Name) != "snapshot" {
		return nil, false
	}

	var payload snapshotEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return nil, false
	}

	if payload.Services == nil {
		payload.Services = []Service{}
	}

	return payload.Services, true
}

func decodeServiceStatusEvent(event wsclient.Event) (Service, bool) {
	if strings.TrimSpace(event.Name) != "service_status" {
		return Service{}, false
	}

	var payload serviceStatusEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return Service{}, false
	}

	serviceName := strings.TrimSpace(payload.Service)
	if serviceName == "" {
		return Service{}, false
	}

	return Service{Name: serviceName, Status: strings.TrimSpace(payload.Status)}, true
}

func decodeLogEvent(event wsclient.Event) (ServiceLog, bool) {
	if strings.TrimSpace(event.Name) != "log" {
		return ServiceLog{}, false
	}

	var payload logEventPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return ServiceLog{}, false
	}

	serviceName := strings.TrimSpace(payload.Service)
	if serviceName == "" {
		return ServiceLog{}, false
	}

	return ServiceLog{
		Seq:       payload.Seq,
		Service:   serviceName,
		Phase:     strings.TrimSpace(payload.Phase),
		Message:   payload.Message,
		Stream:    strings.TrimSpace(payload.Stream),
		Timestamp: strings.TrimSpace(payload.Timestamp),
	}, true
}

func sendServiceAction(ctx context.Context, client *wsclient.Client, action ServiceAction, serviceName string) ServiceActionResult {
	commandName := ""
	switch action {
	case ServiceActionStart:
		commandName = "start_service"
	case ServiceActionStop:
		commandName = "stop_service"
	case ServiceActionRestart:
		commandName = "restart_service"
	default:
		return ServiceActionError
	}

	payload := map[string]string{"service": strings.TrimSpace(serviceName)}
	if payload["service"] == "" {
		return ServiceActionError
	}

	result, ok := runCommand(ctx, client, commandName, payload)
	if !ok {
		return ServiceActionError
	}

	if !result.Accepted {
		switch commandErrorCode(result) {
		case "unknown_service":
			return ServiceActionUnknownService
		case "service_busy":
			return ServiceActionConflict
		default:
			return ServiceActionError
		}
	}

	if result.Result == nil {
		return ServiceActionError
	}
	if result.Result.OK {
		return ServiceActionSuccess
	}

	return ServiceActionServerError
}

func sendGlobalAction(ctx context.Context, client *wsclient.Client, action ServiceAction) ServiceActionResult {
	commandName := ""
	switch action {
	case ServiceActionStart:
		commandName = "start_all"
	case ServiceActionStop:
		commandName = "stop_all"
	default:
		return ServiceActionError
	}

	result, ok := runCommand(ctx, client, commandName, nil)
	if !ok {
		return ServiceActionError
	}

	if !result.Accepted {
		switch commandErrorCode(result) {
		case "service_busy":
			return ServiceActionConflict
		default:
			return ServiceActionError
		}
	}

	if result.Result == nil {
		return ServiceActionError
	}
	if result.Result.OK {
		return ServiceActionSuccess
	}

	return ServiceActionServerError
}

func fetchLogs(ctx context.Context, client *wsclient.Client, serviceName string) ServiceLogsFetchResult {
	return fetchLogsRequestWithKind(ctx, client, serviceName, 0, 0, 0, logsFetchKindInitial)
}

func fetchLogsAfter(ctx context.Context, client *wsclient.Client, serviceName string, afterSeq int64) ServiceLogsFetchResult {
	return fetchLogsRequestWithKind(ctx, client, serviceName, afterSeq, 0, 0, logsFetchKindReplay)
}

// fetchLogsBefore requests entries with seq strictly less than beforeSeq,
// capped at limit (0 = let the server decide). Used by the log-view UI to
// load older history when the user scrolls past the top of the buffer.
func fetchLogsBefore(ctx context.Context, client *wsclient.Client, serviceName string, beforeSeq int64, limit int) ServiceLogsFetchResult {
	return fetchLogsRequestWithKind(ctx, client, serviceName, 0, beforeSeq, limit, logsFetchKindOlder)
}

func fetchLogsRequest(ctx context.Context, client *wsclient.Client, serviceName string, afterSeq int64, beforeSeq int64, limit int) ServiceLogsFetchResult {
	return fetchLogsRequestWithKind(ctx, client, serviceName, afterSeq, beforeSeq, limit, logsFetchKindInitial)
}

func fetchLogsRequestWithKind(ctx context.Context, client *wsclient.Client, serviceName string, afterSeq int64, beforeSeq int64, limit int, kind string) ServiceLogsFetchResult {
	commandPayload := buildGetLogsPayload(serviceName, afterSeq, beforeSeq, limit)
	diagnostics := newServiceLogsFetchDiagnostics(kind, serviceName, commandPayload, afterSeq, beforeSeq, limit, client != nil)

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, hasDeadline := runCtx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, getLogsTimeout)
		defer cancel()
	}

	result, ok, failureReason := runCommandDetailedHook(runCtx, client, "get_logs", commandPayload)
	if !ok {
		return ServiceLogsFetchResult{
			Status:        ServiceLogsFetchRequestFailed,
			FailureReason: normalizeFailureReason(failureReason),
			Diagnostics:   diagnostics,
		}
	}

	if !result.Accepted {
		ackRejectedReason := formatAckRejectedReason(result)
		switch commandErrorCode(result) {
		case "invalid_payload":
			return ServiceLogsFetchResult{Status: ServiceLogsFetchBadRequest, FailureReason: ackRejectedReason, Diagnostics: diagnostics}
		case "unknown_service":
			return ServiceLogsFetchResult{Status: ServiceLogsFetchUnknownService, FailureReason: ackRejectedReason, Diagnostics: diagnostics}
		default:
			return ServiceLogsFetchResult{Status: ServiceLogsFetchError, FailureReason: ackRejectedReason, Diagnostics: diagnostics}
		}
	}

	if result.Result == nil {
		return ServiceLogsFetchResult{Status: ServiceLogsFetchError, FailureReason: "missing result payload", Diagnostics: diagnostics}
	}
	if !result.Result.OK {
		return ServiceLogsFetchResult{Status: ServiceLogsFetchError, FailureReason: formatResultFailureReason(result), Diagnostics: diagnostics}
	}

	decoded, ok := decodeLogsResultPayload(result.Result.Data)
	if !ok {
		return ServiceLogsFetchResult{Status: ServiceLogsFetchError, FailureReason: "decode get_logs result payload failed", Diagnostics: diagnostics}
	}
	decoded.Diagnostics = diagnostics

	return decoded
}

func newServiceLogsFetchDiagnostics(kind string, requestedServiceName string, commandPayload any, afterSeq int64, beforeSeq int64, limit int, clientReady bool) ServiceLogsFetchDiagnostics {
	return ServiceLogsFetchDiagnostics{
		Kind:                 normalizeFetchKind(kind),
		CommandID:            commandIDUnavailable,
		RequestedServiceName: strings.TrimSpace(requestedServiceName),
		PayloadServiceName:   payloadServiceNameFromCommandPayload(commandPayload),
		BeforeSeq:            beforeSeq,
		AfterSeq:             afterSeq,
		Limit:                limit,
		ClientReady:          clientReady,
	}
}

func normalizeFetchKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case logsFetchKindInitial:
		return logsFetchKindInitial
	case logsFetchKindOlder:
		return logsFetchKindOlder
	case logsFetchKindReplay:
		return logsFetchKindReplay
	default:
		return logsFetchKindInitial
	}
}

func payloadServiceNameFromCommandPayload(commandPayload any) string {
	decoded, ok := commandPayload.(map[string]any)
	if !ok {
		return ""
	}

	rawService, ok := decoded["service"]
	if !ok {
		return ""
	}

	serviceName, ok := rawService.(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(serviceName)
}

func normalizeFailureReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return "request failed"
	}
	return trimmed
}

func formatAckRejectedReason(result wsclient.CommandResult) string {
	summary := ""
	if result.AckError != nil {
		summary = commandErrorSummary(result.AckError.Code, result.AckError.Message)
	}
	if summary == "" {
		return "ack rejected"
	}
	return "ack rejected: " + summary
}

func formatResultFailureReason(result wsclient.CommandResult) string {
	if result.Result == nil {
		return "result not ok"
	}

	summary := ""
	if result.Result.Error != nil {
		summary = commandErrorSummary(result.Result.Error.Code, result.Result.Error.Message)
	}
	if summary == "" {
		return "result not ok"
	}
	return "result not ok: " + summary
}

func commandErrorSummary(code string, message string) string {
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	if code == "" && message == "" {
		return ""
	}
	if code != "" && message != "" {
		return code + ": " + message
	}
	if code != "" {
		return code
	}
	return message
}

func buildGetLogsPayload(serviceName string, afterSeq int64, beforeSeq int64, limit int) any {
	payload := map[string]any{}
	trimmedService := strings.TrimSpace(serviceName)
	if trimmedService != "" {
		payload["service"] = trimmedService
	}
	if afterSeq > 0 {
		payload["after_seq"] = afterSeq
	}
	if beforeSeq > 0 {
		payload["before_seq"] = beforeSeq
	}
	if limit > 0 {
		payload["limit"] = limit
	}

	commandPayload := any(nil)
	if len(payload) > 0 {
		commandPayload = payload
	}

	return commandPayload
}

func decodeLogsResultPayload(data json.RawMessage) (ServiceLogsFetchResult, bool) {
	if len(data) == 0 {
		return ServiceLogsFetchResult{Status: ServiceLogsFetchOK, Logs: []ServiceLog{}}, true
	}

	var decoded logsResultPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		return ServiceLogsFetchResult{}, false
	}

	logs := make([]ServiceLog, 0, len(decoded.Entries))
	for _, entry := range decoded.Entries {
		logEntry := ServiceLog{
			Seq:       entry.Seq,
			Service:   strings.TrimSpace(entry.Service),
			Phase:     strings.TrimSpace(entry.Phase),
			Message:   entry.Message,
			Stream:    strings.TrimSpace(entry.Stream),
			Timestamp: strings.TrimSpace(entry.Timestamp),
		}
		if strings.TrimSpace(logEntry.Service) == "" {
			continue
		}
		logs = append(logs, logEntry)
	}

	effectiveLimit := copyIntPointer(decoded.EffectiveLimit)
	if effectiveLimit != nil && *effectiveLimit <= 0 {
		effectiveLimit = nil
	}

	return ServiceLogsFetchResult{
		Status:         ServiceLogsFetchOK,
		Logs:           logs,
		Truncated:      decoded.Truncated,
		EffectiveLimit: effectiveLimit,
	}, true
}

func runCommand(ctx context.Context, client *wsclient.Client, name string, payload any) (wsclient.CommandResult, bool) {
	result, ok, _ := runCommandDetailed(ctx, client, name, payload)
	return result, ok
}

type runCommandSender func(context.Context, *wsclient.Client, string, any) (<-chan wsclient.CommandResult, error)

func runCommandDetailed(ctx context.Context, client *wsclient.Client, name string, payload any) (wsclient.CommandResult, bool, string) {
	return runCommandDetailedWithSender(ctx, client, name, payload, defaultRunCommandSender)
}

func runCommandDetailedWithSender(ctx context.Context, client *wsclient.Client, name string, payload any, sender runCommandSender) (wsclient.CommandResult, bool, string) {
	if client == nil {
		return wsclient.CommandResult{}, false, runCommandFailureClientNil
	}

	if sender == nil {
		sender = defaultRunCommandSender
	}

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	cancel := func() {}
	if _, hasDeadline := runCtx.Deadline(); !hasDeadline {
		runCtx, cancel = context.WithTimeout(runCtx, 5*time.Second)
	}
	defer cancel()

	resultCh, err := sender(runCtx, client, name, payload)
	if err != nil {
		if reason := describeRunContextDone(runCtx); reason != "" {
			return wsclient.CommandResult{}, false, reason
		}
		return wsclient.CommandResult{}, false, fmt.Sprintf("%s: %v", runCommandFailureSendCommandError, err)
	}

	select {
	case <-runCtx.Done():
		return wsclient.CommandResult{}, false, describeRunContextDone(runCtx)
	case result, ok := <-resultCh:
		if !ok {
			return wsclient.CommandResult{}, false, runCommandFailureResultChannelClosed
		}
		if result.Invalidated {
			return wsclient.CommandResult{}, false, runCommandFailureResultInvalidated
		}
		return result, true, ""
	}
}

func defaultRunCommandSender(ctx context.Context, client *wsclient.Client, name string, payload any) (<-chan wsclient.CommandResult, error) {
	return client.SendCommand(ctx, name, payload)
}

func describeRunContextDone(ctx context.Context) string {
	err := ctx.Err()
	if errors.Is(err, context.DeadlineExceeded) {
		return runCommandFailureContextTimeout
	}
	if errors.Is(err, context.Canceled) {
		return runCommandFailureContextCanceled
	}
	if err != nil {
		return fmt.Sprintf("%s: %v", runCommandFailureContextDone, err)
	}
	return ""
}

func commandErrorCode(result wsclient.CommandResult) string {
	if result.AckError == nil {
		return ""
	}
	return strings.TrimSpace(result.AckError.Code)
}
