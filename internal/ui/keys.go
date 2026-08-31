package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Connect    key.Binding
	Rescan     key.Binding
	Toggle     key.Binding
	Disconnect key.Binding
	Forget     key.Binding
	Filter     key.Binding
	Quit       key.Binding
}

var keys = keyMap{
	Connect:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "connect")),
	Rescan:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rescan")),
	Toggle:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "wifi on/off")),
	Disconnect: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disconnect")),
	Forget:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "forget saved")),
	Filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Quit:       key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}

func helpView() string {
	parts := make([]string, 0, 7)
	for _, b := range []key.Binding{
		keys.Connect, keys.Rescan, keys.Toggle, keys.Disconnect,
		keys.Forget, keys.Filter, keys.Quit,
	} {
		h := b.Help()
		parts = append(parts, dimStyle.Render(h.Key)+" "+h.Desc)
	}
	return strings.Join(parts, "  ·  ")
}
