// Package tui implements `cautem term` — k9s-like sandbox browser with live agent observation.
package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cautem/cautem-driver/driver"
	"github.com/cautem/quarshell-cli/internal/service"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	selStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("33")).Bold(true)
	normalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	panelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	denyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	allowStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	ocsfStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
)

const (
	viewList = iota
	viewLogs
	maxLogLines  = 400
	refreshEvery = 2 * time.Second
)

// Action is a post-TUI command.
type Action struct {
	Kind string // connect | exec | logs | ""
	Name string
}

type model struct {
	app      *service.App
	items    []driver.Info
	cursor   int
	err      string
	status   string
	action   Action
	quitting bool
	view     int
	width    int
	height   int

	logName   string
	logLines  []string
	logErr    string
	logCancel context.CancelFunc
	logCh     <-chan tea.Msg
}

type refreshMsg struct {
	items []driver.Info
	err   error
}

type tickMsg time.Time

type logLineMsg struct {
	name string
	line string
}

type logErrMsg struct {
	name string
	err  error
}

type logDoneMsg struct {
	name string
}

// Run starts the interactive terminal UI and returns the chosen action (if any).
func Run(a *service.App) (Action, error) {
	m := model{app: a, width: 80, height: 24}
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return Action{}, err
	}
	out, _ := final.(model)
	if out.logCancel != nil {
		out.logCancel()
	}
	return out.action, nil
}

func (m model) Init() tea.Cmd {
	return tea.Batch(refreshCmd(m.app), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func refreshCmd(a *service.App) tea.Cmd {
	return func() tea.Msg {
		list, err := a.ListSandboxes()
		return refreshMsg{items: list, err: err}
	}
}

func (m *model) stopLogs() {
	if m.logCancel != nil {
		m.logCancel()
		m.logCancel = nil
	}
	m.logCh = nil
	m.logName = ""
	m.logLines = nil
	m.logErr = ""
}

func startLogStream(a *service.App, name string) (context.CancelFunc, <-chan tea.Msg) {
	return startLogStreamOpts(a, service.LogsOpts{Names: []string{name}, Follow: true})
}

func startLogStreamAll(a *service.App) (context.CancelFunc, <-chan tea.Msg) {
	return startLogStreamOpts(a, service.LogsOpts{All: true, Follow: true})
}

func startLogStreamOpts(a *service.App, opt service.LogsOpts) (context.CancelFunc, <-chan tea.Msg) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan tea.Msg, 64)
	name := "*"
	if len(opt.Names) == 1 {
		name = opt.Names[0]
	}
	go func() {
		defer close(ch)
		pr, pw := io.Pipe()
		go func() {
			err := a.LogsToOpts(ctx, opt, pw)
			_ = pw.Close()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				select {
				case ch <- logErrMsg{name: name, err: err}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case ch <- logDoneMsg{name: name}:
			case <-ctx.Done():
			}
		}()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			select {
			case <-ctx.Done():
				_ = pr.Close()
				return
			case ch <- logLineMsg{name: name, line: sc.Text()}:
			}
		}
		_ = pr.Close()
		if ctx.Err() == nil {
			select {
			case ch <- logDoneMsg{name: name}:
			default:
			}
		}
	}()
	return cancel, ch
}

func waitLog(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return logDoneMsg{}
		}
		return msg
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tickCmd()}
		if m.view == viewList {
			cmds = append(cmds, refreshCmd(m.app))
		}
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.stopLogs()
			m.quitting = true
			return m, tea.Quit
		case "esc":
			if m.view == viewLogs {
				m.stopLogs()
				m.view = viewList
				m.status = "sandboxes"
				return m, refreshCmd(m.app)
			}
			m.stopLogs()
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.view == viewList && m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.view == viewList && m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "r":
			if m.view == viewList {
				m.status = "refreshing…"
				return m, refreshCmd(m.app)
			}
			if m.logName != "" {
				name := m.logName
				m.stopLogs()
				cancel, ch := startLogStream(m.app, name)
				m.logCancel = cancel
				m.logCh = ch
				m.logName = name
				m.logLines = nil
				m.logErr = ""
				m.status = "logs " + name
				return m, waitLog(ch)
			}
		case "enter", "c":
			if m.view != viewList || len(m.items) == 0 {
				return m, nil
			}
			m.stopLogs()
			m.action = Action{Kind: "connect", Name: m.items[m.cursor].Name}
			m.quitting = true
			return m, tea.Quit
		case "e":
			if m.view != viewList || len(m.items) == 0 {
				return m, nil
			}
			m.stopLogs()
			m.action = Action{Kind: "exec", Name: m.items[m.cursor].Name}
			m.quitting = true
			return m, tea.Quit
		case "L":
			if m.view != viewList || len(m.items) == 0 {
				return m, nil
			}
			m.stopLogs()
			m.action = Action{Kind: "logs", Name: m.items[m.cursor].Name}
			m.quitting = true
			return m, tea.Quit
		case "l":
			if len(m.items) == 0 {
				return m, nil
			}
			name := m.items[m.cursor].Name
			m.stopLogs()
			m.view = viewLogs
			cancel, ch := startLogStream(m.app, name)
			m.logCancel = cancel
			m.logCh = ch
			m.logName = name
			m.logLines = nil
			m.logErr = ""
			m.status = "observing " + name
			return m, waitLog(ch)
		case "a":
			// Fleet follow: all sandboxes
			if len(m.items) == 0 {
				return m, nil
			}
			m.stopLogs()
			m.view = viewLogs
			cancel, ch := startLogStreamAll(m.app)
			m.logCancel = cancel
			m.logCh = ch
			m.logName = "*"
			m.logLines = nil
			m.logErr = ""
			m.status = "observing all sandboxes"
			return m, waitLog(ch)
		}

	case refreshMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			m.items = nil
		} else {
			m.err = ""
			m.items = msg.items
			if m.cursor >= len(m.items) && m.cursor > 0 {
				m.cursor = len(m.items) - 1
			}
			if m.view == viewList {
				m.status = fmt.Sprintf("%d sandboxes", len(m.items))
			}
		}

	case logLineMsg:
		if msg.name != "" && msg.name != m.logName {
			return m, nil
		}
		m.logLines = append(m.logLines, msg.line)
		if len(m.logLines) > maxLogLines {
			m.logLines = m.logLines[len(m.logLines)-maxLogLines:]
		}
		if m.logCh != nil {
			return m, waitLog(m.logCh)
		}
		return m, nil

	case logErrMsg:
		if msg.name != "" && msg.name != m.logName {
			return m, nil
		}
		m.logErr = msg.err.Error()
		return m, nil

	case logDoneMsg:
		if msg.name != "" && msg.name != m.logName {
			return m, nil
		}
		if m.logErr == "" {
			m.logErr = "(stream ended)"
		}
		return m, nil
	}
	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	if m.view == viewLogs {
		return m.viewLogsPanel()
	}
	return m.viewListPanel()
}

func (m model) viewListPanel() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("cautem term") + "  " + statusStyle.Render(m.status) + "\n")
	b.WriteString(helpStyle.Render("agent observation: press l for live OCSF / sandbox logs") + "\n\n")
	if m.err != "" {
		b.WriteString("error: " + m.err + "\n")
	}
	if len(m.items) == 0 {
		b.WriteString(normalStyle.Render("(no sandboxes)") + "\n")
	}
	for i, it := range m.items {
		line := fmt.Sprintf("%-16s %-10s %s", it.Name, it.Status, it.Image)
		if i == m.cursor {
			b.WriteString(selStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString(normalStyle.Render("  "+line) + "\n")
		}
	}
	b.WriteString("\n" + helpStyle.Render("↑/↓ j/k  enter/c connect  e exec  l live-logs  a fleet-logs  L dump-logs  r refresh  q quit"))
	b.WriteString("\n")
	return b.String()
}

func (m model) viewLogsPanel() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("cautem term") + "  " + panelStyle.Render("observe:"+m.logName) + "  " + statusStyle.Render(m.status) + "\n")
	b.WriteString(helpStyle.Render("live agent activity (OCSF NET/HTTP/PROC/CONFIG) · esc back · r restart · q quit") + "\n\n")
	if m.logErr != "" {
		b.WriteString(denyStyle.Render(m.logErr) + "\n")
	}
	max := m.height - 5
	if max < 5 {
		max = 5
	}
	lines := m.logLines
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	for _, line := range lines {
		b.WriteString(colorizeLog(line) + "\n")
	}
	if len(m.logLines) == 0 && m.logErr == "" {
		b.WriteString(normalStyle.Render("(waiting for log lines…)") + "\n")
	}
	return b.String()
}

func colorizeLog(line string) string {
	switch {
	case strings.Contains(line, " DENIED ") || strings.Contains(line, `"allow":false`):
		return denyStyle.Render(line)
	case strings.Contains(line, " ALLOWED ") || strings.Contains(line, `"allow":true`):
		return allowStyle.Render(line)
	case strings.Contains(line, " OCSF "):
		return ocsfStyle.Render(line)
	default:
		return normalStyle.Render(line)
	}
}
