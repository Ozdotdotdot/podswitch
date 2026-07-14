// Command ascii-headphones renders a rotating headset using only the Go
// standard library. Run it with: go run ./examples/ascii-headphones
package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	width  = 26 // Character columns in the animation.
	height = 14 // Character rows in the animation.
	fps    = 12

	rotationSpeed = 1.1 // Radians per second.
	characterRamp = ".,-~:;=!*#$@"

	cameraDistance  = 5.2
	projectionScale = 36.0
)

type vec3 struct{ x, y, z float64 }

/*
The headset is sampled as two tori (ear pads) and a tube swept along a
semicircular path (the headband). Points and surface normals rotate every
frame. Perspective projection maps a point using x/y / (cameraDistance - z),
and a z-buffer retains only the nearest point in each terminal cell. The dot
product of the rotated normal with a fixed light vector indexes characterRamp:
brighter faces receive denser characters and turn visibly as the model spins.
*/
func main() {
	ticker := time.NewTicker(time.Second / fps)
	defer ticker.Stop()
	defer fmt.Print("\x1b[0m\x1b[?25h")
	fmt.Print("\x1b[2J\x1b[97m\x1b[?25l")

	start := time.Now()
	for range ticker.C {
		f := newFrame()
		a := time.Since(start).Seconds() * rotationSpeed
		drawHeadset(&f, a)
		fmt.Print("\x1b[H", f.string())
	}
}

func drawHeadset(f *frame, angle float64) {
	// Two padded ear cups, each a torus in the x/y plane.
	for _, centerX := range []float64{-0.82, 0.82} {
		for u := 0.0; u < 2*math.Pi; u += 0.18 {
			for v := 0.0; v < 2*math.Pi; v += 0.25 {
				p := vec3{centerX + (0.43+0.16*math.Cos(v))*math.Cos(u), -0.42 + (0.43+0.16*math.Cos(v))*math.Sin(u), 0.16 * math.Sin(v)}
				n := vec3{math.Cos(v) * math.Cos(u), math.Cos(v) * math.Sin(u), math.Sin(v)}
				f.plot(rotate(p, angle), rotate(n, angle))
			}
		}
	}

	// A tube around a half-circle joins the cups as the headband.
	for u := 0.0; u <= math.Pi; u += 0.08 {
		path := vec3{1.20 * math.Cos(u), -0.42 + 1.20*math.Sin(u), 0}
		radial := vec3{math.Cos(u), math.Sin(u), 0}
		for v := 0.0; v < 2*math.Pi; v += 0.25 {
			n := vec3{math.Cos(v) * radial.x, math.Cos(v) * radial.y, math.Sin(v)}
			p := vec3{path.x + 0.10*n.x, path.y + 0.10*n.y, 0.10 * n.z}
			f.plot(rotate(p, angle), rotate(n, angle))
		}
	}
}

type frame struct {
	pixels []byte
	depth  []float64
}

func newFrame() frame {
	f := frame{pixels: make([]byte, width*height), depth: make([]float64, width*height)}
	for i := range f.pixels {
		f.pixels[i], f.depth[i] = ' ', math.Inf(1)
	}
	return f
}

func (f frame) plot(p, n vec3) {
	denom := cameraDistance - p.z
	col := int(float64(width)/2 + projectionScale*p.x/denom)
	row := int(float64(height)/2 - projectionScale*0.5*p.y/denom) // Terminal cells are tall.
	if col < 0 || col >= width || row < 0 || row >= height {
		return
	}
	i := row*width + col
	if denom >= f.depth[i] {
		return
	}
	f.depth[i] = denom
	// The camera is on +z, so light from +z illuminates the visible surface.
	light := normalize(vec3{0.35, 0.55, 1})
	brightness := 0.18 + 0.82*math.Max(0, dot(normalize(n), light))
	f.pixels[i] = characterRamp[int(brightness*float64(len(characterRamp)-1))]
}

func (f frame) string() string {
	var b strings.Builder
	b.Grow((width + 1) * height)
	for row := 0; row < height; row++ {
		b.Write(f.pixels[row*width : (row+1)*width])
		b.WriteByte('\n')
	}
	return b.String()
}

func rotate(p vec3, a float64) vec3 {
	// Yaw only: the headset stays upright while it spins in place.
	ca, sa := math.Cos(a), math.Sin(a)
	return vec3{ca*p.x + sa*p.z, p.y, -sa*p.x + ca*p.z}
}

func dot(a, b vec3) float64 { return a.x*b.x + a.y*b.y + a.z*b.z }
func normalize(v vec3) vec3 {
	length := math.Sqrt(dot(v, v))
	return vec3{v.x / length, v.y / length, v.z / length}
}
