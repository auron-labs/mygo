package retained

// link is a scope that read a value, in the run that read it: a later run
// of the scope makes it stale, so that a scope no longer reading a value
// is not run for it.
type link struct {
	s   *scope
	run uint32
}

// links are the readers of a value.
type links []link

func (l *links) track(c *Context) {
	s := c.scope
	if s == nil {
		return
	}
	if n := len(*l); n > 0 && (*l)[n-1] == (link{s, s.run}) {
		return
	}
	if len(*l) == cap(*l) {
		// Drop the stale before growing.
		kept := (*l)[:0]
		for _, x := range *l {
			if x.s.run == x.run && x.s.node != nil {
				kept = append(kept, x)
			}
		}
		*l = kept
	}
	*l = append(*l, link{s, s.run})
}

// notify marks the scopes reading the value dirty; they read it again as
// they run.
func (l *links) notify(w *Window) {
	for _, x := range *l {
		if x.s.run == x.run && x.s.node != nil {
			w.markDirty(x.s)
		}
	}
	*l = (*l)[:0]
}

// State is a value whose readers the engine knows: setting it to another
// value runs the scopes that read it, and no others.
type State[T comparable] struct {
	v    T
	subs links
}

// Get returns the value, noting that the running scope reads it.
func (s *State[T]) Get(c *Context) T { s.subs.track(c); return s.v }

// Set sets the value; an equal one changes nothing.
func (s *State[T]) Set(w *Window, v T) {
	if s.v == v {
		return
	}
	s.v = v
	s.subs.notify(w)
}

// Slice is a list whose readers the engine knows by index: setting an item
// runs the scopes that read that item, and changing its length those that
// read its length.
type Slice[T comparable] struct {
	items []T
	subs  []links
	lens  links
}

// NewSlice returns a Slice of items.
func NewSlice[T comparable](items []T) *Slice[T] {
	return &Slice[T]{items: items, subs: make([]links, len(items))}
}

// Len returns its length, noting that the running scope reads it.
func (s *Slice[T]) Len(c *Context) int { s.lens.track(c); return len(s.items) }

// At returns item i, noting that the running scope reads it.
func (s *Slice[T]) At(c *Context, i int) T { s.subs[i].track(c); return s.items[i] }

// Set sets item i.
func (s *Slice[T]) Set(w *Window, i int, v T) {
	if s.items[i] == v {
		return
	}
	s.items[i] = v
	s.subs[i].notify(w)
}

// Append adds an item.
func (s *Slice[T]) Append(w *Window, v T) {
	s.items = append(s.items, v)
	s.subs = append(s.subs, nil)
	s.lens.notify(w)
}
