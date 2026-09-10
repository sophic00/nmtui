package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"nmtui/internal/nm"
)

type fakeRunner struct {
	fn func(args []string) (string, error)
}

func (f fakeRunner) Run(_ context.Context, _ time.Duration, _ string, args ...string) (string, error) {
	return f.fn(args)
}

func TestRunVersionAndHelp(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"version"}, &out, &errBuf); code != 0 {
		t.Fatalf("version exit = %d", code)
	}
	if !strings.HasPrefix(out.String(), "nmt ") {
		t.Errorf("version output = %q", out.String())
	}

	out.Reset()
	if code := run([]string{"help"}, &out, &errBuf); code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	for _, want := range []string{"status", "list", "speedtest"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestRunStatusJSON(t *testing.T) {
	runner := fakeRunner{fn: func(args []string) (string, error) {
		switch strings.Join(args, " ") {
		case "-t -f STATE,CONNECTIVITY,WIFI general status":
			return "connected:full:enabled\n", nil
		case "-t -f DEVICE,TYPE device status":
			return "wlan0:wifi\n", nil
		case "-t -f GENERAL.DEVICE,GENERAL.TYPE,GENERAL.STATE,GENERAL.CONNECTION,GENERAL.CON-UUID,IP4.ADDRESS device show wlan0":
			return strings.Join([]string{
				"GENERAL.DEVICE:wlan0",
				"GENERAL.TYPE:wifi",
				"GENERAL.CONNECTION:cube",
				"IP4.ADDRESS[1]:10.0.0.2/24",
			}, "\n"), nil
		}
		return "", fmt.Errorf("unexpected args: %q", args)
	}}

	client := nm.NewClientWithRunner(runner)
	var out, errBuf bytes.Buffer
	if code := runStatus([]string{"--json"}, client, &out, &errBuf); code != 0 {
		t.Fatalf("status exit = %d, stderr=%s", code, errBuf.String())
	}
	var got statusOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", out.String(), err)
	}
	if !got.WifiEnabled || got.Connectivity != "full" || got.Device != "wlan0" ||
		got.Connection != "cube" || got.IP != "10.0.0.2/24" {
		t.Errorf("status output = %+v", got)
	}
}

func TestRunListJSON(t *testing.T) {
	runner := fakeRunner{fn: func(args []string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "device wifi list") {
			return `*:46\:DA\:2A\:D9\:C7\:77:Infra:36:5180 MHz:1170 Mbit/s:79:WPA2:cube` + "\n", nil
		}
		return "", fmt.Errorf("unexpected args: %q", args)
	}}

	client := nm.NewClientWithRunner(runner)
	var out, errBuf bytes.Buffer
	if code := runList([]string{"--json"}, client, &out, &errBuf); code != 0 {
		t.Fatalf("list exit = %d, stderr=%s", code, errBuf.String())
	}
	var got []accessPointOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", out.String(), err)
	}
	if len(got) != 1 || got[0].SSID != "cube" || got[0].Signal != 79 || !got[0].InUse {
		t.Errorf("list output = %+v", got)
	}
}
