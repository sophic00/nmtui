package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"nmtui/internal/nm"
	"nmtui/internal/speedtest"
)

type mode int

const (
	modeList mode = iota
	modePassword
	modeConfirm
	modeSpeedtest
	modeDetails
	modeSaved
	modeHiddenSSID
)

type pwdPurpose int

const (
	pwdConnect pwdPurpose = iota
	pwdEditPassword
	pwdHidden
)

type sortMode int

const (
	sortSignal sortMode = iota
	sortName
	sortChannel
	sortSecurity
)

func (s sortMode) String() string {
	switch s {
	case sortName:
		return "name"
	case sortChannel:
		return "channel"
	case sortSecurity:
		return "security"
	default:
		return "signal"
	}
}

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

	client       *nm.Client
	baseCtx      context.Context
	baseCancel   context.CancelFunc
	actionCancel context.CancelFunc

	// applied tracks the newest request sequence applied per async stream, so
	// responses from superseded requests cannot clobber newer state.
	stateApplied int
	apsApplied   int
	savedApplied int

	table       table.Model
	savedTable  table.Model
	spinner     spinner.Model
	pwdInput    textinput.Model
	hiddenInput textinput.Model
	filter      textinput.Model

	mode          mode
	confirm       confirmKind
	confirmReturn mode
	pwdPurpose    pwdPurpose
	busy          string
	info          string
	warnMsg       string
	errMsg        string
	filtering     bool
	connectSSID   string
	editConn      nm.SavedConnection
	detailAP      nm.AccessPoint
	sort          sortMode
	pendingForget nm.SavedConnection
	ifaceOverride string
	width         int
	height        int

	speedResult *speedtest.Result
	speedCfg    speedtest.Config
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

	saved := table.New(
		table.WithColumns([]table.Column{
			{Title: "NAME", Width: 24},
			{Title: "SSID", Width: 28},
			{Title: "AUTOCONNECT", Width: 12},
		}),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	pwd := textinput.New()
	pwd.EchoMode = textinput.EchoPassword
	pwd.Placeholder = "password"

	hidden := textinput.New()
	hidden.Placeholder = "network name (SSID)"

	f := textinput.New()
	f.Placeholder = "type to filter..."
	f.Prompt = "/"

	ctx, cancel := context.WithCancel(context.Background())

	return Model{
		ifaceOverride: device,
		client:        nm.NewClient(),
		baseCtx:       ctx,
		baseCancel:    cancel,
		table:         t,
		savedTable:    saved,
		spinner:       spinner.New(spinner.WithSpinner(spinner.Dot)),
		pwdInput:      pwd,
		hiddenInput:   hidden,
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

// requestSeq numbers asynchronous nmcli requests across the program so stale
// responses (e.g. a cached scan arriving after a fresh rescan) are dropped
// instead of overwriting newer state.
var requestSeq atomic.Int64

func nextRequestSeq() int { return int(requestSeq.Add(1)) }

func (m Model) Init() tea.Cmd {
	// Paint the list from NetworkManager's cached scan results immediately,
	// then refresh it with a full rescan in the background so the UI is
	// interactive right away instead of waiting out the scan.
	return tea.Batch(
		m.stateCmd(),
		m.savedCmd(),
		m.apsCachedCmd(),
		m.apsCmd(true),
		m.spinner.Tick,
		pollCmd(),
	)
}

type stateMsg struct {
	status nm.Status
	wifi   nm.WifiState
	err    error
	seq    int
}

type apsMsg struct {
	aps []nm.AccessPoint
	err error
	// initial marks the startup cached-list load. The full rescan is still
	// in flight, so busy must stay set until that lands.
	initial bool
	seq     int
}

type savedMsg struct {
	conns []nm.SavedConnection
	err   error
	seq   int
}

type actionMsg struct {
	info   string
	err    error
	rescan bool
}

func (m Model) refreshCmd(rescan bool) tea.Cmd {
	return tea.Batch(m.stateCmd(), m.apsCmd(rescan), m.savedCmd())
}

func (m Model) stateCmd() tea.Cmd {
	client, ctx, iface := m.client, m.baseCtx, m.ifaceOverride
	seq := nextRequestSeq()
	return func() tea.Msg {
		st, err := client.GetStatus(ctx)
		if err != nil {
			return stateMsg{err: err, seq: seq}
		}
		w, err := client.GetWifiState(ctx, iface)
		if err != nil {
			return stateMsg{status: st, err: err, seq: seq}
		}
		return stateMsg{status: st, wifi: w, seq: seq}
	}
}

func (m Model) apsCmd(rescan bool) tea.Cmd {
	client, ctx, iface := m.client, m.baseCtx, m.ifaceOverride
	seq := nextRequestSeq()
	return func() tea.Msg {
		aps, err := client.ListAccessPoints(ctx, rescan, iface)
		return apsMsg{aps: aps, err: err, seq: seq}
	}
}

// apsCachedCmd lists access points without forcing a rescan, so it returns
// NetworkManager's cached results almost instantly. Used for the initial
// paint while the full rescan runs in the background.
func (m Model) apsCachedCmd() tea.Cmd {
	client, ctx, iface := m.client, m.baseCtx, m.ifaceOverride
	seq := nextRequestSeq()
	return func() tea.Msg {
		aps, err := client.ListAccessPoints(ctx, false, iface)
		return apsMsg{aps: aps, err: err, initial: true, seq: seq}
	}
}

func (m Model) savedCmd() tea.Cmd {
	client, ctx := m.client, m.baseCtx
	seq := nextRequestSeq()
	return func() tea.Msg {
		conns, err := client.ListSaved(ctx)
		return savedMsg{conns: conns, err: err, seq: seq}
	}
}

func (m Model) toggleCmd(ctx context.Context, on bool) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		if err := client.ToggleWifi(ctx, on); err != nil {
			return actionMsg{err: err}
		}
		if on {
			return actionMsg{info: "Wi-Fi enabled", rescan: true}
		}
		return actionMsg{info: "Wi-Fi disabled"}
	}
}

func (m Model) connectCmd(ctx context.Context, ssid, password, device string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		if err := client.Connect(ctx, ssid, password, device); err != nil {
			return actionMsg{err: fmt.Errorf("connect to %q failed: %w", ssid, err)}
		}
		return actionMsg{info: "connected to " + ssid, rescan: true}
	}
}

func (m Model) connectHiddenCmd(ctx context.Context, ssid, password, device string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		if err := client.ConnectHidden(ctx, ssid, password, device); err != nil {
			return actionMsg{err: fmt.Errorf("connect to %q failed: %w", ssid, err)}
		}
		return actionMsg{info: "connected to " + ssid, rescan: true}
	}
}

func (m Model) disconnectCmd(ctx context.Context, device string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		if err := client.Disconnect(ctx, device); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{info: "disconnected"}
	}
}

func (m Model) forgetCmd(ctx context.Context, id, name string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		if err := client.Forget(ctx, id); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{info: "forgot " + name}
	}
}

func (m Model) activateSavedCmd(ctx context.Context, uuid, name string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		if err := client.ActivateConnection(ctx, uuid); err != nil {
			return actionMsg{err: fmt.Errorf("connect to %q failed: %w", name, err)}
		}
		return actionMsg{info: "connected to " + name, rescan: true}
	}
}

func (m Model) autoconnectCmd(ctx context.Context, uuid, name string, enable bool) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		if err := client.SetAutoconnect(ctx, uuid, enable); err != nil {
			return actionMsg{err: err}
		}
		state := "disabled"
		if enable {
			state = "enabled"
		}
		return actionMsg{info: "autoconnect " + state + " for " + name}
	}
}

func (m Model) modifyPasswordCmd(ctx context.Context, uuid, name, password string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		if err := client.ModifyPassword(ctx, uuid, password); err != nil {
			return actionMsg{err: fmt.Errorf("update password for %q failed: %w", name, err)}
		}
		return actionMsg{info: "password updated for " + name}
	}
}

// beginAction returns a context for a mutating nmcli call and records its
// cancel func so esc or quit can interrupt the operation.
func (m *Model) beginAction() context.Context {
	if m.actionCancel != nil {
		m.actionCancel()
	}
	base := m.baseCtx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	m.actionCancel = cancel
	return ctx
}

// shutdown cancels every background operation started by the model.
func (m *Model) shutdown() {
	if m.speedCancel != nil {
		m.speedCancel()
		m.speedCancel = nil
	}
	if m.actionCancel != nil {
		m.actionCancel()
		m.actionCancel = nil
	}
	if m.baseCancel != nil {
		m.baseCancel()
		m.baseCancel = nil
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
func (m *Model) startSpeedtest(cfg speedtest.Config) tea.Cmd {
	if cfg.DownloadFor <= 0 || cfg.UploadFor <= 0 {
		cfg = speedtest.DefaultConfig()
	}
	if m.speedCancel != nil {
		m.speedCancel()
		m.speedCancel = nil
	}
	// Phase windows plus ping and worker grace.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.DownloadFor+cfg.UploadFor+20*time.Second)
	m.speedCtx = ctx
	m.speedCancel = cancel
	m.speedGen++
	runID := m.speedGen
	m.mode = modeSpeedtest
	m.speedActive = true
	m.speedResult = nil
	m.speedCfg = cfg
	m.speedProg = speedtest.Progress{}
	m.speedSSID = m.wifi.Active.Name
	m.speedCh = make(chan speedtest.Progress, 32)
	m.busy = "speed testing"
	m.setInfo("")
	ch := m.speedCh

	runCmd := func() tea.Msg {
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
		scanning := m.busy == "" || m.busy == "scanning"
		if scanning && m.mode == modeList && !m.speedActive {
			cmds = append(cmds, m.stateCmd())
		}
		return m, tea.Batch(cmds...)

	case stateMsg:
		if msg.seq != 0 && msg.seq <= m.stateApplied {
			return m, nil
		}
		m.stateApplied = msg.seq
		if msg.err != nil {
			m.setErr(msg.err)
		} else {
			m.status = msg.status
			m.wifi = msg.wifi
		}
		return m, nil

	case apsMsg:
		if msg.seq != 0 && msg.seq <= m.apsApplied {
			return m, nil
		}
		m.apsApplied = msg.seq
		if msg.err != nil {
			if m.status.WifiEnabled && !strings.Contains(msg.err.Error(), "No Wi-Fi device found") {
				m.setErr(msg.err)
			}
		} else {
			m.setAPs(msg.aps)
		}
		// The initial cached load must not clear busy: the background
		// rescan that started alongside it is still running.
		if !msg.initial && m.busy == "scanning" {
			m.busy = ""
		}
		return m, nil

	case savedMsg:
		if msg.seq != 0 && msg.seq <= m.savedApplied {
			return m, nil
		}
		m.savedApplied = msg.seq
		if msg.err != nil {
			m.setErr(msg.err)
		} else {
			m.saved = msg.conns
			m.rebuildSavedTable()
		}
		return m, nil

	case actionMsg:
		m.actionCancel = nil
		m.busy = ""
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				m.setInfo("cancelled")
				return m, nil
			}
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

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.shutdown()
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
	case modeDetails:
		return m.updateDetails(msg)
	case modeSaved:
		return m.updateSaved(msg)
	case modeHiddenSSID:
		return m.updateHiddenSSID(msg)
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

	// While a mutating action is in flight, esc cancels it instead of
	// clearing status messages.
	if msg.String() == "esc" && m.busy != "" && m.busy != "scanning" {
		if m.actionCancel != nil {
			m.actionCancel()
			m.actionCancel = nil
		}
		return m, nil
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
		m.shutdown()
		return m, tea.Quit

	case "/":
		m.filtering = true
		return m, m.filter.Focus()
	}

	if m.busy != "" {
		if m.busy == "scanning" {
			// A scan is a passive refresh, not a mutating action: keep the
			// UI usable and only prevent stacking another rescan on top.
			if msg.String() == "r" {
				return m, nil
			}
		} else {
			// A mutating action is in flight. Allow scrolling the table
			// while busy, but block mutating actions.
			switch msg.String() {
			case "r", "t", "d", "f", "s", "S", "enter", "F", "h", "D":
				return m, nil
			}
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		}
	}

	switch msg.String() {
	case "r":
		m.busy = "scanning"
		m.setInfo("")
		return m, tea.Batch(m.apsCmd(true), m.spinner.Tick)

	case "t":
		on := !m.status.WifiEnabled
		m.busy = "turning Wi-Fi " + onOffWord(on)
		ctx := m.beginAction()
		return m, tea.Batch(m.toggleCmd(ctx, on), m.spinner.Tick)

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

	case "i":
		if len(m.visible) == 0 {
			m.setWarn("no network selected")
			return m, nil
		}
		m.detailAP = m.selectedAP()
		m.mode = modeDetails
		return m, nil

	case "o":
		m.sort = (m.sort + 1) % (sortSecurity + 1)
		m.applyFilter()
		m.setInfo("sorted by " + m.sort.String())
		return m, nil

	case "F":
		m.mode = modeSaved
		m.rebuildSavedTable()
		m.layout()
		return m, nil

	case "h":
		m.mode = modeHiddenSSID
		m.hiddenInput.Reset()
		return m, m.hiddenInput.Focus()

	case "D":
		devices := m.wifi.Devices
		if len(devices) < 2 {
			m.setWarn("no other Wi-Fi device available")
			return m, nil
		}
		cur := m.ifaceOverride
		if cur == "" {
			cur = m.wifi.Device
		}
		next := devices[0]
		for i, d := range devices {
			if d == cur {
				next = devices[(i+1)%len(devices)]
				break
			}
		}
		if next == cur {
			m.setWarn("no other Wi-Fi device available")
			return m, nil
		}
		m.ifaceOverride = next
		m.busy = "scanning"
		m.setInfo("using " + next)
		return m, tea.Batch(m.stateCmd(), m.apsCmd(true), m.spinner.Tick)

	case "s", "S":
		if m.wifi.Active.Name == "" {
			m.setWarn("not connected — join a network first")
			return m, nil
		}
		cfg := speedtest.DefaultConfig()
		if msg.String() == "S" {
			cfg = speedtest.QuickConfig()
		}
		return m, m.startSpeedtest(cfg)
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
		ctx := m.beginAction()
		return m, tea.Batch(m.connectCmd(ctx, ap.SSID, "", m.wifi.Device), m.spinner.Tick)
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
		if m.pwdPurpose == pwdEditPassword {
			m.mode = modeSaved
			m.pwdPurpose = pwdConnect
			m.pwdInput.Reset()
			return m, nil
		}
		m.pwdPurpose = pwdConnect
		m.mode = modeList
		m.pwdInput.Reset()
		return m, nil
	case "enter":
		pw := m.pwdInput.Value()
		if m.pwdPurpose == pwdEditPassword {
			conn := m.editConn
			name := savedDisplayName(conn)
			m.mode = modeSaved
			m.pwdPurpose = pwdConnect
			m.busy = "updating password for " + name
			m.setInfo("")
			ctx := m.beginAction()
			return m, tea.Batch(m.modifyPasswordCmd(ctx, conn.UUID, name, pw), m.spinner.Tick)
		}
		if m.pwdPurpose == pwdHidden {
			ssid := m.connectSSID
			m.mode = modeList
			m.pwdPurpose = pwdConnect
			m.busy = "connecting to " + ssid
			m.setInfo("")
			ctx := m.beginAction()
			return m, tea.Batch(m.connectHiddenCmd(ctx, ssid, pw, m.wifi.Device), m.spinner.Tick)
		}
		ssid := m.connectSSID
		m.mode = modeList
		m.busy = "connecting to " + ssid
		m.setInfo("")
		ctx := m.beginAction()
		return m, tea.Batch(m.connectCmd(ctx, ssid, pw, m.wifi.Device), m.spinner.Tick)
	case "ctrl+r":
		if m.pwdInput.EchoMode == textinput.EchoPassword {
			m.pwdInput.EchoMode = textinput.EchoNormal
		} else {
			m.pwdInput.EchoMode = textinput.EchoPassword
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.pwdInput, cmd = m.pwdInput.Update(msg)
	return m, cmd
}

func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ret := m.confirmReturn
	if ret == 0 {
		ret = modeList
	}
	switch msg.String() {
	case "y", "Y":
		m.mode = ret
		m.confirmReturn = modeList
		switch m.confirm {
		case confirmForget:
			id, name := m.pendingForget.UUID, m.pendingForget.Name
			m.busy = "forgetting " + name
			m.setInfo("")
			ctx := m.beginAction()
			return m, tea.Batch(m.forgetCmd(ctx, id, name), m.spinner.Tick)
		case confirmDisconnect:
			name := m.wifi.Active.Name
			m.busy = "disconnecting from " + name
			m.setInfo("")
			ctx := m.beginAction()
			return m, tea.Batch(m.disconnectCmd(ctx, m.wifi.Device), m.spinner.Tick)
		}
		return m, nil
	case "n", "N", "esc":
		m.mode = ret
		m.confirmReturn = modeList
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
		m.shutdown()
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
		cfg := m.speedCfg
		if cfg.DownloadFor <= 0 {
			cfg = speedtest.DefaultConfig()
		}
		return m, m.startSpeedtest(cfg)
	}
	return m, nil
}

func (m Model) updateDetails(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "i":
		m.mode = modeList
		return m, nil
	case "q":
		m.shutdown()
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateSaved(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While a mutating action is in flight, esc cancels it and other actions
	// are blocked; navigation still works.
	if m.busy != "" && m.busy != "scanning" {
		switch msg.String() {
		case "esc":
			if m.actionCancel != nil {
				m.actionCancel()
				m.actionCancel = nil
			}
			return m, nil
		case "enter", "a", "e", "f", "r", "F":
			return m, nil
		}
	}

	switch msg.String() {
	case "esc", "F":
		m.mode = modeList
		m.layout()
		return m, nil
	case "q":
		m.shutdown()
		return m, tea.Quit
	case "up", "k":
		m.savedTable.MoveUp(1)
		return m, nil
	case "down", "j":
		m.savedTable.MoveDown(1)
		return m, nil
	case "r":
		return m, m.savedCmd()
	case "enter":
		conn := m.selectedSaved()
		if conn == nil {
			m.setWarn("no saved network selected")
			return m, nil
		}
		name := savedDisplayName(*conn)
		m.busy = "connecting to " + name
		m.setInfo("")
		ctx := m.beginAction()
		return m, m.activateSavedCmd(ctx, conn.UUID, name)
	case "a":
		conn := m.selectedSaved()
		if conn == nil {
			m.setWarn("no saved network selected")
			return m, nil
		}
		name := savedDisplayName(*conn)
		enable := conn.Autoconnect != "yes"
		m.busy = "updating autoconnect for " + name
		m.setInfo("")
		ctx := m.beginAction()
		return m, m.autoconnectCmd(ctx, conn.UUID, name, enable)
	case "e":
		conn := m.selectedSaved()
		if conn == nil {
			m.setWarn("no saved network selected")
			return m, nil
		}
		m.mode = modePassword
		m.pwdPurpose = pwdEditPassword
		m.editConn = *conn
		m.pwdInput.Reset()
		return m, m.pwdInput.Focus()
	case "f":
		conn := m.selectedSaved()
		if conn == nil {
			m.setWarn("no saved network selected")
			return m, nil
		}
		m.mode = modeConfirm
		m.confirm = confirmForget
		m.confirmReturn = modeSaved
		m.pendingForget = *conn
		return m, nil
	}
	return m, nil
}

func (m Model) updateHiddenSSID(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.hiddenInput.Reset()
		return m, nil
	case "enter":
		ssid := strings.TrimSpace(m.hiddenInput.Value())
		if ssid == "" {
			m.setWarn("enter the hidden network's SSID")
			return m, nil
		}
		m.connectSSID = ssid
		m.mode = modePassword
		m.pwdPurpose = pwdHidden
		m.pwdInput.Reset()
		return m, m.pwdInput.Focus()
	}
	var cmd tea.Cmd
	m.hiddenInput, cmd = m.hiddenInput.Update(msg)
	return m, cmd
}

func (m Model) selectedSaved() *nm.SavedConnection {
	i := m.savedTable.Cursor()
	if i >= 0 && i < len(m.saved) {
		return &m.saved[i]
	}
	return nil
}

func savedDisplayName(c nm.SavedConnection) string {
	if c.SSID != "" {
		return c.SSID
	}
	return c.Name
}

func (m *Model) rebuildSavedTable() {
	rows := make([]table.Row, 0, len(m.saved))
	for _, c := range m.saved {
		name := sanitizeSSID(c.Name)
		ssid := sanitizeSSID(c.SSID)
		if ssid == "" {
			ssid = "(unknown)"
		}
		auto := "yes"
		if c.Autoconnect != "yes" {
			auto = "no"
		}
		rows = append(rows, table.Row{name, ssid, auto})
	}
	m.savedTable.SetRows(rows)
	if cur := m.savedTable.Cursor(); cur >= len(rows) && len(rows) > 0 {
		m.savedTable.SetCursor(len(rows) - 1)
	}
}

func (m *Model) setAPs(aps []nm.AccessPoint) {
	m.aps = aps
	m.applyFilter()
}

func (m *Model) applyFilter() {
	var selectedKey string
	if cur := m.table.Cursor(); cur >= 0 && cur < len(m.visible) {
		selectedKey = apKey(m.visible[cur])
	}

	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.visible = make([]nm.AccessPoint, 0, len(m.aps))
	for _, ap := range m.aps {
		if q == "" || strings.Contains(strings.ToLower(ap.SSID), q) {
			m.visible = append(m.visible, ap)
		}
	}
	sortAPs(m.visible, m.sort)

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

	if selectedKey != "" {
		for i, ap := range m.visible {
			if apKey(ap) == selectedKey {
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

// apKey identifies an access point across list updates. Hidden networks have
// no SSID, so their BSSID is the only stable key.
func apKey(ap nm.AccessPoint) string {
	if ap.SSID != "" {
		return ap.SSID
	}
	return ap.BSSID
}

// sortAPs orders access points for display, keeping the active connection(s)
// first regardless of the chosen sort mode.
func sortAPs(aps []nm.AccessPoint, mode sortMode) {
	sort.SliceStable(aps, func(i, j int) bool {
		if aps[i].InUse != aps[j].InUse {
			return aps[i].InUse
		}
		switch mode {
		case sortName:
			ni, nj := strings.ToLower(aps[i].SSID), strings.ToLower(aps[j].SSID)
			if ni != nj {
				return ni < nj
			}
		case sortChannel:
			ci, cj := channelNumber(aps[i].Chan), channelNumber(aps[j].Chan)
			if ci != cj {
				return ci < cj
			}
		case sortSecurity:
			si, sj := securityLabel(aps[i].Security), securityLabel(aps[j].Security)
			if si != sj {
				return si < sj
			}
		}
		return aps[i].Signal > aps[j].Signal
	})
}

func channelNumber(ch string) int {
	n, err := strconv.Atoi(strings.TrimSpace(ch))
	if err != nil {
		return 1 << 30
	}
	return n
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
		if _, lines := m.statusMessage(); lines > 0 {
			reserved += lines
		}
	}
	// The help bar may wrap to multiple lines on narrow terminals.
	reserved += len(m.helpLines()) - 1

	h := m.height - reserved
	if h < 3 {
		h = 3
	}
	m.table.SetHeight(h)

	nameW, ssidW := 24, 28
	if m.width > nameW+12+4 {
		ssidW = m.width - nameW - 12 - 4
		if ssidW < 12 {
			ssidW = 12
		}
	}
	m.savedTable.SetColumns([]table.Column{
		{Title: "NAME", Width: nameW},
		{Title: "SSID", Width: ssidW},
		{Title: "AUTOCONNECT", Width: 12},
	})
	m.savedTable.SetWidth(m.width)
	m.savedTable.SetHeight(h)
}

// helpLines returns the help bar for the current mode.
func (m Model) helpLines() []string {
	if m.mode == modeSaved {
		return savedHelpLines(m.width)
	}
	return helpLines(m.width)
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
		conn := &m.saved[i]
		if conn.Type != "" && conn.Type != "802-11-wireless" {
			continue
		}
		// Match on the profile's SSID: users can rename profiles, so the
		// profile Name is not a reliable key.
		if conn.SSID == ssid {
			return conn
		}
		// Fall back to the profile name when the SSID lookup failed.
		if conn.SSID == "" && conn.Name == ssid {
			return conn
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

// statusMessage returns the current status line wrapped to the terminal
// width, along with the number of lines it occupies.
func (m Model) statusMessage() (string, int) {
	var msg, prefix string
	var style lipgloss.Style
	switch {
	case m.errMsg != "":
		msg, prefix, style = m.errMsg, "✗ ", errStyle
	case m.warnMsg != "":
		msg, prefix, style = m.warnMsg, "! ", warnStyle
	case m.info != "":
		msg, prefix, style = m.info, "✓ ", okStyle
	default:
		return "", 0
	}
	text := prefix + msg
	if m.width > 0 {
		text = lipgloss.NewStyle().Width(m.width).Render(text)
	}
	return style.Render(text), strings.Count(text, "\n") + 1
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeList || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.table.MoveUp(1)
	case tea.MouseButtonWheelDown:
		m.table.MoveDown(1)
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder

	wifiTxt := offStyle.Render("off")
	if m.status.WifiEnabled {
		wifiTxt = onStyle.Render("on")
	}
	header := titleStyle.Render("nmtui") +
		"  " + dimStyle.Render("Wi-Fi:") + " " + wifiTxt
	if m.wifi.Device != "" {
		header += "  " + dimStyle.Render("iface:") + " " + m.wifi.Device
	}
	if m.wifi.Active.Name != "" {
		conn := sanitizeSSID(m.wifi.Active.Name)
		if m.wifi.IP != "" {
			conn += " (" + ipOnly(m.wifi.IP) + ")"
		}
		header += "  " + dimStyle.Render("connected:") + " " + okStyle.Render(conn)
		if c := connectivityNote(m.status.Connectivity); c != "" {
			header += "  " + dimStyle.Render("· "+c)
		}
	}
	b.WriteString(header + "\n\n")

	switch m.mode {
	case modePassword:
		title := "Password for " + boldStyle.Render(sanitizeSSID(m.connectSSID))
		switch m.pwdPurpose {
		case pwdEditPassword:
			title = "New password for " + boldStyle.Render(sanitizeSSID(savedDisplayName(m.editConn)))
		case pwdHidden:
			title = "Password for hidden network " + boldStyle.Render(sanitizeSSID(m.connectSSID))
		}
		reveal := "show"
		if m.pwdInput.EchoMode == textinput.EchoNormal {
			reveal = "hide"
		}
		action := "connect"
		if m.pwdPurpose == pwdEditPassword {
			action = "save"
		}
		body := title +
			"\n\n" + m.pwdInput.View() +
			"\n\n" + dimStyle.Render("ctrl+r: "+reveal+"  ·  enter: "+action+"  ·  esc: cancel")
		if m.pwdPurpose == pwdHidden {
			body += "\n" + dimStyle.Render("leave empty for an open network")
		}
		b.WriteString(boxStyle.Render(body))

	case modeHiddenSSID:
		body := "Hidden network\n\n" + m.hiddenInput.View() +
			"\n\n" + dimStyle.Render("enter: continue  ·  esc: cancel")
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

	case modeDetails:
		b.WriteString(m.detailsView())

	case modeSaved:
		if len(m.saved) == 0 {
			b.WriteString(dimStyle.Render("no saved Wi-Fi profiles") + "\n")
		} else {
			b.WriteString(m.savedTable.View() + "\n")
		}

	default:
		switch {
		case !m.status.WifiEnabled:
			b.WriteString(dimStyle.Render("Wi-Fi is disabled — press t to enable") + "\n")
		case len(m.visible) == 0:
			if m.busy == "scanning" {
				b.WriteString(dimStyle.Render("scanning for networks…") + "\n")
			} else {
				b.WriteString(dimStyle.Render("no networks found — press r to scan") + "\n")
			}
		default:
			b.WriteString(m.table.View() + "\n")
		}
		if m.filtering {
			b.WriteString("\n" + m.filter.View())
		}
	}

	b.WriteString("\n")
	if msg, _ := m.statusMessage(); msg != "" {
		b.WriteString(msg)
	}
	if m.busy != "" && m.mode != modeSpeedtest {
		b.WriteString("\n" + m.spinner.View() + " " + m.busy + "...")
	}

	if m.mode == modeList || m.mode == modeSaved {
		b.WriteString("\n\n" + strings.Join(m.helpLines(), "\n"))
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

// ipOnly drops the CIDR suffix nmcli reports (192.168.1.5/24).
func ipOnly(ip string) string {
	if i := strings.IndexByte(ip, '/'); i != -1 {
		return ip[:i]
	}
	return ip
}

// connectivityNote labels degraded connectivity; "full" is the norm and
// needs no note.
func connectivityNote(connectivity string) string {
	switch connectivity {
	case "", "full", "unknown":
		return ""
	default:
		return connectivity
	}
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

func (m Model) detailsView() string {
	ap := m.detailAP
	name := sanitizeSSID(ap.SSID)
	if name == "" {
		name = "(hidden network)"
	}
	var body strings.Builder
	body.WriteString(boldStyle.Render(name) + "\n\n")
	fields := [][2]string{
		{"signal", fmt.Sprintf("%s  %d%%", signalBars(ap.Signal), ap.Signal)},
		{"security", securityLabel(ap.Security)},
		{"bssid", ap.BSSID},
		{"channel", ap.Chan},
		{"frequency", ap.Freq},
		{"rate", ap.Rate},
		{"mode", ap.Mode},
	}
	for _, f := range fields {
		if strings.TrimSpace(f[1]) == "" {
			continue
		}
		fmt.Fprintf(&body, "%s  %s\n", dimStyle.Render(fmt.Sprintf("%-9s", f[0])), f[1])
	}
	body.WriteString("\n" + dimStyle.Render("esc: close"))
	return boxStyle.Render(body.String())
}

func (m Model) speedView() string {
	ssid := sanitizeSSID(m.speedSSID)
	if ssid == "" {
		ssid = sanitizeSSID(m.wifi.Active.Name)
	}
	if ssid == "" {
		ssid = "(unknown)"
	}
	cfg := m.speedCfg
	if cfg.DownloadFor <= 0 {
		cfg = speedtest.DefaultConfig()
	}
	title := "Speed test — " + boldStyle.Render(ssid)
	sub := dimStyle.Render(fmt.Sprintf("%s  ·  %.0fs down + %.0fs up (%d streams)",
		cfg.BaseURL, cfg.DownloadFor.Seconds(), cfg.UploadFor.Seconds(), cfg.Streams))
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
	line := fmt.Sprintf("%s %s  ·  %s", m.spinner.View(), label,
		boldStyle.Render(speedtest.FormatMbps(m.speedProg.InstantMbps)))
	if m.speedProg.AvgMbps > 0 {
		line += dimStyle.Render(fmt.Sprintf("  (avg %s)", speedtest.FormatMbps(m.speedProg.AvgMbps)))
	}
	body.WriteString(line + "\n")
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
