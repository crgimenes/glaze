//go:build darwin || linux

package glaze

import "github.com/crgimenes/glaze/core"

func Init() error { return core.Init() }
