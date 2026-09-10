package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFull(t *testing.T) {
	path := writeConfig(t, `
# nmt config
interface = wlan0
poll_interval = 2s
sort = channel

speedtest_server = https://example.com/
speedtest_duration = 8s
speedtest_streams = 6
speedtest_quick = true
`)

	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if cfg.Interface != "wlan0" {
		t.Errorf("Interface = %q", cfg.Interface)
	}
	if cfg.PollEvery != 2*time.Second {
		t.Errorf("PollEvery = %v", cfg.PollEvery)
	}
	if cfg.Sort != "channel" {
		t.Errorf("Sort = %q", cfg.Sort)
	}
	if cfg.SpeedtestServer != "https://example.com" {
		t.Errorf("SpeedtestServer = %q", cfg.SpeedtestServer)
	}
	if cfg.SpeedtestDuration != 8*time.Second {
		t.Errorf("SpeedtestDuration = %v", cfg.SpeedtestDuration)
	}
	if cfg.SpeedtestStreams != 6 {
		t.Errorf("SpeedtestStreams = %d", cfg.SpeedtestStreams)
	}
	if !cfg.SpeedtestQuick {
		t.Error("SpeedtestQuick = false, want true")
	}
}

func TestLoadWarnings(t *testing.T) {
	path := writeConfig(t, `nonsense
unknown_key = 1
poll_interval = fast
sort = sideways
speedtest_server = ftp://example.com
speedtest_duration = soon
speedtest_streams = many
speedtest_quick = perhaps
empty =
`)
	cfg, warnings, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 9 {
		t.Errorf("got %d warnings, want 9: %v", len(warnings), warnings)
	}
	if cfg != (Config{}) {
		t.Errorf("invalid keys should not populate config, got %+v", cfg)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, warnings, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if cfg != (Config{}) {
		t.Errorf("config = %+v, want zero", cfg)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	if got := DefaultPath(); got != "/tmp/xdg-test/nmt/config" {
		t.Errorf("DefaultPath = %q, want /tmp/xdg-test/nmt/config", got)
	}
}
