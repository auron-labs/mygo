//go:build darwin

package darwin

import (
	"slices"
	"sync"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/platform"
)

// glassView is a system's glass view the content shows (PlaceGlass), in a
// view cutting it to the part of it that shows, as the clips around its
// element cut the hole it leaves.
type glassView struct {
	clip, view id
}

// overGlass stacks the glass over the surface, between the layers of its
// frames (stackOver). PlaceGlassLayers sets it, as frames first draw in
// layers, so that apps whose frames never do link none of it.
var overGlass func(s *surface, places []platform.GlassPlacement)

var passViewOnce sync.Once

// registerPassViewClass registers MyGoPassView, a view that takes no
// input: the pointer goes through it, and what is in it, to the views
// under it, as the content's surface under the glass and the layers.
func registerPassViewClass() {
	classDef("MyGoPassView", "NSView", nil, []objc.MethodDef{
		method("hitTest:", func(self id, _ objc.SEL, p NSPoint) id { return 0 }),
		method("accessibilityChildren", func(self id, _ objc.SEL) id { return nsArray() }),
		method("isAccessibilityElement", func(self id, _ objc.SEL) bool { return false }),
	})
}

// PlaceGlass shows the system's glass where the content shows it: under
// the surface, above the web views, so that the content paints its
// children and border over it; or, Over, over the surface, between the
// layers of the frame (PlaceGlassLayers), so that it frosts what the
// surface drew.
func (s *surface) PlaceGlass(places []platform.GlassPlacement) {
	host := s.w
	if host.closed {
		return
	}
	height := msgRect(host.view, sel("bounds")).Size.Height
	if s.glass == nil {
		s.glass = map[uint64]*glassView{}
	}
	over := false
	shown := make([]*glassView, 0, len(places))
	withPool(func() {
		for _, p := range places {
			if p.Clip.W <= 0 || p.Clip.H <= 0 {
				continue
			}
			over = over || p.Over
			g := s.glass[p.ID]
			if g == nil {
				g = &glassView{
					clip: msgInitRect(send(class("NSView"), "alloc"), sel("initWithFrame:"), NSRect{}),
					view: msgInitRect(send(class("NSGlassEffectView"), "alloc"), sel("initWithFrame:"), NSRect{}),
				}
				send(g.clip, "setWantsLayer:", 1)
				send(g.clip, "addSubview:", uintptr(g.view))
				s.glass[p.ID] = g
			}
			// The clip in the content view, the glass in the clip, neither
			// flipped.
			c, f := p.Clip, p.Frame
			// Cut only where the clips around it cut it: the glass's
			// shadow reaches past its frame.
			send(send(g.clip, "layer"), "setMasksToBounds:", boolArg(c != f))
			msgSetRect(g.clip, sel("setFrame:"), NSRect{Origin: NSPoint{c.X, height - c.Y - c.H}, Size: NSSize{c.W, c.H}})
			msgSetRect(g.view, sel("setFrame:"), NSRect{Origin: NSPoint{f.X - c.X, (c.Y + c.H) - (f.Y + f.H)}, Size: NSSize{f.W, f.H}})
			send(g.view, "setStyle:", uintptr(p.Style))
			msgSetFloat(g.view, sel("setCornerRadius:"), p.CornerRadius)
			if p.Tint == nil {
				send(g.view, "setTintColor:", 0)
			} else {
				send(g.view, "setTintColor:", uintptr(nsColor(*p.Tint)))
			}
			shown = append(shown, g)
		}
		for id, g := range s.glass {
			if !slices.Contains(shown, g) {
				g.remove()
				delete(s.glass, id)
			}
		}
		s.glassShown = s.glassShown[:0]
		for _, g := range shown {
			s.glassShown = append(s.glassShown, g.clip)
		}
		if over != s.glassOver {
			// From under the surface to over it, or back.
			for _, g := range shown {
				send(g.clip, "removeFromSuperview")
			}
			s.glassOver = over
		}
		if over && overGlass != nil {
			overGlass(s, places)
		} else {
			s.stackGlass(s.glassShown, false)
			if s.stack != 0 && len(s.layers) == 0 {
				// Nothing over the surface any more.
				send(s.stack, "removeFromSuperview")
				release(s.stack)
				s.stack = 0
			}
		}
	})
}

// remove takes the glass out of its window and lets go of it.
func (g *glassView) remove() {
	send(g.clip, "removeFromSuperview")
	release(g.view)
	release(g.clip)
}

// stackGlass puts the clips of the glass shown in the order the content
// painted them, under the surface and above the web views, unless they
// are.
func (s *surface) stackGlass(shown []id, force bool) {
	if len(shown) == 0 {
		return
	}
	if !force {
		subviews := send(s.w.view, "subviews")
		n := sendInt(subviews, "count")
		at := 0
		for i := range n {
			v := send(subviews, "objectAtIndex:", uintptr(i))
			if slices.Contains(shown, v) {
				if at >= len(shown) || v != shown[at] {
					at = -1
					break
				}
				at++
			}
		}
		if at == len(shown) {
			return
		}
	}
	for _, v := range shown {
		retain(v)
		send(v, "removeFromSuperview")
		send(s.w.view, "addSubview:positioned:relativeTo:", uintptr(v), nsWindowBelow, uintptr(s.view))
		release(v)
	}
}

// stackOver puts the glass over the surface, in the view holding the
// layers: before each layer, the glass under it and over those before, in
// the order of places, then the glass over all of them.
func (s *surface) stackOver(places []platform.GlassPlacement) {
	stack := s.glassStack()
	order := make([]id, 0, len(places)+len(s.layers))
	next := 0 // the next layer
	for _, p := range places {
		g := s.glass[p.ID]
		if g == nil || p.Clip.W <= 0 || p.Clip.H <= 0 {
			continue
		}
		for next < len(s.layers) && next < p.Layers {
			order = append(order, s.layers[next])
			next++
		}
		order = append(order, g.clip)
	}
	order = append(order, s.layers[next:]...)
	s.setStack(stack, order)
}

// glassStack returns the view holding the glass and the layers over the
// surface, making it first.
func (s *surface) glassStack() id {
	if s.stack != 0 {
		return s.stack
	}
	host := s.w.view
	passViewOnce.Do(registerPassViewClass)
	s.stack = msgInitRect(send(class("MyGoPassView"), "alloc"), sel("initWithFrame:"), msgRect(host, sel("bounds")))
	send(s.stack, "setAutoresizingMask:", nsViewWidthHeightSizable)
	send(host, "addSubview:positioned:relativeTo:", uintptr(s.stack), nsWindowAbove, uintptr(s.view))
	return s.stack
}

// setStack makes views the subviews of stack, in that order from the
// bottom, unless they are.
func (s *surface) setStack(stack id, views []id) {
	subviews := send(stack, "subviews")
	n := sendInt(subviews, "count")
	if n == len(views) {
		same := true
		for i := range n {
			if send(subviews, "objectAtIndex:", uintptr(i)) != views[i] {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	send(stack, "setSubviews:", uintptr(nsArray(views...)))
}

// PlaceGlassLayers shows the layers of the frame over the glass under them
// at rects, and returns their views and layers for renderers to draw into.
func (s *surface) PlaceGlassLayers(rects []platform.RectF) []platform.SurfaceNative {
	host := s.w
	if host.closed {
		return nil
	}
	overGlass = (*surface).stackOver
	var natives []platform.SurfaceNative
	withPool(func() {
		for len(s.layers) > len(rects) {
			v := s.layers[len(s.layers)-1]
			send(v, "removeFromSuperview")
			release(v)
			s.layers = s.layers[:len(s.layers)-1]
		}
		if len(rects) == 0 {
			if s.stack != 0 && !s.glassOver {
				send(s.stack, "removeFromSuperview")
				release(s.stack)
				s.stack = 0
			}
			return
		}
		stack := s.glassStack()
		height := msgRect(host.view, sel("bounds")).Size.Height
		for len(s.layers) < len(rects) {
			v := msgInitRect(send(class("NSView"), "alloc"), sel("initWithFrame:"), NSRect{})
			send(v, "setWantsLayer:", 1)
			send(stack, "addSubview:", uintptr(v))
			s.layers = append(s.layers, v)
		}
		natives = make([]platform.SurfaceNative, len(rects))
		for i, r := range rects {
			v := s.layers[i]
			msgSetRect(v, sel("setFrame:"), NSRect{Origin: NSPoint{r.X, height - r.Y - r.H}, Size: NSSize{r.W, r.H}})
			natives[i] = platform.SurfaceNative{View: uintptr(v), Layer: uintptr(send(v, "layer"))}
		}
	})
	return natives
}

// destroyGlass takes the glass, the layers and the view holding them out
// of the window.
func (s *surface) destroyGlass() {
	for _, g := range s.glass {
		g.remove()
	}
	s.glass, s.glassShown = nil, nil
	for _, v := range s.layers {
		send(v, "removeFromSuperview")
		release(v)
	}
	s.layers = nil
	if s.stack != 0 {
		send(s.stack, "removeFromSuperview")
		release(s.stack)
		s.stack = 0
	}
}
