package embedded

import _ "embed"

const name = "libwebview.so"

//go:embed linux_amd64/libwebview.so
var lib []byte
