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

const animationFPS = 12

var (
	muted   = lipgloss.Color("241")
	faint   = lipgloss.Color("238")
	text    = lipgloss.Color("252")
	accent  = lipgloss.Color("110")
	success = lipgloss.Color("72")
	panel   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(faint).Padding(1, 2)
)

type stateResp struct {
	Holder string        `json:"holder,omitempty"`
	Agents []agentStatus `json:"agents"`
}

type agentStatus struct {
	Host      string `json:"host"`
	Online    bool   `json:"online"`
	Connected bool   `json:"connected"`
	SeenAt    string `json:"seenAt,omitempty"`
}

type stateMsg stateResp
type watchStatusMsg struct{ err error }
type grabResultMsg struct{ err error }
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
	grabbing      bool
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
		if !m.grabbing {
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
			m.message = "grab failed: " + msg.err.Error()
		} else {
			m.message = "handoff requested, waiting for live state"
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if len(m.agents) > 0 && !m.grabbing {
				m.selected = (m.selected + len(m.agents) - 1) % len(m.agents)
			}
		case "down", "j":
			if len(m.agents) > 0 && !m.grabbing {
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
			m.message = "moving AirPods to " + target.Host
			return m, grab(m.baseURL, target.Host)
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	art := lipgloss.NewStyle().Foreground(muted).Render(renderHeadset(time.Since(m.started).Seconds() * .95))
	heading := lipgloss.NewStyle().Bold(true).Foreground(text).Render("AirPods Max")
	subtitle := lipgloss.NewStyle().Foreground(muted).Render("Move audio between your Linux machines")
	card := panel.Width(48).Render(
		lipgloss.Place(48, artHeight, lipgloss.Center, lipgloss.Center, art) + "\n\n" +
			heading + "\n" + subtitle + "\n\n" + m.hostList(),
	)
	status := lipgloss.NewStyle().Foreground(muted).Render(m.message)
	footer := lipgloss.NewStyle().Foreground(muted).Render("↑/↓ select  •  enter grab  •  q quit")
	content := card + "\n\n" + status + "\n" + footer
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
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
			status = lipgloss.NewStyle().Foreground(success).Render("holding")
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
			activity = " " + lipgloss.NewStyle().Foreground(accent).Render(frames[m.pulse%len(frames)])
		}
		b.WriteString(prefix + marker + " " + name + activity + "  " + status + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func tick() tea.Cmd {
	return tea.Tick(time.Second/animationFPS, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func grab(baseURL, host string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]string{"host": host})
		ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
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
