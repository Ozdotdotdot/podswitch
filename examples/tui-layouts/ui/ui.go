// Package ui holds the shared interaction and parametric animation used by
// the throwaway layout demos in the parent directory.
package ui

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	ArtWidth  = 28
	ArtHeight = 15
	subX      = 3
	subY      = 3
	// The perspective projection makes the headset's visual mass read a
	// little right-heavy. This is a presentation correction, in cells, not a
	// change to the 3D rotation itself.
	artOffsetX = -1.0
)

var (
	Muted   = lipgloss.Color("241")
	Faint   = lipgloss.Color("238")
	Text    = lipgloss.Color("252")
	Accent  = lipgloss.Color("110")
	Success = lipgloss.Color("72")
	Panel   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Faint).Padding(1, 2)
)

type tickMsg time.Time
type grabDoneMsg struct{}

// Model provides fake host data and the animation clock for all demos.
type Model struct {
	Width, Height int
	Selected      int
	Grabbing      bool
	Pulse         int
	Started       time.Time
	Hosts         []Host
}

type Host struct {
	Name   string
	Online bool
	Holder bool
}

func NewModel() Model {
	return Model{Started: time.Now(), Hosts: []Host{
		{Name: "laptop", Online: true},
		{Name: "studio", Online: true, Holder: true},
		{Name: "media-pi", Online: false},
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
	case grabDoneMsg:
		m.Grabbing = false
		for i := range m.Hosts {
			m.Hosts[i].Holder = i == m.Selected
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if !m.Grabbing {
				m.Selected = (m.Selected + len(m.Hosts) - 1) % len(m.Hosts)
			}
		case "down", "j":
			if !m.Grabbing {
				m.Selected = (m.Selected + 1) % len(m.Hosts)
			}
		case "enter":
			if !m.Grabbing && m.Hosts[m.Selected].Online && !m.Hosts[m.Selected].Holder {
				m.Grabbing = true
				return m, tea.Tick(1250*time.Millisecond, func(time.Time) tea.Msg { return grabDoneMsg{} })
			}
		}
	}
	return m, nil
}

func tick() tea.Cmd { return tea.Tick(time.Second/12, func(t time.Time) tea.Msg { return tickMsg(t) }) }

func (m Model) Headset() string { return RenderHeadset(time.Since(m.Started).Seconds() * .95) }

func (m Model) HostList() string {
	var b strings.Builder
	for i, host := range m.Hosts {
		marker := lipgloss.NewStyle().Foreground(Muted).Render("○")
		status := lipgloss.NewStyle().Foreground(Muted).Render("offline")
		if host.Online {
			status = lipgloss.NewStyle().Foreground(Muted).Render("online")
		}
		if host.Holder {
			marker = lipgloss.NewStyle().Foreground(Success).Render("●")
			status = lipgloss.NewStyle().Foreground(Success).Render("holding")
		}
		prefix := "  "
		name := lipgloss.NewStyle().Foreground(Text).Render(host.Name)
		if i == m.Selected {
			prefix = lipgloss.NewStyle().Foreground(Accent).Render("› ")
			name = lipgloss.NewStyle().Foreground(Accent).Bold(true).Render(host.Name)
		}
		activity := ""
		if i == m.Selected && m.Grabbing {
			frames := []string{"·", "*", "·", " "}
			activity = " " + lipgloss.NewStyle().Foreground(Accent).Render(frames[m.Pulse%len(frames)])
		}
		b.WriteString(prefix + marker + " " + name + activity + "  " + status + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func Footer() string {
	return lipgloss.NewStyle().Foreground(Muted).Render("↑/↓ select  •  enter grab  •  q quit")
}

// RenderHeadset ports the parametric geometry, yaw rotation, normal-based
// lighting, depth buffer, and contour-aware glyph matching from
// examples/ascii-headphones-max into a single per-frame renderer.
func RenderHeadset(angle float64) string {
	f := newFrame()
	for _, cup := range []struct{ x, y, tilt float64 }{{-.82, -.43, -.27}, {.82, -.43, .27}} {
		ct, st := math.Cos(cup.tilt), math.Sin(cup.tilt)
		for u := 0.; u < 2*math.Pi; u += .11 {
			for v := 0.; v < 2*math.Pi; v += .16 {
				x, y := (.31+.115*math.Cos(v))*math.Cos(u), (.55+.115*math.Cos(v))*math.Sin(u)
				p := vec3{cup.x + ct*x - st*y, cup.y + st*x + ct*y, .115 * math.Sin(v)}
				n := normalize(vec3{ct*math.Cos(v)*math.Cos(u)/.31 - st*math.Cos(v)*math.Sin(u)/.55, st*math.Cos(v)*math.Cos(u)/.31 + ct*math.Cos(v)*math.Sin(u)/.55, math.Sin(v)})
				f.plot(yaw(p, angle), yaw(n, angle))
			}
		}
	}
	for u := .05; u < math.Pi-.05; u += .05 {
		for v := 0.; v < 2*math.Pi; v += .18 {
			path, radial := vec3{1.18 * math.Cos(u), -.34 + 1.33*math.Sin(u), 0}, vec3{math.Cos(u), math.Sin(u), 0}
			n := vec3{math.Cos(v) * radial.x, math.Cos(v) * radial.y, math.Sin(v)}
			f.plot(yaw(vec3{path.x + .105*n.x, path.y + .105*n.y, .105 * n.z}, angle), yaw(n, angle))
		}
	}
	return f.string()
}

type vec3 struct{ x, y, z float64 }
type frame struct{ light, depth []float64 }

func newFrame() frame {
	n := ArtWidth * subX * ArtHeight * subY
	f := frame{make([]float64, n), make([]float64, n)}
	for i := range f.depth {
		f.depth[i] = math.Inf(1)
	}
	return f
}
func (f *frame) plot(p, n vec3) {
	d := 5.4 - p.z
	// The supersampled frame is an even number of pixels wide. Its visual
	// center falls between the two middle pixels, not on the first pixel to
	// their right; using (width-1)/2 removes the slight rightward bias.
	x := int((float64(ArtWidth*subX)-1)/2 + artOffsetX*subX + 38*float64(subX)*p.x/d)
	y := int(float64(ArtHeight*subY)/2 - 19*float64(subY)*p.y/d)
	b := .16 + .84*math.Max(0, dot(normalize(n), normalize(vec3{.35, .55, 1})))
	for oy := -1; oy <= 1; oy++ {
		for ox := -1; ox <= 1; ox++ {
			px, py := x+ox, y+oy
			if px < 0 || px >= ArtWidth*subX || py < 0 || py >= ArtHeight*subY {
				continue
			}
			i := py*ArtWidth*subX + px
			if d < f.depth[i] {
				f.depth[i], f.light[i] = d, b
			}
		}
	}
}
func (f frame) string() string {
	var b strings.Builder
	for r := 0; r < ArtHeight; r++ {
		for c := 0; c < ArtWidth; c++ {
			b.WriteByte(best(f.samples(c, r)))
		}
		if r < ArtHeight-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
func (f frame) samples(c, r int) [9]float64 {
	var v [9]float64
	max := 0.
	for y := 0; y < subY; y++ {
		for x := 0; x < subX; x++ {
			q := f.light[(r*subY+y)*ArtWidth*subX+c*subX+x]
			v[y*subX+x] = q
			max = math.Max(max, q)
		}
	}
	if max > 0 {
		for i := range v {
			v[i] = max * math.Pow(v[i]/max, 1.7)
		}
	}
	return v
}

type glyph struct {
	char byte
	mask [9]float64
}

var glyphs = []glyph{{' ', [9]float64{}}, {'.', [9]float64{0, 0, 0, 0, 0, 0, 0, .35, 0}}, {'^', [9]float64{0, .65, 0, .3, .3, .3, 0, 0, 0}}, {'_', [9]float64{0, 0, 0, 0, 0, 0, .7, .7, .7}}, {'-', [9]float64{0, 0, 0, .7, .7, .7, 0, 0, 0}}, {'/', [9]float64{0, 0, .8, 0, .8, 0, .8, 0, 0}}, {'\\', [9]float64{.8, 0, 0, 0, .8, 0, 0, 0, .8}}, {'|', [9]float64{0, .7, 0, 0, .7, 0, 0, .7, 0}}, {'(', [9]float64{0, .5, .3, .7, 0, 0, 0, .5, .3}}, {')', [9]float64{.3, .5, 0, 0, 0, .7, .3, .5, 0}}, {'=', [9]float64{0, 0, 0, .6, .6, .6, .6, .6, .6}}, {'+', [9]float64{0, .55, 0, .55, .55, .55, 0, .55, 0}}, {'*', [9]float64{.3, .5, .3, .2, .5, .2, .3, .5, .3}}, {'#', [9]float64{.72, .72, .72, .72, .72, .72, .72, .72, .72}}, {'@', [9]float64{.95, .95, .95, .95, .95, .95, .95, .95, .95}}}

func best(t [9]float64) byte {
	c, d := byte(' '), math.Inf(1)
	for _, g := range glyphs {
		n := 0.
		for i := range t {
			x := t[i] - g.mask[i]
			n += x * x
		}
		if n < d {
			c, d = g.char, n
		}
	}
	return c
}
func yaw(p vec3, a float64) vec3 {
	c, s := math.Cos(a), math.Sin(a)
	return vec3{c*p.x + s*p.z, p.y, -s*p.x + c*p.z}
}
func dot(a, b vec3) float64 { return a.x*b.x + a.y*b.y + a.z*b.z }
func normalize(v vec3) vec3 { l := math.Sqrt(dot(v, v)); return vec3{v.x / l, v.y / l, v.z / l} }
