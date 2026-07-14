// Command wide-split is a runnable full-width TUI layout exploration.
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
	left := ui.Panel.Width(ui.ArtWidth).Render(lipgloss.NewStyle().Foreground(ui.Muted).Render("AIRPODS MAX") + "\n\n" + art)
	heading := lipgloss.NewStyle().Bold(true).Foreground(ui.Text).Render("Where should they be?")
	copy := lipgloss.NewStyle().Foreground(ui.Muted).Render("Choose a Linux machine to receive the AirPods.")
	right := ui.Panel.Width(42).Render(heading + "\n" + copy + "\n\n" + m.HostList())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	return lipgloss.NewStyle().Padding(2, 3).Render(body + "\n\n" + ui.Footer())
}
func main() {
	if _, err := tea.NewProgram(model{ui.NewModel()}, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
