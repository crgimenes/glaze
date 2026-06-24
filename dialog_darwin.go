package glaze

import (
	"strings"

	"github.com/ebitengine/purego/objc"
)

// Native file dialogs on macOS, via NSOpenPanel / NSSavePanel. The panels are
// application-modal (runModal), so they must run on the main thread; each
// method dispatches the panel onto the UI thread and blocks the calling
// goroutine until the user dismisses it. The same panel-configuration core is
// shared with the WKUIDelegate <input type=file> handler (configureOpenPanel).

// OpenFile shows a modal open-file dialog and returns the chosen path, or ""
// when cancelled.
func (w *webview) OpenFile(opts FileDialogOptions) (string, error) {
	return firstOr(w.presentOpenPanel(opts, true, false, false), ""), nil
}

// OpenFiles shows a modal open-file dialog that allows multiple selection and
// returns the chosen paths, or nil when cancelled.
func (w *webview) OpenFiles(opts FileDialogOptions) ([]string, error) {
	return w.presentOpenPanel(opts, true, false, true), nil
}

// OpenDirectory shows a modal directory chooser and returns the chosen path, or
// "" when cancelled.
func (w *webview) OpenDirectory(opts FileDialogOptions) (string, error) {
	return firstOr(w.presentOpenPanel(opts, false, true, false), ""), nil
}

// SaveFile shows a modal save-file dialog and returns the chosen path, or ""
// when cancelled.
func (w *webview) SaveFile(opts FileDialogOptions) (string, error) {
	return w.presentSavePanel(opts), nil
}

// presentOpenPanel builds and runs an NSOpenPanel on the UI thread and returns
// the selected filesystem paths (empty when cancelled).
func (w *webview) presentOpenPanel(opts FileDialogOptions, canFiles, canDirs, multiple bool) []string {
	ch := make(chan []string, 1)
	w.Dispatch(func() {
		var paths []string
		autorelease(func() {
			panel := class("NSOpenPanel").Send(sel("openPanel"))
			configureOpenPanel(panel, canFiles, canDirs, multiple, opts)
			if int(panel.Send(sel("runModal"))) == nsModalResponseOK { // #nosec G115 -- NSModalResponse is a small int
				paths = urlArrayPaths(panel.Send(sel("URLs")))
			}
		})
		ch <- paths
	})
	return <-ch
}

// presentSavePanel builds and runs an NSSavePanel on the UI thread and returns
// the chosen path (empty when cancelled).
func (w *webview) presentSavePanel(opts FileDialogOptions) string {
	ch := make(chan string, 1)
	w.Dispatch(func() {
		var path string
		autorelease(func() {
			panel := class("NSSavePanel").Send(sel("savePanel"))
			applyCommonPanelOptions(panel, opts)
			if opts.Filename != "" {
				panel.Send(sel("setNameFieldStringValue:"), nsstr(opts.Filename))
			}
			types := allowedFileTypes(opts.Filters)
			if types != 0 {
				panel.Send(sel("setAllowedFileTypes:"), types)
			}
			if int(panel.Send(sel("runModal"))) == nsModalResponseOK { // #nosec G115 -- NSModalResponse is a small int
				path = urlPath(panel.Send(sel("URL")))
			}
		})
		ch <- path
	})
	return <-ch
}

// configureOpenPanel applies the open-panel settings shared by the public
// OpenFile/OpenFiles/OpenDirectory methods and the WKUIDelegate file chooser.
func configureOpenPanel(panel objc.ID, canFiles, canDirs, multiple bool, opts FileDialogOptions) {
	panel.Send(sel("setCanChooseFiles:"), canFiles)
	panel.Send(sel("setCanChooseDirectories:"), canDirs)
	panel.Send(sel("setAllowsMultipleSelection:"), multiple)
	applyCommonPanelOptions(panel, opts)
	types := allowedFileTypes(opts.Filters)
	if types != 0 {
		panel.Send(sel("setAllowedFileTypes:"), types)
	}
}

// applyCommonPanelOptions sets the title and initial directory common to the
// open and save panels.
func applyCommonPanelOptions(panel objc.ID, opts FileDialogOptions) {
	if opts.Title != "" {
		// On modern macOS the panel has no title-bar text; setMessage shows the
		// label prominently above the file list, which is the visible spot.
		panel.Send(sel("setMessage:"), nsstr(opts.Title))
	}
	if opts.Directory != "" {
		url := class("NSURL").Send(sel("fileURLWithPath:"), nsstr(opts.Directory))
		panel.Send(sel("setDirectoryURL:"), url)
	}
}

// allowedFileTypes flattens the filter extensions into an NSArray<NSString*> of
// bare extensions for setAllowedFileTypes:. It returns 0 (no restriction) when
// there are no extensions or any filter is a wildcard.
func allowedFileTypes(filters []FileFilter) objc.ID {
	var exts []string
	for _, f := range filters {
		for _, e := range f.Extensions {
			if e == "" || e == "*" {
				return 0
			}
			exts = append(exts, strings.TrimPrefix(e, "."))
		}
	}
	if len(exts) == 0 {
		return 0
	}
	arr := class("NSMutableArray").Send(sel("array"))
	for _, e := range exts {
		arr.Send(sel("addObject:"), nsstr(e))
	}
	return arr
}

// urlArrayPaths reads the filesystem paths out of an NSArray<NSURL*>.
func urlArrayPaths(urls objc.ID) []string {
	if urls == 0 {
		return nil
	}
	n := int(urls.Send(sel("count"))) // #nosec G115 -- NSArray count is a small non-negative int
	paths := make([]string, 0, n)
	for i := range n {
		p := urlPath(urls.Send(sel("objectAtIndex:"), uint(i)))
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// urlPath returns the filesystem path of an NSURL ("" for a nil URL).
func urlPath(url objc.ID) string {
	if url == 0 {
		return ""
	}
	return cstr(url.Send(sel("path")).Send(sel("UTF8String")))
}

// firstOr returns the first element of s, or def when s is empty.
func firstOr(s []string, def string) string {
	if len(s) == 0 {
		return def
	}
	return s[0]
}
