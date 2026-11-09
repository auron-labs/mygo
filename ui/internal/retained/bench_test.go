package retained

import (
	"fmt"
	"testing"
)

func benchRows(b *testing.B, n int, mode Mode, tracked bool, step func(w *Window, v *rows, i int)) {
	w, v := newRowsWindow(n, mode, tracked)
	for i := range 3 {
		step(w, v, i)
	}
	runs := w.Runs
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		step(w, v, i)
		i++
	}
	b.ReportMetric(float64(w.Runs-runs)/float64(i), "scopes/op")
}

// rerun runs the whole view with nothing changed, as Window.Update does.
func rerun(w *Window, v *rows, i int) { w.Update(); w.Frame() }

// plainLabel changes a plain label and runs the whole view, as ui needs
// today for any change.
func plainLabel(w *Window, v *rows, i int) {
	v.plain[5] = fmt.Sprint("Row 5 ", i%7)
	w.Update()
	w.Frame()
}

// trackedLabel changes a label read through a Slice.
func trackedLabel(w *Window, v *rows, i int) {
	v.labels.Set(w, 5, labels7[i%7])
	w.Frame()
}

var labels7 = func() (l [7]string) {
	for i := range l {
		l[i] = fmt.Sprint("Row 5 ", i)
	}
	return
}()

// idle runs a frame with nothing changed.
func idle(w *Window, v *rows, i int) { w.Frame() }

// hoverRead moves the hover between two rows' buttons, read as built.
func hoverRead(w *Window, v *rows, i int) {
	if !v.hoverRead {
		v.hoverRead = true
		w.Update()
		w.Frame()
	}
	b5, b6 := w.Find("Row 5").next, w.Find("Row 6").next
	if i%2 == 0 {
		w.Hover(b6, b5)
	} else {
		w.Hover(b5, b6)
	}
	w.Frame()
}

// clickTracked clicks a row's button in a tracked window: its row runs.
func clickTracked(w *Window, v *rows, i int) {
	w.SetTracked(true)
	w.Click(w.Find("Row 5").next)
	w.Frame()
}

// BenchmarkFresh3000Rerun builds, lays out and places 3,000 rows anew, as
// ui does each frame today, in this spike's small engine.
func BenchmarkFresh3000Rerun(b *testing.B) { benchRows(b, 3000, Fresh, false, rerun) }

// BenchmarkReplay3000Rerun runs the whole view of 3,000 rows, updating its
// nodes in place: what Window.Update costs.
func BenchmarkReplay3000Rerun(b *testing.B) { benchRows(b, 3000, Replay, false, rerun) }

// BenchmarkRestart3000Rerun is BenchmarkReplay3000Rerun keeping each row's
// closure to run it again.
func BenchmarkRestart3000Rerun(b *testing.B) { benchRows(b, 3000, Restart, false, rerun) }

// BenchmarkFresh3000PlainLabel changes a plain label of 3,000 rows.
func BenchmarkFresh3000PlainLabel(b *testing.B) { benchRows(b, 3000, Fresh, false, plainLabel) }

// BenchmarkReplay3000PlainLabel changes a plain label: the whole view runs,
// and one text lays out again.
func BenchmarkReplay3000PlainLabel(b *testing.B) { benchRows(b, 3000, Replay, false, plainLabel) }

// BenchmarkReplay3000TrackedLabel changes a label read through a Slice: the
// root runs, skipping every row but the one.
func BenchmarkReplay3000TrackedLabel(b *testing.B) { benchRows(b, 3000, Replay, true, trackedLabel) }

// BenchmarkRestart3000TrackedLabel changes a label read through a Slice: its
// row alone runs.
func BenchmarkRestart3000TrackedLabel(b *testing.B) { benchRows(b, 3000, Restart, true, trackedLabel) }

// BenchmarkReplay3000Idle is a frame with nothing changed.
func BenchmarkReplay3000Idle(b *testing.B) { benchRows(b, 3000, Replay, true, idle) }

// BenchmarkReplay3000HoverRead moves the hover between buttons whose rows
// read it as they build.
func BenchmarkReplay3000HoverRead(b *testing.B) { benchRows(b, 3000, Replay, true, hoverRead) }

// BenchmarkRestart3000HoverRead is BenchmarkReplay3000HoverRead with
// Restart.
func BenchmarkRestart3000HoverRead(b *testing.B) { benchRows(b, 3000, Restart, true, hoverRead) }

// BenchmarkRestart3000ClickTracked clicks a button of a tracked window.
func BenchmarkRestart3000ClickTracked(b *testing.B) {
	benchRows(b, 3000, Restart, true, clickTracked)
}

// BenchmarkReplay3000ClickTracked clicks a button of a tracked window.
func BenchmarkReplay3000ClickTracked(b *testing.B) { benchRows(b, 3000, Replay, true, clickTracked) }
