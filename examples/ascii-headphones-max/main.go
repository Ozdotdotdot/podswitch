// Command ascii-headphones-max renders an AirPods Max-inspired rotating headset.
// Run it with: go run ./examples/ascii-headphones-max
package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	width  = 28 // Output character columns.
	height = 15 // Output character rows.
	fps    = 12

	rotationSpeed    = 0.95 // Radians per second; yaw only, so it stays upright.
	contrastExponent = 1.7  // Higher values make edges select stronger contour glyphs.

	subX = 3 // Supersamples per character cell horizontally.
	subY = 3 // Supersamples per character cell vertically.

	cameraDistance  = 5.4
	projectionScale = 38.0
)

type vec3 struct{ x, y, z float64 }

/*
This variant follows the shape-aware idea from Alex Harri's ASCII rendering
article. Geometry is first rendered into a 3x3 supersampled buffer per output
character. The nine local luminance samples are contrast-enhanced, then matched
against small occupancy masks for glyphs such as /, \\, ^, _, and |. Therefore
edge cells choose a glyph whose shape follows the projected contour, while
solid regions still choose denser glyphs. The scene itself remains parametric
3D geometry with normal-based lighting and a depth buffer; no frames are
authored by hand.
*/
func main() {
	ticker := time.NewTicker(time.Second / fps)
	defer ticker.Stop()
	defer fmt.Print("\x1b[0m\x1b[?25h")
	fmt.Print("\x1b[2J\x1b[97m\x1b[?25l")

	start := time.Now()
	for range ticker.C {
		f := newFrame()
		drawHeadset(&f, time.Since(start).Seconds()*rotationSpeed)
		fmt.Print("\x1b[H", f.string())
	}
}

func drawHeadset(f *frame, angle float64) {
	// The reference has tall, slightly splayed oval cups. These are elliptical tori.
	drawCup(f, -0.82, -0.43, -0.27, angle)
	drawCup(f, 0.82, -0.43, 0.27, angle)

	// A broad, open semicircular tube makes the band distinct from the cups.
	for u := 0.05; u < math.Pi-0.05; u += 0.05 {
		path := vec3{1.18 * math.Cos(u), -0.34 + 1.33*math.Sin(u), 0}
		radial := vec3{math.Cos(u), math.Sin(u), 0}
		for v := 0.0; v < 2*math.Pi; v += 0.18 {
			n := vec3{math.Cos(v) * radial.x, math.Cos(v) * radial.y, math.Sin(v)}
			p := vec3{path.x + 0.105*n.x, path.y + 0.105*n.y, 0.105 * n.z}
			f.plot(yaw(p, angle), yaw(n, angle))
		}
	}
}

func drawCup(f *frame, cx, cy, tilt, angle float64) {
	ct, st := math.Cos(tilt), math.Sin(tilt)
	for u := 0.0; u < 2*math.Pi; u += 0.11 {
		for v := 0.0; v < 2*math.Pi; v += 0.16 {
			// An elliptical torus gives each cup an outer shell and a recessed opening.
			x := (0.31 + 0.115*math.Cos(v)) * math.Cos(u)
			y := (0.55 + 0.115*math.Cos(v)) * math.Sin(u)
			p := vec3{cx + ct*x - st*y, cy + st*x + ct*y, 0.115 * math.Sin(v)}
			n := vec3{ct*math.Cos(v)*math.Cos(u)/0.31 - st*math.Cos(v)*math.Sin(u)/0.55, st*math.Cos(v)*math.Cos(u)/0.31 + ct*math.Cos(v)*math.Sin(u)/0.55, math.Sin(v)}
			f.plot(yaw(p, angle), yaw(normalize(n), angle))
		}
	}
}

type frame struct {
	light []float64
	depth []float64
}

func newFrame() frame {
	n := width * subX * height * subY
	f := frame{light: make([]float64, n), depth: make([]float64, n)}
	for i := range f.depth {
		f.depth[i] = math.Inf(1)
	}
	return f
}

func (f *frame) plot(p, n vec3) {
	denom := cameraDistance - p.z
	x := int(float64(width*subX)/2 + projectionScale*float64(subX)*p.x/denom)
	y := int(float64(height*subY)/2 - projectionScale*0.5*float64(subY)*p.y/denom)
	brightness := 0.16 + 0.84*math.Max(0, dot(normalize(n), normalize(vec3{0.35, 0.55, 1})))

	// A tiny point splat prevents holes between the sampled parametric points.
	for oy := -1; oy <= 1; oy++ {
		for ox := -1; ox <= 1; ox++ {
			px, py := x+ox, y+oy
			if px < 0 || px >= width*subX || py < 0 || py >= height*subY {
				continue
			}
			i := py*width*subX + px
			if denom < f.depth[i] {
				f.depth[i], f.light[i] = denom, brightness
			}
		}
	}
}

type glyph struct {
	char byte
	mask [9]float64 // Approximate 3x3 occupancy of the rendered glyph.
}

var glyphs = []glyph{
	{' ', [9]float64{}},
	{'.', [9]float64{0, 0, 0, 0, 0, 0, 0, .35, 0}},
	{'^', [9]float64{0, .65, 0, .3, .3, .3, 0, 0, 0}},
	{'_', [9]float64{0, 0, 0, 0, 0, 0, .7, .7, .7}},
	{'-', [9]float64{0, 0, 0, .7, .7, .7, 0, 0, 0}},
	{'/', [9]float64{0, 0, .8, 0, .8, 0, .8, 0, 0}},
	{'\\', [9]float64{.8, 0, 0, 0, .8, 0, 0, 0, .8}},
	{'|', [9]float64{0, .7, 0, 0, .7, 0, 0, .7, 0}},
	{'(', [9]float64{0, .5, .3, .7, 0, 0, 0, .5, .3}},
	{')', [9]float64{.3, .5, 0, 0, 0, .7, .3, .5, 0}},
	{'=', [9]float64{0, 0, 0, .6, .6, .6, .6, .6, .6}},
	{'+', [9]float64{0, .55, 0, .55, .55, .55, 0, .55, 0}},
	{'*', [9]float64{.3, .5, .3, .2, .5, .2, .3, .5, .3}},
	{'#', [9]float64{.72, .72, .72, .72, .72, .72, .72, .72, .72}},
	{'@', [9]float64{.95, .95, .95, .95, .95, .95, .95, .95, .95}},
}

func (f frame) string() string {
	var b strings.Builder
	b.Grow((width + 1) * height)
	for row := 0; row < height; row++ {
		for col := 0; col < width; col++ {
			b.WriteByte(bestGlyph(f.samples(col, row)))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func (f frame) samples(col, row int) [9]float64 {
	var v [9]float64
	max := 0.0
	for y := 0; y < subY; y++ {
		for x := 0; x < subX; x++ {
			value := f.light[(row*subY+y)*width*subX+col*subX+x]
			v[y*subX+x] = value
			max = math.Max(max, value)
		}
	}
	if max == 0 {
		return v
	}
	for i := range v {
		v[i] = max * math.Pow(v[i]/max, contrastExponent)
	}
	return v
}

func bestGlyph(target [9]float64) byte {
	best, bestDistance := byte(' '), math.Inf(1)
	for _, candidate := range glyphs {
		distance := 0.0
		for i := range target {
			delta := target[i] - candidate.mask[i]
			distance += delta * delta
		}
		if distance < bestDistance {
			best, bestDistance = candidate.char, distance
		}
	}
	return best
}

func yaw(p vec3, a float64) vec3 {
	ca, sa := math.Cos(a), math.Sin(a)
	return vec3{ca*p.x + sa*p.z, p.y, -sa*p.x + ca*p.z}
}

func dot(a, b vec3) float64 { return a.x*b.x + a.y*b.y + a.z*b.z }
func normalize(v vec3) vec3 {
	length := math.Sqrt(dot(v, v))
	return vec3{v.x / length, v.y / length, v.z / length}
}
