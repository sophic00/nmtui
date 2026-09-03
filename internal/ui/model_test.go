package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"nmtui/internal/nm"
)

func TestSignalBars(t *testing.T) {
	tests := []struct {
		signal int
		want   string
	}{
		{95, "▂▄▆█"},
		{80, "▂▄▆█"},
		{60, "▂▄▆ "},
		{40, "▂▄  "},
		{10, "▂   "},
		{0, ""},
	}
	for _, tt := range tests {
		if got := signalBars(tt.signal); got != tt.want {
			t.Errorf("signalBars(%d) = %q, want %q", tt.signal, got, tt.want)
		}
	}
}

func TestApplyFilter(t *testing.T) {
	m := NewModel()
	m.aps = []nm.AccessPoint{
		{SSID: "HomeNet", Signal: 80},
		{SSID: "Cafe Guest", Signal: 60},
		{SSID: "corporate-net", Signal: 40},
	}

	m.filter.SetValue("")
	m.applyFilter()
	if len(m.visible) != 3 {
		t.Fatalf("no filter: expected 3 visible, got %d", len(m.visible))
	}

	m.filter.SetValue("cafe")
	m.applyFilter()
	if len(m.visible) != 1 || m.visible[0].SSID != "Cafe Guest" {
		t.Errorf("filter 'cafe': got %+v, want only Cafe Guest", m.visible)
	}

	m.filter.SetValue("NET")
	m.applyFilter()
	if len(m.visible) != 2 {
		t.Errorf("filter 'NET' (case-insensitive): got %d visible, want 2", len(m.visible))
	}

	m.filter.SetValue("zzz")
	m.applyFilter()
	if len(m.visible) != 0 {
		t.Errorf("filter with no match: got %d visible, want 0", len(m.visible))
	}
}

func TestSavedFor(t *testing.T) {
	m := NewModel()
	m.saved = []nm.SavedConnection{
		{Name: "J-VIT", UUID: "uuid-1", Type: "802-11-wireless"},
		{Name: "cube", UUID: "uuid-2", Type: "802-11-wireless"},
		{Name: "vpn-home", UUID: "uuid-3", Type: "tun"},
	}
	if m.savedFor("cube") == nil {
		t.Error("savedFor(cube) should find profile")
	}
	if m.savedFor("vpn-home") != nil {
		t.Error("savedFor(vpn-home) should ignore non-wifi profiles")
	}
	if m.savedFor("nope") != nil {
		t.Error("savedFor(nope) should be nil")
	}
	if m.savedFor("") != nil {
		t.Error("savedFor(empty) should be nil")
	}
}

func TestSanitizeSSID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"NormalNetwork", "NormalNetwork"},
		{"Space Network", "Space Network"},
		{"ANSI\x1b[31mRed\x1b[0m", "ANSI?[31mRed?[0m"},
		{"Line\nBreak\rReturn", "Line?Break?Return"},
		{"Tab\tSeparated", "Tab?Separated"},
		{"Emoji 🔥 Network 🚀", "Emoji 🔥 Network 🚀"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := sanitizeSSID(tt.in); got != tt.want {
			t.Errorf("sanitizeSSID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEmptyListActions(t *testing.T) {
	m := NewModel()
	m.busy = ""
	m.visible = nil

	// Enter on empty list
	m2, _ := m.updateList(tea.KeyMsg{Type: tea.KeyEnter})
	mod := m2.(Model)
	if mod.warnMsg != "no network selected" {
		t.Errorf("enter on empty list: got warnMsg %q, want 'no network selected'", mod.warnMsg)
	}

	// Forget on empty list
	m3, _ := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	mod2 := m3.(Model)
	if mod2.warnMsg != "no network selected" {
		t.Errorf("forget on empty list: got warnMsg %q, want 'no network selected'", mod2.warnMsg)
	}
}
