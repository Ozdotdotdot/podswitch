package tui

import (
	"math"
	"strings"
)

const (
	artWidth   = 28
	artHeight  = 15
	artSubX    = 3
	artSubY    = 3
	artOffsetX = -2.0 // presentation correction for the perspective-weighted silhouette
)

// renderHeadset is the parametric 3D, depth-buffered, contour-aware ASCII
// renderer from examples/ascii-headphones-max, adapted for Bubble Tea ticks.
func renderHeadset(angle float64) string {
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
	n := artWidth * artSubX * artHeight * artSubY
	f := frame{light: make([]float64, n), depth: make([]float64, n)}
	for i := range f.depth {
		f.depth[i] = math.Inf(1)
	}
	return f
}
func (f *frame) plot(p, n vec3) {
	d := 5.4 - p.z
	x := int((float64(artWidth*artSubX)-1)/2 + artOffsetX*artSubX + 38*float64(artSubX)*p.x/d)
	y := int(float64(artHeight*artSubY)/2 - 19*float64(artSubY)*p.y/d)
	b := .16 + .84*math.Max(0, dot(normalize(n), normalize(vec3{.35, .55, 1})))
	for oy := -1; oy <= 1; oy++ {
		for ox := -1; ox <= 1; ox++ {
			px, py := x+ox, y+oy
			if px < 0 || px >= artWidth*artSubX || py < 0 || py >= artHeight*artSubY {
				continue
			}
			i := py*artWidth*artSubX + px
			if d < f.depth[i] {
				f.depth[i], f.light[i] = d, b
			}
		}
	}
}
func (f frame) string() string {
	var b strings.Builder
	for r := 0; r < artHeight; r++ {
		for c := 0; c < artWidth; c++ {
			b.WriteByte(bestGlyph(f.samples(c, r)))
		}
		if r < artHeight-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
func (f frame) samples(c, r int) [9]float64 {
	var v [9]float64
	max := 0.
	for y := 0; y < artSubY; y++ {
		for x := 0; x < artSubX; x++ {
			q := f.light[(r*artSubY+y)*artWidth*artSubX+c*artSubX+x]
			v[y*artSubX+x] = q
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

func bestGlyph(t [9]float64) byte {
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
