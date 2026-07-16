// Package tui provides the interactive podswitch client.
package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/Ozdotdotdot/podswitch/internal/config"
)

const (
	animationFPS = 12
	// This must exceed the coordinator's whole-handoff deadline. The inline
	// pulse remains visible while a slow host waits for real PipeWire
	// resources instead of failing from an arbitrary client deadline.
	grabTimeout   = 140 * time.Second
	toggleTimeout = 15 * time.Second
	mediaTimeout  = 15 * time.Second
)

var (
	muted   = lipgloss.Color("241")
	faint   = lipgloss.Color("238")
	text    = lipgloss.Color("252")
	accent  = lipgloss.Color("110")
	success = lipgloss.Color("72")
	panel   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(faint).Padding(1, 2)
)

type stateResp struct {
	Holder          string        `json:"holder,omitempty"`
	ConnectedHosts  []string      `json:"connectedHosts"`
	AudioOwner      string        `json:"audioOwner,omitempty"`
	ActiveAudioHost string        `json:"activeAudioHost,omitempty"`
	ActiveSource    string        `json:"activeSource,omitempty"`
	SourceType      string        `json:"sourceType,omitempty"`
	SourceSeenAt    string        `json:"sourceSeenAt,omitempty"`
	Agents          []agentStatus `json:"agents"`
}

type agentStatus struct {
	Host      string `json:"host"`
	Online    bool   `json:"online"`
	Connected bool   `json:"connected"`
	Playing   bool   `json:"playing"`
	SeenAt    string `json:"seenAt,omitempty"`
}

type stateMsg stateResp
type watchStatusMsg struct{ err error }
type grabResultMsg struct{ err error }
type toggleResultMsg struct {
	host    string
	playing *bool
	err     error
}
type mediaResultMsg struct {
	host, action string
	err          error
}
type tickMsg time.Time

// Run resolves the coordinator then starts the full-screen interactive UI.
// State changes come exclusively from the persistent /ws/watch connection.
func Run(explicitCoordinator string) error {
	addr, err := config.ResolveCoordinatorAddr(explicitCoordinator)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := tea.NewProgram(newModel(normalizeHTTP(addr)), tea.WithAltScreen())
	go watchState(ctx, p, normalizeWS(addr))
	_, err = p.Run()
	return err
}

type model struct {
	width, height int
	selected      int
	agents        []agentStatus
	audioOwner    string
	sourceType    string
	grabbing      bool
	grabTarget    string
	grabConnected bool
	grabSucceeded bool
	toggling      bool
	toggleTarget  string
	mediaBusy     bool
	mediaTarget   string
	pulse         int
	message       string
	watchErr      error
	started       time.Time
	baseURL       string
}

func newModel(baseURL string) model {
	return model{baseURL: baseURL, started: time.Now(), message: "connecting to coordinator"}
}

func (m model) Init() tea.Cmd { return tick() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.pulse++
		return m, tick()
	case stateMsg:
		selectedHost := ""
		if m.selected < len(m.agents) {
			selectedHost = m.agents[m.selected].Host
		}
		m.agents = append([]agentStatus(nil), msg.Agents...)
		m.audioOwner = msg.AudioOwner
		m.sourceType = msg.SourceType
		sort.Slice(m.agents, func(i, j int) bool { return m.agents[i].Host < m.agents[j].Host })
		if len(m.agents) == 0 {
			m.selected = 0
		} else {
			m.selected = min(m.selected, len(m.agents)-1)
			for i, agent := range m.agents {
				if agent.Host == selectedHost {
					m.selected = i
					break
				}
			}
		}
		m.watchErr = nil
		completed := false
		if m.grabTarget != "" {
			m.grabConnected = false
			for _, agent := range m.agents {
				if agent.Host == m.grabTarget && agent.Connected {
					m.grabConnected = true
					if m.grabSucceeded {
						m.message = "handoff complete"
						m.grabTarget = ""
						m.grabConnected = false
						m.grabSucceeded = false
						completed = true
					}
					break
				}
			}
		}
		if !m.grabbing && !completed && m.grabTarget == "" && !m.grabSucceeded && (m.message == "connecting to coordinator" || m.message == "reconnecting to coordinator") {
			m.message = "live updates connected"
		}
	case watchStatusMsg:
		m.watchErr = msg.err
		if msg.err != nil && !m.grabbing {
			m.message = "reconnecting to coordinator"
		}
	case grabResultMsg:
		m.grabbing = false
		if msg.err != nil {
			// BlueZ can briefly report Connected before the agent has finished
			// creating and routing the PipeWire sink. A failed command is never
			// a completed handoff, regardless of that transient state.
			m.grabTarget = ""
			m.grabConnected = false
			m.grabSucceeded = false
			m.message = "grab failed: " + msg.err.Error()
		} else if m.grabConnected {
			m.message = "handoff complete"
			m.grabTarget = ""
			m.grabConnected = false
		} else {
			m.grabSucceeded = true
			m.message = "handoff requested, waiting for live state"
		}
	case toggleResultMsg:
		m.toggling = false
		m.toggleTarget = ""
		if msg.err != nil {
			m.message = "play/pause failed: " + msg.err.Error()
		} else if msg.playing != nil && *msg.playing {
			m.message = "playing on " + msg.host
		} else if msg.playing != nil {
			m.message = "paused on " + msg.host
		} else {
			m.message = "playback toggled on " + msg.host
		}
	case mediaResultMsg:
		m.mediaBusy = false
		m.mediaTarget = ""
		if msg.err != nil {
			m.message = mediaLabel(msg.action) + " failed: " + msg.err.Error()
		} else {
			m.message = mediaSuccess(msg.action) + " on " + msg.host
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if len(m.agents) > 0 && !m.grabbing && !m.toggling && !m.mediaBusy {
				m.selected = (m.selected + len(m.agents) - 1) % len(m.agents)
			}
		case "down", "j":
			if len(m.agents) > 0 && !m.grabbing && !m.toggling && !m.mediaBusy {
				m.selected = (m.selected + 1) % len(m.agents)
			}
		case "enter":
			if len(m.agents) == 0 || m.grabbing {
				break
			}
			target := m.agents[m.selected]
			if !target.Online {
				m.message = target.Host + " is offline"
				break
			}
			if target.Connected {
				m.message = target.Host + " already holds the AirPods"
				break
			}
			m.grabbing = true
			m.grabTarget = target.Host
			m.grabConnected = false
			m.grabSucceeded = false
			m.message = "moving AirPods to " + target.Host
			return m, grab(m.baseURL, target.Host)
		case "p":
			if len(m.agents) == 0 || m.grabbing || m.toggling || m.mediaBusy {
				break
			}
			target := m.agents[m.selected]
			if !target.Online {
				m.message = target.Host + " is offline"
				break
			}
			m.toggling = true
			m.toggleTarget = target.Host
			return m, toggle(m.baseURL, target.Host)
		case "[", "]", "<", ">":
			if len(m.agents) == 0 || m.grabbing || m.toggling || m.mediaBusy {
				break
			}
			target := m.agents[m.selected]
			if !target.Online {
				m.message = target.Host + " is offline"
				break
			}
			action := mediaActionForKey(msg.String())
			m.mediaBusy = true
			m.mediaTarget = target.Host
			return m, media(m.baseURL, target.Host, action)
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	card := m.renderCard()
	// The stacked layout's height is dominated by the fixed-size art block.
	// Once it (plus the status/footer lines below it) can't fit, switch to
	// the side-by-side layout instead of letting the top of the card scroll
	// off-screen — but only if that layout actually fits the width too.
	if lipgloss.Height(card)+4 > m.height {
		if compact := m.renderCompactCard(); lipgloss.Width(compact) <= m.width {
			card = compact
		}
	}
	status := lipgloss.NewStyle().Foreground(muted).Render(m.message)
	footerWidth := min(lipgloss.Width(card), m.width)
	footer := lipgloss.NewStyle().Foreground(muted).Width(footerWidth).Align(lipgloss.Center).
		Render("↑/↓ select  •  enter grab  •  p play/pause  •  [/] volume  •  </> track  •  q quit")
	content := lipgloss.JoinVertical(lipgloss.Center, card, "", status, footer)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m model) renderCard() string {
	art := lipgloss.NewStyle().Foreground(muted).Render(renderHeadset(time.Since(m.started).Seconds() * .95))
	heading := lipgloss.NewStyle().Bold(true).Foreground(text).Render("AirPods Max")
	subtitle := lipgloss.NewStyle().Foreground(muted).Render("Move audio between your Linux machines")
	return panel.Width(48).Render(
		lipgloss.Place(48, artHeight, lipgloss.Center, lipgloss.Center, art) + "\n\n" +
			heading + "\n" + subtitle + "\n\n" + m.hostList(),
	)
}

// renderCompactCard places the art beside the host list instead of above it,
// trading width (plentiful in a short-but-wide terminal) for height (scarce
// there) so the card still fits without clipping. It also uses tighter
// padding and a cropped art block, since every row/column shaved here is one
// more terminal size this layout can still fit.
func (m model) renderCompactCard() string {
	art := lipgloss.NewStyle().Foreground(muted).Render(compactHeadset(time.Since(m.started).Seconds() * .95))
	heading := lipgloss.NewStyle().Bold(true).Foreground(text).Render("AirPods Max")
	subtitle := lipgloss.NewStyle().Foreground(muted).Render("Move audio between your Linux machines")
	right := heading + "\n" + subtitle + "\n\n" + m.hostList()
	row := lipgloss.JoinHorizontal(lipgloss.Center, art, "  ", right)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(faint).Padding(1, 1).Render(row)
}

// compactHeadset crops renderHeadset's dead margin: across the full rotation
// the top 3 and bottom 2 rows are always blank, so the compact layout can
// reclaim that height instead of rendering it as empty space.
func compactHeadset(angle float64) string {
	lines := strings.Split(renderHeadset(angle), "\n")
	return strings.Join(lines[3:len(lines)-2], "\n")
}

func (m model) hostList() string {
	if len(m.agents) == 0 {
		return lipgloss.NewStyle().Foreground(muted).Render("No agents connected yet.")
	}
	var b strings.Builder
	for i, agent := range m.agents {
		marker := lipgloss.NewStyle().Foreground(muted).Render("○")
		status := lipgloss.NewStyle().Foreground(muted).Render("offline")
		if agent.Online {
			status = lipgloss.NewStyle().Foreground(muted).Render("online")
		}
		if agent.Connected {
			marker = lipgloss.NewStyle().Foreground(success).Render("●")
			status = lipgloss.NewStyle().Foreground(success).Render("connected")
		}
		if agent.Host == m.audioOwner {
			sourceType := m.sourceType
			if sourceType == "none" {
				sourceType = "idle"
			}
			if sourceType == "" {
				sourceType = "unknown"
			}
			status = lipgloss.NewStyle().Foreground(success).Bold(true).Render("audio owner · " + sourceType)
		}
		prefix := "  "
		name := lipgloss.NewStyle().Foreground(text).Render(agent.Host)
		if i == m.selected {
			prefix = lipgloss.NewStyle().Foreground(accent).Render("› ")
			name = lipgloss.NewStyle().Foreground(accent).Bold(true).Render(agent.Host)
		}
		activity := ""
		if i == m.selected && m.grabbing {
			frames := []string{"·", "*", "·", " "}
			activity += " " + lipgloss.NewStyle().Foreground(accent).Render(frames[m.pulse%len(frames)])
		}
		if i == m.selected && m.toggling && m.toggleTarget == agent.Host {
			frames := []string{"·", "*", "·", " "}
			activity += " " + lipgloss.NewStyle().Foreground(accent).Render(frames[m.pulse%len(frames)])
		}
		if i == m.selected && m.mediaBusy && m.mediaTarget == agent.Host {
			frames := []string{"·", "*", "·", " "}
			activity += " " + lipgloss.NewStyle().Foreground(accent).Render(frames[m.pulse%len(frames)])
		}
		if agent.Playing {
			activity += " " + musicIndicator(m.pulse)
		}
		b.WriteString(prefix + marker + " " + name + "  " + status + activity + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// musicIndicator keeps the note in one fixed terminal cell while a single
// sparkle slowly travels outward. It signals playback without making the
// host list feel like a visualizer.
func musicIndicator(pulse int) string {
	frames := []string{"♪ · ", "♪ ✧ ", "♪  ✦", "♪   "}
	return lipgloss.NewStyle().Foreground(success).Render(frames[(pulse/8)%len(frames)])
}

func tick() tea.Cmd {
	return tea.Tick(time.Second/animationFPS, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func grab(baseURL, host string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]string{"host": host})
		ctx, cancel := context.WithTimeout(context.Background(), grabTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/grab", bytes.NewReader(body))
		if err != nil {
			return grabResultMsg{err}
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return grabResultMsg{err}
		}
		defer resp.Body.Close()
		var result struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		if resp.StatusCode != http.StatusOK {
			if result.Error == "" {
				result.Error = resp.Status
			}
			return grabResultMsg{fmt.Errorf("%s", result.Error)}
		}
		return grabResultMsg{}
	}
}

func toggle(baseURL, host string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]string{"host": host})
		ctx, cancel := context.WithTimeout(context.Background(), toggleTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/toggle", bytes.NewReader(body))
		if err != nil {
			return toggleResultMsg{host: host, err: err}
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return toggleResultMsg{host: host, err: err}
		}
		defer resp.Body.Close()
		var result struct {
			Playing *bool  `json:"playing"`
			Error   string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		if resp.StatusCode != http.StatusOK {
			if result.Error == "" {
				result.Error = resp.Status
			}
			return toggleResultMsg{host: host, err: fmt.Errorf("%s", result.Error)}
		}
		return toggleResultMsg{host: host, playing: result.Playing}
	}
}

func media(baseURL, host, action string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]string{"host": host, "action": action})
		ctx, cancel := context.WithTimeout(context.Background(), mediaTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/media", bytes.NewReader(body))
		if err != nil {
			return mediaResultMsg{host: host, action: action, err: err}
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return mediaResultMsg{host: host, action: action, err: err}
		}
		defer resp.Body.Close()
		var result struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		if resp.StatusCode != http.StatusOK {
			if result.Error == "" {
				result.Error = resp.Status
			}
			return mediaResultMsg{host: host, action: action, err: fmt.Errorf("%s", result.Error)}
		}
		return mediaResultMsg{host: host, action: action}
	}
}

func mediaActionForKey(key string) string {
	switch key {
	case "[":
		return "volume-down"
	case "]":
		return "volume-up"
	case "<":
		return "previous"
	case ">":
		return "next"
	default:
		return ""
	}
}
func mediaLabel(action string) string {
	switch action {
	case "volume-down":
		return "volume down"
	case "volume-up":
		return "volume up"
	case "previous":
		return "previous track"
	case "next":
		return "next track"
	default:
		return "media action"
	}
}
func mediaSuccess(action string) string {
	switch action {
	case "volume-down":
		return "volume lowered"
	case "volume-up":
		return "volume raised"
	case "previous":
		return "previous track"
	case "next":
		return "next track"
	default:
		return "media action complete"
	}
}

func watchState(ctx context.Context, program *tea.Program, endpoint string) {
	backoff := time.Second
	for ctx.Err() == nil {
		conn, _, err := websocket.Dial(ctx, endpoint, nil)
		if err == nil {
			program.Send(watchStatusMsg{})
			backoff = time.Second
			for ctx.Err() == nil {
				var state stateResp
				if err = wsjson.Read(ctx, conn, &state); err != nil {
					break
				}
				program.Send(stateMsg(state))
			}
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}
		if ctx.Err() != nil {
			return
		}
		program.Send(watchStatusMsg{err})
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, 10*time.Second)
	}
}

func normalizeHTTP(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimSuffix(addr, "/")
	}
	return "http://" + strings.TrimSuffix(addr, "/")
}

func normalizeWS(addr string) string {
	httpURL := normalizeHTTP(addr)
	u, err := url.Parse(httpURL)
	if err != nil {
		return "ws://" + strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://") + "/ws/watch"
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/ws/watch"
	u.RawQuery = ""
	return u.String()
}
