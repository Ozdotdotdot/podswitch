// Command corner-picker is a runnable corner-art, centered-picker exploration.
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
	corner := lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.NewStyle().Foreground(ui.Muted).Render("AIRPODS MAX") + "\n" + art)
	picker := ui.Panel.Width(42).Render(lipgloss.NewStyle().Bold(true).Foreground(ui.Text).Render("Pick a destination") + "\n\n" + m.HostList())
	available := m.Width - lipgloss.Width(corner) - 4
	if available < 48 {
		return lipgloss.NewStyle().Padding(1, 2).Render(corner + "\n" + picker + "\n\n" + ui.Footer())
	}
	// Keep the body and footer within one terminal-sized canvas. Previously
	// the footer itself was given terminal height, then appended below the
	// body, which pushed the corner art and picker above the visible region.
	bodyHeight := max(1, m.Height-2)
	right := lipgloss.Place(available, bodyHeight, lipgloss.Center, lipgloss.Center, picker)
	body := lipgloss.JoinHorizontal(lipgloss.Top, corner, right)
	footer := lipgloss.Place(m.Width, 2, lipgloss.Center, lipgloss.Center, ui.Footer())
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.Place(m.Width, bodyHeight, lipgloss.Left, lipgloss.Top, body), footer)
}
func main() {
	if _, err := tea.NewProgram(model{ui.NewModel()}, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
