package nm

import (
	"sort"
	"strconv"
	"strings"
)

func splitTerse(line string) []string {
	var fields []string
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
	fields = append(fields, b.String())
	return fields
}

func parseWifiList(out string) []AccessPoint {
	var aps []AccessPoint

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := splitTerse(line)
		if len(f) < 7 {
			continue
		}
		sig, _ := strconv.Atoi(strings.TrimSpace(f[4]))
		aps = append(aps, AccessPoint{
			InUse:    strings.TrimSpace(f[0]) == "*",
			Chan:     strings.TrimSpace(f[2]),
			Signal:   sig,
			Security: strings.TrimSpace(f[5]),
			SSID:     strings.Join(f[6:], ":"),
		})
	}

	sort.SliceStable(aps, func(i, j int) bool {
		if aps[i].InUse != aps[j].InUse {
			return aps[i].InUse
		}
		return aps[i].Signal > aps[j].Signal
	})
	return aps
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

func parseActiveConnections(out string) []ActiveConnection {
	var conns []ActiveConnection

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := splitTerse(line)
		if len(f) < 4 {
			continue
		}
		conns = append(conns, ActiveConnection{
			UUID:   f[0],
			Type:   f[1],
			Device: f[2],
			Name:   strings.Join(f[3:], ":"),
		})
	}
	return conns
}

func parseRadioState(out string) bool {
	return strings.TrimSpace(out) == "enabled"
}

func parseGeneralStatus(out string) (state, connectivity string) {
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
		return state, connectivity
	}
	return "", ""
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
