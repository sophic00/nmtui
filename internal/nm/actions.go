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

// GetWifiState returns the state of iface. When iface is empty the first
// Wi-Fi device reported by NetworkManager is used.
func (c *Client) GetWifiState(ctx context.Context, iface string) (WifiState, error) {
	if iface == "" {
		out, err := c.run(ctx, defaultTimeout, "-t", "-f", "DEVICE,TYPE", "device", "status")
		if err != nil {
			return WifiState{}, err
		}
		iface = parseWifiDevice(out)
	}
	if iface == "" {
		return WifiState{}, nil
	}

	out, err := c.run(ctx, defaultTimeout, "-t", "-f",
		"GENERAL.DEVICE,GENERAL.TYPE,GENERAL.STATE,GENERAL.CONNECTION,GENERAL.CON-UUID,IP4.ADDRESS",
		"device", "show", iface)
	if err != nil {
		return WifiState{}, err
	}
	return parseDeviceState(out)
}

func (c *Client) ListAccessPoints(ctx context.Context, rescan bool, iface string) ([]AccessPoint, error) {
	rescanFlag := "no"
	timeout := defaultTimeout
	if rescan {
		rescanFlag = "yes"
		timeout = scanTimeout
	}
	args := []string{
		"-t", "-f", "IN-USE,BSSID,MODE,CHAN,FREQ,RATE,SIGNAL,SECURITY,SSID",
		"device", "wifi", "list",
	}
	if iface != "" {
		args = append(args, "ifname", iface)
	}
	args = append(args, "--rescan", rescanFlag)

	out, err := c.run(ctx, timeout, args...)
	if err != nil {
		return nil, err
	}
	return parseWifiList(out), nil
}

func (c *Client) ListSaved(ctx context.Context) ([]SavedConnection, error) {
	conns, err := c.listProfiles(ctx)
	if err != nil {
		return nil, err
	}
	c.resolveSavedSSIDs(ctx, conns)
	return conns, nil
}

// listProfiles lists saved connections without resolving their SSIDs.
func (c *Client) listProfiles(ctx context.Context) ([]SavedConnection, error) {
	out, err := c.run(ctx, defaultTimeout, "-t", "-f", "UUID,TYPE,AUTOCONNECT,NAME", "connection", "show")
	if err != nil {
		return nil, err
	}
	return parseSavedConnections(out), nil
}

// removeNewProfiles deletes profiles named after ssid that appeared since the
// snapshot, i.e. ones a failed connect attempt left behind. Pre-existing
// profiles are never touched.
func (c *Client) removeNewProfiles(ctx context.Context, ssid string, before []SavedConnection) {
	after, err := c.listProfiles(ctx)
	if err != nil {
		return
	}
	known := make(map[string]bool, len(before))
	for _, conn := range before {
		known[conn.UUID] = true
	}
	for _, conn := range after {
		if known[conn.UUID] || conn.Name != ssid {
			continue
		}
		_, _ = c.run(ctx, defaultTimeout, "connection", "delete", conn.UUID)
	}
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
	// Snapshot saved profiles before connecting so a failed attempt can
	// remove only the profile nmcli created, never a pre-existing one.
	before, beforeErr := c.listProfiles(ctx)

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
	if err != nil && beforeErr == nil {
		c.removeNewProfiles(ctx, ssid, before)
	}
	return err
}

func (c *Client) Disconnect(ctx context.Context, device string) error {
	_, err := c.run(ctx, defaultTimeout, "device", "disconnect", device)
	return err
}

// ActivateConnection brings up a saved profile by UUID.
func (c *Client) ActivateConnection(ctx context.Context, uuid string) error {
	_, err := c.run(ctx, connectTimeout, "--wait", connectWait, "connection", "up", uuid)
	return err
}

func (c *Client) SetAutoconnect(ctx context.Context, uuid string, enabled bool) error {
	value := "no"
	if enabled {
		value = "yes"
	}
	_, err := c.run(ctx, defaultTimeout, "connection", "modify", uuid, "connection.autoconnect", value)
	return err
}

// ModifyPassword replaces the stored Wi-Fi password for a profile. nmcli does
// not prompt for modify values, so the password is passed as an argument; it
// is briefly visible in the process list.
func (c *Client) ModifyPassword(ctx context.Context, uuid, password string) error {
	_, err := c.run(ctx, defaultTimeout, "connection", "modify", uuid, "802-11-wireless-security.psk", password)
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
