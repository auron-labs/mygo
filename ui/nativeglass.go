package ui

import (
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/scene"
)

// NativeGlassSpec is how a NativeGlassMaterial asks for the system's glass:
// its style (0 regular, 1 clear) and tint, A 0 for none.
type NativeGlassSpec struct {
	Style int
	Tint  Color
}

// NativeGlassMaterial is a Material drawn by the system where it has
// Liquid Glass (macOS 26 and later), instead of by the renderer: the
// material leaves a hole in the frame (Painter.Hole) where the system's
// glass view shows. A window drawing on the GPU draws the frame in layers
// around the glass, which then frosts what the elements under it painted,
// and shows under what paints after it over it, its children; elsewhere
// the glass shows under the frame, through the hole, where it frosts what
// is under the window's frames, as a ui.WebView's page.
type NativeGlassMaterial interface {
	Material
	// NativeGlass asks for the system's glass, or reports false for the
	// material's own drawing.
	NativeGlass() (NativeGlassSpec, bool)
}

// noteNativeGlass notes where the frame shows the system's glass of e,
// which the surface places under it (Surface.PlaceGlass).
func (rt *engine) noteNativeGlass(e *node, clip Rect, invisible bool, spec NativeGlassSpec) {
	if invisible || rt.commitHidden {
		return
	}
	b := e.visualRect(Rect{X: e.x, Y: e.y, W: e.w, H: e.h})
	if b.W <= 0 || b.H <= 0 {
		return
	}
	v := intersect(b, clip)
	if v.W <= 0 || v.H <= 0 {
		return
	}
	var tint *platform.Color
	if spec.Tint.A > 0 {
		tint = &platform.Color{R: spec.Tint.R, G: spec.Tint.G, B: spec.Tint.B, A: spec.Tint.A}
	}
	r := e.radius
	rt.glassPlaces = append(rt.glassPlaces, platform.GlassPlacement{
		ID:           e.id,
		Frame:        rectF(b),
		Clip:         rectF(v),
		Radii:        [4]float64{float64(r[0]), float64(r[1]), float64(r[2]), float64(r[3])},
		Style:        spec.Style,
		Tint:         tint,
		CornerRadius: float64(max(r[0], r[1], r[2], r[3])),
	})
}

// placeNativeGlass tells the host where the frame shows the system's glass,
// in the window's DIPs.
func (rt *engine) placeNativeGlass() {
	if len(rt.glassPlaces) == 0 && !rt.glassShown {
		return
	}
	rt.glassShown = len(rt.glassPlaces) > 0
	rt.host.placeGlass(rt.windowGlass(rt.glassPlaces))
}

// windowGlass maps where the frame shows the system's glass to the
// window's DIPs, in place: places is the frame's own (glassPlaces).
func (rt *engine) windowGlass(places []platform.GlassPlacement) []platform.GlassPlacement {
	if !rt.zoomed() {
		return places
	}
	z := float64(rt.contentZoom)
	for i := range places {
		p := &places[i]
		p.Frame, p.Clip = rt.toWindow(p.Frame), rt.toWindow(p.Clip)
		for j := range p.Radii {
			p.Radii[j] *= z
		}
		p.CornerRadius *= z
	}
	return places
}

// glassIndex returns 1 + the index among the frame's glass of the system's
// glass of e, which its hole marks (scene.OpHole's Start), or 0 for none.
func (rt *engine) glassIndex(e *node) int32 {
	for i := range rt.glassPlaces {
		if rt.glassPlaces[i].ID == e.id {
			return int32(i + 1)
		}
	}
	return 0
}

// NativeGlassSupported reports whether the window's surface draws the
// system's glass, as macOS 26's NSGlassEffectView does.
func (p *Painter) NativeGlassSupported() bool { return p.rt.host.nativeGlass() }

// Hole leaves a rounded hole in the frame, through which what is under the
// window's surface shows: the page of a ui.WebView, or a system's glass
// view that a NativeGlassMaterial places. Its own ink, as a material's,
// paints nothing there. The hole a NativeGlassMaterial leaves as it paints
// marks where its glass shows, which a window drawing its frame in layers
// around the glass draws as none.
func (p *Painter) Hole(r Rect, radii [4]float32) {
	if r.W <= 0 || r.H <= 0 || p.opacity == 0 {
		return
	}
	if p.glass != 0 {
		useGlassLayers()
	}
	p.add(scene.Op{Kind: scene.OpHole, Rect: p.snap(r), Radii: p.radii(radii), Continuous: continuousCorners, Opacity: p.opacity,
		Start: p.glass})
	p.glass = 0 // one hole marks the glass
	if c := intersect(transformRect(p.transform, r), p.clip); c.W > 0 && c.H > 0 {
		p.holes = append(p.holes, c)
	}
}
