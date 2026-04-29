package tui

import (
	"bytes"
	"strings"
	"testing"
)

func runCLIForTest(args []string, launcher tuiLauncher) (int, string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runCLI(args, &stdout, &stderr, launcher)
	return code, stdout.String(), stderr.String()
}

func TestRunCLIHelpLongFlag(t *testing.T) {
	code, stdout, stderr := runCLIForTest([]string{"--help"}, nil)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	if !strings.Contains(stdout, "FlowLayer TUI 1.0.0") {
		t.Fatalf("expected version header in help output, got %q", stdout)
	}
	if !strings.Contains(stdout, "-h, --help") {
		t.Fatalf("expected help option in output, got %q", stdout)
	}
}

func TestRunCLIHelpShortFlag(t *testing.T) {
	code, stdout, stderr := runCLIForTest([]string{"-h"}, nil)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	if !strings.Contains(stdout, "Usage:") {
		t.Fatalf("expected usage section in help output, got %q", stdout)
	}
}

func TestRunCLIVersion(t *testing.T) {
	code, stdout, stderr := runCLIForTest([]string{"--version"}, nil)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	if strings.TrimSpace(stdout) != "flowlayer-client-tui 1.0.0" {
		t.Fatalf("unexpected version output: %q", stdout)
	}
}

func TestRunCLIUnsupportedShortVersionFlag(t *testing.T) {
	code, _, stderr := runCLIForTest([]string{"-v"}, nil)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr, "Error: unknown flag: -v") {
		t.Fatalf("expected unknown flag error, got %q", stderr)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Fatalf("expected help output after error, got %q", stderr)
	}
}

func TestRunCLIUnexpectedPositionalArgument(t *testing.T) {
	code, _, stderr := runCLIForTest([]string{"abc"}, nil)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr, "Error: unexpected argument: abc") {
		t.Fatalf("expected positional argument error, got %q", stderr)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Fatalf("expected help output after error, got %q", stderr)
	}
}

func TestRunCLIUnknownFlag(t *testing.T) {
	code, _, stderr := runCLIForTest([]string{"-tagada"}, nil)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr, "Error: unknown flag: -tagada") {
		t.Fatalf("expected unknown flag error, got %q", stderr)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Fatalf("expected help output after error, got %q", stderr)
	}
}

func TestRunCLIWithoutArgsLaunchesTUIPath(t *testing.T) {
	called := false
	var captured runtimeOptions
	launcher := func(options runtimeOptions) error {
		called = true
		captured = options
		return nil
	}

	code, stdout, stderr := runCLIForTest(nil, launcher)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !called {
		t.Fatal("expected launcher to be called")
	}
	if captured.addr != defaultAddrFallback {
		t.Fatalf("expected default addr %q, got %q", defaultAddrFallback, captured.addr)
	}
	if captured.token != defaultTokenFallback {
		t.Fatalf("expected default token %q, got %q", defaultTokenFallback, captured.token)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout, got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
}
