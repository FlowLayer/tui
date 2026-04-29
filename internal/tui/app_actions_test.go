package tui

import "testing"

func TestActionForServiceKey_StartRestartRouting(t *testing.T) {
	testCases := []struct {
		name   string
		status string
		want   ServiceAction
	}{
		{name: "stopped maps to start", status: "stopped", want: ServiceActionStart},
		{name: "failed maps to start", status: "failed", want: ServiceActionStart},
		{name: "running maps to restart", status: "running", want: ServiceActionRestart},
		{name: "ready maps to restart", status: "ready", want: ServiceActionRestart},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			action, ok := actionForServiceKey("s", testCase.status)
			if !ok {
				t.Fatal("expected s key to resolve to an action")
			}
			if action != testCase.want {
				t.Fatalf("action = %q, want %q", action, testCase.want)
			}
		})
	}
}

func TestActionForServiceKey_StopKey(t *testing.T) {
	action, ok := actionForServiceKey("x", "running")
	if !ok {
		t.Fatal("expected x key to resolve to an action")
	}
	if action != ServiceActionStop {
		t.Fatalf("action = %q, want %q", action, ServiceActionStop)
	}
}

func TestActionForServiceKey_StartUnsupportedStatusNoAction(t *testing.T) {
	testCases := []struct {
		name   string
		status string
	}{
		{name: "starting has no action", status: "starting"},
		{name: "stopping has no action", status: "stopping"},
		{name: "unknown has no action", status: "unknown"},
		{name: "empty has no action", status: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			action, ok := actionForServiceKey("s", testCase.status)
			if ok {
				t.Fatal("expected s key to resolve to no action")
			}
			if action != "" {
				t.Fatalf("action = %q, want empty", action)
			}
		})
	}
}
