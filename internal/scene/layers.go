package scene

import (
	"image"
	"math"
)

// Layers splits a frame around the system's glass it shows (an OpHole
// whose Start is set), for a window that draws it as views of the system
// between layers of its own, as macOS's NSGlassEffectView: Main holds what
// the glass shows through, what the frame painted before the glass and
// what it painted after that lies elsewhere, and each of Layers what it
// painted after the glass over it, in a smaller frame of its own that
// shows over the glass. The glass then frosts and bends what the frame
// painted under it, rather than what is under the window's frames.
//
// Compositing the layers over Main, each at its Rect, draws what drawing
// the frame whole draws with the glass's holes left out, as the ops keep
// their order wherever they overlap. Effects that read their backdrop in
// a layer see that layer's alone, as they see none of the page through a
// hole; a web view's hole in a layer shows what Main and the glass show
// under it.
type Layers struct {
	Main   Scene
	Layers []Layer
	// Under holds, for the glass of the OpHole whose Start is i + 1, how
	// many of Layers are under it: it shows over Main and those, and under
	// the others. A glass the frame does not paint is under none.
	Under []int
	// Order holds the glass the frame paints (the OpHole's Start - 1) in
	// the order it paints them, which is their order where they overlap
	// among those under the same layers.
	Order []int32

	// level is the level of each op of the scene: 0 for Main, or the
	// place in paint order of the last glass it shows over (1 for the
	// first). levelOf maps levels to Layers, -1 for none.
	level   []int32
	levelOf []int32
	// starts holds the first level of each level's layer, and before how
	// many layers are under the layer starting at a level.
	starts, before []int32
	held           []Rect
	// glass are the boxes of the glass in paint order, and boxes, by
	// level, those of the ops on each level.
	glass []Rect
	boxes [][]Rect
	clips []Rect
	rects []Rect
}

// Layer is a part of a frame that shows over the glass under it.
type Layer struct {
	Scene Scene
	// Rect is where it shows in the frame, in its pixels.
	Rect image.Rectangle
}

// maxBoxes is how many boxes of its ops a level keeps before it keeps
// their union: what is tested against those is drawn over the glass,
// in the layer, which a box too large only makes larger.
const maxBoxes = 16

// HasGlass reports whether s shows the system's glass (an OpHole whose
// Start is set).
func HasGlass(s *Scene) bool {
	for i := range s.Ops {
		if s.Ops[i].Kind == OpHole && s.Ops[i].Start > 0 {
			return true
		}
	}
	return false
}

// Split splits s around its glass into at most max layers over Main, and
// reports whether it could. It cannot when a hole of glass is drawn under
// a transform that does more than move it, which no view of the system
// shows, or when the frame needs more layers than max: the caller then
// draws s whole, the glass under it showing through its holes. Main and
// Layers hold what s holds until s changes.
func (l *Layers) Split(s *Scene, max int) bool {
	l.reset(s)
	if !l.assign(s) {
		return false
	}
	n := l.group(s)
	if n > max {
		return false
	}
	l.build(s, n)
	return true
}

func (l *Layers) reset(s *Scene) {
	l.level = l.level[:0]
	l.glass, l.Order = l.glass[:0], l.Order[:0]
	l.boxes = l.boxes[:0]
	l.Under = l.Under[:0]
	n := int32(0)
	for i := range s.Ops {
		if op := &s.Ops[i]; op.Kind == OpHole && op.Start > n {
			n = op.Start
		}
	}
	for range n {
		l.Under = append(l.Under, 0)
	}
}

// assign finds the level of each op: the place in paint order of the last
// glass under it, at the least, as it shows over the glass painted before
// it that it overlaps, and over the ops it overlaps. An op over none stays
// in Main, the frame under all the glass, at level 0, as do the holes of
// glass and the clips, which every layer keeps.
func (l *Layers) assign(s *Scene) bool {
	clip := Rect{0, 0, float32(s.Width), float32(s.Height)}
	l.clips = l.clips[:0]
	l.nextBoxes() // Main's, which nothing tests
	for i := range s.Ops {
		op := &s.Ops[i]
		switch op.Kind {
		case OpPushClip:
			l.clips = append(l.clips, clip)
			clip = clip.Intersect(clipBounds(op))
			l.level = append(l.level, 0)
			continue
		case OpPopClip:
			if n := len(l.clips); n > 0 {
				clip = l.clips[n-1]
				l.clips = l.clips[:n-1]
			}
			l.level = append(l.level, 0)
			continue
		}
		b := outsetRect(drawnBounds(s, op), reachOf(op)).Intersect(clip)
		if op.Kind == OpHole && op.Start > 0 {
			if op.Transform.Set && !op.Transform.moves() {
				return false
			}
			l.glass = append(l.glass, b)
			l.Order = append(l.Order, op.Start-1)
			l.nextBoxes()
			l.level = append(l.level, 0)
			continue
		}
		level := int32(0)
		if !b.Empty() {
		find:
			for j := len(l.glass); j >= 1; j-- {
				if overlaps(l.glass[j-1], b) {
					level = int32(j)
					break
				}
				for _, o := range l.boxes[j] {
					if overlaps(o, b) {
						level = int32(j)
						break find
					}
				}
			}
		}
		l.level = append(l.level, level)
		if level > 0 {
			l.addBox(int(level), b)
		}
	}
	return true
}

// nextBoxes adds an empty list of boxes for the next level.
func (l *Layers) nextBoxes() {
	n := len(l.boxes)
	if n < cap(l.boxes) {
		l.boxes = l.boxes[:n+1]
		l.boxes[n] = l.boxes[n][:0]
		return
	}
	l.boxes = append(l.boxes, nil)
}

// addBox adds b to the boxes of the ops of level, or to their union once
// it holds maxBoxes.
func (l *Layers) addBox(level int, b Rect) {
	boxes := l.boxes[level]
	if len(boxes) < maxBoxes {
		l.boxes[level] = append(boxes, b)
		return
	}
	u := boxes[0]
	for _, o := range boxes[1:] {
		u = union(u, o)
	}
	l.boxes[level] = append(boxes[:0], union(u, b))
}

// group merges the levels into layers: the level of a glass goes in the
// layer of the levels before it unless an op of that layer overlaps the
// glass, which must show over it. Each glass then shows under the layer of
// its level, over those before, and over all of those when its layer has
// no ops, which makes none. It returns how many layers it makes.
func (l *Layers) group(s *Scene) int {
	n := len(l.glass)
	l.levelOf = resize(l.levelOf, n+1)
	l.starts = resize(l.starts, n+1)
	l.before = resize(l.before, n+1)
	start := int32(0)
	l.held = l.held[:0]
	for j := 1; j <= n; j++ {
		split := start == 0
		for _, o := range l.held {
			if overlaps(o, l.glass[j-1]) {
				split = true
				break
			}
		}
		if split {
			start = int32(j)
			l.held = l.held[:0]
		}
		l.starts[j] = start
		l.held = append(l.held, l.boxes[j]...)
	}
	// The layers whose levels have ops, numbered in order.
	for j := range l.levelOf {
		l.levelOf[j] = -1
	}
	for i, lv := range l.level {
		if lv > 0 && s.Ops[i].Kind != OpPushClip && s.Ops[i].Kind != OpPopClip {
			l.levelOf[l.starts[lv]] = 0 // has ops; numbered below
		}
	}
	layers := int32(0)
	for j := 1; j <= n; j++ {
		if l.starts[j] != int32(j) {
			continue
		}
		l.before[j] = layers
		if l.levelOf[j] == 0 {
			l.levelOf[j] = layers
			layers++
		}
	}
	for j := 1; j <= n; j++ {
		st := l.starts[j]
		if g := l.Order[j-1]; int(g) < len(l.Under) {
			l.Under[g] = int(l.before[st])
		}
		if j != int(st) {
			l.levelOf[j] = l.levelOf[st]
		}
	}
	return int(layers)
}

// resize returns s with n elements, reusing its memory.
func resize(s []int32, n int) []int32 {
	if cap(s) < n {
		return make([]int32, n)
	}
	return s[:n]
}

// build makes Main and the n layers.
func (l *Layers) build(s *Scene, n int) {
	m := &l.Main
	m.Width, m.Height, m.Scale, m.Zoom = s.Width, s.Height, s.Scale, s.Zoom
	m.Clear, m.Background = s.Clear, s.Background
	m.Glyphs, m.Stops, m.Effects, m.Words, m.Wide = s.Glyphs, s.Stops, s.Effects, s.Words, s.Wide
	m.Text, m.MaskAtlas, m.ColorAtlas = s.Text, s.MaskAtlas, s.ColorAtlas
	m.Canvases, m.Drawn = s.Canvases, s.Drawn
	clear(m.Ops)
	m.Ops = m.Ops[:0]
	for len(l.Layers) < n {
		l.Layers = append(l.Layers, Layer{})
	}
	for i := n; i < len(l.Layers); i++ {
		l.Layers[i].Scene.Reset(0, 0, Color{})
	}
	l.Layers = l.Layers[:n]
	// Where each layer shows: its ops' boxes, on whole pixels.
	l.rects = append(l.rects[:0], make([]Rect, n)...)
	rects := l.rects
	clip := Rect{0, 0, float32(s.Width), float32(s.Height)}
	l.clips = l.clips[:0]
	for i := range s.Ops {
		op := &s.Ops[i]
		switch op.Kind {
		case OpPushClip:
			l.clips = append(l.clips, clip)
			clip = clip.Intersect(clipBounds(op))
			continue
		case OpPopClip:
			if k := len(l.clips); k > 0 {
				clip = l.clips[k-1]
				l.clips = l.clips[:k-1]
			}
			continue
		}
		if lv := l.level[i]; lv > 0 && !(op.Kind == OpHole && op.Start > 0) {
			if k := l.levelOf[lv]; k >= 0 {
				if b := outsetRect(drawnBounds(s, op), reachOf(op)).Intersect(clip); !b.Empty() {
					rects[k] = union(rects[k], b)
				}
			}
		}
	}
	frame := image.Rect(0, 0, s.Width, s.Height)
	for k := range l.Layers {
		r := rects[k]
		px := image.Rect(int(math.Floor(float64(r.X))), int(math.Floor(float64(r.Y))),
			int(math.Ceil(float64(r.X+r.W))), int(math.Ceil(float64(r.Y+r.H)))).Intersect(frame)
		ly := &l.Layers[k]
		ly.Rect = px
		d := &ly.Scene
		d.Reset(px.Dx(), px.Dy(), Color{})
		d.Scale, d.Zoom = s.Scale, s.Zoom
		d.Background = Color{}
		d.Stops, d.Words, d.Wide = s.Stops, s.Words, s.Wide
		d.Text, d.MaskAtlas, d.ColorAtlas = s.Text, s.MaskAtlas, s.ColorAtlas
	}
	for i := range s.Ops {
		op := &s.Ops[i]
		switch {
		case op.Kind == OpPushClip || op.Kind == OpPopClip:
			// Every layer keeps the clips, so that its ops are cut as
			// they would be in the frame.
			m.Ops = append(m.Ops, *op)
			for k := range l.Layers {
				ly := &l.Layers[k]
				ly.Scene.Ops = append(ly.Scene.Ops, moved(s, &ly.Scene, *op, ly.Rect.Min))
			}
		case op.Kind == OpHole && op.Start > 0:
			// The glass, which the window shows between the layers.
		case l.level[i] == 0 || l.levelOf[l.level[i]] < 0:
			m.Ops = append(m.Ops, *op)
		default:
			ly := &l.Layers[l.levelOf[l.level[i]]]
			ly.Scene.Ops = append(ly.Scene.Ops, moved(s, &ly.Scene, *op, ly.Rect.Min))
		}
	}
}

// moved returns op of s for the layer d, whose frame starts at origin in
// that of s: its glyphs and effect in d, and its geometry moved by
// -origin, which only folds into the op's transform, or its rectangles
// where it has none, so that it draws the pixels it would draw in s.
func moved(s, d *Scene, op Op, origin image.Point) Op {
	dx, dy := -float32(origin.X), -float32(origin.Y)
	switch op.Kind {
	case OpGlyphs:
		start := int32(len(d.Glyphs))
		d.Glyphs = append(d.Glyphs, s.Glyphs[op.Start:op.End]...)
		if !op.Transform.Set {
			moveGlyphs(d.Glyphs[start:], dx, dy)
		}
		op.Start, op.End = start, int32(len(d.Glyphs))
	case OpEffect:
		e := s.Effects[op.Start]
		if e.Masked() {
			start := int32(len(d.Glyphs))
			d.Glyphs = append(d.Glyphs, s.Glyphs[e.Glyphs[0]:e.Glyphs[1]]...)
			if !op.Transform.Set {
				moveGlyphs(d.Glyphs[start:], dx, dy)
			}
			e.Glyphs = [2]int32{start, int32(len(d.Glyphs))}
		}
		if !op.Transform.Set {
			e.Origin = [2]float32{e.Origin[0] + dx, e.Origin[1] + dy}
		}
		op.Start = int32(len(d.Effects))
		d.Effects = append(d.Effects, e)
	}
	if op.Transform.Set {
		op.Transform = Translation(dx, dy).Mul(op.Transform)
		return op
	}
	op.Rect.X, op.Rect.Y = op.Rect.X+dx, op.Rect.Y+dy
	op.Cast.X, op.Cast.Y = op.Cast.X+dx, op.Cast.Y+dy
	op.moveGradient(dx, dy)
	return op
}

// moveGlyphs moves glyphs drawn without their op's transform by (dx, dy):
// their bitmaps, or their own transforms.
func moveGlyphs(glyphs []Glyph, dx, dy float32) {
	for i := range glyphs {
		g := &glyphs[i]
		if g.Transform.Set {
			g.Transform = Translation(dx, dy).Mul(g.Transform)
			continue
		}
		g.X, g.Y = g.X+dx, g.Y+dy
	}
}

// drawnBounds returns where op of s draws, before the clips around it.
func drawnBounds(s *Scene, op *Op) Rect {
	switch op.Kind {
	case OpShadow:
		if op.Inset {
			return op.Transform.Bounds(op.Cast)
		}
		e := 1.5*op.Blur + 1
		return op.Transform.Bounds(Rect{X: op.Rect.X - e, Y: op.Rect.Y - e, W: op.Rect.W + 2*e, H: op.Rect.H + 2*e})
	case OpGlyphs:
		return glyphsBounds(s.Glyphs, op.Start, op.End, op.Transform)
	case OpEffect:
		b := op.Transform.Bounds(op.Rect)
		if op.Start >= 0 && int(op.Start) < len(s.Effects) {
			if e := &s.Effects[op.Start]; e.Masked() {
				b = union(b, glyphsBounds(s.Glyphs, e.Glyphs[0], e.Glyphs[1], op.Transform))
			}
		}
		return b
	}
	return op.Transform.Bounds(op.Rect)
}

// glyphsBounds returns the box of glyphs[start:end] under m.
func glyphsBounds(glyphs []Glyph, start, end int32, m Affine) Rect {
	var b Rect
	if start < 0 || end > int32(len(glyphs)) || start >= end {
		return b
	}
	for _, g := range glyphs[start:end] {
		// The GPU's renderers draw the bitmap's own size.
		r := Rect{X: g.X, Y: g.Y, W: max(g.W, float32(g.UW)), H: max(g.H, float32(g.VH))}
		if g.Transform.Set {
			r = g.Transform.Bounds(r)
		}
		b = union(b, m.Bounds(r))
	}
	return b
}

// reachOf is how far beyond its bounds an op's antialiasing reaches, as
// the GPU's renderers draw it, glyphs at whole pixels (Differ.Reach).
func reachOf(op *Op) float32 {
	if op.Transform.Set {
		return 5
	}
	return 2
}

// clipBounds returns where a clip lets drawing through, its antialiased
// edge included: drawing a clip cuts to nothing still shows there.
func clipBounds(op *Op) Rect {
	return outsetRect(op.Transform.Bounds(op.Rect), reachOf(op))
}

// overlaps reports whether a and b, the boxes ops draw in with their
// reach, share pixels.
func overlaps(a, b Rect) bool {
	return !a.Empty() && !b.Empty() && a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func outsetRect(r Rect, d float32) Rect {
	if r.Empty() {
		return r
	}
	return Rect{r.X - d, r.Y - d, r.W + 2*d, r.H + 2*d}
}

// union returns the box around a and b, either of which may be empty.
func union(a, b Rect) Rect {
	if a.Empty() {
		return b
	}
	if b.Empty() {
		return a
	}
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	return Rect{x0, y0, max(a.X+a.W, b.X+b.W) - x0, max(a.Y+a.H, b.Y+b.H) - y0}
}
