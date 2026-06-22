package glaze

import "strings"

// bridgePostFn (the transport the injected bridge uses to reach Go) is
// platform-specific: WebKit message handlers on macOS/Linux, chrome.webview on
// Windows. It is defined per backend (webview_bridge_webkit.go / the Windows
// backend).

// createInitScript returns the document-start bridge, ported from webview's
// engine_base.hh create_init_script, so the JS API is identical to the native
// library backend (window.__webview__ with Promise-based call()/onReply() and
// onBind()/onUnbind()).
func createInitScript(postFn string) string {
	return `(function() {
  'use strict';
  function generateId() {
    var crypto = window.crypto || window.msCrypto;
    var bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    return Array.prototype.slice.call(bytes).map(function(n) {
      var s = n.toString(16);
      return ((s.length % 2) == 1 ? '0' : '') + s;
    }).join('');
  }
  var Webview = (function() {
    var _promises = {};
    function Webview_() {}
    Webview_.prototype.post = function(message) {
      return (` + postFn + `)(message);
    };
    Webview_.prototype.call = function(method) {
      var _id = generateId();
      var _params = Array.prototype.slice.call(arguments, 1);
      var promise = new Promise(function(resolve, reject) {
        _promises[_id] = { resolve: resolve, reject: reject };
      });
      this.post(JSON.stringify({
        id: _id,
        method: method,
        params: _params
      }));
      return promise;
    };
    Webview_.prototype.onReply = function(id, status, result) {
      var promise = _promises[id];
      if (result !== undefined) {
        try {
          result = JSON.parse(result);
        } catch (e) {
          promise.reject(new Error("Failed to parse binding result as JSON"));
          return;
        }
      }
      if (status === 0) {
        promise.resolve(result);
      } else {
        promise.reject(result);
      }
    };
    Webview_.prototype.onBind = function(name) {
      if (window.hasOwnProperty(name)) {
        throw new Error('Property "' + name + '" already exists');
      }
      window[name] = (function() {
        var params = [name].concat(Array.prototype.slice.call(arguments));
        return Webview_.prototype.call.apply(this, params);
      }).bind(this);
    };
    Webview_.prototype.onUnbind = function(name) {
      if (!window.hasOwnProperty(name)) {
        throw new Error('Property "' + name + '" does not exist');
      }
      delete window[name];
    };
    return Webview_;
  })();
  window.__webview__ = new Webview();
})()`
}

// createBindScript returns the document-start script that re-binds every
// currently-bound name, ported from webview's create_bind_script.
func createBindScript(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = marshalJSON(n)
	}
	jsNames := "[" + strings.Join(parts, ",") + "]"
	return `(function() {
  'use strict';
  var methods = ` + jsNames + `;
  methods.forEach(function(name) {
    window.__webview__.onBind(name);
  });
})()`
}
