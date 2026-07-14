// Package ui is shared by the runnable music-indicator explorations.
package ui

import (
	"strings"
	"time"

	art "github.com/Ozdotdotdot/podswitch/examples/tui-layouts/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	muted   = lipgloss.Color("241")
	faint   = lipgloss.Color("238")
	text    = lipgloss.Color("252")
	accent  = lipgloss.Color("110")
	playing = lipgloss.Color("72")
	panel   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(faint).Padding(1, 2)
)

type tickMsg time.Time

type Host struct {
	Name    string
	Holding bool
	Playing bool
}

type Model struct {
	Width, Height int
	Selected      int
	Pulse         int
	Started       time.Time
	Hosts         []Host
}

func NewModel() Model {
	return Model{Started: time.Now(), Hosts: []Host{
		{Name: "laptop"},
		{Name: "studio", Holding: true, Playing: true},
		{Name: "media-pi"},
	}}
}

func (m Model) Init() tea.Cmd { return tick() }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width, m.Height = msg.Width, msg.Height
	case tickMsg:
		m.Pulse++
		return m, tick()
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			m.Selected = (m.Selected + len(m.Hosts) - 1) % len(m.Hosts)
		case "down", "j":
			m.Selected = (m.Selected + 1) % len(m.Hosts)
		case "p":
			m.Hosts[m.Selected].Playing = !m.Hosts[m.Selected].Playing
		}
	}
	return m, nil
}

func tick() tea.Cmd { return tea.Tick(time.Second/12, func(t time.Time) tea.Msg { return tickMsg(t) }) }

func (m Model) View(kind, caption string) string {
	if m.Width == 0 {
		return ""
	}
	artwork := lipgloss.NewStyle().Foreground(muted).Render(art.RenderHeadset(time.Since(m.Started).Seconds() * .95))
	heading := lipgloss.NewStyle().Bold(true).Foreground(text).Render("AirPods Max")
	subtitle := lipgloss.NewStyle().Foreground(muted).Render(caption)
	card := panel.Width(48).Render(
		lipgloss.Place(48, art.ArtHeight, lipgloss.Center, lipgloss.Center, artwork) + "\n\n" +
			heading + "\n" + subtitle + "\n\n" + m.HostList(kind),
	)
	footer := lipgloss.NewStyle().Foreground(muted).Render("↑/↓ select  •  p play/pause  •  q quit")
	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, card+"\n\n"+footer)
}

func (m Model) HostList(kind string) string {
	var b strings.Builder
	for i, host := range m.Hosts {
		prefix := "  "
		name := lipgloss.NewStyle().Foreground(text).Render(host.Name)
		if i == m.Selected {
			prefix = lipgloss.NewStyle().Foreground(accent).Render("› ")
			name = lipgloss.NewStyle().Foreground(accent).Bold(true).Render(host.Name)
		}
		holder := lipgloss.NewStyle().Foreground(muted).Render("online")
		if host.Holding {
			holder = lipgloss.NewStyle().Foreground(playing).Render("holding")
		}
		indicator := ""
		if host.Playing {
			indicator = "  " + m.indicator(kind)
		}
		b.WriteString(prefix + "○ " + name + "  " + holder + indicator + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (m Model) indicator(kind string) string {
	style := lipgloss.NewStyle().Foreground(playing)
	switch kind {
	case "jingle":
		// The note never moves. A single sparkle drifts away from it, then
		// disappears. Keeping this a four-cell slot means the host row is stable
		// even on terminals that repaint the animation slowly.
		frames := []string{"♪ · ", "♪ ✧ ", "♪  ✦", "♪   "}
		return style.Render(frames[(m.Pulse/8)%len(frames)])
	case "equalizer":
		frames := []string{"▁▃▆▂", "▃▆▂▅", "▆▂▅▃", "▂▅▃▆"}
		return style.Render(frames[m.Pulse%len(frames)])
	case "spinner":
		frames := []string{"◜", "◠", "◝", "◡", "◟", "◞"}
		return style.Render(frames[m.Pulse%len(frames)] + " playing")
	default:
		frames := []string{"▁▂▄▆▄▂", "▂▄▆▄▂▁", "▄▆▄▂▁▂", "▆▄▂▁▂▄"}
		return style.Render(frames[m.Pulse%len(frames)] + " playing")
	}
}
