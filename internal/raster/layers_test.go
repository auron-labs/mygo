package raster

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/internal/scene"
)

// glassScene returns a random scene showing the system's glass of n
// places (holes whose Start is set), some ops under transforms that move
// them, and, with turns, some under transforms that turn them.
func (m *sceneMaker) glassScene(n int, turns bool) *scene.Scene {
	// Premultiplied pixels, which no color exceeds its alpha in: others
	// saturate, differently as layers composite.
	pix := m.pixels(8 * 8 * 4)
	for i := 0; i < len(pix); i += 4 {
		for c := range 3 {
			pix[i+c] = uint8(int(pix[i+c]) * int(pix[i+3]) / 255)
		}
	}
	m.img = scene.NewImageRGBA(8, 8, pix)
	s := m.scene()
	for i := range s.Ops {
		op := &s.Ops[i]
		if op.Kind == scene.OpPushClip || op.Kind == scene.OpPopClip {
			continue
		}
		switch m.rnd.IntN(6) {
		case 0:
			op.Transform = scene.Translation(float32(m.rnd.IntN(21)-10), m.rnd.Float32()*8)
		case 1:
			if turns {
				op.Transform = scene.Translation(80, 60).Mul(scene.Rotation(m.rnd.Float32()*40 - 20)).Mul(scene.Translation(-80, -60))
			}
		}
	}
	// The glass, each at a random place among the ops, its children after
	// it, over it, as an element's are.
	for g := range n {
		at := m.rnd.IntN(len(s.Ops) + 1)
		// Not inside a clip that ends right away: anywhere is fine, as
		// clips nest around the hole like any op.
		r := m.rect()
		hole := scene.Op{Kind: scene.OpHole, Rect: r, Radii: [4]float32{8, 8, 8, 8}, Start: int32(g + 1)}
		if m.rnd.IntN(4) == 0 {
			hole.Transform = scene.Translation(float32(m.rnd.IntN(9)-4), 0)
		}
		kids := []scene.Op{hole}
		for range m.rnd.IntN(3) {
			op := m.op(s)
			if op.Kind != scene.OpGlyphs {
				op.Rect = scene.Rect{X: r.X + m.rnd.Float32()*r.W/2, Y: r.Y + m.rnd.Float32()*r.H/2, W: r.W / 2, H: r.H / 2}
			}
			kids = append(kids, op)
		}
		s.Ops = append(s.Ops[:at], append(kids, s.Ops[at:]...)...)
	}
	return s
}

// render renders s into a new image of its size.
func render(s *scene.Scene) *Image {
	m := NewImage(s.Width, s.Height)
	Render(m, s)
	return m
}

// compose composites src, premultiplied, over dst at (x, y).
func compose(dst, src *Image, x, y int) {
	for j := range src.H {
		for i := range src.W {
			dx, dy := x+i, y+j
			if dx < 0 || dy < 0 || dx >= dst.W || dy >= dst.H {
				continue
			}
			s := src.Pix[j*src.Stride+4*i:][:4]
			d := dst.Pix[dy*dst.Stride+4*dx:][:4]
			a := 255 - int(s[3])
			for c := range 4 {
				d[c] = uint8(min(int(s[c])+(int(d[c])*a+127)/255, 255))
			}
		}
	}
}

// maxDiff returns the largest difference of a channel between a and b,
// and where.
func maxDiff(a, b *Image) (int, int, int) {
	worst, wx, wy := 0, 0, 0
	for y := range a.H {
		for x := range a.W {
			for c := range 4 {
				d := int(a.Pix[y*a.Stride+4*x+c]) - int(b.Pix[y*b.Stride+4*x+c])
				if d < 0 {
					d = -d
				}
				if d > worst {
					worst, wx, wy = d, x, y
				}
			}
		}
	}
	return worst, wx, wy
}

// withGlass returns s with the hole of each glass painted as an opaque
// fill of its own color, as the glass would cover what is under it, and
// the glass's fills alone, each in a frame with the clips around it.
func withGlass(s *scene.Scene) (*scene.Scene, map[int32]*scene.Scene) {
	whole := *s
	whole.Ops = nil
	alone := map[int32]*scene.Scene{}
	var open []scene.Op
	for _, op := range s.Ops {
		switch op.Kind {
		case scene.OpPushClip:
			open = append(open, op)
		case scene.OpPopClip:
			open = open[:len(open)-1]
		}
		if op.Kind == scene.OpHole && op.Start > 0 {
			fill := op
			fill.Kind, fill.Start = scene.OpFill, 0
			fill.Color = scene.Color{R: uint8(40 * op.Start), G: 200, B: uint8(255 - 50*op.Start), A: 255}
			whole.Ops = append(whole.Ops, fill)
			g := &scene.Scene{Width: s.Width, Height: s.Height}
			g.Ops = append(g.Ops, open...)
			g.Ops = append(g.Ops, fill)
			for range open {
				g.Ops = append(g.Ops, scene.Op{Kind: scene.OpPopClip})
			}
			alone[op.Start] = g
			continue
		}
		whole.Ops = append(whole.Ops, op)
	}
	return &whole, alone
}

// TestLayersDrawTheFrame checks that the layers a frame is split into
// around its glass draw what the frame draws whole: composited over Main
// with each glass between them, under the layers Under says, in paint
// order, they show the frame with the glass where it painted it, over what
// it painted before and under what it painted after over it. Moved by
// whole pixels, what a layer draws is what the frame draws, but for
// rounding as layers composite, a unit at each.
func TestLayersDrawTheFrame(t *testing.T) {
	testLayers(t, false, func(layers int, got, want *Image) string {
		if d, x, y := maxDiff(got, want); d > 2+layers {
			return fmt.Sprintf("differ by %d at %d,%d", d, x, y)
		}
		return ""
	})
}

// TestLayersDrawTurnedOps checks the layers of frames with ops that turn.
// The CPU renderer samples the edges of what turns on a grid, so moving
// them can flip a sample or two of an edge's pixel: few pixels may differ,
// by little more than that, unlike ops out of order, which change whole
// shapes.
func TestLayersDrawTurnedOps(t *testing.T) {
	testLayers(t, true, func(_ int, got, want *Image) string {
		n := 0
		for i := range got.Pix {
			d := int(got.Pix[i]) - int(want.Pix[i])
			if d > 3 || d < -3 {
				n++
			}
			if d > 48 || d < -48 {
				return fmt.Sprintf("differ by %d at %d,%d", d, i%got.Stride/4, i/got.Stride)
			}
		}
		if n > len(got.Pix)/400 {
			return fmt.Sprintf("differ in %d channels", n)
		}
		return ""
	})
}

func testLayers(t *testing.T, turns bool, check func(layers int, got, want *Image) string) {
	var l scene.Layers
	layered := 0
	const frames = 400
	for seed := range uint64(frames) {
		m := newSceneMaker(seed)
		s := m.glassScene(1+int(seed%3), turns)
		if !l.Split(s, 8) {
			t.Fatalf("seed %d: not split", seed)
		}
		if len(l.Layers) > 0 {
			layered++
		}
		whole, alone := withGlass(s)
		want := render(whole)
		got := render(&l.Main)
		for k := 0; k <= len(l.Layers); k++ {
			// The glass under layer k, in paint order, then the layer.
			for _, g := range l.Order {
				if l.Under[g] == k {
					compose(got, render(alone[g+1]), 0, 0)
				}
			}
			if k < len(l.Layers) {
				ly := &l.Layers[k]
				compose(got, render(&ly.Scene), ly.Rect.Min.X, ly.Rect.Min.Y)
			}
		}
		if msg := check(len(l.Layers), got, want); msg != "" {
			t.Fatalf("seed %d: the layers %s from the frame (%d layers, under %v, order %v)%s",
				seed, msg, len(l.Layers), l.Under, l.Order, describe(s))
		}
	}
	if layered < frames/3 {
		t.Errorf("only %d of %d frames made layers", layered, frames)
	}
}

func describe(s *scene.Scene) string {
	out := ""
	for i, op := range s.Ops {
		out += fmt.Sprintf("\n  %d: kind %d rect %v start %d transformed %v", i, op.Kind, op.Rect, op.Start, op.Transform.Set)
	}
	return out
}

// TestLayersKeepMainWhole checks that a frame whose glass nothing paints
// over makes no layer, and that Main is the frame without the glass's
// holes.
func TestLayersKeepMainWhole(t *testing.T) {
	m := newSceneMaker(1)
	s := m.scene()
	n := len(s.Ops)
	s.Ops = append(s.Ops, scene.Op{Kind: scene.OpHole, Rect: scene.Rect{X: 10, Y: 10, W: 40, H: 20}, Start: 1})
	var l scene.Layers
	if !l.Split(s, 4) {
		t.Fatal("not split")
	}
	if len(l.Layers) != 0 || len(l.Main.Ops) != n || l.Under[0] != 0 {
		t.Errorf("%d layers, %d ops in Main of %d, under %v", len(l.Layers), len(l.Main.Ops), n, l.Under)
	}
}

// TestLayersMergeSiblingGlass checks that panes of glass side by side, each
// with its child over it, as a group of buttons, share one layer.
func TestLayersMergeSiblingGlass(t *testing.T) {
	s := &scene.Scene{Width: 200, Height: 60}
	s.Ops = append(s.Ops, scene.Op{Kind: scene.OpFill, Rect: scene.Rect{W: 200, H: 60}, Color: scene.Color{R: 255, A: 255}})
	for i := range 3 {
		x := float32(10 + 60*i)
		s.Ops = append(s.Ops,
			scene.Op{Kind: scene.OpHole, Rect: scene.Rect{X: x, Y: 10, W: 50, H: 40}, Start: int32(i + 1)},
			scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: x + 15, Y: 20, W: 20, H: 20}, Color: scene.Color{B: 255, A: 255}})
	}
	var l scene.Layers
	if !l.Split(s, 4) {
		t.Fatal("not split")
	}
	if len(l.Layers) != 1 || l.Under[0] != 0 || l.Under[1] != 0 || l.Under[2] != 0 {
		t.Fatalf("%d layers, under %v; want one over all the glass", len(l.Layers), l.Under)
	}
	if r := l.Layers[0].Rect; r.Dx() > 160 || r.Dy() > 30 {
		t.Errorf("the layer is at %v, larger than the children with their reach", r)
	}
	if len(l.Main.Ops) != 1 {
		t.Errorf("Main has %d ops, want the background alone", len(l.Main.Ops))
	}
}

// TestLayersStackOverlappingGlass checks that a glass painted over another
// glass's child shows over that child: they need a layer each.
func TestLayersStackOverlappingGlass(t *testing.T) {
	s := &scene.Scene{Width: 100, Height: 100}
	s.Ops = append(s.Ops,
		scene.Op{Kind: scene.OpHole, Rect: scene.Rect{X: 0, Y: 0, W: 60, H: 60}, Start: 1},
		scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 30, Y: 30, W: 20, H: 20}, Color: scene.Color{B: 255, A: 255}},
		scene.Op{Kind: scene.OpHole, Rect: scene.Rect{X: 40, Y: 40, W: 50, H: 50}, Start: 2},
		scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 60, Y: 60, W: 20, H: 20}, Color: scene.Color{G: 255, A: 255}})
	var l scene.Layers
	if !l.Split(s, 4) {
		t.Fatal("not split")
	}
	if len(l.Layers) != 2 || l.Under[0] != 0 || l.Under[1] != 1 {
		t.Fatalf("%d layers, under %v; want a layer over each glass", len(l.Layers), l.Under)
	}
	if !l.Split(s, 1) {
		return
	}
	t.Error("split into more layers than allowed")
}

// TestLayersRefuseReshapedGlass checks that glass under a transform that
// turns it, which no view of the system shows, is not split.
func TestLayersRefuseReshapedGlass(t *testing.T) {
	s := &scene.Scene{Width: 100, Height: 100}
	s.Ops = append(s.Ops, scene.Op{Kind: scene.OpHole, Rect: scene.Rect{X: 10, Y: 10, W: 40, H: 40}, Start: 1, Transform: scene.Rotation(10)})
	var l scene.Layers
	if l.Split(s, 4) {
		t.Error("split glass that turns")
	}
}
