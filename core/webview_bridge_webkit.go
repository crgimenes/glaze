//go:build darwin || linux

package core

// bridgePostFn for the WebKit backends (macOS WKWebView, Linux WebKitGTK): the
// script message handler registered under the name "__webview__".
const bridgePostFn = `function(message) {
  return window.webkit.messageHandlers.__webview__.postMessage(message);
}`
