package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"nmtui/internal/config"
	"nmtui/internal/nm"
	"nmtui/internal/speedtest"
	"nmtui/internal/ui"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "status":
			return runStatus(args[1:], nm.NewClient(), stdout, stderr)
		case "list":
			return runList(args[1:], nm.NewClient(), stdout, stderr)
		case "speedtest":
			return runSpeedtest(args[1:], stdout, stderr)
		case "help":
			usage(stdout)
			return 0
		case "version":
			fmt.Fprintf(stdout, "nmt %s\n", version)
			return 0
		}
	}
	return runTUI(args, stdout, stderr)
}

func usage(w io.Writer) {
	fmt.Fprint(w, `nmt - terminal UI for managing Wi-Fi with NetworkManager

Usage:
  nmt [flags]                                   run the TUI
  nmt status [--json]                           print Wi-Fi status
  nmt list [--json] [--rescan] [--interface N]  list nearby networks
  nmt speedtest [--json] [--quick] [--server U] [--duration D] [--streams N]
  nmt version
  nmt help

TUI flags:
  -i, --interface <name>  Wi-Fi interface to manage (e.g. wlan0)
  -v, --version           Print version information and exit
  -h, --help              Show help information and exit

Configuration: $XDG_CONFIG_HOME/nmt/config (key = value). Set NMT_CONFIG to
use another path. Environment: NMT_INTERFACE, NMT_POLL_INTERVAL,
NMT_SPEEDTEST_SERVER.
`)
}

func runTUI(args []string, stdout, stderr io.Writer) int {
	cfg, warnings := loadConfig(stderr)

	fs := flag.NewFlagSet("nmt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		showVersion bool
		showHelp    bool
		iface       = firstNonEmpty(os.Getenv("NMT_INTERFACE"), cfg.Interface)
	)
	fs.BoolVar(&showVersion, "v", false, "print version and exit")
	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	fs.BoolVar(&showHelp, "h", false, "show help and exit")
	fs.BoolVar(&showHelp, "help", false, "show help and exit")
	fs.StringVar(&iface, "i", iface, "Wi-Fi interface to use (e.g. wlan0)")
	fs.StringVar(&iface, "interface", iface, "Wi-Fi interface to use (e.g. wlan0)")
	fs.Usage = func() { usage(stderr) }

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if showHelp {
		usage(stdout)
		return 0
	}
	if showVersion {
		fmt.Fprintf(stdout, "nmt %s\n", version)
		return 0
	}

	if _, err := exec.LookPath("nmcli"); err != nil {
		fmt.Fprintln(stderr, "nmt requires nmcli (install the 'networkmanager' package)")
		return 1
	}

	pollEvery := cfg.PollEvery
	if env := os.Getenv("NMT_POLL_INTERVAL"); env != "" {
		if d, err := time.ParseDuration(env); err == nil && d > 0 {
			pollEvery = d
		} else {
			warnings = append(warnings, "invalid NMT_POLL_INTERVAL "+env)
		}
	}

	server := firstNonEmpty(os.Getenv("NMT_SPEEDTEST_SERVER"), cfg.SpeedtestServer)
	full := speedtest.DefaultConfig()
	quick := speedtest.QuickConfig()
	if server != "" {
		full.BaseURL = server
		quick.BaseURL = server
	}
	if cfg.SpeedtestDuration > 0 {
		full.DownloadFor = cfg.SpeedtestDuration
		full.UploadFor = cfg.SpeedtestDuration
	}
	if cfg.SpeedtestStreams > 0 {
		full.Streams = cfg.SpeedtestStreams
	}

	model := ui.NewModelWithOptions(ui.Options{
		Device:         iface,
		PollEvery:      pollEvery,
		Sort:           cfg.Sort,
		SpeedtestFull:  full,
		SpeedtestQuick: quick,
		DefaultQuick:   cfg.SpeedtestQuick,
		Warnings:       warnings,
	})

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func loadConfig(stderr io.Writer) (config.Config, []string) {
	path := firstNonEmpty(os.Getenv("NMT_CONFIG"), config.DefaultPath())
	cfg, warnings, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "warning: config: %v\n", err)
	}
	return cfg, warnings
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

type statusOutput struct {
	WifiEnabled  bool   `json:"wifi_enabled"`
	State        string `json:"state,omitempty"`
	Connectivity string `json:"connectivity,omitempty"`
	Device       string `json:"device,omitempty"`
	Connection   string `json:"connection,omitempty"`
	IP           string `json:"ip,omitempty"`
}

func runStatus(args []string, client *nm.Client, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	st, err := client.GetStatus(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	out := statusOutput{
		WifiEnabled:  st.WifiEnabled,
		State:        st.State,
		Connectivity: st.Connectivity,
	}
	if w, err := client.GetWifiState(ctx, ""); err == nil {
		out.Device = w.Device
		out.Connection = w.Active.Name
		out.IP = w.IP
	}

	if *jsonOut {
		return writeJSON(stdout, stderr, out)
	}
	fmt.Fprintf(stdout, "wifi: %s\n", onOff(st.WifiEnabled))
	if out.Device != "" {
		fmt.Fprintf(stdout, "device: %s\n", out.Device)
	}
	if out.Connection != "" {
		fmt.Fprintf(stdout, "connection: %s\n", out.Connection)
	}
	if out.IP != "" {
		fmt.Fprintf(stdout, "ip: %s\n", out.IP)
	}
	fmt.Fprintf(stdout, "state: %s\nconnectivity: %s\n", orDash(out.State), orDash(out.Connectivity))
	return 0
}

type accessPointOutput struct {
	SSID      string `json:"ssid"`
	BSSID     string `json:"bssid,omitempty"`
	Signal    int    `json:"signal"`
	Security  string `json:"security"`
	Channel   string `json:"channel,omitempty"`
	Frequency string `json:"frequency,omitempty"`
	Rate      string `json:"rate,omitempty"`
	InUse     bool   `json:"in_use"`
}

func runList(args []string, client *nm.Client, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	rescan := fs.Bool("rescan", false, "force a scan")
	iface := fs.String("interface", "", "Wi-Fi interface")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	aps, err := client.ListAccessPoints(ctx, *rescan, *iface)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	if *jsonOut {
		out := make([]accessPointOutput, 0, len(aps))
		for _, ap := range aps {
			out = append(out, accessPointOutput{
				SSID:      ap.SSID,
				BSSID:     ap.BSSID,
				Signal:    ap.Signal,
				Security:  ap.Security,
				Channel:   ap.Chan,
				Frequency: ap.Freq,
				Rate:      ap.Rate,
				InUse:     ap.InUse,
			})
		}
		return writeJSON(stdout, stderr, out)
	}

	if len(aps) == 0 {
		fmt.Fprintln(stdout, "no networks found")
		return 0
	}
	for _, ap := range aps {
		marker := " "
		if ap.InUse {
			marker = "*"
		}
		name := ap.SSID
		if name == "" {
			name = "(hidden)"
		}
		security := ap.Security
		if nm.IsOpenSecurity(security) {
			security = "open"
		}
		fmt.Fprintf(stdout, "%s %3d%%  %-24s %-14s %s\n", marker, ap.Signal, name, security, ap.Chan)
	}
	return 0
}

type speedtestOutput struct {
	DownloadMbps float64 `json:"download_mbps"`
	UploadMbps   float64 `json:"upload_mbps"`
	LatencyMs    float64 `json:"latency_ms"`
	JitterMs     float64 `json:"jitter_ms"`
	Server       string  `json:"server"`
}

func runSpeedtest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("speedtest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "print JSON")
	quick := fs.Bool("quick", false, "5s down + 5s up, 2 streams")
	server := fs.String("server", "", "base URL of the speed test server")
	duration := fs.Duration("duration", 0, "duration of each phase")
	streams := fs.Int("streams", 0, "parallel streams")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg := speedtest.DefaultConfig()
	if *quick {
		cfg = speedtest.QuickConfig()
	}
	if *server != "" {
		cfg.BaseURL = strings.TrimRight(*server, "/")
	}
	if *duration > 0 {
		cfg.DownloadFor = *duration
		cfg.UploadFor = *duration
	}
	if *streams > 0 {
		cfg.Streams = *streams
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.DownloadFor+cfg.UploadFor+30*time.Second)
	defer cancel()

	var report func(speedtest.Progress)
	if !*jsonOut {
		report = func(p speedtest.Progress) {
			fmt.Fprintf(stderr, "\r%-8s %s", p.Phase, speedtest.FormatMbps(p.InstantMbps))
		}
	}
	res, err := speedtest.Run(ctx, cfg, report)
	if !*jsonOut {
		fmt.Fprintln(stderr)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	if *jsonOut {
		return writeJSON(stdout, stderr, speedtestOutput{
			DownloadMbps: res.DownloadMbps,
			UploadMbps:   res.UploadMbps,
			LatencyMs:    res.LatencyMs,
			JitterMs:     res.JitterMs,
			Server:       res.Server,
		})
	}
	fmt.Fprintf(stdout, "download: %s\n", speedtest.FormatMbps(res.DownloadMbps))
	fmt.Fprintf(stdout, "upload:   %s\n", speedtest.FormatMbps(res.UploadMbps))
	fmt.Fprintf(stdout, "ping:     %.0f ms\n", res.LatencyMs)
	if res.JitterMs > 0 {
		fmt.Fprintf(stdout, "jitter:   %.1f ms\n", res.JitterMs)
	}
	return 0
}

func writeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
