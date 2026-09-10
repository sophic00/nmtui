package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

type keyMap struct {
	Connect      key.Binding
	Rescan       key.Binding
	Toggle       key.Binding
	Disconnect   key.Binding
	Forget       key.Binding
	Details      key.Binding
	Filter       key.Binding
	Sort         key.Binding
	Speedtest    key.Binding
	Quicktest    key.Binding
	SavedNetwork key.Binding
	Hidden       key.Binding
	Device       key.Binding
	Quit         key.Binding
}

var keys = keyMap{
	Connect:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "connect")),
	Rescan:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rescan")),
	Toggle:       key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "wifi on/off")),
	Disconnect:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disconnect")),
	Forget:       key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "forget saved")),
	Details:      key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "details")),
	Filter:       key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Sort:         key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "sort")),
	Speedtest:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "speedtest")),
	Quicktest:    key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "quick test")),
	SavedNetwork: key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "saved networks")),
	Hidden:       key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "hidden network")),
	Device:       key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "switch device")),
	Quit:         key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}

var mainBindings = []key.Binding{
	keys.Connect, keys.Rescan, keys.Toggle, keys.Disconnect,
	keys.Forget, keys.Details, keys.Filter, keys.Sort,
	keys.Speedtest, keys.Quicktest, keys.SavedNetwork, keys.Hidden,
	keys.Device, keys.Quit,
}

var savedBindings = []key.Binding{
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "connect")),
	key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "autoconnect")),
	key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "password")),
	key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "forget")),
	key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
	key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

// bindingLines renders the key/desc segments with full descriptions. Fitting
// is handled by wrapParts, which wraps to the terminal width, so no
// abbreviated labels are needed.
func bindingLines(bindings []key.Binding, width int) []string {
	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		h := b.Help()
		parts = append(parts, dimStyle.Render(h.Key)+" "+h.Desc)
	}
	return wrapParts(parts, width)
}

// wrapParts packs help segments into as few lines as fit within width. The
// bubbletea renderer truncates overflowing lines, so a fixed one-line help
// bar would be cut off on narrow terminals; instead we wrap.
func wrapParts(parts []string, width int) []string {
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

// helpParts returns the main-view help segments, unwrapped.
func helpParts(width int) []string {
	parts := make([]string, 0, len(mainBindings))
	for _, b := range mainBindings {
		h := b.Help()
		parts = append(parts, dimStyle.Render(h.Key)+" "+h.Desc)
	}
	return parts
}

func helpLines(width int) []string {
	return bindingLines(mainBindings, width)
}

func savedHelpLines(width int) []string {
	return bindingLines(savedBindings, width)
}

func helpView(width int) string {
	return strings.Join(helpLines(width), "\n")
}
