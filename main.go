package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"nmtui/internal/ui"
)

var version = "0.3.0"

func main() {
	var (
		showVersion bool
		showHelp    bool
		iface       string
	)

	flag.BoolVar(&showVersion, "v", false, "print version and exit")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&showHelp, "h", false, "show help and exit")
	flag.BoolVar(&showHelp, "help", false, "show help and exit")
	flag.StringVar(&iface, "i", "", "Wi-Fi interface to use (e.g. wlan0)")
	flag.StringVar(&iface, "interface", "", "Wi-Fi interface to use (e.g. wlan0)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "nmtui - terminal UI for managing Wi-Fi with NetworkManager\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n  nmtui [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fmt.Fprintf(os.Stderr, "  -i, --interface <name>  Wi-Fi interface to manage (e.g. wlan0)\n")
		fmt.Fprintf(os.Stderr, "  -v, --version           Print version information and exit\n")
		fmt.Fprintf(os.Stderr, "  -h, --help              Show help information and exit\n")
	}

	flag.Parse()

	if showHelp {
		flag.Usage()
		os.Exit(0)
	}

	if showVersion {
		fmt.Printf("nmtui %s\n", version)
		os.Exit(0)
	}

	if _, err := exec.LookPath("nmcli"); err != nil {
		fmt.Fprintln(os.Stderr, "nmtui requires nmcli (install the 'networkmanager' package)")
		os.Exit(1)
	}

	p := tea.NewProgram(ui.NewModelWithDevice(iface), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
