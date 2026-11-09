package glass

import (
	"math"

	"github.com/egoist/mygo/internal/scene"
)

// pixels draws the glass on the CPU, as its shaders do on the GPU.
type pixels struct {
	m          material
	rect       scene.Rect
	radii      [4]float32
	lx, ly     float32
	// across and along are how much the edge darkens where it faces
	// across the light and along it (glassEdge).
	across, along float32
	toneTable  [toneSteps + 1]float32
	toneLinear bool
}

// toneSteps is how many lightnesses the CPU keeps the tone curve at,
// between which it is a line, within 3e-5 of the curve: its power makes
// it slow on the CPU.
const toneSteps = 1024

// Begin reads the material of op and readies its tone curve.
func (px *pixels) Begin(op *scene.EffectOp, rect scene.Rect, radii [4]float32) {
	p := &op.Params
	px.m = material{
		blur:  op.Blur,
		bezel: p[0][0], refraction: p[0][1], rim: p[0][2], rimWidth: p[0][3],
		tint: p[1],
		low:  p[2][0], high: p[2][1], curve: p[2][2], saturation: p[2][3],
	}
	px.lx, px.ly, px.across, px.along = p[3][0], p[3][1], p[3][2], p[3][3]
	px.rect, px.radii = rect, radii
	px.toneLinear = px.m.curve == 1
	if !px.toneLinear {
		for i := range px.toneTable {
			px.toneTable[i] = 1 - pow(1-float32(i)/toneSteps, px.m.curve)
		}
	}
}

// tone is the material's tone from the table.
func (px *pixels) tone(c [3]float32) [3]float32 {
	m := &px.m
	l := 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
	var v float32
	if px.toneLinear {
		v = min(max(l, 0), 1)
	} else {
		x := min(max(l, 0), 1) * toneSteps
		i := min(int(x), toneSteps-1)
		f := x - float32(i)
		v = px.toneTable[i] + (px.toneTable[i+1]-px.toneTable[i])*f
	}
	return toneAt(c, l, m.low+(m.high-m.low)*v, m)
}

// Color returns the premultiplied color of the glass at the pixel center
// (x, y), as the shaders' effect does: the pane, inside its shape less a
// pixel, and its edge around it.
func (px *pixels) Color(x, y float32, b *scene.BackdropImage) [4]float32 {
	m := &px.m
	in := -scene.SDRoundRect(px.rect, px.radii, x, y) - 1
	t := max(in, 0)
	var nx, ny float32
	if t < m.bezel || t < 3*m.rimWidth || in < 0.5 {
		nx, ny = normal(x, y, px.rect, px.radii)
	}
	facing := abs(nx*px.lx + ny*px.ly)
	sx, sy := x, y
	if t < m.bezel {
		d := m.refraction * lens(t, m.bezel)
		sx, sy = x-nx*d, y-ny*d
	}
	c := px.tone(b.Sample(sx, sy))
	for i := range c {
		c[i] += (m.tint[i] - c[i]) * m.tint[3]
	}
	if m.rim > 0 && t < 3*m.rimWidth {
		// The light along the rim, added, as plus lighter adds it.
		q := t / m.rimWidth
		a := facing * m.rim * float32(math.Exp(float64(-q*q)))
		for i := range c {
			c[i] = min(c[i]+a, 1)
		}
	}
	g := min(max(in+0.5, 0), 1)
	if g >= 1 {
		return [4]float32{c[0], c[1], c[2], 1}
	}
	k := b.Sample(x, y)
	a := glassEdge(0.2126*k[0]+0.7152*k[1]+0.0722*k[2], facing, px.across, px.along)
	return [4]float32{c[0] * g, c[1] * g, c[2] * g, g + a*(1-g)}
}

// glassEdge returns how much the edge, a pixel wide around the pane,
// darkens what is under it, of lightness l: by k, where the edge faces
// across the light, across, and along it, along, as Core Animation's plus
// darker subtracts it, as an alpha that does so over the backdrop the pane
// reads, at most 0.67, as NSGlassEffectView's edge over dark content.
func glassEdge(l, facing, across, along float32) float32 {
	k := along + (across-along)*(1-facing)
	return min(k/max(l, 1e-4), 0.67)
}
