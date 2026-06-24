package glaze

// This file defines the cross-platform surface for the native file dialogs.
// Upstream webview has no such feature; it is a glaze extension. The per-OS
// implementations live in dialog_darwin.go, dialog_windows.go and
// dialog_linux.go and are reached through the WebView interface methods
// OpenFile/OpenFiles/SaveFile/OpenDirectory.

// FileFilter restricts a file dialog to files of a given kind.
type FileFilter struct {
	// Name is the human-readable label for this filter (e.g. "Images").
	Name string

	// Extensions lists the file extensions WITHOUT the leading dot
	// (e.g. {"png", "jpg"}). An empty list, or an entry "*", matches any file.
	Extensions []string
}

// FileDialogOptions configures a native file dialog. The zero value is valid:
// it shows a default dialog rooted at the platform's default directory with no
// type filtering.
type FileDialogOptions struct {
	// Title overrides the dialog's title.
	Title string

	// Directory is the initial directory the dialog displays, as a filesystem
	// path. Empty uses the platform default (usually the last-used directory).
	Directory string

	// Filename is the suggested file name. It is used by SaveFile and ignored
	// by the open and directory dialogs.
	Filename string

	// Filters limits the selectable file types. An empty list shows all files.
	// Filters are advisory: a platform may present them differently or let the
	// user override them.
	Filters []FileFilter
}
