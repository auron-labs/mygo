// Package retained is a spike, not used by ui: it tries what an engine for
// ui's immediate-mode API costs when it keeps its nodes from frame to frame
// and reruns only the scopes whose inputs changed.
//
// The view is plain Go, as ui's: constructors in the order the code runs
// them, Children closures, if and for. Each frame does one of these:
//
//   - A full rerun: the view runs whole. Constructors take the node they
//     made in the last frame at their place (or by Key) and update it in
//     place: a setter writes only a value that differs, and marks the node
//     needing layout or paint (a write barrier). What a node was not given
//     this time goes back to its default. Window.Update asks for one, as
//     anything the engine cannot see changing (plain Go fields) needs.
//   - A scoped frame: only scopes whose tracked inputs changed run again
//     (State, Slice, a click on an element they built, a hover they read).
//     A scope that runs reruns the scopes inside it, as their closures may
//     capture what it computed; the others keep their nodes. Two ways to
//     reach them are tried: Restart runs the closure the scope stored as it
//     last ran, and Replay runs the view from the root, skipping the
//     Children of every clean scope that leads to no dirty one.
//   - Nothing, when nothing changed.
//
// Layout follows the marks: a node lays out again only when it, or the
// size of a child, changed. Positions are relative to the parent, so
// nothing walks the tree to place it in the window. Fresh, for comparison,
// builds every node anew and lays out and places all of it each frame, as
// ui does today.
//
// The audit runs the whole view once more and reports each scope whose
// nodes it changes though the frame did not run it: a read of something
// the engine does not track, as a plain Go field changed without Update.
package retained

import (
	"math"
	"runtime"
	"unsafe"
)

// Mode is how a frame reaches the scopes that changed.
type Mode uint8

const (
	// Restart runs the closures the dirty scopes kept (Children), and
	// reaches those that kept none from the root, as Replay does.
	Restart Mode = iota
	// Replay runs the view from the root, skipping clean Children.
	Replay
	// Fresh builds, lays out and places every node every frame, as ui does
	// today.
	Fresh
)

type kind uint8

const (
	kindBox kind = iota
	kindRow
	kindText
	kindButton
	kindScroll
)

// prop is a property a setter sets, kept in node.set as the view runs.
type prop uint16

const (
	propGap prop = 1 << iota
	propPad
	propGrow
	propHeight
	propBg
	propColor
)

// Window holds a view and the nodes it built.
type Window struct {
	c        Context
	view     func(c *Context)
	root     *node
	rootSc   *scope
	width    float32
	mode     Mode
	frame    uint32
	full     bool // the next frame reruns the whole view
	dirty    []*scope
	free     []*node
	scopes   []*scope // freed, for new Children
	tracked  bool // Update reruns nothing: all the view reads is tracked
	auditing bool
	stale    []*scope
	states   map[uint64]*freshState // Fresh's per-element state, as ui's
	// Counts, for tests and benchmarks.
	Runs, Nodes, Laid int
}

// freshState is what Fresh looks up for each element, as ui's state.
type freshState struct {
	id      uint64
	seen    uint32
	x, y    float32
	focused bool
}

// Context builds a view.
type Context struct {
	w      *Window
	scope  *scope
	parent *node // whose children the scope builds
	at     *node // the next of the last run's children to take
	cur    *node // the node being configured
	key    any
	// inDirty tells that a scope whose inputs changed is running: every
	// scope inside it runs too.
	inDirty bool
	replay  bool
	fired   bool // a handler ran (Clicked) in this run
	index   uint64 // Fresh's: the place of the next node in its scope
}

// scope is a Children closure: the nodes it builds, and what it read.
type scope struct {
	node      *node // whose children it builds
	parent    *scope
	depth     int
	run       uint32 // runs so far, for links
	frame     uint32 // the frame that last ran it
	before    uint32 // the frame that ran it before that, for the audit
	code      uintptr
	fn        func() // Restart's
	dirty     bool
	pathDirty bool // a scope inside it is dirty
}

type node struct {
	w                          *Window
	kind                       kind
	parent, first, last        *node
	prev, next                 *node
	scope                      *scope // that builds it
	kids                       *scope // its Children's
	key                        any
	text                       string
	gap, grow, height          float32
	pad                        [4]float32
	bg, color                  uint32
	set, prevSet               prop
	clicks                     int
	hovered                    bool
	hoverBy                    *scope
	hoverRun                   uint32
	needLayout, kidLayout      bool
	queued, laid, needPaint    bool
	dirtyKids                  []*node
	availW                     float32
	x, y, w2, h                float32
	ax, ay                     float32 // Fresh's window position
	id                         uint64  // Fresh's identity
	scrollY                    float32
}

// NewWindow makes a window of view, width DIPs wide.
func NewWindow(view func(c *Context), width float32, mode Mode) *Window {
	w := &Window{view: view, width: width, mode: mode, full: true, states: map[uint64]*freshState{}}
	w.c.w = w
	w.root = &node{w: w, kind: kindBox}
	w.rootSc = &scope{node: w.root}
	w.root.kids = w.rootSc
	return w
}

// Update asks for the whole view to run again, as for a change of a plain
// Go value. A tracked window ignores it.
func (w *Window) Update() {
	if !w.tracked {
		w.full = true
	}
}

// SetTracked has Update run nothing: the view promises that what it reads
// that changes is tracked (State, Slice), which the audit checks.
func (w *Window) SetTracked(t bool) { w.tracked = t }

func (w *Window) markDirty(s *scope) {
	if s.dirty {
		return
	}
	s.dirty = true
	w.dirty = append(w.dirty, s)
	for p := s.parent; p != nil && !p.pathDirty; p = p.parent {
		p.pathDirty = true
	}
}

// Frame runs what changed, lays it out, and reports whether it ran any of
// the view.
func (w *Window) Frame() bool {
	w.frame++
	runs := w.Runs
	for pass := 0; pass < 3; pass++ {
		w.c.fired = false
		switch {
		case w.full || w.mode == Fresh && len(w.dirty) > 0:
			w.full = false
			w.runRoot(false)
		case len(w.dirty) == 0:
		case w.mode == Replay:
			w.runRoot(true)
		case w.mode == Restart:
			w.restart()
		}
		if !w.c.fired || w.tracked {
			break
		}
		// A handler changed what the engine cannot see: the whole view
		// shows it, as ui's second pass does.
		w.full = true
	}
	w.clearDirty()
	if w.mode == Fresh {
		w.layoutFresh()
	} else {
		w.layout(w.root, w.width)
	}
	return w.Runs != runs
}

func (w *Window) clearDirty() {
	for _, s := range w.dirty {
		s.dirty = false
		for p := s; p != nil && p.pathDirty; p = p.parent {
			p.pathDirty = false
		}
	}
	clear(w.dirty)
	w.dirty = w.dirty[:0]
	w.rootSc.pathDirty = false
}

func (w *Window) runRoot(replay bool) {
	c := &w.c
	c.replay = replay
	c.inDirty = !replay
	if w.mode == Fresh {
		// Every node anew, as ui builds them.
		w.drop(w.root.first)
		w.root.first, w.root.last = nil, nil
	}
	w.runScope(w.rootSc, func() { w.view(c) })
	c.replay = false
}

// restart runs the dirty scopes' stored closures, outermost first; one
// inside another that ran ran with it.
func (w *Window) restart() {
	// Insertion sort by depth: few are dirty.
	d := w.dirty
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j].depth < d[j-1].depth; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
	replay := false
	for _, s := range d {
		if s.frame == w.frame {
			continue // it ran inside another
		}
		if s.fn == nil {
			// Built by ChildrenReplay, or the root: reach it from the root.
			replay = true
			continue
		}
		c := &w.c
		saved := c.inDirty
		c.inDirty = true
		w.runScope(s, s.fn)
		c.inDirty = saved
	}
	if replay {
		w.runRoot(true)
	}
}

func codeOf(fn func()) uintptr {
	// A func value points to its closure, whose first word is the code.
	return **(**uintptr)(unsafe.Pointer(&fn))
}

func (w *Window) runScope(s *scope, fn func()) {
	c := &w.c
	savedScope, savedParent, savedAt, savedCur, savedKey, savedIndex := c.scope, c.parent, c.at, c.cur, c.key, c.index
	savedDirty := c.inDirty
	if s.dirty {
		c.inDirty = true
	}
	c.scope, c.parent, c.at, c.cur, c.key = s, s.node, s.node.first, nil, nil
	s.run++
	if s.frame != w.frame {
		s.before, s.frame = s.frame, w.frame
	}
	c.index = 0
	s.code = codeOf(fn)
	w.Runs++
	fn()
	c.finish()
	// What the last run built past what this one did goes.
	for n := c.at; n != nil; {
		next := n.next
		w.remove(n)
		n = next
	}
	c.scope, c.parent, c.at, c.cur, c.key, c.index = savedScope, savedParent, savedAt, savedCur, savedKey, savedIndex
	c.inDirty = savedDirty
}

// Key gives the next element an identity of its own among its siblings.
func (c *Context) Key(k any) *Context { c.key = k; return c }

// take returns the node of kind k at the scope's place: the last run's
// there, updated in place, or a new one.
func (c *Context) take(k kind) *node {
	c.finish()
	w := c.w
	key := c.key
	c.key = nil
	old := c.at
	var n *node
	switch {
	case old != nil && old.kind == k && old.key == key:
		n = old
		c.at = old.next
	default:
		if old != nil && key != nil {
			// A keyed node further on comes here.
			for o := old.next; o != nil; o = o.next {
				if o.kind == k && o.key == key {
					w.unlink(o)
					w.insertBefore(c.parent, o, old)
					n = o
					c.parent.changed(true)
					break
				}
			}
		}
		if n == nil {
			n = w.alloc(k)
			n.key = key
			if old != nil && old.key == nil && key == nil {
				// Another kind in its place: it goes.
				c.at = old.next
				w.remove(old)
				w.insertBefore(c.parent, n, c.at)
			} else {
				w.insertBefore(c.parent, n, old)
			}
			n.parent.changed(true)
		}
	}
	n.scope = c.scope
	n.prevSet, n.set = n.set, 0
	c.cur = n
	w.Nodes++
	if w.mode == Fresh {
		// ui hashes each element's identity and looks its state up.
		c.index++
		n.id = n.parent.id*1099511628211 ^ c.index
		st := w.states[n.id]
		if st == nil {
			st = &freshState{id: n.id}
			w.states[n.id] = st
		}
		st.seen = w.frame
	}
	return n
}

// finish puts back to their defaults the properties the node had last time
// and was not given this time.
func (c *Context) finish() {
	n := c.cur
	if n == nil {
		return
	}
	c.cur = nil
	gone := n.prevSet &^ n.set
	if gone == 0 {
		return
	}
	if gone&propGap != 0 {
		n.setF(0, &n.gap, 0, true)
	}
	if gone&propPad != 0 && n.pad != ([4]float32{}) {
		n.pad = [4]float32{}
		n.changed(true)
	}
	if gone&propGrow != 0 {
		n.setF(0, &n.grow, 0, true)
	}
	if gone&propHeight != 0 {
		n.setF(0, &n.height, 0, true)
	}
	if gone&propBg != 0 {
		n.setU(0, &n.bg, 0)
	}
	if gone&propColor != 0 {
		n.setU(0, &n.color, 0)
	}
	n.set &^= gone
}

func (w *Window) alloc(k kind) *node {
	var n *node
	if l := len(w.free); l > 0 {
		n = w.free[l-1]
		w.free = w.free[:l-1]
		kids := n.dirtyKids[:0]
		*n = node{dirtyKids: kids}
	} else {
		n = &node{}
	}
	n.w, n.kind = w, k
	n.needLayout, n.needPaint = true, true
	return n
}

func (w *Window) insertBefore(p, n, before *node) {
	n.parent = p
	if before == nil {
		n.prev, n.next = p.last, nil
		if p.last != nil {
			p.last.next = n
		} else {
			p.first = n
		}
		p.last = n
		return
	}
	n.prev, n.next = before.prev, before
	if before.prev != nil {
		before.prev.next = n
	} else {
		p.first = n
	}
	before.prev = n
}

func (w *Window) unlink(n *node) {
	p := n.parent
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		p.first = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		p.last = n.prev
	}
	n.prev, n.next = nil, nil
}

func (w *Window) remove(n *node) {
	p := n.parent
	w.unlink(n)
	p.changed(true)
	w.drop(n)
}

// drop frees n and the nodes inside it.
func (w *Window) drop(n *node) {
	for n != nil {
		next := n.next
		w.drop(n.first)
		if s := n.kids; s != nil {
			// Its scope is gone: what it read no longer reaches it.
			s.run++
			s.node, s.fn, s.parent = nil, nil, nil
			s.dirty, s.pathDirty = false, false
			w.scopes = append(w.scopes, s)
		}
		n.parent, n.first, n.last, n.prev, n.next = nil, nil, nil, nil, nil
		w.free = append(w.free, n)
		n = next
	}
}

// changed notes that a value of n changed: it lays out again when layout
// reads it, and paints again.
func (n *node) changed(layout bool) {
	w := n.w
	if w.auditing {
		// The scope running made the change: stale if the frame audited
		// did not run it.
		if s := w.c.scope; s != nil && s.before != w.frame-1 {
			w.noteStale(s)
		}
	}
	n.needPaint = true
	if !layout || n.needLayout {
		return
	}
	n.needLayout = true
	for cur := n; cur.parent != nil && !cur.queued; cur = cur.parent {
		cur.queued = true
		p := cur.parent
		p.dirtyKids = append(p.dirtyKids, cur)
		p.kidLayout = true
	}
}

func (n *node) setF(p prop, f *float32, v float32, layout bool) *node {
	n.set |= p
	if *f != v {
		*f = v
		n.changed(layout)
	}
	return n
}

func (n *node) setU(p prop, f *uint32, v uint32) *node {
	n.set |= p
	if *f != v {
		*f = v
		n.changed(false)
	}
	return n
}

// Node is an element the view built, as ui's Element.
type Node = node

// Column makes a column of children.
func Column(c *Context) *Node { return c.take(kindBox) }

// Row makes a row of children.
func Row(c *Context) *Node { return c.take(kindRow) }

// Scroll makes a column that scrolls, as high as Height.
func Scroll(c *Context) *Node { return c.take(kindScroll) }

// Text makes a text.
func Text(c *Context, s string) *Node {
	n := c.take(kindText)
	if n.text != s {
		n.text = s
		n.changed(true)
	}
	return n
}

// Button makes a button showing label, a text inside it, as ui's.
func Button(c *Context, label string) *Node {
	n := c.take(kindButton)
	n.setPad(6, 14)
	t := n.first
	if t == nil {
		t = c.w.alloc(kindText)
		t.scope = c.scope
		c.w.insertBefore(n, t, nil)
		n.changed(true)
	}
	if t.text != label {
		t.text = label
		t.changed(true)
	}
	c.w.Nodes++
	return n
}

func (n *node) setPad(y, x float32) *node {
	n.set |= propPad
	if p := [4]float32{y, x, y, x}; n.pad != p {
		n.pad = p
		n.changed(true)
	}
	return n
}

// Gap spaces the children.
func (n *Node) Gap(v float32) *Node { return n.setF(propGap, &n.gap, v, true) }

// Padding pads the content, vertically and horizontally.
func (n *Node) Padding(y, x float32) *Node { return n.setPad(y, x) }

// Grow takes a share of the room left in a row.
func (n *Node) Grow(v float32) *Node { return n.setF(propGrow, &n.grow, v, true) }

// Height sets the height.
func (n *Node) Height(v float32) *Node { return n.setF(propHeight, &n.height, v, true) }

// Background sets the background.
func (n *Node) Background(rgb uint32) *Node { return n.setU(propBg, &n.bg, rgb) }

// Color sets the text's color.
func (n *Node) Color(rgb uint32) *Node { return n.setU(propColor, &n.color, rgb) }

// ChildrenReplay is Children without keeping fn: a frame that reruns only
// what changed reaches the scope by running the view from the root,
// skipping every clean scope on the way (Replay). Not keeping fn lets Go
// keep the closure off the heap.
func (n *Node) ChildrenReplay(fn func()) *Node {
	w := n.w
	c := &w.c
	c.finish()
	s := n.kids
	if s == nil || s.node != n {
		s = w.newScope(n)
	}
	s.parent, s.depth = c.scope, c.scope.depth+1
	if c.replay && !c.inDirty && !s.dirty && !s.pathDirty {
		return n // clean: it keeps what it built
	}
	w.runScope(s, fn)
	return n
}

func (w *Window) newScope(n *node) *scope {
	var s *scope
	if l := len(w.scopes); l > 0 {
		s = w.scopes[l-1]
		w.scopes = w.scopes[:l-1]
	} else {
		s = &scope{}
	}
	s.node = n
	n.kids = s
	return s
}

// Children builds the node's children in fn, a scope of its own, which
// keeps fn: a frame that reruns only what changed runs the closures of the
// scopes whose inputs changed again, and no others (Restart). Keeping it
// makes Go allocate the closure each time the view runs it, which only a
// frame running the whole view does for every scope.
func (n *Node) Children(fn func()) *Node {
	w := n.w
	c := &w.c
	c.finish()
	s := n.kids
	if s == nil || s.node != n {
		s = w.newScope(n)
	}
	s.parent, s.depth = c.scope, c.scope.depth+1
	s.fn = fn
	w.runScope(s, fn)
	return n
}

// Clicked reports whether the node was clicked, as ui's: the click makes
// the scope that built it run.
func (n *Node) Clicked() bool {
	if n.clicks == 0 {
		return false
	}
	n.clicks--
	n.w.c.fired = true
	return true
}

// Hovered reports whether the pointer is over the node: its hover changing
// runs the scope that read it.
func (n *Node) Hovered() bool {
	c := &n.w.c
	n.hoverBy, n.hoverRun = c.scope, c.scope.run
	return n.hovered
}

// Click clicks n, as a press and release over it.
func (w *Window) Click(n *Node) {
	n.clicks++
	if n.scope != nil {
		w.markDirty(n.scope)
	}
}

// Hover moves the pointer over n, or over nothing.
func (w *Window) Hover(old, n *Node) {
	for _, x := range [2]*node{old, n} {
		if x == nil {
			continue
		}
		x.hovered = x == n
		if s := x.hoverBy; s != nil && s.run == x.hoverRun && s.node != nil {
			w.markDirty(s)
		} else {
			x.needPaint = true
		}
	}
}

// Find returns the first node showing text s, for tests.
func (w *Window) Find(s string) *Node {
	var find func(n *node) *node
	find = func(n *node) *node {
		for k := n.first; k != nil; k = k.next {
			if k.text == s {
				return k
			}
			if f := find(k); f != nil {
				return f
			}
		}
		return nil
	}
	return find(w.root)
}

// noteStale reports a scope whose nodes the audit changed.
func (w *Window) noteStale(s *scope) {
	for _, x := range w.stale {
		if x == s {
			return
		}
	}
	w.stale = append(w.stale, s)
}

// Audit runs the whole view and returns where the scopes it found stale
// were written: those whose nodes it changed though the last frame did not
// run them, as when they read a plain Go value changed without Update.
func (w *Window) Audit() []string {
	w.auditing = true
	w.stale = w.stale[:0]
	w.frame++
	w.runRoot(false)
	w.layout(w.root, w.width)
	w.auditing = false
	var at []string
	for _, s := range w.stale {
		if f := runtime.FuncForPC(s.code); f != nil {
			file, line := f.FileLine(s.code)
			at = append(at, f.Name()+" "+file+":"+itoa(line))
		}
	}
	return at
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for ; i > 0; i /= 10 {
		p--
		b[p] = byte('0' + i%10)
	}
	return string(b[p:])
}

var inf = float32(math.Inf(1))
