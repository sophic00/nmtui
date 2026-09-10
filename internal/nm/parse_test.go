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
		{"trailing backslash", "a\\", []string{"a\\"}},
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
		` :FC\:0A\:81\:DD\:62\:30:Infra:11:2462 MHz:195 Mbit/s:44:WPA2:Weak Signal`,
		`*:46\:DA\:2A\:D9\:C7\:77:Infra:60:5180 MHz:1170 Mbit/s:49:WPA3:Home\:Net`,
		` :B4\:C7\:99\:4A\:4A\:30:Infra:6:2437 MHz:130 Mbit/s:79::Open Cafe`,
		` :30\:CB\:C7\:90\:ED\:30:Infra:1:2412 MHz:130 Mbit/s:92:WPA2 802.1X:Corporate`,
		"",
	}, "\n")

	aps := parseWifiList(out)
	if len(aps) != 4 {
		t.Fatalf("expected 4 APs, got %d", len(aps))
	}

	if !aps[0].InUse || aps[0].SSID != "Home:Net" || aps[0].Signal != 49 || aps[0].Security != "WPA3" || aps[0].Chan != "60" {
		t.Errorf("in-use AP not first or fields wrong: %+v", aps[0])
	}
	if aps[0].BSSID != "46:DA:2A:D9:C7:77" || aps[0].Freq != "5180 MHz" || aps[0].Rate != "1170 Mbit/s" {
		t.Errorf("in-use AP details wrong: %+v", aps[0])
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
	out := ` :AA\:BB\:CC\:DD\:EE\:01:Infra:6:2437 MHz:130 Mbit/s:80:WPA2:`
	aps := parseWifiList(out)
	if len(aps) != 1 || aps[0].SSID != "" || aps[0].BSSID != "AA:BB:CC:DD:EE:01" {
		t.Errorf("hidden network (empty SSID) mishandled: %+v", aps)
	}
}

func TestParseWifiListDeduplication(t *testing.T) {
	out := strings.Join([]string{
		` :AA\:AA\:AA\:AA\:AA\:01:Infra:1:2412 MHz:130 Mbit/s:50:WPA2:Campus`,
		` :AA\:AA\:AA\:AA\:AA\:02:Infra:6:2437 MHz:130 Mbit/s:85:WPA2:Campus`,
		` :AA\:AA\:AA\:AA\:AA\:03:Infra:11:2462 MHz:130 Mbit/s:65:WPA2:Campus`,
		` :BB\:BB\:BB\:BB\:BB\:01:Infra:1:2412 MHz:130 Mbit/s:70:WPA2:HomeNet`,
		`*:BB\:BB\:BB\:BB\:BB\:02:Infra:6:2437 MHz:130 Mbit/s:40:WPA2:HomeNet`, // In-use, weaker signal than 70
		` :CC\:CC\:CC\:CC\:CC\:01:Infra:1:2412 MHz:130 Mbit/s:80:WPA2:`,        // Hidden 1
		` :CC\:CC\:CC\:CC\:CC\:02:Infra:6:2437 MHz:130 Mbit/s:75:WPA2:`,        // Hidden 2
	}, "\n")

	aps := parseWifiList(out)
	// Should have:
	// 1. HomeNet (in-use, signal 40)
	// 2. Campus (deduped, highest signal 85)
	// 3. Hidden 1
	// 4. Hidden 2
	if len(aps) != 4 {
		t.Fatalf("expected 4 APs after deduplication, got %d", len(aps))
	}

	if aps[0].SSID != "HomeNet" || !aps[0].InUse {
		t.Errorf("expected in-use HomeNet first, got: %+v", aps[0])
	}
	if aps[1].SSID != "Campus" || aps[1].Signal != 85 {
		t.Errorf("expected Campus with strongest signal 85, got: %+v", aps[1])
	}
	if aps[2].SSID != "" || aps[3].SSID != "" {
		t.Errorf("expected both hidden networks preserved, got: %+v and %+v", aps[2], aps[3])
	}
	if aps[2].BSSID == aps[3].BSSID {
		t.Errorf("hidden networks should keep distinct BSSIDs, got %q twice", aps[2].BSSID)
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

func TestParseRadioState(t *testing.T) {
	if !parseRadioState("enabled\n") {
		t.Error("enabled should parse as true")
	}
	if parseRadioState("disabled") {
		t.Error("disabled should parse as false")
	}
}

func TestParseGeneralStatus(t *testing.T) {
	state, conn, wifi := parseGeneralStatus("connected:full:enabled\n")
	if state != "connected" || conn != "full" || !wifi {
		t.Errorf("got state=%q connectivity=%q wifi=%v, want connected/full/true", state, conn, wifi)
	}

	state, conn, wifi = parseGeneralStatus("connected (locally):limited:disabled\n")
	if state != "connected (locally)" || conn != "limited" || wifi {
		t.Errorf("got state=%q connectivity=%q wifi=%v, want connected (locally)/limited/false", state, conn, wifi)
	}

	state, conn, wifi = parseGeneralStatus("connected:full\n")
	if state != "connected" || conn != "full" || wifi {
		t.Errorf("got state=%q connectivity=%q wifi=%v, want connected/full/false", state, conn, wifi)
	}
}

func TestParseDeviceState(t *testing.T) {
	connected := strings.Join([]string{
		"GENERAL.DEVICE:wlan0",
		"GENERAL.TYPE:wifi",
		"GENERAL.STATE:100 (connected)",
		"GENERAL.CONNECTION:cube",
		"GENERAL.CON-UUID:abc-123",
		"IP4.ADDRESS[1]:172.26.100.172/24",
		"IP4.GATEWAY:172.26.100.180",
	}, "\n")
	st, err := parseDeviceState(connected)
	if err != nil {
		t.Fatalf("connected device: %v", err)
	}
	if st.Device != "wlan0" || st.IP != "172.26.100.172/24" {
		t.Errorf("device/IP wrong: %+v", st)
	}
	if st.Active.Name != "cube" || st.Active.UUID != "abc-123" || st.Active.Device != "wlan0" {
		t.Errorf("active connection wrong: %+v", st.Active)
	}

	disconnected := "GENERAL.DEVICE:wlan0\nGENERAL.TYPE:wifi\nGENERAL.CONNECTION:\nGENERAL.CON-UUID:\n"
	st, err = parseDeviceState(disconnected)
	if err != nil {
		t.Fatalf("disconnected device: %v", err)
	}
	if st.Device != "wlan0" || st.Active.Name != "" {
		t.Errorf("expected no active connection, got %+v", st)
	}

	if _, err := parseDeviceState("GENERAL.DEVICE:eth0\nGENERAL.TYPE:ethernet\n"); err == nil {
		t.Error("non-wifi device should return an error")
	}
	if _, err := parseDeviceState(""); err == nil {
		t.Error("empty output should return an error")
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

func TestParseConnectionSSID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "802-11-wireless.ssid:cube\n", "cube"},
		{"escaped colon", `802-11-wireless.ssid:HOME\:5G`, "HOME:5G"},
		{"empty ssid", "802-11-wireless.ssid:\n", ""},
		{"unexpected property", "connection.id:cube\n", ""},
		{"blank input", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseConnectionSSID(tt.in); got != tt.want {
				t.Errorf("parseConnectionSSID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
