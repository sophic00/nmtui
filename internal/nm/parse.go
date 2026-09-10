package nm

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func splitTerse(line string) []string {
	if strings.IndexByte(line, '\\') == -1 {
		return strings.Split(line, ":")
	}

	fields := make([]string, 0, 8)
	var b strings.Builder
	escaped := false

	for _, r := range line {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == ':':
			fields = append(fields, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	if escaped {
		b.WriteRune('\\')
	}
	fields = append(fields, b.String())
	return fields
}

// parseWifiList expects fields in the order requested by ListAccessPoints:
// IN-USE,BSSID,MODE,CHAN,FREQ,RATE,SIGNAL,SECURITY,SSID. SSID is last because
// it is the only field allowed to contain unescaped colons.
func parseWifiList(out string) []AccessPoint {
	var aps []AccessPoint

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := splitTerse(line)
		if len(f) < 9 {
			continue
		}
		sig, _ := strconv.Atoi(strings.TrimSpace(f[6]))
		aps = append(aps, AccessPoint{
			InUse:    strings.TrimSpace(f[0]) == "*",
			BSSID:    strings.TrimSpace(f[1]),
			Mode:     strings.TrimSpace(f[2]),
			Chan:     strings.TrimSpace(f[3]),
			Freq:     strings.TrimSpace(f[4]),
			Rate:     strings.TrimSpace(f[5]),
			Signal:   sig,
			Security: strings.TrimSpace(f[7]),
			SSID:     strings.Join(f[8:], ":"),
		})
	}

	sort.SliceStable(aps, func(i, j int) bool {
		if aps[i].InUse != aps[j].InUse {
			return aps[i].InUse
		}
		return aps[i].Signal > aps[j].Signal
	})

	seen := make(map[string]bool, len(aps))
	deduped := make([]AccessPoint, 0, len(aps))
	for _, ap := range aps {
		key := ap.SSID
		if key == "" {
			// Hidden networks have no SSID; keep one row per BSSID so
			// the same network is not listed repeatedly.
			key = ap.BSSID
		}
		if key != "" {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		deduped = append(deduped, ap)
	}
	return deduped
}

func parseSavedConnections(out string) []SavedConnection {
	var conns []SavedConnection

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := splitTerse(line)
		if len(f) < 4 || f[1] != "802-11-wireless" {
			continue
		}
		conns = append(conns, SavedConnection{
			UUID:        f[0],
			Type:        f[1],
			Autoconnect: f[2],
			Name:        strings.Join(f[3:], ":"),
		})
	}
	return conns
}

// parseConnectionSSID extracts the value of the 802-11-wireless.ssid property
// from `nmcli -t -f 802-11-wireless.ssid connection show <uuid>` output.
func parseConnectionSSID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := splitTerse(line)
		if len(f) >= 2 && f[0] == "802-11-wireless.ssid" {
			return strings.Join(f[1:], ":")
		}
	}
	return ""
}

func parseRadioState(out string) bool {
	return strings.TrimSpace(out) == "enabled"
}

func parseGeneralStatus(out string) (state, connectivity string, wifiEnabled bool) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := splitTerse(line)
		state = f[0]
		if len(f) > 1 {
			connectivity = f[1]
		}
		if len(f) > 2 {
			wifiEnabled = parseRadioState(f[2])
		}
		return state, connectivity, wifiEnabled
	}
	return "", "", false
}

// parseDeviceState parses `nmcli -t -f ... device show <iface>` output into a
// WifiState. A non-Wi-Fi device is reported as an error so an invalid
// --interface value surfaces clearly instead of silently showing nothing.
func parseDeviceState(out string) (WifiState, error) {
	var st WifiState
	var devType, conName, conUUID string
	for _, line := range strings.Split(out, "\n") {
		f := splitTerse(strings.TrimSpace(line))
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "GENERAL.DEVICE":
			st.Device = f[1]
		case "GENERAL.TYPE":
			devType = f[1]
		case "GENERAL.CONNECTION":
			if f[1] != "" && f[1] != "--" {
				conName = f[1]
			}
		case "GENERAL.CON-UUID":
			if f[1] != "" && f[1] != "--" {
				conUUID = f[1]
			}
		}
	}
	if st.Device == "" {
		return WifiState{}, fmt.Errorf("device not found")
	}
	if devType != "" && devType != "wifi" {
		return WifiState{}, fmt.Errorf("%s is not a Wi-Fi device", st.Device)
	}
	st.IP = parseDeviceIP(out)
	if conName != "" {
		st.Active = ActiveConnection{
			Device: st.Device,
			Type:   "802-11-wireless",
			Name:   conName,
			UUID:   conUUID,
		}
	}
	return st, nil
}

func parseDeviceIP(out string) string {
	for _, line := range strings.Split(out, "\n") {
		f := splitTerse(strings.TrimSpace(line))
		if len(f) == 2 && strings.HasPrefix(f[0], "IP4.ADDRESS") && f[1] != "" {
			return f[1]
		}
	}
	return ""
}

func parseWifiDevice(out string) string {
	for _, line := range strings.Split(out, "\n") {
		f := splitTerse(strings.TrimSpace(line))
		if len(f) >= 2 && f[1] == "wifi" {
			return f[0]
		}
	}
	return ""
}
