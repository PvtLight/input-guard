// Package art draws Input Guard's icons from shapes, at any size, with no
// image files: the brand mark (the same artwork as the Mac app's Brand.swift)
// for the .exe, the settings window and the notice, and the padlocks for the
// notification-area icon.
package art

import (
	"image"
	"image/color"
	"math"
)

type point struct{ x, y float64 }

// A layer is a color and a signed distance to its shape, in icon units: the
// icon is 100 units across, y down. Negative is inside.
type layer struct {
	color color.NRGBA
	shape func(x, y float64) float64
}

var (
	tile  = color.NRGBA{0x14, 0x38, 0x33, 0xFF} // NSColor(0.08, 0.22, 0.20)
	mint  = color.NRGBA{0x87, 0xD4, 0xB5, 0xFF} // NSColor(0.53, 0.83, 0.71)
	pale  = color.NRGBA{0xDE, 0xF5, 0xE8, 0xFF} // NSColor(0.87, 0.96, 0.91)
	amber = color.NRGBA{0xF2, 0xB1, 0x3B, 0xFF}
)

func roundRect(x0, y0, x1, y1, r float64) func(x, y float64) float64 {
	cx, cy := (x0+x1)/2, (y0+y1)/2
	hx, hy := (x1-x0)/2-r, (y1-y0)/2-r
	return func(x, y float64) float64 {
		dx, dy := math.Abs(x-cx)-hx, math.Abs(y-cy)-hy
		return math.Hypot(math.Max(dx, 0), math.Max(dy, 0)) + math.Min(math.Max(dx, dy), 0) - r
	}
}

func segmentDistance(x, y float64, a, b point) float64 {
	vx, vy := b.x-a.x, b.y-a.y
	t := 0.0
	if l := vx*vx + vy*vy; l > 0 {
		t = math.Max(0, math.Min(1, ((x-a.x)*vx+(y-a.y)*vy)/l))
	}
	return math.Hypot(x-(a.x+t*vx), y-(a.y+t*vy))
}

// stroke is a line of the given width along the points, with round ends and joins.
func stroke(points []point, width float64) func(x, y float64) float64 {
	return func(x, y float64) float64 {
		d := math.Inf(1)
		for i := 1; i < len(points); i++ {
			d = math.Min(d, segmentDistance(x, y, points[i-1], points[i]))
		}
		return d - width/2
	}
}

func cubic(p0, p1, p2, p3 point, steps int) []point {
	var out []point
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		u := 1 - t
		out = append(out, point{
			u*u*u*p0.x + 3*u*u*t*p1.x + 3*u*t*t*p2.x + t*t*t*p3.x,
			u*u*u*p0.y + 3*u*u*t*p1.y + 3*u*t*t*p2.y + t*t*t*p3.y,
		})
	}
	return out
}

func arc(cx, cy, r, from, to float64, steps int) []point {
	var out []point
	for i := 0; i <= steps; i++ {
		a := from + (to-from)*float64(i)/float64(steps)
		out = append(out, point{cx + r*math.Cos(a), cy + r*math.Sin(a)})
	}
	return out
}

func union(shapes ...func(x, y float64) float64) func(x, y float64) float64 {
	return func(x, y float64) float64 {
		d := math.Inf(1)
		for _, s := range shapes {
			d = math.Min(d, s(x, y))
		}
		return d
	}
}

func render(size int, layers []layer) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	unit := 100 / float64(size) // icon units per pixel
	for py := range size {
		for px := range size {
			x, y := (float64(px)+0.5)*unit, (float64(py)+0.5)*unit
			var r, g, b, a float64 // premultiplied
			for _, l := range layers {
				coverage := math.Max(0, math.Min(1, 0.5-l.shape(x, y)/unit))
				la := coverage * float64(l.color.A) / 255
				r = r*(1-la) + float64(l.color.R)/255*la
				g = g*(1-la) + float64(l.color.G)/255*la
				b = b*(1-la) + float64(l.color.B)/255*la
				a = a*(1-la) + la
			}
			if a > 0 {
				img.SetNRGBA(px, py, color.NRGBA{
					uint8(math.Round(r / a * 255)), uint8(math.Round(g / a * 255)),
					uint8(math.Round(b / a * 255)), uint8(math.Round(a * 255)),
				})
			}
		}
	}
	return img
}

// thick keeps lines at least about a pixel wide in small icons.
func thick(width float64, size int) float64 { return math.Max(width, 110/float64(size)) }

// BrandMark is the Mac app's icon: a pause mark inside a soft shield on a
// dark green tile.
func BrandMark(size int) *image.NRGBA {
	start := point{26, 30}
	shield := []point{start}
	shield = append(shield, cubic(start, point{34, 28}, point{43, 23}, point{50, 20}, 12)...)
	shield = append(shield, cubic(point{50, 20}, point{57, 23}, point{66, 28}, point{74, 30}, 12)...)
	shield = append(shield, point{74, 49})
	shield = append(shield, cubic(point{74, 49}, point{74, 63}, point{63, 73}, point{50, 78}, 16)...)
	shield = append(shield, cubic(point{50, 78}, point{37, 73}, point{26, 63}, point{26, 49}, 16)...)
	shield = append(shield, start)
	return render(size, []layer{
		{tile, roundRect(5, 5, 95, 95, 22)},
		{mint, stroke(shield, thick(4.5, size))},
		{pale, union(roundRect(39, 37, 46, 59, 3.5), roundRect(54, 37, 61, 59, 3.5))},
	})
}

// TrayIcon is the notification-area icon: an open padlock on the dark tile
// while unlocked, and a closed one on an amber tile while locked, so the
// state shows at a glance.
func TrayIcon(size int, locked bool) *image.NRGBA {
	background, lock, hole := tile, mint, tile
	lift, rightLeg := 11.0, 34.0
	if locked {
		background, lock, hole = amber, tile, amber
		lift, rightLeg = 0, 50
	}
	w := thick(10, size)
	top := 36 - lift
	shackle := []point{{34, 50}, {34, top}}
	shackle = append(shackle, arc(50, top, 16, math.Pi, 2*math.Pi, 24)...)
	shackle = append(shackle, point{66, math.Min(rightLeg-lift*0.2, 50)})
	layers := []layer{
		{background, roundRect(4, 4, 96, 96, 20)},
		{lock, stroke(shackle, w)},
		{lock, roundRect(22, 48, 78, 88, 8)},
	}
	if size >= 24 {
		layers = append(layers, layer{hole, union(roundRect(46.5, 60, 53.5, 76, 3.5), roundRect(44, 58, 56, 68, 6))})
	}
	return render(size, layers)
}
