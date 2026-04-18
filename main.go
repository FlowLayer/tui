package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func main() {
	addrFlag := flag.String("addr", "", "FlowLayer target host:port")
	tokenFlag := flag.String("token", "", "FlowLayer bearer token")
	configFlag := flag.String("config", "", "FlowLayer config file (.json/.jsonc)")
	flag.Parse()

	explicitFlags := map[string]bool{}
	flag.CommandLine.Visit(func(current *flag.Flag) {
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
