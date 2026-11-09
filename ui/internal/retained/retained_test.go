package retained

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

// rows is the fixture of ui's frame-cost benchmarks: n rows of a label and
// a button inside a scroll container.
type rows struct {
	n         int
	plain     []string       // labels the engine does not see
	labels    *Slice[string] // labels it does, when set
	restart   bool
	hoverRead bool
	clicked   int
}

func newRows(n int, tracked bool) *rows {
	v := &rows{n: n, clicked: -1}
	for i := range n {
		v.plain = append(v.plain, fmt.Sprintf("Row %d", i))
	}
	if tracked {
		v.labels = NewSlice(append([]string(nil), v.plain...))
	}
	return v
}

func (v *rows) label(c *Context, i int) string {
	if v.labels != nil {
		return v.labels.At(c, i)
	}
	return v.plain[i]
}

func (v *rows) row(c *Context, i int) {
	Text(c, v.label(c, i)).Grow(1)
	b := Button(c, "Open")
	if v.hoverRead && b.Hovered() {
		b.Background(0xff0000)
	}
	if b.Clicked() {
		v.clicked = i
	}
}

func (v *rows) view(c *Context) {
	Column(c).ChildrenReplay(func() {
		Scroll(c).Height(700).ChildrenReplay(func() {
			for i := range v.n {
				r := Row(c).Gap(8).Padding(4, 8)
				if v.restart {
					r.Children(func() { v.row(c, i) })
				} else {
					r.ChildrenReplay(func() { v.row(c, i) })
				}
			}
		})
	})
}

func newRowsWindow(n int, mode Mode, tracked bool) (*Window, *rows) {
	v := newRows(n, tracked)
	v.restart = mode == Restart
	w := NewWindow(v.view, 980, mode)
	w.Frame()
	return w, v
}

// dump writes the tree with its properties and boxes, to compare windows.
func dump(n *node, b *strings.Builder, depth int) {
	fmt.Fprintf(b, "%s%d %q gap=%v pad=%v grow=%v h=%v bg=%x color=%x box=%v,%v,%v,%v\n",
		strings.Repeat(" ", depth), n.kind, n.text, n.gap, n.pad, n.grow, n.height, n.bg, n.color, n.x, n.y, n.w2, n.h)
	for k := n.first; k != nil; k = k.next {
		dump(k, b, depth+1)
	}
}

func treeOf(w *Window) string {
	var b strings.Builder
	dump(w.root, &b, 0)
	return b.String()
}

// randomView is a view whose shape and properties follow seed, with
// elements and properties under ifs, keyed elements moving, and nested
// scopes: what reconciling in place must get as building anew does.
type randomView struct {
	seed  uint64
	plain []int
}

func (v *randomView) view(c *Context) {
	r := rand.New(rand.NewPCG(v.seed, 1))
	var build func(depth int)
	build = func(depth int) {
		n := r.IntN(5)
		for i := range n {
			if r.IntN(4) == 0 {
				c.Key(fmt.Sprint("k", r.IntN(3)))
			}
			switch r.IntN(4) {
			case 0:
				t := Text(c, fmt.Sprint("t", r.IntN(4), v.plain[i%len(v.plain)]))
				if r.IntN(2) == 0 {
					t.Color(uint32(r.IntN(3)))
				}
				if r.IntN(3) == 0 {
					t.Grow(1)
				}
			case 1:
				Button(c, fmt.Sprint("b", r.IntN(3)))
			default:
				var e *Node
				if r.IntN(2) == 0 {
					e = Row(c)
				} else {
					e = Column(c)
				}
				if r.IntN(2) == 0 {
					e.Gap(float32(r.IntN(3) * 4))
				}
				if r.IntN(2) == 0 {
					e.Padding(float32(r.IntN(3)), float32(r.IntN(3)))
				}
				if r.IntN(3) == 0 {
					e.Background(uint32(r.IntN(4)))
				}
				if depth < 3 {
					e.ChildrenReplay(func() { build(depth + 1) })
				}
			}
		}
	}
	Column(c).ChildrenReplay(func() { build(0) })
}

// TestRetainedMatchesFresh changes a random view frame after frame: the
// windows that keep their nodes, and lay out by the marks setters leave,
// hold the tree a window building it anew holds, with the same boxes.
func TestRetainedMatchesFresh(t *testing.T) {
	for seed := uint64(0); seed < 300; seed++ {
		v := &randomView{seed: seed, plain: []int{0, 1, 2}}
		fresh := NewWindow(v.view, 400, Fresh)
		replay := NewWindow(v.view, 400, Replay)
		for step := 0; step < 12; step++ {
			if step > 0 {
				// Mostly the same shape, sometimes another.
				if step%3 == 0 {
					v.seed = seed*977 + uint64(step)
				}
				v.plain[step%3] = step
				fresh.Update()
				replay.Update()
			}
			fresh.Frame()
			replay.Frame()
			if f, r := treeOf(fresh), treeOf(replay); f != r {
				t.Fatalf("seed %d step %d: retained differs from fresh:\n%s\nfresh:\n%s", seed, step, r, f)
			}
		}
	}
}

// TestPropertyGoneGoesBack sets a property under an if: once the if no
// longer sets it, it goes back to its default, and the layout follows.
func TestPropertyGoneGoesBack(t *testing.T) {
	on := true
	w := NewWindow(func(c *Context) {
		r := Row(c)
		if on {
			r.Gap(10).Background(7)
		}
		r.ChildrenReplay(func() { Text(c, "a"); Text(c, "b") })
	}, 300, Replay)
	w.Frame()
	r := w.root.first
	if r.gap != 10 || r.bg != 7 || r.last.x != 7+10 {
		t.Fatalf("set: gap %v bg %v b at %v", r.gap, r.bg, r.last.x)
	}
	on = false
	w.Update()
	w.Frame()
	if r != w.root.first {
		t.Fatal("the row was not kept")
	}
	if r.gap != 0 || r.bg != 0 || r.last.x != 7 {
		t.Fatalf("unset: gap %v bg %v b at %v", r.gap, r.bg, r.last.x)
	}
}

// TestTrackedChangeRunsOneRow sets a label read through a Slice: one row's
// scope runs, and the window shows what one built anew shows.
func TestTrackedChangeRunsOneRow(t *testing.T) {
	for _, mode := range []Mode{Replay, Restart} {
		w, v := newRowsWindow(200, mode, true)
		before := w.Runs
		v.labels.Set(w, 5, "Changed")
		if !w.Frame() {
			t.Fatal("nothing ran")
		}
		if got := w.Runs - before; mode == Restart && got != 1 || mode == Replay && got != 4 {
			// Replay runs the root, the column, the scroll container and
			// the row.
			t.Errorf("mode %d: %d scopes ran", mode, got)
		}
		fresh, fv := newRowsWindow(200, Fresh, false)
		fv.plain[5] = "Changed"
		fresh.Update()
		fresh.Frame()
		if treeOf(fresh) != treeOf(w) {
			t.Fatalf("mode %d: differs from fresh", mode)
		}
		if w.Frame() {
			t.Errorf("mode %d: a frame with nothing changed ran the view", mode)
		}
	}
}

// TestClickRunsItsScope clicks a row's button: the row's scope runs and
// sees the click. Its handler changed a plain value, so the whole view
// runs after it, unless the window is tracked.
func TestClickRunsItsScope(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		w, v := newRowsWindow(100, Replay, true)
		w.SetTracked(tracked)
		b := w.Find("Row 7").next
		before := w.Runs
		w.Click(b)
		w.Frame()
		if v.clicked != 7 {
			t.Fatalf("tracked %v: clicked %d", tracked, v.clicked)
		}
		full := w.Runs-before > 100
		if full == tracked {
			t.Errorf("tracked %v: ran %d scopes", tracked, w.Runs-before)
		}
	}
}

// TestHoverReadRunsOneRow hovers buttons whose hover the rows read as
// they build: the rows the hover entered and left run, and the background
// set while hovered goes once it is not.
func TestHoverReadRunsOneRow(t *testing.T) {
	w, v := newRowsWindow(100, Restart, true)
	v.hoverRead = true
	w.Update()
	w.Frame()
	b5, b6 := w.Find("Row 5").next, w.Find("Row 6").next
	before := w.Runs
	w.Hover(nil, b5)
	w.Frame()
	if b5.bg != 0xff0000 || w.Runs-before != 1 {
		t.Fatalf("over row 5: bg %x, %d scopes ran", b5.bg, w.Runs-before)
	}
	before = w.Runs
	w.Hover(b5, b6)
	w.Frame()
	if b5.bg != 0 || b6.bg != 0xff0000 || w.Runs-before != 2 {
		t.Fatalf("over row 6: %x %x, %d scopes ran", b5.bg, b6.bg, w.Runs-before)
	}
}

// TestAuditFindsUntrackedRead changes a plain label of a tracked window,
// whose Update runs nothing: the frame shows the old label, and the audit
// tells where the row that reads it is.
func TestAuditFindsUntrackedRead(t *testing.T) {
	w, v := newRowsWindow(50, Restart, false)
	w.SetTracked(true)
	v.plain[5] = "Changed"
	w.Update()
	w.Frame()
	if w.Find("Changed") != nil {
		t.Fatal("the label changed without the row running")
	}
	stale := w.Audit()
	if len(stale) != 1 || !strings.Contains(stale[0], "retained_test.go:") || !strings.Contains(stale[0], "view") {
		t.Fatalf("audit found %q", stale)
	}
	if w.Find("Changed") == nil {
		t.Fatal("the audit did not show the label")
	}
	t.Logf("stale: %s", stale[0])
	if s := w.Audit(); len(s) != 0 {
		t.Fatalf("a second audit found %q", s)
	}
}

// TestHitTest finds the element under points as a search of every box does.
func TestHitTest(t *testing.T) {
	w, _ := newRowsWindow(60, Replay, false)
	var all []*node
	var walk func(n *node)
	walk = func(n *node) {
		all = append(all, n)
		for k := n.first; k != nil; k = k.next {
			walk(k)
		}
	}
	walk(w.root.first)
	for y := float32(0); y < 720; y += 3 {
		for x := float32(0); x < 980; x += 7 {
			var want *node
			for _, n := range all {
				bx, by, bw, bh := n.Box()
				if x >= bx && x < bx+bw && y >= by && y < by+bh && (n.parent.kind != kindScroll || y < 700) {
					want = n // the last, innermost in paint order
				}
			}
			if y >= 700 {
				continue
			}
			if got := w.HitTest(x, y); got != want {
				t.Fatalf("at %v,%v: %p, want %p", x, y, got, want)
			}
		}
	}
}
