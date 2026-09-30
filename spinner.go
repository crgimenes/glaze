//go:build darwin || linux

package glaze

import "time"

// spinnerDelay is how long a page held back by HideUntilLoaded may take
// before the window shows a spinner: a fast page never flashes one.
const spinnerDelay = 300 * time.Millisecond
