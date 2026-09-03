package ui

import (
	"testing"

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
