package retained

// Layout is a small flexbox: columns stack their children, as wide as the
// column; rows put them side by side, Grow sharing the room left; a scroll
// container is a column as high as its Height. Text measures 7 DIPs a
// letter and 16 high, standing in for the text system's measurements,
// which ui caches by text already.
//
// It follows the marks setters leave (node.changed): a node laid out at
// the same width that neither changed nor has a child that did keeps its
// box. One whose children alone changed lays out those again, and goes on
// to place its children only if one of them changed size. Boxes are
// relative to the parent.

func textWidth(s string) float32 { return float32(7 * len(s)) }

// layout lays n out availW wide.
func (w *Window) layout(n *node, availW float32) {
	if n.laid && !n.needLayout && n.availW == availW {
		if !n.kidLayout {
			return
		}
		moved := false
		for _, k := range n.dirtyKids {
			k.queued = false
			if k.parent != n {
				continue // gone since
			}
			ow, oh := k.w2, k.h
			w.layout(k, k.availW)
			if k.w2 != ow || k.h != oh {
				moved = true
			}
		}
		n.dirtyKids = n.dirtyKids[:0]
		n.kidLayout = false
		if !moved {
			return
		}
	}
	w.Laid++
	for _, k := range n.dirtyKids {
		k.queued = false
	}
	n.dirtyKids = n.dirtyKids[:0]
	n.kidLayout, n.needLayout, n.laid, n.availW = false, false, true, availW
	padY, padX := n.pad[0]+n.pad[2], n.pad[1]+n.pad[3]
	switch n.kind {
	case kindText:
		n.w2, n.h = min(textWidth(n.text), availW), 16
		if n.grow > 0 {
			n.w2 = availW
		}
	case kindRow, kindButton:
		inner := availW - padX
		var used, grow float32
		for k := n.first; k != nil; k = k.next {
			if k.grow > 0 {
				grow += k.grow
				continue
			}
			w.layout(k, inner)
			used += k.w2
		}
		gaps := n.gap * float32(max(count(n)-1, 0))
		left := max(inner-used-gaps, 0)
		x, h := n.pad[3], float32(0)
		for k := n.first; k != nil; k = k.next {
			if k.grow > 0 {
				w.layout(k, left*k.grow/grow)
			}
			k.x, k.y = x, n.pad[0]
			x += k.w2 + n.gap
			h = max(h, k.h)
		}
		n.h = h + padY
		if n.kind == kindButton {
			n.w2 = x - n.gap + n.pad[1]
		} else {
			n.w2 = availW
		}
	default: // columns and scroll containers
		inner := availW - padX
		y := n.pad[0]
		for k := n.first; k != nil; k = k.next {
			w.layout(k, inner)
			k.x, k.y = n.pad[3], y
			y += k.h + n.gap
		}
		if n.first != nil {
			y -= n.gap
		}
		n.w2, n.h = availW, y+n.pad[2]
		if n.height > 0 {
			n.h = n.height
		}
	}
}

func count(n *node) int {
	i := 0
	for k := n.first; k != nil; k = k.next {
		i++
	}
	return i
}

// layoutFresh lays every node out and places it in the window, as ui does
// each frame today (layoutTree, then commit).
func (w *Window) layoutFresh() {
	var mark func(n *node)
	mark = func(n *node) {
		n.needLayout, n.laid = true, false
		for k := n.first; k != nil; k = k.next {
			mark(k)
		}
	}
	mark(w.root)
	w.layout(w.root, w.width)
	var place func(n *node, x, y float32)
	place = func(n *node, x, y float32) {
		n.ax, n.ay = x+n.x, y+n.y
		for k := n.first; k != nil; k = k.next {
			place(k, n.ax, n.ay-n.scrollY)
		}
	}
	place(w.root, 0, 0)
}

// Box is a node's box in the window, found from its parents' on demand,
// in place of commit placing every node each frame.
func (n *Node) Box() (x, y, w, h float32) {
	x, y = n.x, n.y
	for p := n.parent; p != nil; p = p.parent {
		x += p.x
		y += p.y - p.scrollY
	}
	return x, y, n.w2, n.h
}

// HitTest returns the innermost node at (x, y), going down from the root
// through the children holding the point, a binary search among a column's.
func (w *Window) HitTest(x, y float32) *Node {
	n := w.root
	for {
		lx, ly := x, y
		var found *node
		if n.kind == kindBox || n.kind == kindScroll {
			found = searchColumn(n, ly+n.scrollY)
		} else {
			for k := n.first; k != nil; k = k.next {
				if lx >= k.x && lx < k.x+k.w2 && ly >= k.y && ly < k.y+k.h {
					found = k
				}
			}
		}
		if found == nil || !(lx >= found.x && lx < found.x+found.w2) {
			if n == w.root {
				return nil
			}
			return n
		}
		x, y = lx-found.x, ly+n.scrollY-found.y
		n = found
	}
}

// searchColumn finds the child of column n spanning y, by its children's
// order, which is their order down the column. A linked list has no index:
// this walks it, a stand-in for the index a real engine keeps.
func searchColumn(n *node, y float32) *node {
	for k := n.first; k != nil; k = k.next {
		if y >= k.y && y < k.y+k.h {
			return k
		}
		if k.y > y {
			return nil
		}
	}
	return nil
}
