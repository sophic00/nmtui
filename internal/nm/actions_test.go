package nm

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRunner records invocations and answers them from a scripted function.
type fakeRunner struct {
	mu     sync.Mutex
	calls  [][]string
	stdins []string
	fn     func(args []string) (string, error)
}

func (f *fakeRunner) Run(_ context.Context, _ time.Duration, stdin string, args ...string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string(nil), args...))
	f.stdins = append(f.stdins, stdin)
	f.mu.Unlock()
	if f.fn == nil {
		return "", nil
	}
	return f.fn(args)
}

func TestListSavedResolvesSSID(t *testing.T) {
	runner := &fakeRunner{fn: func(args []string) (string, error) {
		switch strings.Join(args, " ") {
		case "-t -f UUID,TYPE,AUTOCONNECT,NAME connection show":
			return strings.Join([]string{
				"u1:802-11-wireless:yes:Home Profile",
				"u2:802-11-wireless:yes:cube",
				"u3:tun:yes:tailscale0",
				"",
			}, "\n"), nil
		case "-t -f 802-11-wireless.ssid connection show u1":
			return "802-11-wireless.ssid:HOME\\:5G\n", nil
		case "-t -f 802-11-wireless.ssid connection show u2":
			return "802-11-wireless.ssid:cube\n", nil
		}
		return "", fmt.Errorf("unexpected args: %q", args)
	}}

	conns, err := NewClientWithRunner(runner).ListSaved(context.Background())
	if err != nil {
		t.Fatalf("ListSaved: %v", err)
	}
	if len(conns) != 2 {
		t.Fatalf("expected 2 wifi profiles, got %d: %+v", len(conns), conns)
	}
	if conns[0].Name != "Home Profile" || conns[0].SSID != "HOME:5G" {
		t.Errorf("renamed profile not resolved: %+v", conns[0])
	}
	if conns[1].SSID != "cube" {
		t.Errorf("profile SSID = %q, want cube", conns[1].SSID)
	}
}

func TestListSavedSSIDLookupFailureKeepsProfile(t *testing.T) {
	runner := &fakeRunner{fn: func(args []string) (string, error) {
		if strings.HasSuffix(strings.Join(args, " "), "connection show") {
			return "u1:802-11-wireless:yes:cube\n", nil
		}
		return "", fmt.Errorf("boom")
	}}

	conns, err := NewClientWithRunner(runner).ListSaved(context.Background())
	if err != nil {
		t.Fatalf("ListSaved: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("expected 1 profile, got %d: %+v", len(conns), conns)
	}
	if conns[0].SSID != "" || conns[0].Name != "cube" {
		t.Errorf("failed SSID lookup should keep profile with empty SSID, got %+v", conns[0])
	}
}

// connectCall returns the recorded connect invocation and its stdin.
func connectCall(t *testing.T, runner *fakeRunner) ([]string, string) {
	t.Helper()
	for i, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), "device wifi connect") {
			return call, runner.stdins[i]
		}
	}
	t.Fatal("no connect call recorded")
	return nil, ""
}

func TestConnectSendsPasswordOnStdin(t *testing.T) {
	runner := &fakeRunner{}
	err := NewClientWithRunner(runner).Connect(context.Background(), "HomeNet", "s3cret", "wlan0")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	call, stdin := connectCall(t, runner)
	want := []string{"--ask", "--wait", connectWait, "device", "wifi", "connect", "HomeNet", "ifname", "wlan0"}
	if !reflect.DeepEqual(call, want) {
		t.Errorf("args = %q, want %q", call, want)
	}
	if stdin != "s3cret\n" {
		t.Errorf("stdin = %q, want password followed by newline", stdin)
	}
}

func TestConnectOpenNetwork(t *testing.T) {
	runner := &fakeRunner{}
	if err := NewClientWithRunner(runner).Connect(context.Background(), "Open", "", ""); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	call, stdin := connectCall(t, runner)
	want := []string{"--wait", connectWait, "device", "wifi", "connect", "Open"}
	if !reflect.DeepEqual(call, want) {
		t.Errorf("args = %q, want %q", call, want)
	}
	if stdin != "" {
		t.Errorf("stdin = %q, want empty for open networks", stdin)
	}
}

func TestConnectRemovesProfileLeftByFailure(t *testing.T) {
	listCalls := 0
	runner := &fakeRunner{fn: func(args []string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.HasSuffix(joined, "connection show"):
			listCalls++
			if listCalls == 1 {
				return "old-uuid:802-11-wireless:yes:Other\n", nil
			}
			return "old-uuid:802-11-wireless:yes:Other\nnew-uuid:802-11-wireless:yes:HomeNet\n", nil
		case strings.Contains(joined, "device wifi connect"):
			return "", fmt.Errorf("Error: Connection activation failed")
		case joined == "connection delete new-uuid":
			return "", nil
		}
		return "", fmt.Errorf("unexpected args: %q", args)
	}}

	err := NewClientWithRunner(runner).Connect(context.Background(), "HomeNet", "wrong", "wlan0")
	if err == nil {
		t.Fatal("expected connect failure")
	}

	deleted := false
	for _, call := range runner.calls {
		if strings.Join(call, " ") == "connection delete new-uuid" {
			deleted = true
		}
	}
	if !deleted {
		t.Error("profile created by the failed attempt should be deleted")
	}
}

func TestConnectKeepsPreexistingProfile(t *testing.T) {
	profiles := "old-uuid:802-11-wireless:yes:HomeNet\n"
	runner := &fakeRunner{fn: func(args []string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.HasSuffix(joined, "connection show"):
			return profiles, nil
		case strings.Contains(joined, "device wifi connect"):
			return "", fmt.Errorf("Error: Connection activation failed")
		}
		return "", fmt.Errorf("unexpected args: %q", args)
	}}

	err := NewClientWithRunner(runner).Connect(context.Background(), "HomeNet", "wrong", "wlan0")
	if err == nil {
		t.Fatal("expected connect failure")
	}
	for _, call := range runner.calls {
		if strings.HasPrefix(strings.Join(call, " "), "connection delete") {
			t.Errorf("pre-existing profile must not be deleted: %q", call)
		}
	}
}

func TestGetWifiStateAutoDetectsDevice(t *testing.T) {
	runner := &fakeRunner{fn: func(args []string) (string, error) {
		switch strings.Join(args, " ") {
		case "-t -f DEVICE,TYPE device status":
			return "eth0:ethernet\nwlan0:wifi\n", nil
		case "-t -f GENERAL.DEVICE,GENERAL.TYPE,GENERAL.STATE,GENERAL.CONNECTION,GENERAL.CON-UUID,IP4.ADDRESS device show wlan0":
			return strings.Join([]string{
				"GENERAL.DEVICE:wlan0",
				"GENERAL.TYPE:wifi",
				"GENERAL.CONNECTION:cube",
				"GENERAL.CON-UUID:u1",
				"IP4.ADDRESS[1]:10.0.0.2/24",
			}, "\n"), nil
		}
		return "", fmt.Errorf("unexpected args: %q", args)
	}}

	st, err := NewClientWithRunner(runner).GetWifiState(context.Background(), "")
	if err != nil {
		t.Fatalf("GetWifiState: %v", err)
	}
	if st.Device != "wlan0" || st.Active.Name != "cube" || st.IP != "10.0.0.2/24" {
		t.Errorf("auto-detected state wrong: %+v", st)
	}
}

func TestGetWifiStateHonorsInterface(t *testing.T) {
	runner := &fakeRunner{fn: func(args []string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "device show wlan1") {
			return "GENERAL.DEVICE:wlan1\nGENERAL.TYPE:wifi\n", nil
		}
		return "", fmt.Errorf("unexpected args: %q", args)
	}}

	st, err := NewClientWithRunner(runner).GetWifiState(context.Background(), "wlan1")
	if err != nil {
		t.Fatalf("GetWifiState: %v", err)
	}
	if st.Device != "wlan1" {
		t.Errorf("device = %q, want wlan1", st.Device)
	}
	// An explicit interface must not require the auto-detect call.
	for _, call := range runner.calls {
		if reflect.DeepEqual(call, []string{"-t", "-f", "DEVICE,TYPE", "device", "status"}) {
			t.Error("explicit interface should skip device auto-detection")
		}
	}
}

func TestGetWifiStateRejectsNonWifiDevice(t *testing.T) {
	runner := &fakeRunner{fn: func(args []string) (string, error) {
		return "GENERAL.DEVICE:eth0\nGENERAL.TYPE:ethernet\n", nil
	}}
	_, err := NewClientWithRunner(runner).GetWifiState(context.Background(), "eth0")
	if err == nil || !strings.Contains(err.Error(), "not a Wi-Fi device") {
		t.Errorf("error = %v, want 'not a Wi-Fi device'", err)
	}
}

func TestSavedProfileActions(t *testing.T) {
	runner := &fakeRunner{}
	c := NewClientWithRunner(runner)

	if err := c.ActivateConnection(context.Background(), "uuid-1"); err != nil {
		t.Fatalf("ActivateConnection: %v", err)
	}
	if err := c.SetAutoconnect(context.Background(), "uuid-1", true); err != nil {
		t.Fatalf("SetAutoconnect(true): %v", err)
	}
	if err := c.SetAutoconnect(context.Background(), "uuid-1", false); err != nil {
		t.Fatalf("SetAutoconnect(false): %v", err)
	}
	if err := c.ModifyPassword(context.Background(), "uuid-1", "s3cret"); err != nil {
		t.Fatalf("ModifyPassword: %v", err)
	}

	want := [][]string{
		{"--wait", connectWait, "connection", "up", "uuid-1"},
		{"connection", "modify", "uuid-1", "connection.autoconnect", "yes"},
		{"connection", "modify", "uuid-1", "connection.autoconnect", "no"},
		{"connection", "modify", "uuid-1", "802-11-wireless-security.psk", "s3cret"},
	}
	if len(runner.calls) != len(want) {
		t.Fatalf("got %d calls, want %d: %q", len(runner.calls), len(want), runner.calls)
	}
	for i, w := range want {
		if !reflect.DeepEqual(runner.calls[i], w) {
			t.Errorf("call %d = %q, want %q", i, runner.calls[i], w)
		}
	}
}

func TestListAccessPointsPassesInterface(t *testing.T) {
	runner := &fakeRunner{fn: func(args []string) (string, error) {
		return "", nil
	}}
	_, err := NewClientWithRunner(runner).ListAccessPoints(context.Background(), false, "wlan0")
	if err != nil {
		t.Fatalf("ListAccessPoints: %v", err)
	}
	joined := strings.Join(runner.calls[0], " ")
	if !strings.Contains(joined, "device wifi list ifname wlan0 --rescan no") {
		t.Errorf("args = %q, want ifname and --rescan no", joined)
	}
}
