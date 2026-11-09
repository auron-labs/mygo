package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/surface"
)

// holeGlass is the system's glass, as the glass plugin's native glass: a
// hole where the glass shows.
type holeGlass struct{}

func (holeGlass) PaintMaterial(p *Painter, box Rect, radii [4]float32) { p.Hole(box, radii) }
func (holeGlass) NativeGlass() (NativeGlassSpec, bool)                 { return NativeGlassSpec{}, true }

// flatGlassSurface is a test surface showing the system's glass under its
// frames, and glassSurface one showing it in layers too.
type flatGlassSurface struct {
	testSurface
	places [][]platform.GlassPlacement
}

func (s *flatGlassSurface) NativeGlassSupported() bool { return true }
func (s *flatGlassSurface) PlaceGlass(p []platform.GlassPlacement) {
	s.places = append(s.places, slices.Clone(p))
}

type glassSurface struct {
	flatGlassSurface
	layers [][]platform.RectF
	views  []uintptr
	next   uintptr
}

func (s *glassSurface) PlaceGlassLayers(rects []platform.RectF) []platform.SurfaceNative {
	s.layers = append(s.layers, slices.Clone(rects))
	// The views of the layers there were stay; new ones are made.
	for len(s.views) < len(rects) {
		s.next++
		s.views = append(s.views, 100+s.next)
	}
	s.views = s.views[:len(rects)]
	var natives []platform.SurfaceNative
	for _, v := range s.views {
		natives = append(natives, platform.SurfaceNative{View: v, Layer: v})
	}
	return natives
}

// glassGPU is a test GPU renderer that keeps the kinds of the ops of the
// last scene it drew, and whether it presents with the window's changes.
type glassGPU struct {
	testGPU
	native platform.SurfaceNative
	kinds  []scene.Kind
	holes  int
	width  int
	synced bool
}

func (g *glassGPU) Render(s *scene.Scene) error {
	if err := g.testGPU.Render(s); err != nil {
		return err
	}
	g.kinds, g.holes, g.width = g.kinds[:0], 0, s.Width
	for _, op := range s.Ops {
		g.kinds = append(g.kinds, op.Kind)
		if op.Kind == scene.OpHole {
			g.holes++
		}
	}
	return nil
}

func (g *glassGPU) SetSync(on bool) { g.synced = on }

// glassHost returns a window host on s showing view, its GPU renderers,
// the window's first, and a function drawing a frame.
func glassHost(t *testing.T, s platform.Surface, view func(*context)) (*windowHost, *[]*glassGPU, func()) {
	t.Setenv("MYGO_GPU", "")
	var made []*glassGPU
	newGPU = func(n platform.SurfaceNative) (gpuRenderer, error) {
		g := &glassGPU{native: n}
		made = append(made, g)
		return g, nil
	}
	t.Cleanup(func() { newGPU = newGPURenderer })
	h := &windowHost{conn: &surface.Conn{Surface: s}}
	h.rt = newRuntime(view, h)
	return h, &made, func() {
		h.rt.askBuild()
		h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	}
}

// glassView shows a pane of the system's glass with text over it, over a
// colored box, and, when showGlass is false, no glass.
func glassView(showGlass *bool) func(*context) {
	return func(c *context) {
		coreBox(c).Width(200).Height(100).Background(RGB(200, 40, 40)).Children(func() {
			if *showGlass {
				coreBox(c).Absolute().Left(20).Top(20).Width(120).Height(40).Radius(12).Material(holeGlass{}).Children(func() {
					coreText(c, "On glass")
				})
			}
		})
	}
}

func TestGlassDrawsInLayers(t *testing.T) {
	show := true
	s := &glassSurface{}
	h, made, frame := glassHost(t, s, glassView(&show))
	frame()
	if len(*made) != 2 {
		t.Fatalf("%d renderers made, want the window's and a layer's", len(*made))
	}
	main, layer := (*made)[0], (*made)[1]
	if !strings.Contains(h.path, "layers") || main.holes != 0 || main.frames != 1 || layer.frames != 1 {
		t.Fatalf("path %q, %d holes in the window's frame, %d and %d frames", h.path, main.holes, main.frames, layer.frames)
	}
	if !slices.Contains(layer.kinds, scene.OpGlyphs) || slices.Contains(main.kinds, scene.OpGlyphs) {
		t.Errorf("the text is not over the glass: window %v, layer %v", main.kinds, layer.kinds)
	}
	if layer.native.View != 101 || !main.synced || !layer.synced {
		t.Errorf("layer drawn into %v; synced %v and %v", layer.native, main.synced, layer.synced)
	}
	if n := len(s.layers); n != 1 || len(s.layers[0]) != 1 {
		t.Fatalf("layers placed %v", s.layers)
	}
	// The text's ink, with its reach, in the glass.
	if r := s.layers[0][0]; r.X < 12 || r.Y < 12 || r.X+r.W > 148 || r.Y+r.H > 68 || layer.width != int(r.W) {
		t.Errorf("the layer is at %v, %d pixels wide; want around the text, in the glass", r, layer.width)
	}
	p := s.places[len(s.places)-1]
	if len(p) != 1 || !p[0].Over || p[0].Layers != 0 || p[0].Frame != (platform.RectF{X: 20, Y: 20, W: 120, H: 40}) {
		t.Fatalf("glass placed %+v; want over the frame, under the layer", p)
	}

	// The same frame again places nothing again and keeps the renderers.
	frame()
	if len(s.layers) != 1 || len(s.places) != 1 || len(*made) != 2 || layer.frames != 2 {
		t.Errorf("a frame again: %d layer placements, %d glass placements, %d renderers, %d layer frames",
			len(s.layers), len(s.places), len(*made), layer.frames)
	}

	// The glass goes: so do the layers, their renderers and the sync.
	show = false
	frame()
	if got := s.layers[len(s.layers)-1]; got != nil || layer.released != 1 || main.synced {
		t.Errorf("without glass: layers %v, the layer's renderer released %d times, synced %v", got, layer.released, main.synced)
	}
	if p := s.places[len(s.places)-1]; len(p) != 0 {
		t.Errorf("glass placed %+v; want none", p)
	}

	// It comes back, with a renderer of its own again.
	show = true
	frame()
	if len(*made) != 3 || (*made)[2].frames != 1 || !s.places[len(s.places)-1][0].Over {
		t.Errorf("back: %d renderers, glass %+v", len(*made), s.places[len(s.places)-1])
	}
}

// TestGlassOverNothing checks that glass nothing paints over makes no
// layer, but shows over the frame.
func TestGlassOverNothing(t *testing.T) {
	s := &glassSurface{}
	h, made, frame := glassHost(t, s, func(c *context) {
		coreBox(c).Width(200).Height(100).Background(RGB(200, 40, 40)).Children(func() {
			coreBox(c).Absolute().Left(20).Top(20).Width(120).Height(40).Material(holeGlass{})
		})
	})
	frame()
	p := s.places[len(s.places)-1]
	if len(*made) != 1 || (*made)[0].holes != 0 || !strings.Contains(h.path, "layers") || len(p) != 1 || !p[0].Over {
		t.Errorf("%d renderers, %d holes, path %q, glass %+v", len(*made), (*made)[0].holes, h.path, p)
	}
}

// TestGlassLayerFailing checks that a frame whose layer cannot be drawn
// draws whole, the glass under it.
func TestGlassLayerFailing(t *testing.T) {
	show := true
	s := &glassSurface{}
	h, made, frame := glassHost(t, s, glassView(&show))
	newGPU = func(n platform.SurfaceNative) (gpuRenderer, error) {
		if n.View != 0 {
			return nil, errors.New("no layer for you")
		}
		g := &glassGPU{native: n}
		*made = append(*made, g)
		return g, nil
	}
	frame()
	main := (*made)[0]
	if len(*made) != 1 || main.holes != 1 || main.synced || strings.Contains(h.path, "layers") {
		t.Fatalf("%d renderers, %d holes, synced %v, path %q; want the frame whole", len(*made), main.holes, main.synced, h.path)
	}
	if p := s.places[len(s.places)-1]; len(p) != 1 || p[0].Over {
		t.Errorf("glass placed %+v; want under the frame", p)
	}
	if got := s.layers[len(s.layers)-1]; got != nil {
		t.Errorf("layers %v left", got)
	}
}

// TestGlassWithoutLayers checks that a surface that shows the glass under
// its frames alone gets the frame whole, with its hole.
func TestGlassWithoutLayers(t *testing.T) {
	show := true
	s := &flatGlassSurface{}
	_, made, frame := glassHost(t, s, glassView(&show))
	frame()
	if len(*made) != 1 || (*made)[0].holes != 1 {
		t.Fatalf("%d renderers, %d holes", len(*made), (*made)[0].holes)
	}
	if p := s.places[len(s.places)-1]; len(p) != 1 || p[0].Over || p[0].Layers != 0 {
		t.Errorf("glass placed %+v", p)
	}
}

// TestGlassLayersRenew checks that a surface making its native objects
// again (SurfaceRenew) gets renderers for its layers anew.
func TestGlassLayersRenew(t *testing.T) {
	show := true
	s := &glassSurface{}
	h, made, frame := glassHost(t, s, glassView(&show))
	frame()
	h.event(platform.SurfaceEvent{Kind: platform.SurfaceRenew})
	frame()
	if len(*made) != 4 || (*made)[0].released != 1 || (*made)[1].released != 1 || (*made)[3].frames != 1 {
		t.Errorf("after renewing: %d renderers made, the old ones released %d and %d times",
			len(*made), (*made)[0].released, (*made)[1].released)
	}
}

// TestGlassZoomed checks that the glass of a zoomed window is placed in
// the window's DIPs, as its web views are.
func TestGlassZoomed(t *testing.T) {
	rt := &engine{contentZoom: 2}
	places := rt.windowGlass([]platform.GlassPlacement{{Frame: platform.RectF{X: 10, Y: 5, W: 20, H: 8}, Clip: platform.RectF{X: 10, Y: 5, W: 20, H: 8},
		Radii: [4]float64{4, 4, 4, 4}, CornerRadius: 4}})
	if p := places[0]; p.Frame != (platform.RectF{X: 20, Y: 10, W: 40, H: 16}) || p.Clip != p.Frame || p.CornerRadius != 8 || p.Radii[0] != 8 {
		t.Errorf("zoomed glass placed %+v", p)
	}
}
