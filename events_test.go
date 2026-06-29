package glaze

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// eventsFakeWV is a WebView that records the Init/Bind/Eval the events bridge
// performs and runs Dispatch synchronously, so the whole Go side is testable
// without a real window. The embedded stub supplies the rest of the interface.
type eventsFakeWV struct {
	*bindMethodsWebViewStub

	mu     sync.Mutex
	initJS []string
	evalJS []string
	bound  map[string]any
}

func newEventsFakeWV() *eventsFakeWV {
	return &eventsFakeWV{
		bindMethodsWebViewStub: &bindMethodsWebViewStub{},
		bound:                  map[string]any{},
	}
}

func (f *eventsFakeWV) Init(js string) {
	f.mu.Lock()
	f.initJS = append(f.initJS, js)
	f.mu.Unlock()
}

func (f *eventsFakeWV) Eval(js string) {
	f.mu.Lock()
	f.evalJS = append(f.evalJS, js)
	f.mu.Unlock()
}

func (f *eventsFakeWV) Dispatch(fn func()) { fn() }

func (f *eventsFakeWV) Bind(name string, fn any) error {
	f.mu.Lock()
	f.bound[name] = fn
	f.mu.Unlock()
	return nil
}

func (f *eventsFakeWV) lastEval() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.evalJS) == 0 {
		return ""
	}
	return f.evalJS[len(f.evalJS)-1]
}

func TestNewEventsInstallsBridge(t *testing.T) {
	f := newEventsFakeWV()
	_, err := NewEvents(f)
	if err != nil {
		t.Fatalf("NewEvents: %v", err)
	}
	if len(f.initJS) != 1 || !strings.Contains(f.initJS[0], "window.glaze") {
		t.Fatalf("events JS not injected via Init: %q", f.initJS)
	}
	if _, ok := f.bound[eventsBindName]; !ok {
		t.Fatalf("bridge %q not bound; bound names: %v", eventsBindName, keysOf(f.bound))
	}
}

func TestEmitFiresGoHandlerAndEvalsJS(t *testing.T) {
	f := newEventsFakeWV()
	ev, err := NewEvents(f)
	if err != nil {
		t.Fatal(err)
	}

	var got []json.RawMessage
	ev.On("greet", func(args ...json.RawMessage) { got = args })

	err = ev.Emit("greet", map[string]any{"name": "crg"}, 42)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	// Go handler ran with both arguments as raw JSON.
	if len(got) != 2 {
		t.Fatalf("handler received %d args, want 2", len(got))
	}
	var payload struct {
		Name string `json:"name"`
	}
	err = json.Unmarshal(got[0], &payload)
	if err != nil || payload.Name != "crg" {
		t.Fatalf("arg 0 = %s (err %v), want {name:crg}", got[0], err)
	}
	if string(got[1]) != "42" {
		t.Fatalf("arg 1 = %s, want 42", got[1])
	}

	// JS listeners were notified via an Eval carrying the encoded payload.
	js := f.lastEval()
	if !strings.Contains(js, `_dispatch("greet"`) {
		t.Fatalf("Eval did not dispatch the event: %q", js)
	}
	if !strings.Contains(js, `"name":"crg"`) || !strings.Contains(js, "42") {
		t.Fatalf("Eval missing the payload: %q", js)
	}
}

func TestReceiveFromJSDispatchesToGo(t *testing.T) {
	f := newEventsFakeWV()
	ev, err := NewEvents(f)
	if err != nil {
		t.Fatal(err)
	}

	var got []json.RawMessage
	ev.On("ui:click", func(args ...json.RawMessage) { got = args })

	// Simulate the page calling the bound bridge function (a JS-side emit).
	bridge, ok := f.bound[eventsBindName].(func(string, []json.RawMessage))
	if !ok {
		t.Fatalf("bound bridge has unexpected type %T", f.bound[eventsBindName])
	}
	bridge("ui:click", []json.RawMessage{json.RawMessage(`"save"`)})

	if len(got) != 1 || string(got[0]) != `"save"` {
		t.Fatalf("Go handler received %v, want [\"save\"]", got)
	}
}

func TestOnCancelStopsHandler(t *testing.T) {
	f := newEventsFakeWV()
	ev, _ := NewEvents(f)

	n := 0
	cancel := ev.On("tick", func(args ...json.RawMessage) { n++ })

	_ = ev.Emit("tick")
	cancel()
	_ = ev.Emit("tick")

	if n != 1 {
		t.Fatalf("handler fired %d times, want 1 (cancelled after first emit)", n)
	}
}

func TestOffRemovesAllHandlers(t *testing.T) {
	f := newEventsFakeWV()
	ev, _ := NewEvents(f)

	n := 0
	ev.On("x", func(args ...json.RawMessage) { n++ })
	ev.On("x", func(args ...json.RawMessage) { n++ })

	_ = ev.Emit("x")
	if n != 2 {
		t.Fatalf("both handlers should fire: got %d, want 2", n)
	}

	ev.Off("x")
	_ = ev.Emit("x")
	if n != 2 {
		t.Fatalf("no handler should fire after Off: got %d, want 2", n)
	}
}

func TestEmitRejectsUnencodableData(t *testing.T) {
	f := newEventsFakeWV()
	ev, _ := NewEvents(f)

	fired := false
	ev.On("bad", func(args ...json.RawMessage) { fired = true })

	err := ev.Emit("bad", make(chan int))
	if err == nil {
		t.Fatal("Emit should fail to encode a channel")
	}
	if fired {
		t.Fatal("no handler should fire when encoding fails")
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
