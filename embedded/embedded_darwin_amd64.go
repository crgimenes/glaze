package embedded

import _ "embed"

const name = "libwebview.dylib"

//go:embed darwin_amd64/libwebview.dylib
var lib []byte
