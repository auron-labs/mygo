//go:build windows

package windows

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestFixedFramelessWindowIsAPopup(t *testing.T) {
	fixed := &window{opts: &platform.WindowOptions{Frameless: true}}
	if style, _ := fixed.styles(); style&wsPopup == 0 || style&(wsCaption|wsThickFrame|wsSysMenu) != 0 {
		t.Errorf("frameless window that cannot be resized, minimized or maximized: style %#x, want a popup without caption or frame", style)
	}
	resizable := &window{opts: &platform.WindowOptions{Frameless: true, Resizable: true}}
	if style, _ := resizable.styles(); style&wsPopup != 0 || style&wsThickFrame == 0 {
		t.Errorf("resizable frameless window: style %#x, want an overlapped window with a sizing frame", style)
	}
}
