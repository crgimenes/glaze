package embedded

import _ "embed"

const name = "webview.dll"

//go:embed windows_arm64/webview.dll
var lib []byte
