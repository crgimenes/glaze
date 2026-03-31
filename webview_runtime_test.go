package glaze

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ebitengine/purego"
)

func TestBindingCallbackReturnsViaDispatch(t *testing.T) {
	type bindingCall struct {
		id  string
		req string
	}
	calls := make(chan bindingCall, 1)
	rt := &glazeRuntime{
		dispatchMap: make(map[uintptr]func()),
		bindingMap: map[uintptr]bindingEntry{
			7: {
				w: 99,
				fn: func(id, req string) (any, error) {
					calls <- bindingCall{id: id, req: req}
					return 42, nil
				},
			},
		},
		boundNames: make(map[string]uintptr),
	}
	rt.initCallbacks()

	var sawDispatch atomic.Bool
	type response struct {
		handle       uintptr
		id           string
		status       int
		result       string
		usedDispatch bool
	}
	returned := make(chan response, 1)

	rt.pDispatch = purego.NewCallback(func(handle, cb, arg uintptr) uintptr {
		sawDispatch.Store(true)
		purego.SyscallN(cb, handle, arg)
		return 0
	})
	rt.pReturn = purego.NewCallback(func(handle, idPtr, status, resultPtr uintptr) uintptr {
		returned <- response{
			handle:       handle,
			id:           goString(idPtr),
			status:       int(status),
			result:       goString(resultPtr),
			usedDispatch: sawDispatch.Load(),
		}
		return 0
	})

	idBytes, idPtr := cString("seq-1")
	reqBytes, reqPtr := cString(`[]`)
	purego.SyscallN(rt.bindingCB, uintptr(idPtr), uintptr(reqPtr), 7)
	runtime.KeepAlive(idBytes)
	runtime.KeepAlive(reqBytes)

	select {
	case got := <-calls:
		if got.id != "seq-1" {
			t.Fatalf("binding id = %q, want %q", got.id, "seq-1")
		}
		if got.req != `[]` {
			t.Fatalf("binding req = %q, want %q", got.req, `[]`)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for binding call")
	}

	select {
	case got := <-returned:
		if got.handle != 99 {
			t.Fatalf("return handle = %d, want %d", got.handle, 99)
		}
		if got.id != "seq-1" {
			t.Fatalf("return id = %q, want %q", got.id, "seq-1")
		}
		if got.status != 0 {
			t.Fatalf("return status = %d, want %d", got.status, 0)
		}
		if got.result != "42" {
			t.Fatalf("return result = %q, want %q", got.result, "42")
		}
		if !got.usedDispatch {
			t.Fatal("expected binding return to be dispatched back to the UI thread")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for binding return")
	}
}
