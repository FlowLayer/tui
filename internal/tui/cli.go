package tui

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

const binaryName = "flowlayer-client-tui"

const (
	ansiReset = "\x1b[0m"
	ansiRedCode = "\x1b[31m"
)

var version = "1.1.0"

type tuiLauncher func(runtimeOptions) error

// Main is the entry point for the flowlayer-client-tui binary. It is invoked
// from cmd/flowlayer-client-tui/main.go.
func Main() {
	exitCode := runCLI(os.Args[1:], os.Stdout, os.Stderr, launchTUI)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func runCLI(args []string, stdout io.Writer, stderr io.Writer, launcher tuiLauncher) int {
	if launcher == nil {
		launcher = launchTUI
	}

	if hasHelpArg(args) {
		printOnboardingHelp(stdout)
		return 0
	}

	if hasVersionArg(args) {
		fmt.Fprintf(stdout, "%s %s\n", binaryName, version)
		return 0
	}

	fs := flag.NewFlagSet(binaryName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	addrFlag := fs.String("addr", "", "FlowLayer server address")
	tokenFlag := fs.String("token", "", "Bearer token")
	configFlag := fs.String("config", "", "FlowLayer config file (.json/.jsonc)")

	if err := fs.Parse(args); err != nil {
		return printUsageError(stderr, normalizeFlagError(err))
	}

	if fs.NArg() > 0 {
		return printUsageError(stderr, fmt.Sprintf("unexpected argument: %s", fs.Arg(0)))
	}

	explicitFlags := map[string]bool{}
	fs.Visit(func(current *flag.Flag) {
		explicitFlags[current.Name] = true
	})

	options, err := resolveRuntimeOptions(
		*configFlag,
		*addrFlag,
		explicitFlags["addr"],
		*tokenFlag,
		explicitFlags["token"],
	)
	if err != nil {
		return printUsageError(stderr, err.Error())
	}

	if err := launcher(options); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

func hasHelpArg(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}

	return false
}

func hasVersionArg(args []string) bool {
	for _, arg := range args {
		if arg == "--version" {
			return true
		}
	}

	return false
}

func printOnboardingHelp(w io.Writer) {
	fmt.Fprintf(w, "FlowLayer TUI %s\n\n", version)
	fmt.Fprintln(w, "Terminal client for a running FlowLayer server.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s\n", binaryName)
	fmt.Fprintf(w, "  %s -addr 127.0.0.1:6999\n", binaryName)
	fmt.Fprintf(w, "  %s -addr 127.0.0.1:6999 -token <bearer>\n", binaryName)
	fmt.Fprintf(w, "  %s -config /path/to/flowlayer.jsonc\n", binaryName)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -addr <host:port>   FlowLayer server address")
	fmt.Fprintln(w, "  -token <value>      Bearer token")
	fmt.Fprintln(w, "  -config <path>      Read addr/token from config")
	fmt.Fprintln(w, "  -h, --help          Show help")
	fmt.Fprintln(w, "  --version           Print version")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Documentation:")
	fmt.Fprintln(w, "  https://flowlayer.tech/tui/")
}

func printCLIError(w io.Writer, message string) {
	text := fmt.Sprintf("Error: %s", strings.TrimSpace(message))
	if shouldUseColor() {
		fmt.Fprintf(w, "%s\n\n", ansiRed(text))
		return
	}
	fmt.Fprintf(w, "%s\n\n", text)
}

func printUsageError(w io.Writer, message string) int {
	printCLIError(w, message)
	printOnboardingHelp(w)
	return 2
}

func normalizeFlagError(err error) string {
	if err == nil {
		return ""
	}

	const unknownFlagPrefix = "flag provided but not defined: "
	message := strings.TrimSpace(err.Error())
	if strings.HasPrefix(message, unknownFlagPrefix) {
		return "unknown flag: " + strings.TrimSpace(strings.TrimPrefix(message, unknownFlagPrefix))
	}

	return message
}

func shouldUseColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTTY(os.Stderr)
}

func isTTY(file *os.File) bool {
	if file == nil {
		return false
	}
	stat, err := file.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func ansiRed(text string) string {
	return ansiRedCode + text + ansiReset
}

func launchTUI(options runtimeOptions) error {
	// Force a modern color profile in runtime so themed styles are rendered
	// deterministically even when terminal auto-detection falls back to ASCII.
	lipgloss.SetColorProfile(termenv.TrueColor)

	// Override the terminal's default background and foreground so that ANSI
	// resets emitted by raw log content (Nest, compilers, shells) land on the
	// FlowLayer theme colors instead of the host terminal palette.
	output := termenv.DefaultOutput()
	output.Profile = termenv.TrueColor
	origBg := output.BackgroundColor()
	origFg := output.ForegroundColor()
	output.SetBackgroundColor(termenv.RGBColor(string(colorBackground)))
	output.SetForegroundColor(termenv.RGBColor(string(colorTextDefault)))
	defer output.SetBackgroundColor(origBg)
	defer output.SetForegroundColor(origFg)

	model := newModel(options.addr, options.token, nil)
	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		return err
	}

	return nil
}
