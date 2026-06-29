package glaze

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Events is a lightweight publish/subscribe bridge between Go and JavaScript,
// layered entirely on the public Bind/Init/Eval primitives (no extra native
// code). Create one per WebView with NewEvents, then Emit and subscribe on
// either side: an event reaches every listener on both sides exactly once, and
// neither side echoes back to create a loop.
//
// The matching JavaScript API is installed on the page as window.glaze.events:
//
//	glaze.events.on("app:ready", (info) => { ... });
//	glaze.events.emit("ui:save", "untitled.txt");
//
// Events is safe for concurrent use.
type Events struct {
	w WebView

	mu     sync.RWMutex
	subs   map[string][]eventSub
	nextID uint64
}

// EventHandler receives the event's arguments, each as the raw JSON the emitter
// sent, to unmarshal into whatever type the handler expects.
type EventHandler func(args ...json.RawMessage)

type eventSub struct {
	id      uint64
	handler EventHandler
}

// eventsBindName is the Go function the injected JS calls to forward a
// JS-side emit into Go. It must match the name referenced in eventsJS.
const eventsBindName = "__glaze_event__"

// NewEvents installs the events bridge on w and returns the handle used to emit
// and subscribe from Go. Call it once per WebView, before Run. The error is
// non-nil only if the underlying Bind fails.
func NewEvents(w WebView) (*Events, error) {
	e := &Events{
		w:    w,
		subs: make(map[string][]eventSub),
	}
	w.Init(eventsJS)
	err := w.Bind(eventsBindName, e.receiveFromJS)
	if err != nil {
		return nil, fmt.Errorf("glaze: install events bridge: %w", err)
	}
	return e, nil
}

// On subscribes handler to the named event and returns a function that cancels
// just this subscription. Handlers for a JS-originated event run on the binding
// goroutine; handlers for a Go-originated event run on the goroutine that called
// Emit. Re-enter the UI thread with Dispatch if a handler touches the window.
func (e *Events) On(name string, handler EventHandler) (cancel func()) {
	e.mu.Lock()
	e.nextID++
	id := e.nextID
	e.subs[name] = append(e.subs[name], eventSub{id: id, handler: handler})
	e.mu.Unlock()
	return func() { e.remove(name, id) }
}

// Off removes every handler subscribed to the named event.
func (e *Events) Off(name string) {
	e.mu.Lock()
	delete(e.subs, name)
	e.mu.Unlock()
}

// Emit publishes an event to every listener on both sides. Each value in data
// becomes one argument delivered to the handlers (Go handlers receive it as raw
// JSON, JS handlers as a decoded value). It is safe to call from any goroutine;
// the JS-side listeners are notified on the UI thread. Emit returns an error
// only if a value in data cannot be JSON-encoded, in which case nothing is
// published.
func (e *Events) Emit(name string, data ...any) error {
	raw := make([]json.RawMessage, len(data))
	parts := make([]string, len(data))
	for i := range data {
		b, err := json.Marshal(data[i])
		if err != nil {
			return fmt.Errorf("glaze: encode event %q argument %d: %w", name, i, err)
		}
		raw[i] = b
		parts[i] = string(b)
	}

	// Go-side listeners, synchronously on the caller's goroutine.
	e.dispatch(name, raw)

	// JS-side listeners, on the UI thread. _dispatch only fires local JS
	// listeners, so this does not bounce back to Go.
	payload := "[" + strings.Join(parts, ",") + "]"
	js := "(function(){var g=window.glaze;if(g&&g.events){g.events._dispatch(" + marshalJSON(name) + "," + payload + ");}})()"
	e.w.Dispatch(func() { e.w.Eval(js) })
	return nil
}

// receiveFromJS is the bound function the page calls when JS emits. It fans the
// event out to the Go handlers only (the JS side already notified its own
// listeners), so there is no echo.
func (e *Events) receiveFromJS(name string, args []json.RawMessage) {
	e.dispatch(name, args)
}

// dispatch runs every Go handler for name. Handlers are copied out under the
// lock and called without it, so a handler may subscribe, cancel, or emit
// without deadlocking.
func (e *Events) dispatch(name string, args []json.RawMessage) {
	e.mu.RLock()
	subs := e.subs[name]
	handlers := make([]EventHandler, len(subs))
	for i := range subs {
		handlers[i] = subs[i].handler
	}
	e.mu.RUnlock()

	for _, h := range handlers {
		h(args...)
	}
}

func (e *Events) remove(name string, id uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	subs := e.subs[name]
	for i := range subs {
		if subs[i].id == id {
			e.subs[name] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(e.subs[name]) == 0 {
		delete(e.subs, name)
	}
}

// eventsJS is injected at document start on every navigation. It exposes
// window.glaze.events with on/off/emit, keeps a local listener table, and routes
// a JS-side emit to Go through eventsBindName. _dispatch is the inbound path Go
// uses to reach JS listeners; it deliberately does not forward back to Go.
const eventsJS = `(function() {
  'use strict';
  if (window.glaze && window.glaze.events) { return; }
  var listeners = {};
  function on(name, fn) {
    (listeners[name] = listeners[name] || []).push(fn);
    return function() { off(name, fn); };
  }
  function off(name, fn) {
    if (!listeners[name]) { return; }
    if (!fn) { delete listeners[name]; return; }
    listeners[name] = listeners[name].filter(function(f) { return f !== fn; });
    if (listeners[name].length === 0) { delete listeners[name]; }
  }
  function fire(name, args) {
    var fns = listeners[name];
    if (!fns) { return; }
    fns.slice().forEach(function(fn) {
      try {
        fn.apply(null, args);
      } catch (e) {
        console.error('glaze: event handler for "' + name + '" threw:', e);
      }
    });
  }
  function emit(name) {
    var args = Array.prototype.slice.call(arguments, 1);
    fire(name, args);
    if (typeof window.__glaze_event__ === 'function') {
      var p = window.__glaze_event__(name, args);
      if (p && typeof p.catch === 'function') { p.catch(function() {}); }
    }
  }
  function _dispatch(name, args) { fire(name, args); }
  window.glaze = window.glaze || {};
  window.glaze.events = { on: on, off: off, emit: emit, _dispatch: _dispatch };
})()`
