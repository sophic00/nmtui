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

func TestConnectSendsPasswordOnStdin(t *testing.T) {
	runner := &fakeRunner{}
	err := NewClientWithRunner(runner).Connect(context.Background(), "HomeNet", "s3cret", "wlan0")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	want := []string{"--ask", "--wait", connectWait, "device", "wifi", "connect", "HomeNet", "ifname", "wlan0"}
	if !reflect.DeepEqual(runner.calls[0], want) {
		t.Errorf("args = %q, want %q", runner.calls[0], want)
	}
	if runner.stdins[0] != "s3cret\n" {
		t.Errorf("stdin = %q, want password followed by newline", runner.stdins[0])
	}
}

func TestConnectOpenNetwork(t *testing.T) {
	runner := &fakeRunner{}
	if err := NewClientWithRunner(runner).Connect(context.Background(), "Open", "", ""); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	want := []string{"--wait", connectWait, "device", "wifi", "connect", "Open"}
	if !reflect.DeepEqual(runner.calls[0], want) {
		t.Errorf("args = %q, want %q", runner.calls[0], want)
	}
	if runner.stdins[0] != "" {
		t.Errorf("stdin = %q, want empty for open networks", runner.stdins[0])
	}
}
