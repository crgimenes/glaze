package glaze

import "errors"

// errDialogUnsupported is returned by the file-dialog methods until the Linux
// (GtkFileChooserNative) backend lands.
var errDialogUnsupported = errors.New("glaze: native file dialogs are not yet implemented on linux")

func (w *webview) OpenFile(opts FileDialogOptions) (string, error) {
	_ = opts
	return "", errDialogUnsupported
}

func (w *webview) OpenFiles(opts FileDialogOptions) ([]string, error) {
	_ = opts
	return nil, errDialogUnsupported
}

func (w *webview) SaveFile(opts FileDialogOptions) (string, error) {
	_ = opts
	return "", errDialogUnsupported
}

func (w *webview) OpenDirectory(opts FileDialogOptions) (string, error) {
	_ = opts
	return "", errDialogUnsupported
}
