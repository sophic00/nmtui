package nm

import (
	"strings"
	"testing"
)

func TestSplitTerse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"plain", "a:b:c", []string{"a", "b", "c"}},
		{"escaped colon", `my\:net:x`, []string{"my:net", "x"}},
		{"escaped backslash", `a\\:b`, []string{"a\\", "b"}},
		{"empty fields", "::", []string{"", "", ""}},
		{"single field", "enabled", []string{"enabled"}},
		{"trailing empty", "a:", []string{"a", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitTerse(tt.in)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("splitTerse(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseWifiList(t *testing.T) {
	out := strings.Join([]string{
		` :Infra:11:195 Mbit/s:44:WPA2:Weak Signal`,
		`*:Infra:60:270 Mbit/s:49:WPA3:Home\:Net`,
		` :Infra:6:130 Mbit/s:79::Open Cafe`,
		` :Infra:1:130 Mbit/s:92:WPA2 802.1X:Corporate`,
		"",
	}, "\n")

	aps := parseWifiList(out)
	if len(aps) != 4 {
		t.Fatalf("expected 4 APs, got %d", len(aps))
	}

	if !aps[0].InUse || aps[0].SSID != "Home:Net" || aps[0].Signal != 49 || aps[0].Security != "WPA3" || aps[0].Chan != "60" {
		t.Errorf("in-use AP not first or fields wrong: %+v", aps[0])
	}
	if aps[1].SSID != "Corporate" || aps[1].Signal != 92 || aps[1].Security != "WPA2 802.1X" {
		t.Errorf("second AP wrong (should be sorted by signal desc): %+v", aps[1])
	}
	if aps[2].SSID != "Open Cafe" || aps[2].Security != "" {
		t.Errorf("open AP wrong: %+v", aps[2])
	}
	if aps[3].SSID != "Weak Signal" || aps[3].Signal != 44 {
		t.Errorf("last AP wrong: %+v", aps[3])
	}
}

func TestParseWifiListHiddenNetwork(t *testing.T) {
	out := " :Infra:6:130 Mbit/s:80:WPA2:"
	aps := parseWifiList(out)
	if len(aps) != 1 || aps[0].SSID != "" {
		t.Errorf("hidden network (empty SSID) mishandled: %+v", aps)
	}
}

func TestParseSavedConnections(t *testing.T) {
	out := strings.Join([]string{
		`9379a045-40d0-4114-b2a6-451770aa9ebf:802-11-wireless:yes:J-VIT`,
		`947eaebc-9a92-4862-ad16-3ea668824bd9:802-11-wireless:yes:arin's phone`,
		`ded4c82d-d024-4a0c-94e9-af1c41a979bc:loopback:yes:lo`,
	}, "\n")

	conns := parseSavedConnections(out)
	if len(conns) != 2 {
		t.Fatalf("expected 2 wifi connections (loopback ignored), got %d", len(conns))
	}
	if conns[0].Name != "J-VIT" || conns[0].Type != "802-11-wireless" || conns[0].Autoconnect != "yes" {
		t.Errorf("first connection wrong: %+v", conns[0])
	}
	if conns[1].Name != "arin's phone" {
		t.Errorf("name with spaces wrong: %+v", conns[1])
	}
}

func TestParseActiveConnections(t *testing.T) {
	out := `a4ca6cee-0db4-4280-9ec6-8fc34c73f1dd:tun:tailscale0:tailscale0
9379a045-40d0-4114-b2a6-451770aa9ebf:802-11-wireless:wlan0:J-VIT`

	conns := parseActiveConnections(out)
	if len(conns) != 2 {
		t.Fatalf("expected 2 active connections, got %d", len(conns))
	}
	if conns[1].Device != "wlan0" || conns[1].Name != "J-VIT" || conns[1].Type != "802-11-wireless" {
		t.Errorf("wifi active connection wrong: %+v", conns[1])
	}
}

func TestParseRadioState(t *testing.T) {
	if !parseRadioState("enabled\n") {
		t.Error("enabled should parse as true")
	}
	if parseRadioState("disabled") {
		t.Error("disabled should parse as false")
	}
}

func TestParseGeneralStatus(t *testing.T) {
	state, conn := parseGeneralStatus("connected:full\n")
	if state != "connected" || conn != "full" {
		t.Errorf("got state=%q connectivity=%q, want connected/full", state, conn)
	}

	state, conn = parseGeneralStatus("connected (locally):limited\n")
	if state != "connected (locally)" || conn != "limited" {
		t.Errorf("got state=%q connectivity=%q", state, conn)
	}
}

func TestParseDeviceIP(t *testing.T) {
	out := "IP4.ADDRESS[1]:172.16.170.93/21\nIP4.ADDRESS[2]:10.0.0.5/24\n"
	if ip := parseDeviceIP(out); ip != "172.16.170.93/21" {
		t.Errorf("got %q, want 172.16.170.93/21", ip)
	}
	if ip := parseDeviceIP(""); ip != "" {
		t.Errorf("empty output should give empty IP, got %q", ip)
	}
}

func TestParseWifiDevice(t *testing.T) {
	out := "wlan0:wifi\ntailscale0:tun\nlo:loopback\np2p-dev-wlan0:wifi-p2p\n"
	if dev := parseWifiDevice(out); dev != "wlan0" {
		t.Errorf("got %q, want wlan0 (wifi-p2p must not match)", dev)
	}
}

func TestIsOpenSecurity(t *testing.T) {
	for _, s := range []string{"", "--", "open", "  "} {
		if !IsOpenSecurity(s) {
			t.Errorf("IsOpenSecurity(%q) should be true", s)
		}
	}
	for _, s := range []string{"WPA2", "WPA3", "WEP"} {
		if IsOpenSecurity(s) {
			t.Errorf("IsOpenSecurity(%q) should be false", s)
		}
	}
}
