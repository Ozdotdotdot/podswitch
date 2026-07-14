// Command spinner previews a minimal orbit playback indicator.
package main

import (
	"fmt"
	"os"

	"github.com/Ozdotdotdot/podswitch/examples/music-indicators/ui"
	tea "github.com/charmbracelet/bubbletea"
)

type model struct{ ui.Model }

func (m model) Init() tea.Cmd { return m.Model.Init() }
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && (k.String() == "q" || k.String() == "ctrl+c") {
		return m, tea.Quit
	}
	next, cmd := m.Model.Update(msg)
	m.Model = next
	return m, cmd
}
func (m model) View() string { return m.Model.View("spinner", "Minimal orbit") }
func main() {
	if _, err := tea.NewProgram(model{ui.NewModel()}, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
