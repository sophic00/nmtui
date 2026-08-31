package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"nmtui/internal/ui"
)

func main() {
	if _, err := exec.LookPath("nmcli"); err != nil {
		fmt.Fprintln(os.Stderr, "nmtui requires nmcli (install the 'networkmanager' package)")
		os.Exit(1)
	}

	p := tea.NewProgram(ui.NewModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
