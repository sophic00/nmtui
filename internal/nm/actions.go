package nm

import (
	"context"
	"strings"
	"sync"
)

// savedSSIDWorkers bounds how many `connection show <uuid>` lookups run in
// parallel while resolving saved-profile SSIDs.
const savedSSIDWorkers = 8

func (c *Client) GetStatus(ctx context.Context) (Status, error) {
	genOut, err := c.run(ctx, defaultTimeout, "-t", "-f", "STATE,CONNECTIVITY,WIFI", "general", "status")
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

func (c *Client) GetWifiState(ctx context.Context) (WifiState, error) {
	devOut, err := c.run(ctx, defaultTimeout, "-t", "-f", "DEVICE,TYPE,STATE,CONNECTION,CON-UUID", "device", "status")
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
		ipOut, err := c.run(ctx, defaultTimeout, "-t", "-f", "IP4.ADDRESS", "device", "show", st.Device)
		if err == nil {
			st.IP = parseDeviceIP(ipOut)
		}
	}
	return st, nil
}

func (c *Client) ListAccessPoints(ctx context.Context, rescan bool) ([]AccessPoint, error) {
	rescanFlag := "no"
	timeout := defaultTimeout
	if rescan {
		rescanFlag = "yes"
		timeout = scanTimeout
	}
	out, err := c.run(ctx, timeout,
		"-t", "-f", "IN-USE,MODE,CHAN,RATE,SIGNAL,SECURITY,SSID",
		"device", "wifi", "list", "--rescan", rescanFlag)
	if err != nil {
		return nil, err
	}
	return parseWifiList(out), nil
}

func (c *Client) ListSaved(ctx context.Context) ([]SavedConnection, error) {
	out, err := c.run(ctx, defaultTimeout, "-t", "-f", "UUID,TYPE,AUTOCONNECT,NAME", "connection", "show")
	if err != nil {
		return nil, err
	}
	conns := parseSavedConnections(out)
	c.resolveSavedSSIDs(ctx, conns)
	return conns, nil
}

// resolveSavedSSIDs fills in each profile's wireless SSID. Profile names can
// be renamed independently of the SSID, so the SSID property is the only
// reliable way to match a saved profile against a scanned access point.
func (c *Client) resolveSavedSSIDs(ctx context.Context, conns []SavedConnection) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, savedSSIDWorkers)
	for i := range conns {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			out, err := c.run(ctx, defaultTimeout,
				"-t", "-f", "802-11-wireless.ssid", "connection", "show", conns[i].UUID)
			if err != nil {
				return
			}
			conns[i].SSID = parseConnectionSSID(out)
		}(i)
	}
	wg.Wait()
}

func (c *Client) ToggleWifi(ctx context.Context, enable bool) error {
	state := "off"
	if enable {
		state = "on"
	}
	_, err := c.run(ctx, defaultTimeout, "radio", "wifi", state)
	return err
}

func (c *Client) Connect(ctx context.Context, ssid, password, device string) error {
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

	_, err := c.runWithStdin(ctx, connectTimeout, stdin, args...)
	return err
}

func (c *Client) Disconnect(ctx context.Context, device string) error {
	_, err := c.run(ctx, defaultTimeout, "device", "disconnect", device)
	return err
}

func (c *Client) Forget(ctx context.Context, id string) error {
	_, err := c.run(ctx, defaultTimeout, "connection", "delete", id)
	return err
}

func IsOpenSecurity(security string) bool {
	s := strings.TrimSpace(security)
	return s == "" || s == "--" || s == "open"
}
