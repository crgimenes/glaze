package menu_test

import (
	"errors"
	"testing"

	"github.com/crgimenes/glaze/menu"
)

// The sentinel must wrap the standard errors.ErrUnsupported, so a caller can
// tell "no menu backend on this platform" from a real failure with the one
// check the standard library defines for it, without naming this package.
func TestErrUnsupportedWrapsStdlib(t *testing.T) {
	if !errors.Is(menu.ErrUnsupported, errors.ErrUnsupported) {
		t.Errorf("menu.ErrUnsupported does not wrap errors.ErrUnsupported (got %v)", menu.ErrUnsupported)
	}
}
