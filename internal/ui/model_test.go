package ui

import (
	"strings"
	"testing"
	"time"

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

	// Test cursor retention when list changes
	m.filter.SetValue("")
	m.aps = []nm.AccessPoint{
		{SSID: "NetA", Signal: 90},
		{SSID: "NetB", Signal: 80},
		{SSID: "NetC", Signal: 70},
	}
	m.applyFilter()
	m.table.SetCursor(1) // highlighting NetB
	if m.selectedAP().SSID != "NetB" {
		t.Fatalf("expected NetB selected, got %q", m.selectedAP().SSID)
	}

	// Reorder APs (e.g. signal changes)
	m.aps = []nm.AccessPoint{
		{SSID: "NetC", Signal: 95},
		{SSID: "NetA", Signal: 90},
		{SSID: "NetB", Signal: 80},
	}
	m.applyFilter()
	if m.table.Cursor() != 2 || m.selectedAP().SSID != "NetB" {
		t.Errorf("cursor should track NetB to index 2, got index %d with SSID %q", m.table.Cursor(), m.selectedAP().SSID)
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

func TestEscClearsFilter(t *testing.T) {
	m := NewModel()
	m.busy = ""
	m.aps = []nm.AccessPoint{
		{SSID: "NetA", Signal: 90},
		{SSID: "NetB", Signal: 80},
	}
	m.filter.SetValue("NetA")
	m.applyFilter()
	if len(m.visible) != 1 {
		t.Fatalf("expected 1 filtered AP, got %d", len(m.visible))
	}

	m2, _ := m.updateList(tea.KeyMsg{Type: tea.KeyEsc})
	mod := m2.(Model)
	if mod.filter.Value() != "" {
		t.Errorf("expected filter to be cleared on esc, got %q", mod.filter.Value())
	}
	if len(mod.visible) != 2 {
		t.Errorf("expected 2 visible APs after clearing filter, got %d", len(mod.visible))
	}
}

func TestStartConnectEnterprise(t *testing.T) {
	m := NewModel()
	m.busy = ""
	ap := nm.AccessPoint{
		SSID:     "eduroam",
		Security: "WPA2 802.1X",
	}

	m2, _ := m.startConnect(ap)
	mod := m2.(Model)
	if mod.mode == modePassword {
		t.Error("should not prompt for password on unconfigured 802.1X network")
	}
	if !strings.Contains(mod.warnMsg, "802.1X") {
		t.Errorf("expected 802.1X warning, got %q", mod.warnMsg)
	}
}

func TestLayoutAndHelpView(t *testing.T) {
	m := NewModel()
	m.width = 100
	m.height = 30
	m.layout()

	// Wide screen helpView
	wideHelp := helpView(100)
	if !strings.Contains(wideHelp, "rescan") {
		t.Errorf("expected full description on wide screen, got %q", wideHelp)
	}

	// Narrow screen helpView
	narrowHelp := helpView(60)
	if !strings.Contains(narrowHelp, "scan") {
		t.Errorf("expected compact description on narrow screen, got %q", narrowHelp)
	}

	// Dynamic column width test
	cols := m.table.Columns()
	var ssidColWidth int
	for _, c := range cols {
		if c.Title == "SSID" {
			ssidColWidth = c.Width
			break
		}
	}
	if ssidColWidth < 50 {
		t.Errorf("expected expanded SSID column on width 100, got %d", ssidColWidth)
	}
}

func TestPollMsg(t *testing.T) {
	m := NewModel()
	m.busy = ""
	m.mode = modeList

	// Trigger pollMsg
	_, cmd := m.Update(pollMsg(time.Now()))
	if cmd == nil {
		t.Error("expected batch command scheduled on pollMsg, got nil")
	}
}
