package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"nmtui/internal/nm"
	"nmtui/internal/speedtest"
)

type mode int

const (
	modeList mode = iota
	modePassword
	modeConfirm
	modeSpeedtest
)

type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmForget
	confirmDisconnect
)

type Model struct {
	aps     []nm.AccessPoint
	visible []nm.AccessPoint
	saved   []nm.SavedConnection
	status  nm.Status
	wifi    nm.WifiState

	table    table.Model
	spinner  spinner.Model
	pwdInput textinput.Model
	filter   textinput.Model

	mode          mode
	confirm       confirmKind
	busy          string
	info          string
	warnMsg       string
	errMsg        string
	filtering     bool
	connectSSID   string
	pendingForget nm.SavedConnection
	ifaceOverride string
	width         int
	height        int

	speedResult *speedtest.Result
	speedProg   speedtest.Progress
	speedActive bool
	speedSSID   string
	speedCh     chan speedtest.Progress
	speedCtx    context.Context
	speedCancel context.CancelFunc
	speedGen    int
}

func NewModelWithDevice(device string) Model {
	cols := []table.Column{
		{Title: "", Width: 2},
		{Title: "SIGNAL", Width: 6},
		{Title: "SSID", Width: 32},
		{Title: "SECURITY", Width: 18},
		{Title: "CHAN", Width: 4},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	pwd := textinput.New()
	pwd.EchoMode = textinput.EchoPassword
	pwd.Placeholder = "password"

	f := textinput.New()
	f.Placeholder = "type to filter..."
	f.Prompt = "/"

	return Model{
		ifaceOverride: device,
		table:         t,
		spinner:       spinner.New(spinner.WithSpinner(spinner.Dot)),
		pwdInput:      pwd,
		filter:        f,
		busy:          "scanning",
	}
}

func NewModel() Model {
	return NewModelWithDevice("")
}

type pollMsg time.Time

func pollCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return pollMsg(t)
	})
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(true), m.spinner.Tick, pollCmd())
}

type stateMsg struct {
	status nm.Status
	wifi   nm.WifiState
	err    error
}

type apsMsg struct {
	aps []nm.AccessPoint
	err error
}

type savedMsg struct {
	conns []nm.SavedConnection
	err   error
}

type actionMsg struct {
	info   string
	err    error
	rescan bool
}

func (m Model) refreshCmd(rescan bool) tea.Cmd {
	return tea.Batch(m.stateCmd(), apsCmd(rescan), savedCmd())
}

func (m Model) stateCmd() tea.Cmd {
	return func() tea.Msg {
		st, err := nm.GetStatus()
		if err != nil {
			return stateMsg{err: err}
		}
		w, err := nm.GetWifiState()
		if err != nil {
			return stateMsg{status: st, err: err}
		}
		if m.ifaceOverride != "" {
			w.Device = m.ifaceOverride
		}
		return stateMsg{status: st, wifi: w}
	}
}

func apsCmd(rescan bool) tea.Cmd {
	return func() tea.Msg {
		aps, err := nm.ListAccessPoints(rescan)
		return apsMsg{aps: aps, err: err}
	}
}

func savedCmd() tea.Cmd {
	return func() tea.Msg {
		conns, err := nm.ListSaved()
		return savedMsg{conns: conns, err: err}
	}
}

func toggleCmd(on bool) tea.Cmd {
	return func() tea.Msg {
		if err := nm.ToggleWifi(on); err != nil {
			return actionMsg{err: err}
		}
		if on {
			return actionMsg{info: "Wi-Fi enabled", rescan: true}
		}
		return actionMsg{info: "Wi-Fi disabled"}
	}
}

func connectCmd(ssid, password, device string) tea.Cmd {
	return func() tea.Msg {
		if err := nm.Connect(ssid, password, device); err != nil {
			return actionMsg{err: fmt.Errorf("connect to %q failed: %w", ssid, err)}
		}
		return actionMsg{info: "connected to " + ssid, rescan: true}
	}
}

func disconnectCmd(device string) tea.Cmd {
	return func() tea.Msg {
		if err := nm.Disconnect(device); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{info: "disconnected"}
	}
}

func forgetCmd(id, name string) tea.Cmd {
	return func() tea.Msg {
		if err := nm.Forget(id); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{info: "forgot " + name}
	}
}

type speedProgressMsg speedtest.Progress

type speedDoneMsg struct {
	result speedtest.Result
	err    error
	runID  int
}

// startSpeedtest enters speedtest mode and launches the time-based
// download/upload run plus a progress listener. Caller must have checked
// connection state and busy flag.
func (m *Model) startSpeedtest() tea.Cmd {
	if m.speedCancel != nil {
		m.speedCancel()
		m.speedCancel = nil
	}
	// 10s down + 10s up + ping overhead; give workers grace to exit.
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	m.speedCtx = ctx
	m.speedCancel = cancel
	m.speedGen++
	runID := m.speedGen
	m.mode = modeSpeedtest
	m.speedActive = true
	m.speedResult = nil
	m.speedProg = speedtest.Progress{}
	m.speedSSID = m.wifi.Active.Name
	m.speedCh = make(chan speedtest.Progress, 32)
	m.busy = "speed testing"
	m.setInfo("")
	ch := m.speedCh

	runCmd := func() tea.Msg {
		cfg := speedtest.DefaultConfig()
		res, err := speedtest.Run(ctx, cfg, func(p speedtest.Progress) {
			select {
			case ch <- p:
			case <-ctx.Done():
			default:
				// Drop ticks if the UI is behind rather than blocking workers.
				select {
				case ch <- p:
				default:
				}
			}
		})
		return speedDoneMsg{result: res, err: err, runID: runID}
	}
	return tea.Batch(runCmd, listenSpeedCmd(ctx, ch), m.spinner.Tick)
}

func listenSpeedCmd(ctx context.Context, ch chan speedtest.Progress) tea.Cmd {
	return func() tea.Msg {
		select {
		case p, ok := <-ch:
			if !ok {
				return nil
			}
			return speedProgressMsg(p)
		case <-ctx.Done():
			return nil
		}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case spinner.TickMsg:
		if m.busy != "" {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case pollMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, pollCmd())
		if m.busy == "" && m.mode == modeList && !m.speedActive {
			cmds = append(cmds, m.stateCmd())
		}
		return m, tea.Batch(cmds...)

	case stateMsg:
		if msg.err != nil {
			m.setErr(msg.err)
		} else {
			m.status = msg.status
			m.wifi = msg.wifi
		}
		return m, nil

	case apsMsg:
		if msg.err != nil {
			if m.status.WifiEnabled && !strings.Contains(msg.err.Error(), "No Wi-Fi device found") {
				m.setErr(msg.err)
			}
		} else {
			m.setAPs(msg.aps)
		}
		if m.busy == "scanning" {
			m.busy = ""
		}
		return m, nil

	case savedMsg:
		if msg.err != nil {
			m.setErr(msg.err)
		} else {
			m.saved = msg.conns
		}
		return m, nil

	case actionMsg:
		m.busy = ""
		if msg.err != nil {
			m.setErr(msg.err)
		} else {
			m.setInfo(msg.info)
		}
		return m, m.refreshCmd(msg.rescan)

	case speedProgressMsg:
		m.speedProg = speedtest.Progress(msg)
		if m.speedActive && m.speedCh != nil && m.speedCtx != nil {
			return m, listenSpeedCmd(m.speedCtx, m.speedCh)
		}
		return m, nil

	case speedDoneMsg:
		// Drop stale results from a previous run (e.g. esc then quick re-run).
		if msg.runID != 0 && msg.runID != m.speedGen {
			return m, nil
		}
		m.speedActive = false
		m.busy = ""
		if m.speedCancel != nil {
			m.speedCancel()
			m.speedCancel = nil
		}
		m.speedCtx = nil
		if msg.err != nil {
			// User cancelled via esc: mode already back to list, stay silent.
			if errors.Is(msg.err, context.Canceled) && m.mode != modeSpeedtest {
				return m, nil
			}
			m.setErr(msg.err)
			return m, nil
		}
		m.speedResult = &msg.result
		m.setInfo("")
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		if m.speedCancel != nil {
			m.speedCancel()
			m.speedCancel = nil
		}
		m.speedCtx = nil
		m.speedActive = false
		return m, tea.Quit
	}

	switch m.mode {
	case modePassword:
		return m.updatePassword(msg)
	case modeConfirm:
		return m.updateConfirm(msg)
	case modeSpeedtest:
		return m.updateSpeedtest(msg)
	default:
		return m.updateList(msg)
	}
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			m.filter.Blur()
			m.filter.SetValue("")
			m.applyFilter()
			m.layout()
			return m, nil
		case "enter":
			m.filtering = false
			m.filter.Blur()
			m.layout()
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.applyFilter()
		m.layout()
		return m, cmd
	}

	switch msg.String() {
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.applyFilter()
			m.layout()
			return m, nil
		}
		m.info = ""
		m.warnMsg = ""
		m.errMsg = ""
		return m, nil

	case "q":
		return m, tea.Quit

	case "/":
		m.filtering = true
		return m, m.filter.Focus()
	}

	if m.busy != "" {
		// Allow scrolling the table while busy, but block mutating actions
		switch msg.String() {
		case "r", "t", "d", "f", "s", "enter":
			return m, nil
		}
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "r":
		m.busy = "scanning"
		m.setInfo("")
		return m, tea.Batch(apsCmd(true), m.spinner.Tick)

	case "t":
		on := !m.status.WifiEnabled
		m.busy = "turning Wi-Fi " + onOffWord(on)
		return m, tea.Batch(toggleCmd(on), m.spinner.Tick)

	case "d":
		if m.wifi.Active.Name == "" {
			m.setWarn("no active connection")
			return m, nil
		}
		m.mode = modeConfirm
		m.confirm = confirmDisconnect
		return m, nil

	case "f":
		if len(m.visible) == 0 {
			m.setWarn("no network selected")
			return m, nil
		}
		ap := m.selectedAP()
		if ap.SSID == "" {
			m.setWarn("cannot forget: hidden network has no SSID")
			return m, nil
		}
		conn := m.savedFor(ap.SSID)
		if conn == nil {
			m.setWarn("no saved profile for " + ap.SSID)
			return m, nil
		}
		m.mode = modeConfirm
		m.confirm = confirmForget
		m.pendingForget = *conn
		return m, nil

	case "enter":
		if len(m.visible) == 0 {
			m.setWarn("no network selected")
			return m, nil
		}
		return m.startConnect(m.selectedAP())

	case "s":
		if m.wifi.Active.Name == "" {
			m.setWarn("not connected — join a network first")
			return m, nil
		}
		return m, m.startSpeedtest()
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) startConnect(ap nm.AccessPoint) (tea.Model, tea.Cmd) {
	if ap.SSID == "" {
		m.setWarn("cannot connect: hidden network")
		return m, nil
	}
	if ap.InUse {
		m.setWarn("already connected to " + ap.SSID)
		return m, nil
	}
	if m.savedFor(ap.SSID) != nil || nm.IsOpenSecurity(ap.Security) {
		m.busy = "connecting to " + ap.SSID
		m.setInfo("")
		return m, tea.Batch(connectCmd(ap.SSID, "", m.wifi.Device), m.spinner.Tick)
	}
	if strings.Contains(ap.Security, "802.1X") {
		m.setWarn("802.1X enterprise network requires a pre-configured profile")
		return m, nil
	}
	m.mode = modePassword
	m.connectSSID = ap.SSID
	m.pwdInput.Reset()
	return m, m.pwdInput.Focus()
}

func (m Model) updatePassword(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.pwdInput.Reset()
		return m, nil
	case "enter":
		pw := m.pwdInput.Value()
		ssid := m.connectSSID
		m.mode = modeList
		m.busy = "connecting to " + ssid
		m.setInfo("")
		return m, tea.Batch(connectCmd(ssid, pw, m.wifi.Device), m.spinner.Tick)
	}
	var cmd tea.Cmd
	m.pwdInput, cmd = m.pwdInput.Update(msg)
	return m, cmd
}

func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.mode = modeList
		switch m.confirm {
		case confirmForget:
			id, name := m.pendingForget.UUID, m.pendingForget.Name
			m.busy = "forgetting " + name
			m.setInfo("")
			return m, tea.Batch(forgetCmd(id, name), m.spinner.Tick)
		case confirmDisconnect:
			name := m.wifi.Active.Name
			m.busy = "disconnecting from " + name
			m.setInfo("")
			return m, tea.Batch(disconnectCmd(m.wifi.Device), m.spinner.Tick)
		}
		return m, nil
	case "n", "N", "esc":
		m.mode = modeList
		m.confirm = confirmNone
		return m, nil
	}
	return m, nil
}

func (m Model) updateSpeedtest(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.speedCancel != nil {
			m.speedCancel()
			m.speedCancel = nil
		}
		m.speedCtx = nil
		m.speedActive = false
		m.busy = ""
		m.mode = modeList
		return m, nil
	case "q":
		if m.speedCancel != nil {
			m.speedCancel()
			m.speedCancel = nil
		}
		m.speedCtx = nil
		m.speedActive = false
		return m, tea.Quit
	case "r":
		if m.speedActive {
			return m, nil
		}
		if m.wifi.Active.Name == "" {
			m.setWarn("not connected — join a network first")
			m.mode = modeList
			return m, nil
		}
		return m, m.startSpeedtest()
	}
	return m, nil
}

func (m *Model) setAPs(aps []nm.AccessPoint) {
	m.aps = aps
	m.applyFilter()
}

func (m *Model) applyFilter() {
	var selectedSSID string
	if cur := m.table.Cursor(); cur >= 0 && cur < len(m.visible) {
		selectedSSID = m.visible[cur].SSID
	}

	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.visible = make([]nm.AccessPoint, 0, len(m.aps))
	for _, ap := range m.aps {
		if q == "" || strings.Contains(strings.ToLower(ap.SSID), q) {
			m.visible = append(m.visible, ap)
		}
	}

	rows := make([]table.Row, 0, len(m.visible))
	for _, ap := range m.visible {
		name := sanitizeSSID(ap.SSID)
		if name == "" {
			name = "(hidden network)"
		}
		marker := ""
		if ap.InUse {
			marker = "*"
		}
		rows = append(rows, table.Row{
			marker,
			signalBars(ap.Signal),
			name,
			securityLabel(ap.Security),
			ap.Chan,
		})
	}
	m.table.SetRows(rows)

	if selectedSSID != "" {
		for i, ap := range m.visible {
			if ap.SSID == selectedSSID {
				m.table.SetCursor(i)
				return
			}
		}
	}

	if cur := m.table.Cursor(); cur >= len(rows) {
		if len(rows) == 0 {
			m.table.SetCursor(0)
		} else {
			m.table.SetCursor(len(rows) - 1)
		}
	}
}

func (m *Model) layout() {
	if m.width == 0 {
		return
	}

	ssidWidth := m.width - 42
	if ssidWidth < 16 {
		ssidWidth = 16
	} else if ssidWidth > 64 {
		ssidWidth = 64
	}

	cols := []table.Column{
		{Title: "", Width: 2},
		{Title: "SIGNAL", Width: 6},
		{Title: "SSID", Width: ssidWidth},
		{Title: "SECURITY", Width: 16},
		{Title: "CHAN", Width: 4},
	}
	m.table.SetColumns(cols)
	m.table.SetWidth(m.width)

	reserved := 8
	if m.filtering {
		reserved += 2
	}
	if m.busy != "" {
		reserved += 2
	}
	if m.errMsg != "" || m.warnMsg != "" || m.info != "" {
		reserved++
	}
	// The help bar may wrap to multiple lines on narrow terminals.
	reserved += len(helpLines(m.width)) - 1

	h := m.height - reserved
	if h < 3 {
		h = 3
	}
	m.table.SetHeight(h)
}

func (m Model) selectedAP() nm.AccessPoint {
	i := m.table.Cursor()
	if i >= 0 && i < len(m.visible) {
		return m.visible[i]
	}
	return nm.AccessPoint{}
}

func (m Model) savedFor(ssid string) *nm.SavedConnection {
	if ssid == "" {
		return nil
	}
	for i := range m.saved {
		if (m.saved[i].Type == "" || m.saved[i].Type == "802-11-wireless") && m.saved[i].Name == ssid {
			return &m.saved[i]
		}
	}
	return nil
}

func (m *Model) setInfo(s string) {
	m.info = s
	m.warnMsg = ""
	m.errMsg = ""
}

func (m *Model) setWarn(s string) {
	m.warnMsg = s
	m.info = ""
	m.errMsg = ""
}

func (m *Model) setErr(err error) {
	m.errMsg = err.Error()
	m.info = ""
	m.warnMsg = ""
}

func (m Model) View() string {
	var b strings.Builder

	wifiTxt := offStyle.Render("off")
	if m.status.WifiEnabled {
		wifiTxt = onStyle.Render("on")
	}
	header := titleStyle.Render("nmtui") +
		"  " + dimStyle.Render("Wi-Fi:") + " " + wifiTxt
	if m.wifi.Active.Name != "" {
		conn := sanitizeSSID(m.wifi.Active.Name)
		if m.wifi.IP != "" {
			conn += " (" + m.wifi.IP + ")"
		}
		header += "  " + dimStyle.Render("connected:") + " " + okStyle.Render(conn)
	}
	b.WriteString(header + "\n\n")

	switch m.mode {
	case modePassword:
		body := "Password for " + boldStyle.Render(sanitizeSSID(m.connectSSID)) +
			"\n\n" + m.pwdInput.View() +
			"\n\n" + dimStyle.Render("enter: connect  ·  esc: cancel")
		b.WriteString(boxStyle.Render(body))

	case modeConfirm:
		var q string
		switch m.confirm {
		case confirmForget:
			q = "Forget saved network " + boldStyle.Render(sanitizeSSID(m.pendingForget.Name)) + "?"
		case confirmDisconnect:
			q = "Disconnect from " + boldStyle.Render(sanitizeSSID(m.wifi.Active.Name)) + "?"
		default:
			q = "Are you sure?"
		}
		body := q + "\n\n" + dimStyle.Render("y: yes  ·  n/esc: no")
		b.WriteString(boxStyle.Render(body))

	case modeSpeedtest:
		b.WriteString(m.speedView())

	default:
		switch {
		case !m.status.WifiEnabled:
			b.WriteString(dimStyle.Render("Wi-Fi is disabled — press t to enable") + "\n")
		case len(m.visible) == 0:
			b.WriteString(dimStyle.Render("no networks found — press r to scan") + "\n")
		default:
			b.WriteString(m.table.View() + "\n")
		}
		if m.filtering {
			b.WriteString("\n" + m.filter.View())
		}
	}

	b.WriteString("\n")
	switch {
	case m.errMsg != "":
		b.WriteString(errStyle.Render("✗ " + m.errMsg))
	case m.warnMsg != "":
		b.WriteString(warnStyle.Render("! " + m.warnMsg))
	case m.info != "":
		b.WriteString(okStyle.Render("✓ " + m.info))
	}
	if m.busy != "" && m.mode != modeSpeedtest {
		b.WriteString("\n" + m.spinner.View() + " " + m.busy + "...")
	}

	if m.mode == modeList {
		b.WriteString("\n\n" + helpView(m.width))
	}
	return b.String()
}

func signalBars(signal int) string {
	switch {
	case signal >= 80:
		return "▂▄▆█"
	case signal >= 55:
		return "▂▄▆ "
	case signal >= 30:
		return "▂▄  "
	case signal > 0:
		return "▂   "
	default:
		return ""
	}
}

func securityLabel(security string) string {
	if nm.IsOpenSecurity(security) {
		return "open"
	}
	return security
}

func onOffWord(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func sanitizeSSID(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			b.WriteRune('?')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (m Model) speedView() string {
	ssid := sanitizeSSID(m.speedSSID)
	if ssid == "" {
		ssid = sanitizeSSID(m.wifi.Active.Name)
	}
	if ssid == "" {
		ssid = "(unknown)"
	}
	cfg := speedtest.DefaultConfig()
	title := "Speed test — " + boldStyle.Render(ssid)
	sub := dimStyle.Render("server: " + cfg.BaseURL + "  ·  10s down + 10s up (4 streams)")
	var body strings.Builder
	body.WriteString(title + "\n" + sub + "\n\n")

	if m.speedResult != nil {
		r := m.speedResult
		fmt.Fprintf(&body, "  ↓ %s\n", okStyle.Render(speedtest.FormatMbps(r.DownloadMbps)+" down"))
		fmt.Fprintf(&body, "  ↑ %s\n", okStyle.Render(speedtest.FormatMbps(r.UploadMbps)+" up"))
		fmt.Fprintf(&body, "  ping %.0f ms", r.LatencyMs)
		if r.JitterMs > 0 {
			fmt.Fprintf(&body, "  ·  jitter %.1f ms", r.JitterMs)
		}
		body.WriteString("\n\n" + dimStyle.Render("r: re-run  ·  esc: close"))
		return boxStyle.Render(body.String())
	}

	if m.errMsg != "" {
		// Error is rendered below the box by View; keep panel contextual.
		body.WriteString(dimStyle.Render("test failed — see error below") + "\n\n")
		body.WriteString(dimStyle.Render("r: retry  ·  esc: close"))
		return boxStyle.Render(body.String())
	}

	if !m.speedActive {
		body.WriteString(dimStyle.Render("preparing…") + "\n\n")
		body.WriteString(dimStyle.Render("esc: cancel"))
		return boxStyle.Render(body.String())
	}

	phase := m.speedProg.Phase
	if phase == "" {
		body.WriteString(m.spinner.View() + " measuring ping…\n\n")
		body.WriteString(dimStyle.Render("~20s total · esc: cancel"))
		return boxStyle.Render(body.String())
	}
	var expected time.Duration
	var label string
	switch phase {
	case speedtest.PhaseDownload:
		expected, label = cfg.DownloadFor, "download"
	case speedtest.PhaseUpload:
		expected, label = cfg.UploadFor, "upload"
	default:
		expected, label = cfg.DownloadFor, phase
	}
	pct := 0.0
	if expected > 0 {
		pct = float64(m.speedProg.Elapsed) / float64(expected)
		if pct < 0 {
			pct = 0
		}
		if pct > 1 {
			pct = 1
		}
	}
	fmt.Fprintf(&body, "%s %s  ·  %s\n", m.spinner.View(), label,
		boldStyle.Render(speedtest.FormatMbps(m.speedProg.InstantMbps)))
	body.WriteString("  " + progressBar(pct, m.speedBarWidth()) + "\n")
	fmt.Fprintf(&body, "  %s\n\n", dimStyle.Render(fmt.Sprintf("%.0fs / %.0fs", m.speedProg.Elapsed.Seconds(), expected.Seconds())))
	body.WriteString(dimStyle.Render("esc: cancel"))
	return boxStyle.Render(body.String())
}

func (m Model) speedBarWidth() int {
	// Scale the progress bar with terminal width so the panel visibly
	// adapts on resize; fall back to 30 before the first WindowSizeMsg.
	if m.width <= 0 {
		return 30
	}
	w := m.width - 24
	if w < 10 {
		return 10
	}
	if w > 50 {
		return 50
	}
	return w
}

func progressBar(pct float64, width int) string {
	if width <= 0 {
		width = 20
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(pct*float64(width) + 0.5)
	var b strings.Builder
	for i := 0; i < width; i++ {
		if i < filled {
			b.WriteString("█")
		} else {
			b.WriteString("░")
		}
	}
	return b.String()
}
