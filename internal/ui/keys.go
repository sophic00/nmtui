package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

type keyMap struct {
	Connect    key.Binding
	Rescan     key.Binding
	Toggle     key.Binding
	Disconnect key.Binding
	Forget     key.Binding
	Filter     key.Binding
	Speedtest  key.Binding
	Quit       key.Binding
}

var keys = keyMap{
	Connect:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "connect")),
	Rescan:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rescan")),
	Toggle:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "wifi on/off")),
	Disconnect: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disconnect")),
	Forget:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "forget saved")),
	Filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Speedtest:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "speedtest")),
	Quit:       key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}

// helpParts renders the key/desc segments, using compact descriptions when
// the terminal is narrow.
func helpParts(width int) []string {
	parts := make([]string, 0, 8)
	for _, b := range []key.Binding{
		keys.Connect, keys.Rescan, keys.Toggle, keys.Disconnect,
		keys.Forget, keys.Filter, keys.Speedtest, keys.Quit,
	} {
		h := b.Help()
		desc := h.Desc
		if width > 0 && width < 80 {
			switch h.Key {
			case "enter":
				desc = "conn"
			case "r":
				desc = "scan"
			case "t":
				desc = "on/off"
			case "d":
				desc = "disc"
			case "f":
				desc = "forget"
			case "s":
				desc = "test"
			}
		}
		parts = append(parts, dimStyle.Render(h.Key)+" "+desc)
	}
	return parts
}

// helpLines packs the help segments into as few lines as fit within width.
// The bubbletea renderer truncates overflowing lines, so a fixed one-line
// help bar would be cut off on narrow terminals; instead we wrap.
func helpLines(width int) []string {
	parts := helpParts(width)
	sep := "  ·  "
	if width <= 0 {
		return []string{strings.Join(parts, sep)}
	}
	lines := make([]string, 0, 2)
	cur := ""
	for _, p := range parts {
		cand := p
		if cur != "" {
			cand = cur + sep + p
		}
		if lipgloss.Width(cand) <= width {
			cur = cand
		} else {
			if cur != "" {
				lines = append(lines, cur)
			}
			cur = p
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func helpView(width int) string {
	return strings.Join(helpLines(width), "\n")
}
