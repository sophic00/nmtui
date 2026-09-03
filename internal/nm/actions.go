package nm

import "strings"

func GetStatus() (Status, error) {
	genOut, err := run(defaultTimeout, "-t", "-f", "STATE,CONNECTIVITY,WIFI", "general", "status")
	if err != nil {
		return Status{}, err
	}
	state, connectivity, wifiEnabled := parseGeneralStatus(genOut)
	return Status{
		WifiEnabled:  wifiEnabled,
		State:        state,
		Connectivity: connectivity,
	}, nil
}

func GetWifiState() (WifiState, error) {
	devOut, err := run(defaultTimeout, "-t", "-f", "DEVICE,TYPE,STATE,CONNECTION,CON-UUID", "device", "status")
	if err != nil {
		return WifiState{}, err
	}

	dev, active := parseWifiDeviceStatus(devOut)
	st := WifiState{
		Device: dev,
		Active: active,
	}
	if st.Device == "" {
		return st, nil
	}

	if st.Active.Name != "" {
		ipOut, err := run(defaultTimeout, "-t", "-f", "IP4.ADDRESS", "device", "show", st.Device)
		if err == nil {
			st.IP = parseDeviceIP(ipOut)
		}
	}
	return st, nil
}

func ListAccessPoints(rescan bool) ([]AccessPoint, error) {
	rescanFlag := "no"
	timeout := defaultTimeout
	if rescan {
		rescanFlag = "yes"
		timeout = scanTimeout
	}
	out, err := run(timeout,
		"-t", "-f", "IN-USE,MODE,CHAN,RATE,SIGNAL,SECURITY,SSID",
		"device", "wifi", "list", "--rescan", rescanFlag)
	if err != nil {
		return nil, err
	}
	return parseWifiList(out), nil
}

func ListSaved() ([]SavedConnection, error) {
	out, err := run(defaultTimeout, "-t", "-f", "UUID,TYPE,AUTOCONNECT,NAME", "connection", "show")
	if err != nil {
		return nil, err
	}
	return parseSavedConnections(out), nil
}

func ToggleWifi(enable bool) error {
	state := "off"
	if enable {
		state = "on"
	}
	_, err := run(defaultTimeout, "radio", "wifi", state)
	return err
}

func Connect(ssid, password, device string) error {
	var args []string
	var stdin string

	if password != "" {
		args = []string{"--ask", "--wait", connectWait, "device", "wifi", "connect", ssid}
		stdin = password + "\n"
	} else {
		args = []string{"--wait", connectWait, "device", "wifi", "connect", ssid}
	}

	if device != "" {
		args = append(args, "ifname", device)
	}

	_, err := runWithStdin(connectTimeout, stdin, args...)
	return err
}

func Disconnect(device string) error {
	_, err := run(defaultTimeout, "device", "disconnect", device)
	return err
}

func Forget(id string) error {
	_, err := run(defaultTimeout, "connection", "delete", id)
	return err
}

func IsOpenSecurity(security string) bool {
	s := strings.TrimSpace(security)
	return s == "" || s == "--" || s == "open"
}
