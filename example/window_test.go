//go:build wasm

package example

import (
	"syscall/js"
	"testing"
)

func TestWindow(t *testing.T) {
	window := js.Global().Get("window")
	if window.IsUndefined() || window.IsNull() {
		t.Skip("window is not available in this runtime")
	}

	width := window.Get("innerWidth").Int()
	height := window.Get("innerHeight").Int()

	t.Logf("window innerWidth=%d, innerHeight=%d", width, height)

	if width <= 1024 {
		t.Errorf("expected window.innerWidth > 1024, got %d", width)
	}

	if height <= 768 {
		t.Errorf("expected window.innerHeight > 768, got %d", height)
	}
}
