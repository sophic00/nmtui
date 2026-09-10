// Package config loads the optional key=value configuration file.
package config

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds user-tunable settings. Zero values mean "not set" so callers
// can apply their own defaults.
type Config struct {
	Interface         string
	PollEvery         time.Duration
	Sort              string
	SpeedtestServer   string
	SpeedtestDuration time.Duration
	SpeedtestStreams  int
	SpeedtestQuick    bool
}

// DefaultPath returns $XDG_CONFIG_HOME/nmt/config, falling back to
// ~/.config/nmt/config.
func DefaultPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "nmt", "config")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "nmt", "config")
}

// Load reads path. A missing file is not an error. Unknown keys and invalid
// values produce warnings and are ignored.
func Load(path string) (Config, []string, error) {
	var cfg Config
	if path == "" {
		return cfg, nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil, nil
		}
		return cfg, nil, err
	}
	defer f.Close()

	var warnings []string
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			warnings = append(warnings, fmt.Sprintf("%s:%d: expected key = value", path, lineNo))
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == "" {
			warnings = append(warnings, fmt.Sprintf("%s:%d: %s has no value", path, lineNo, key))
			continue
		}
		switch key {
		case "interface":
			cfg.Interface = value
		case "poll_interval":
			d, err := time.ParseDuration(value)
			if err != nil || d <= 0 {
				warnings = append(warnings, fmt.Sprintf("%s:%d: invalid poll_interval %q", path, lineNo, value))
				continue
			}
			cfg.PollEvery = d
		case "sort":
			switch value {
			case "signal", "name", "channel", "security":
				cfg.Sort = value
			default:
				warnings = append(warnings, fmt.Sprintf("%s:%d: invalid sort %q", path, lineNo, value))
			}
		case "speedtest_server":
			if u, err := url.Parse(value); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				warnings = append(warnings, fmt.Sprintf("%s:%d: invalid speedtest_server %q", path, lineNo, value))
				continue
			}
			cfg.SpeedtestServer = strings.TrimRight(value, "/")
		case "speedtest_duration":
			d, err := time.ParseDuration(value)
			if err != nil || d <= 0 {
				warnings = append(warnings, fmt.Sprintf("%s:%d: invalid speedtest_duration %q", path, lineNo, value))
				continue
			}
			cfg.SpeedtestDuration = d
		case "speedtest_streams":
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				warnings = append(warnings, fmt.Sprintf("%s:%d: invalid speedtest_streams %q", path, lineNo, value))
				continue
			}
			cfg.SpeedtestStreams = n
		case "speedtest_quick":
			b, err := strconv.ParseBool(value)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s:%d: invalid speedtest_quick %q", path, lineNo, value))
				continue
			}
			cfg.SpeedtestQuick = b
		default:
			warnings = append(warnings, fmt.Sprintf("%s:%d: unknown key %q", path, lineNo, key))
		}
	}
	if err := sc.Err(); err != nil {
		return cfg, warnings, err
	}
	return cfg, warnings, nil
}
