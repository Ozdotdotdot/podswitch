// Command ascii-headphones-cup renders a rotating ear cup using only the Go
// standard library. Run it with: go run ./examples/ascii-headphones-cup
package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	width  = 24 // Character columns in the animation.
	height = 13 // Character rows in the animation.
	fps    = 12

	rotationSpeed = 1.3 // Radians per second.
	characterRamp = ".,-~:;=!*#$@"

	cameraDistance  = 4.0
	projectionScale = 28.0
)

type vec3 struct{ x, y, z float64 }

/*
Each sampled torus point and its outward normal are rotated together. The
camera projects x/y by 1 / (cameraDistance - z); the closest sample per cell
wins the z-buffer. A normalized dot product between the rotated normal and a
fixed light direction selects a character from characterRamp, producing the
shading cues that make the rotation readable.
*/
func main() {
	ticker := time.NewTicker(time.Second / fps)
	defer ticker.Stop()
	defer fmt.Print("\x1b[0m\x1b[?25h") // Restore attributes and cursor on Ctrl-C.
	fmt.Print("\x1b[2J\x1b[97m\x1b[?25l")

	start := time.Now()
	for range ticker.C {
		frame := newFrame()
		a := time.Since(start).Seconds() * rotationSpeed
		// A torus models the padded ring of a single ear cup.
		for u := 0.0; u < 2*math.Pi; u += 0.16 {
			for v := 0.0; v < 2*math.Pi; v += 0.22 {
				p := vec3{
					x: (0.72 + 0.25*math.Cos(v)) * math.Cos(u),
					y: (0.72 + 0.25*math.Cos(v)) * math.Sin(u),
					z: 0.25 * math.Sin(v),
				}
				n := vec3{math.Cos(v) * math.Cos(u), math.Cos(v) * math.Sin(u), math.Sin(v)}
				frame.plot(rotate(p, a), rotate(n, a))
			}
		}
		fmt.Print("\x1b[H", frame.string())
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
	// Two axes avoid the ambiguous flat-on rotation of a single spin axis.
	ca, sa := math.Cos(a), math.Sin(a)
	x, z := ca*p.x+sa*p.z, -sa*p.x+ca*p.z
	cb, sb := math.Cos(a*0.63), math.Sin(a*0.63)
	return vec3{x, cb*p.y - sb*z, sb*p.y + cb*z}
}

func dot(a, b vec3) float64 { return a.x*b.x + a.y*b.y + a.z*b.z }
func normalize(v vec3) vec3 {
	length := math.Sqrt(dot(v, v))
	return vec3{v.x / length, v.y / length, v.z / length}
}
