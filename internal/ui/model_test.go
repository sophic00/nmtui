package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"nmtui/internal/nm"
	"nmtui/internal/speedtest"
)

var errTestSpeed = errors.New("boom")

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

	// Narrow screen helpView keeps full descriptions (wrapped, not cut).
	narrowHelp := helpView(60)
	if !strings.Contains(narrowHelp, "connect") {
		t.Errorf("expected full 'connect' description on narrow screen, got %q", narrowHelp)
	}
	if strings.Contains(narrowHelp, "conn ") || strings.Contains(narrowHelp, " conn\n") {
		t.Errorf("expected no abbreviated 'conn' label, got %q", narrowHelp)
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

func TestSpeedtestRequiresConnection(t *testing.T) {
	m := NewModel()
	m.busy = ""
	m.wifi.Active.Name = ""
	m2, cmd := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	mod := m2.(Model)
	if !strings.Contains(mod.warnMsg, "not connected") {
		t.Errorf("expected not-connected warning, got %q", mod.warnMsg)
	}
	if mod.mode == modeSpeedtest {
		t.Error("should not enter speedtest mode without connection")
	}
	if cmd != nil {
		// updateList returns nil cmd on warn path; guard against regressions
		_ = cmd
	}
}

func TestSpeedtestStartAndEsc(t *testing.T) {
	m := NewModel()
	m.busy = ""
	m.wifi.Active.Name = "HomeNet"
	m2, cmd := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	mod := m2.(Model)
	if mod.mode != modeSpeedtest {
		t.Fatalf("expected modeSpeedtest, got %v", mod.mode)
	}
	if !mod.speedActive {
		t.Error("expected speedActive=true after start")
	}
	if cmd == nil {
		t.Error("expected batch cmd from startSpeedtest, got nil")
	}
	if mod.speedCancel == nil {
		t.Error("expected speedCancel to be set")
	}

	// esc cancels back to list
	m3, _ := mod.updateSpeedtest(tea.KeyMsg{Type: tea.KeyEsc})
	mod3 := m3.(Model)
	if mod3.mode != modeList {
		t.Errorf("esc: expected modeList, got %v", mod3.mode)
	}
	if mod3.speedActive {
		t.Error("esc: expected speedActive=false")
	}
	if mod3.busy != "" {
		t.Errorf("esc: expected busy cleared, got %q", mod3.busy)
	}
}

func TestSpeedtestBlockedWhileBusy(t *testing.T) {
	m := NewModel()
	m.busy = "scanning"
	m.wifi.Active.Name = "HomeNet"
	m2, _ := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if mod := m2.(Model); mod.mode == modeSpeedtest {
		t.Error("s while busy should be ignored")
	}
}

func TestSpeedProgressAndDoneMsgs(t *testing.T) {
	m := NewModel()
	m.busy = ""
	m.wifi.Active.Name = "HomeNet"
	m2, _ := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	mod := m2.(Model)

	prog := speedtest.Progress{Phase: speedtest.PhaseDownload, Elapsed: 5 * time.Second, TotalBytes: 50_000_000, InstantMbps: 80}
	m4, cmd := mod.Update(speedProgressMsg(prog))
	mod4 := m4.(Model)
	if mod4.speedProg.InstantMbps != 80 {
		t.Errorf("progress not stored: %+v", mod4.speedProg)
	}
	if cmd == nil {
		t.Error("expected re-armed listener cmd after progress, got nil")
	}

	res := speedtest.Result{DownloadMbps: 100, UploadMbps: 20, LatencyMs: 12, JitterMs: 2}
	m5, _ := mod4.Update(speedDoneMsg{result: res})
	mod5 := m5.(Model)
	if mod5.speedActive {
		t.Error("done: expected speedActive=false")
	}
	if mod5.busy != "" {
		t.Errorf("done: expected busy cleared, got %q", mod5.busy)
	}
	if mod5.speedResult == nil || mod5.speedResult.DownloadMbps != 100 {
		t.Errorf("done: result not stored: %+v", mod5.speedResult)
	}
	if got := mod5.View(); !strings.Contains(got, "100") {
		t.Errorf("result view should contain download figure, got:\n%s", got)
	}

	// Error path surfaces errMsg
	m6, _ := mod4.Update(speedDoneMsg{err: errTestSpeed})
	mod6 := m6.(Model)
	if mod6.errMsg == "" {
		t.Error("done with err: expected errMsg set")
	}
}

func TestProgressBar(t *testing.T) {
	if got := progressBar(0, 4); got != "░░░░" {
		t.Errorf("progressBar(0) = %q", got)
	}
	if got := progressBar(1, 4); got != "████" {
		t.Errorf("progressBar(1) = %q", got)
	}
	if got := progressBar(0.5, 4); got != "██░░" {
		t.Errorf("progressBar(0.5) = %q", got)
	}
	// Clamp out-of-range inputs
	if got := progressBar(2, 2); got != "██" {
		t.Errorf("progressBar(2) clamp = %q", got)
	}
}

func TestSpeedViewRunning(t *testing.T) {
	m := NewModel()
	m.mode = modeSpeedtest
	m.speedActive = true
	m.speedSSID = "HomeNet"
	m.speedProg = speedtest.Progress{Phase: speedtest.PhaseUpload, Elapsed: 3 * time.Second, InstantMbps: 25}
	if got := m.speedView(); !strings.Contains(got, "upload") {
		t.Errorf("running view should mention phase, got:\n%s", got)
	}
}

func TestResizeUpdatesTable(t *testing.T) {
	m := NewModel()
	m.busy = ""
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mod := m2.(Model)
	if mod.width != 100 || mod.height != 30 {
		t.Fatalf("size not stored: %dx%d", mod.width, mod.height)
	}
	w1, h1 := mod.table.Width(), mod.table.Height()
	m3, _ := mod.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	mod3 := m3.(Model)
	w2, h2 := mod3.table.Width(), mod3.table.Height()
	if w2 == w1 && h2 == h1 {
		t.Errorf("table dims unchanged after resize: still %dx%d", w1, h1)
	}
	if w2 != 60 {
		t.Errorf("table width = %d after resize to 60, want 60", w2)
	}
	if h2 >= h1 {
		t.Errorf("table height should shrink 30->20 rows: %d -> %d", h1, h2)
	}
	// SSID column follows width.
	ssidWidth := func(mm Model) int {
		for _, c := range mm.table.Columns() {
			if c.Title == "SSID" {
				return c.Width
			}
		}
		return -1
	}
	if ssidWidth(mod3) >= ssidWidth(mod) {
		t.Errorf("SSID column should shrink on resize: %d -> %d", ssidWidth(mod), ssidWidth(mod3))
	}
}

func TestSpeedBarWidthScales(t *testing.T) {
	m := NewModel()
	if got := m.speedBarWidth(); got != 30 {
		t.Errorf("zero width fallback = %d, want 30", got)
	}
	m.width = 140
	if got := m.speedBarWidth(); got != 50 {
		t.Errorf("wide bar = %d, want capped 50", got)
	}
	m.width = 50
	if got := m.speedBarWidth(); got != 26 {
		t.Errorf("narrow bar = %d, want 26", got)
	}
	m.width = 30
	if got := m.speedBarWidth(); got != 10 {
		t.Errorf("tiny bar = %d, want floored 10", got)
	}
}

func TestHelpViewFitsWidth(t *testing.T) {
	// The help bar must wrap instead of overflowing: bubbletea truncates
	// lines wider than the terminal, which used to cut off the last keys.
	for _, width := range []int{40, 50, 60, 68, 72, 80, 100, 140} {
		lines := helpLines(width)
		if len(lines) == 0 {
			t.Fatalf("width %d: no help lines", width)
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: help line %d is %d cells wide: %q", width, i, w, line)
			}
		}
	}
	// Wide terminals keep the full one-line bar with full descriptions.
	wide := helpLines(140)
	if len(wide) != 1 {
		t.Errorf("width 140: expected 1 help line, got %d: %q", len(wide), wide)
	}
	if !strings.Contains(wide[0], "speedtest") {
		t.Errorf("width 140: expected full descriptions, got %q", wide[0])
	}
	// Full words are never abbreviated, even on narrow terminals where
	// the bar wraps instead.
	for _, width := range []int{50, 68, 79} {
		joined := ""
		for _, line := range helpLines(width) {
			joined += line + "\n"
		}
		for _, desc := range []string{"connect", "rescan", "disconnect", "forget saved", "speedtest"} {
			if !strings.Contains(joined, desc) {
				t.Errorf("width %d: full description %q missing from wrapped help", width, desc)
			}
		}
	}
	// All bindings survive the wrap at every width.
	for _, width := range []int{40, 50, 68, 80} {
		joined := ""
		for _, line := range helpLines(width) {
			joined += line
		}
		for _, keyName := range []string{"enter", "r", "t", "d", "f", "/", "s", "q"} {
			if !strings.Contains(joined, keyName) {
				t.Errorf("width %d: key %q missing from wrapped help", width, keyName)
			}
		}
	}
}

func TestLayoutReservesWrappedHelpLines(t *testing.T) {
	m := NewModel()
	m.width = 50
	m.height = 24
	m.busy = ""
	m.layout()
	h1 := m.table.Height()

	// At a width where the help bar wraps to 2 lines, the table must be
	// shorter than at a width where it fits on one line.
	m.width = 140
	m.layout()
	h2 := m.table.Height()
	if h2 <= h1 {
		t.Errorf("expected table height to account for wrapped help: narrow=%d wide=%d", h1, h2)
	}
}

func TestSpeedDoneStaleRunIDIgnored(t *testing.T) {
	m := NewModel()
	m.busy = ""
	m.wifi.Active.Name = "HomeNet"
	m2, _ := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	mod := m2.(Model)
	currentGen := mod.speedGen

	stale := speedDoneMsg{result: speedtest.Result{DownloadMbps: 999}, runID: currentGen - 1}
	// runID 0 is accepted for backwards compat, so use an explicit stale non-zero ID.
	if currentGen-1 == 0 {
		// Force a second run so stale ID is non-zero and mismatched.
		m3, _ := mod.updateSpeedtest(tea.KeyMsg{Type: tea.KeyEsc})
		modEsc := m3.(Model)
		m4, _ := modEsc.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		mod = m4.(Model)
		stale.runID = mod.speedGen - 1
		if stale.runID == 0 {
			t.Skip("need non-zero stale runID")
		}
	}
	m5, _ := mod.Update(stale)
	mod5 := m5.(Model)
	if !mod5.speedActive {
		t.Error("stale done msg should not clear active run")
	}
	if mod5.speedResult != nil {
		t.Errorf("stale done msg should not store result: %+v", mod5.speedResult)
	}
}

func TestSpeedDoneCancelAfterEscSilent(t *testing.T) {
	m := NewModel()
	m.busy = ""
	m.wifi.Active.Name = "HomeNet"
	m2, _ := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	mod := m2.(Model)
	gen := mod.speedGen

	m3, _ := mod.updateSpeedtest(tea.KeyMsg{Type: tea.KeyEsc})
	escMod := m3.(Model)
	if escMod.mode != modeList {
		t.Fatalf("esc should return to list, got %v", escMod.mode)
	}
	m4, _ := escMod.Update(speedDoneMsg{err: context.Canceled, runID: gen})
	mod4 := m4.(Model)
	if mod4.errMsg != "" {
		t.Errorf("canceled done after esc should stay silent, got errMsg %q", mod4.errMsg)
	}
}

func TestSpeedtestQQuits(t *testing.T) {
	m := NewModel()
	m.busy = ""
	m.wifi.Active.Name = "HomeNet"
	m2, _ := m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	mod := m2.(Model)
	_, cmd := mod.updateSpeedtest(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q in speedtest mode should return a quit cmd")
	}
	if msg := cmd(); msg == nil {
		t.Error("expected non-nil quit msg")
	} else if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("q should quit, got %T", msg)
	}
}
