package tui

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FlowLayer/tui/internal/wsclient"
)

func TestValidateAddress(t *testing.T) {
	testCases := []struct {
		name  string
		addr  string
		valid bool
	}{
		{name: "ipv4", addr: "127.0.0.1:6999", valid: true},
		{name: "hostname", addr: "localhost:8080", valid: true},
		{name: "ipv6", addr: "[::1]:6999", valid: true},
		{name: "missing port", addr: "127.0.0.1", valid: false},
		{name: "invalid port", addr: "127.0.0.1:abc", valid: false},
		{name: "empty host", addr: ":8080", valid: false},
		{name: "empty", addr: "", valid: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := validateAddress(testCase.addr); got != testCase.valid {
				t.Fatalf("validateAddress(%q) = %v, want %v", testCase.addr, got, testCase.valid)
			}
		})
	}
}

func TestConnectWSClientInvalidAddress(t *testing.T) {
	result, client := connectWSClient(context.Background(), "bad-address", "")
	if result.Status != StatusInvalidAddress {
		t.Fatalf("status = %q, want %q", result.Status, StatusInvalidAddress)
	}
	if client != nil {
		t.Fatal("client should be nil for invalid address")
	}
}

func TestDecodeHelloEventValid(t *testing.T) {
	event := wsclient.Event{
		Name: "hello",
		Data: json.RawMessage(`{"protocol_version":1,"server":"flowlayer","capabilities":["get_snapshot"]}`),
	}

	payload, ok := decodeHelloEvent(event)
	if !ok {
		t.Fatal("expected hello payload to decode")
	}
	if payload.ProtocolVersion != 1 {
		t.Fatalf("protocol version = %d, want 1", payload.ProtocolVersion)
	}
}

func TestDecodeSnapshotEventValid(t *testing.T) {
	event := wsclient.Event{
		Name: "snapshot",
		Data: json.RawMessage(`{"services":[{"name":"billing","status":"ready"}]}`),
	}

	services, ok := decodeSnapshotEvent(event)
	if !ok {
		t.Fatal("expected snapshot payload to decode")
	}
	if len(services) != 1 {
		t.Fatalf("services len = %d, want 1", len(services))
	}
	if services[0].Name != "billing" || services[0].Status != "ready" {
		t.Fatalf("service = %+v, want billing/ready", services[0])
	}
}

func TestDecodeServiceStatusEventValid(t *testing.T) {
	event := wsclient.Event{
		Name: "service_status",
		Data: json.RawMessage(`{"service":"users","status":"running","timestamp":"2026-04-14T12:00:00Z"}`),
	}

	service, ok := decodeServiceStatusEvent(event)
	if !ok {
		t.Fatal("expected service_status payload to decode")
	}
	if service.Name != "users" || service.Status != "running" {
		t.Fatalf("service = %+v, want users/running", service)
	}
}

func TestDecodeLogEventValidAndSeq(t *testing.T) {
	event := wsclient.Event{
		Name: "log",
		Data: json.RawMessage(`{"seq":42,"service":"billing","phase":"start","stream":"stdout","message":"hello","timestamp":"2026-04-14T12:00:00Z"}`),
	}

	entry, ok := decodeLogEvent(event)
	if !ok {
		t.Fatal("expected log payload to decode")
	}
	if entry.Seq != 42 {
		t.Fatalf("seq = %d, want 42", entry.Seq)
	}
	if entry.Service != "billing" || entry.Message != "hello" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestSendServiceActionWithoutClient(t *testing.T) {
	result := sendServiceAction(context.Background(), nil, ServiceActionRestart, "billing")
	if result != ServiceActionError {
		t.Fatalf("result = %q, want %q", result, ServiceActionError)
	}
}

func TestFetchLogsWithoutClient(t *testing.T) {
	result := fetchLogs(context.Background(), nil, "billing")
	if result.Status != ServiceLogsFetchRequestFailed {
		t.Fatalf("status = %q, want %q", result.Status, ServiceLogsFetchRequestFailed)
	}
}

func TestFetchLogsAfterWithoutClient(t *testing.T) {
	result := fetchLogsAfter(context.Background(), nil, "billing", 42)
	if result.Status != ServiceLogsFetchRequestFailed {
		t.Fatalf("status = %q, want %q", result.Status, ServiceLogsFetchRequestFailed)
	}
}

func TestBuildGetLogsPayloadEmptyIsNil(t *testing.T) {
	payload := buildGetLogsPayload("", 0, 0, 0)
	if payload != nil {
		t.Fatalf("payload = %#v, want nil", payload)
	}
}

func TestBuildGetLogsPayloadIncludesServiceWithoutImplicitLimit(t *testing.T) {
	payload := buildGetLogsPayload(" billing ", 0, 0, 0)
	decoded, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", payload)
	}

	if decoded["service"] != "billing" {
		t.Fatalf("payload.service = %#v, want %q", decoded["service"], "billing")
	}
	if _, hasLimit := decoded["limit"]; hasLimit {
		t.Fatal("did not expect payload.limit without explicit override")
	}
}

func TestBuildGetLogsPayloadIncludesAfterSeq(t *testing.T) {
	payload := buildGetLogsPayload("billing", 42, 0, 0)
	decoded, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", payload)
	}

	if decoded["service"] != "billing" {
		t.Fatalf("payload.service = %#v, want %q", decoded["service"], "billing")
	}
	if decoded["after_seq"] != int64(42) {
		t.Fatalf("payload.after_seq = %#v, want %d", decoded["after_seq"], 42)
	}
	if _, hasLimit := decoded["limit"]; hasLimit {
		t.Fatal("did not expect payload.limit for standard TUI requests")
	}
}

func TestBuildGetLogsPayloadIncludesBeforeSeqAndLimit(t *testing.T) {
	payload := buildGetLogsPayload("billing", 0, 99, 200)
	decoded, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", payload)
	}

	if decoded["before_seq"] != int64(99) {
		t.Fatalf("payload.before_seq = %#v, want %d", decoded["before_seq"], 99)
	}
	if decoded["limit"] != 200 {
		t.Fatalf("payload.limit = %#v, want %d", decoded["limit"], 200)
	}
	if _, hasAfter := decoded["after_seq"]; hasAfter {
		t.Fatal("did not expect payload.after_seq when only before_seq was set")
	}
}

func TestFetchLogsBeforeWithoutClient(t *testing.T) {
	result := fetchLogsBefore(context.Background(), nil, "billing", 99, 100)
	if result.Status != ServiceLogsFetchRequestFailed {
		t.Fatalf("status = %q, want %q", result.Status, ServiceLogsFetchRequestFailed)
	}
}

func TestDecodeLogsResultPayloadCapturesTruncated(t *testing.T) {
	decoded, ok := decodeLogsResultPayload(json.RawMessage(`{
		"entries":[{"seq":42,"service":"ping","phase":"start","stream":"stdout","message":"hello","timestamp":"2026-04-18T12:00:00Z"}],
		"truncated":true,
		"effective_limit":5
	}`))
	if !ok {
		t.Fatal("expected payload decoding to succeed")
	}
	if decoded.Status != ServiceLogsFetchOK {
		t.Fatalf("status = %q, want %q", decoded.Status, ServiceLogsFetchOK)
	}
	if !decoded.Truncated {
		t.Fatal("expected truncated=true")
	}
	if len(decoded.Logs) != 1 {
		t.Fatalf("logs len = %d, want 1", len(decoded.Logs))
	}
	if decoded.Logs[0].Service != "ping" {
		t.Fatalf("service = %q, want %q", decoded.Logs[0].Service, "ping")
	}
	if decoded.EffectiveLimit == nil || *decoded.EffectiveLimit != 5 {
		t.Fatalf("effective limit = %v, want 5", decoded.EffectiveLimit)
	}
}

func TestDecodeLogsResultPayloadWithoutEffectiveLimitRemainsCompatible(t *testing.T) {
	decoded, ok := decodeLogsResultPayload(json.RawMessage(`{
		"entries":[{"seq":7,"service":"billing","phase":"run","stream":"stdout","message":"line","timestamp":"2026-04-18T12:00:00Z"}],
		"truncated":false
	}`))
	if !ok {
		t.Fatal("expected payload decoding to succeed")
	}
	if decoded.EffectiveLimit != nil {
		t.Fatalf("effective limit = %v, want nil", *decoded.EffectiveLimit)
	}
}
