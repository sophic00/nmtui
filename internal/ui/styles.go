package ui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	onStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	offStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	boldStyle  = lipgloss.NewStyle().Bold(true)
	boxStyle   = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(1, 2)
)
