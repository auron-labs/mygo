package ui

import (
	"log"
	"slices"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/scene"
)

// maxGlassLayers is how many layers a frame showing the system's glass is
// drawn in over the surface's frame, each with a GPU renderer of its own.
// Glass that its children cover, as a toolbar's buttons, takes one, as do
// panes beside each other; a frame needing more, as panes stacked on each
// other, draws whole, the glass under it showing through its holes.
const maxGlassLayers = 4

// glassLayers draws the frames of a window showing the system's glass in
// layers around it (scene.Layers, platform.GlassLayerSurface), so that the
// glass, over the surface's frame, frosts the elements painted under it,
// and the elements painted over it show over it.
type glassLayers struct {
	split scene.Layers
	// on tells that the last frame drew in layers; rects are where the
	// layers show, in the window's DIPs, and natives what the surface
	// gave for them, which gpus draw into.
	on      bool
	rects   []platform.RectF
	natives []platform.SurfaceNative
	gpus    []gpuRenderer
	wide    []bool
	// under and order are the last frame's split.Under and split.Order,
	// by which the glass is placed between the layers.
	under []int
	order []int32
	// places are the glass the engine placed last, in the window's DIPs,
	// which the host places again as the layers change.
	places []platform.GlassPlacement
}

// glassLayering draws a frame in layers around the system's glass it
// shows (presentLayered), and glassLayered places the glass between them
// (glassLayers.layered). Painter.Hole sets them as it first marks glass,
// so that apps showing none link none of it.
var (
	glassLayering func(h *windowHost, s *scene.Scene) bool
	glassLayered  func(g *glassLayers, places []platform.GlassPlacement) []platform.GlassPlacement
)

// useGlassLayers has windows draw their frames in layers around the
// system's glass.
func useGlassLayers() {
	if glassLayering == nil {
		glassLayering, glassLayered = (*windowHost).presentLayered, (*glassLayers).layered
	}
}

// syncRenderer is a GPU renderer that can present its frames with the
// window's other changes (Metal's, with Core Animation's transaction), as
// frames drawn in layers between views of the system must.
type syncRenderer interface{ SetSync(on bool) }

// presentLayered draws s in layers around the system's glass it shows,
// where the surface can, and reports whether it did. Otherwise, the glass
// goes back under the surface, and the caller draws s whole.
func (h *windowHost) presentLayered(s *scene.Scene) bool {
	g := &h.glass
	ls, ok := h.conn.Surface.(platform.GlassLayerSurface)
	if !ok || h.gpu == nil || !scene.HasGlass(s) || !g.split.Split(s, maxGlassLayers) {
		h.unlayer()
		return false
	}
	// Where the layers show, in the window's DIPs: the frame's pixels are
	// the window's, Scale/Zoom a DIP.
	k := float64(s.Scale)
	if k <= 0 {
		k = 1
	}
	if s.Zoom > 0 {
		k /= float64(s.Zoom)
	}
	rects := make([]platform.RectF, len(g.split.Layers))
	for i, l := range g.split.Layers {
		r := l.Rect
		rects[i] = platform.RectF{X: float64(r.Min.X) / k, Y: float64(r.Min.Y) / k, W: float64(r.Dx()) / k, H: float64(r.Dy()) / k}
	}
	if !g.on || len(g.natives) != len(rects) || !slices.Equal(rects, g.rects) {
		natives := ls.PlaceGlassLayers(rects)
		if len(natives) != len(rects) {
			h.unlayer()
			return false
		}
		g.setNatives(natives)
		g.rects = append(g.rects[:0], rects...)
	}
	for i, n := range g.natives {
		if g.gpus[i] != nil {
			continue
		}
		r, err := newGPU(n)
		if err != nil || r == nil || software(r) {
			if err != nil {
				log.Printf("mygo: drawing the glass's layers whole: %v", err)
			}
			if r != nil {
				r.Release()
			}
			h.unlayer()
			return false
		}
		g.gpus[i], g.wide[i] = r, false
	}
	// The surface's frame and the layers present with the glass's views,
	// in one transaction of the system's, so that they show together.
	if r, ok := h.gpu.(syncRenderer); ok {
		r.SetSync(true)
	}
	if !h.render(&g.split.Main) {
		h.unlayer()
		return false
	}
	for i := range g.split.Layers {
		r := g.gpus[i]
		if w, ok := r.(wideRenderer); ok && g.wide[i] != h.wide {
			g.wide[i] = w.SetWide(h.wide)
		}
		if sr, ok := r.(syncRenderer); ok {
			sr.SetSync(true)
		}
		if err := r.Render(&g.split.Layers[i].Scene); err != nil {
			log.Printf("mygo: drawing the glass's layers whole: %v", err)
			h.unlayer()
			// The surface's frame, whole, over the glass under it again.
			return false
		}
	}
	changed := !g.on || !slices.Equal(g.under, g.split.Under) || !slices.Equal(g.order, g.split.Order)
	g.on = true
	g.under = append(g.under[:0], g.split.Under...)
	g.order = append(g.order[:0], g.split.Order...)
	if changed && g.places != nil {
		// A frame painted again, without placing its glass, as it moves.
		h.sendGlass()
	}
	return true
}

// unlayer stops drawing frames in layers: the layers go, and the glass
// shows under the surface again.
func (h *windowHost) unlayer() {
	g := &h.glass
	if r, ok := h.gpu.(syncRenderer); ok {
		r.SetSync(false)
	}
	placed := g.on || len(g.natives) > 0
	g.releaseGPUs()
	g.natives = g.natives[:0]
	if placed {
		if ls, ok := h.conn.Surface.(platform.GlassLayerSurface); ok {
			ls.PlaceGlassLayers(nil)
		}
	}
	if !g.on {
		return
	}
	g.on = false
	g.rects, g.under, g.order = g.rects[:0], g.under[:0], g.order[:0]
	if g.places != nil {
		h.sendGlass()
	}
}

// setNatives takes the layers the surface gave, keeping the renderers of
// those it gave before.
func (g *glassLayers) setNatives(natives []platform.SurfaceNative) {
	gpus := make([]gpuRenderer, len(natives))
	wide := make([]bool, len(natives))
	for i, n := range natives {
		if i < len(g.natives) && g.natives[i] == n && i < len(g.gpus) {
			gpus[i], wide[i] = g.gpus[i], g.wide[i]
			g.gpus[i] = nil
		}
	}
	g.releaseGPUs()
	g.gpus, g.wide = gpus, wide
	g.natives = append(g.natives[:0], natives...)
}

func (g *glassLayers) releaseGPUs() {
	for i, r := range g.gpus {
		if r != nil {
			r.Release()
			g.gpus[i] = nil
		}
	}
	g.gpus = g.gpus[:0]
}

// layered returns places as the surface shows them: over the frame,
// between the layers under and over each, in the order the frame painted
// them, while the frame draws in layers. places is not changed.
func (g *glassLayers) layered(places []platform.GlassPlacement) []platform.GlassPlacement {
	if !g.on {
		return places
	}
	out := make([]platform.GlassPlacement, 0, len(places))
	at := make([]int, len(places)) // the place of each in paint order
	for i := range at {
		at[i] = len(g.order) + i // not painted: after those painted
	}
	for k, i := range g.order {
		if int(i) < len(at) {
			at[i] = k
		}
	}
	for i, p := range places {
		p.Over = true
		if i < len(g.under) {
			p.Layers = g.under[i]
		}
		out = append(out, p)
	}
	idx := make([]int, len(out))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		if out[a].Layers != out[b].Layers {
			return out[a].Layers - out[b].Layers
		}
		return at[a] - at[b]
	})
	sorted := make([]platform.GlassPlacement, len(out))
	for k, i := range idx {
		sorted[k] = out[i]
	}
	return sorted
}
