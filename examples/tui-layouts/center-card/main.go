// Command center-card is a runnable centered-card TUI layout exploration.
package main

import (
	"fmt"
	"os"

	"github.com/Ozdotdotdot/podswitch/examples/tui-layouts/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct{ ui.Model }

func (m model) Init() tea.Cmd { return m.Model.Init() }
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && (k.String() == "q" || k.String() == "ctrl+c") {
		return m, tea.Quit
	}
	n, c := m.Model.Update(msg)
	m.Model = n
	return m, c
}
func (m model) View() string {
	if m.Width == 0 {
		return ""
	}
	art := lipgloss.NewStyle().Foreground(ui.Muted).Render(m.Headset())
	head := lipgloss.NewStyle().Bold(true).Foreground(ui.Text).Render("AirPods Max")
	sub := lipgloss.NewStyle().Foreground(ui.Muted).Render("Move audio between your machines")
	card := ui.Panel.Width(48).Render(lipgloss.Place(48, ui.ArtHeight, lipgloss.Center, lipgloss.Center, art) + "\n\n" + head + "\n" + sub + "\n\n" + m.HostList())
	content := card + "\n\n" + ui.Footer()
	return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, content)
}
func main() {
	if _, err := tea.NewProgram(model{ui.NewModel()}, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
