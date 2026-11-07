// Package glaze is a thin entry point: the implementation, and the
// documentation of each symbol, live in package core.
package glaze

import (
	"html/template"
	"unsafe"

	"github.com/crgimenes/glaze/core"
)

type (
	AppOptions        = core.AppOptions
	AppReadyInfo      = core.AppReadyInfo
	AppTransport      = core.AppTransport
	EventHandler      = core.EventHandler
	Events            = core.Events
	FileDialogOptions = core.FileDialogOptions
	FileFilter        = core.FileFilter
	Hint              = core.Hint
	NavigationEvent   = core.NavigationEvent
	NavigationKind    = core.NavigationKind
	Options           = core.Options
	SchemeHandler     = core.SchemeHandler
	SchemeRequest     = core.SchemeRequest
	SchemeResponse    = core.SchemeResponse
	ScriptDialog      = core.ScriptDialog
	ScriptDialogKind  = core.ScriptDialogKind
	Snapshotter       = core.Snapshotter
	WebView           = core.WebView
)

const (
	AppTransportAuto = core.AppTransportAuto
	AppTransportTCP  = core.AppTransportTCP
	AppTransportUnix = core.AppTransportUnix

	HintNone  = core.HintNone
	HintMin   = core.HintMin
	HintMax   = core.HintMax
	HintFixed = core.HintFixed

	NavigationFinished = core.NavigationFinished
	NavigationFailed   = core.NavigationFailed

	ScriptAlert   = core.ScriptAlert
	ScriptConfirm = core.ScriptConfirm
	ScriptPrompt  = core.ScriptPrompt
)

var (
	ErrBridgeDisabled  = core.ErrBridgeDisabled
	ErrHiddenWindow    = core.ErrHiddenWindow
	ErrIconUnsupported = core.ErrIconUnsupported
)

func New(debug bool) (WebView, error) { return core.New(debug) }

func NewWindow(debug bool, window unsafe.Pointer) (WebView, error) {
	return core.NewWindow(debug, window)
}

func NewWithOptions(opts Options) (WebView, error) { return core.NewWithOptions(opts) }

func NewEvents(w WebView) (*Events, error) { return core.NewEvents(w) }

func AppWindow(opts AppOptions) error { return core.AppWindow(opts) }

func BindMethods(w WebView, prefix string, obj any) ([]string, error) {
	return core.BindMethods(w, prefix, obj)
}

func RenderHTML(tpl *template.Template, name string, data any) (string, error) {
	return core.RenderHTML(tpl, name, data)
}

func SetAppIcon(png []byte) error { return core.SetAppIcon(png) }
