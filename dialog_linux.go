// Native file dialogs on Linux, via GtkFileChooserNative + gtk_native_dialog_run.
// The run is application-modal (it spins its own nested main loop), so it must
// run on the GTK/UI thread; each method dispatches onto that thread (Dispatch ->
// g_idle) and blocks the calling goroutine until the user dismisses the dialog,
// matching the macOS NSOpenPanel backend.
//
// Stack split (like webview_linux.go): GTK3 returns results as plain paths
// (char* / GSList*), GTK4 returns GFile* / GListModel* (which need GIO to turn
// into paths). The extra symbols are resolved lazily from the library handles
// ensureInit kept (gtkLib/glibLib) plus libgio for the GTK4 path.

package glaze

import (
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	gtkFileChooserActionOpen         = 0
	gtkFileChooserActionSave         = 1
	gtkFileChooserActionSelectFolder = 2

	gtkResponseAccept = -3
)

// --- bound dialog functions ------------------------------------------------

var (
	gtkFileChooserNativeNew         func(title string, parent uintptr, action int, accept, cancel string) uintptr
	gtkNativeDialogRun              func(dialog uintptr) int
	gtkFileChooserSetSelectMultiple func(chooser uintptr, multiple bool)
	gtkFileChooserSetCurrentName    func(chooser uintptr, name string)

	gtkFileFilterNew        func() uintptr
	gtkFileFilterSetName    func(filter uintptr, name string)
	gtkFileFilterAddPattern func(filter uintptr, pattern string)
	gtkFileChooserAddFilter func(chooser, filter uintptr)

	// GTK3 path-based result + folder selection.
	gtkFileChooserGetFilename      func(chooser uintptr) uintptr // char*
	gtkFileChooserGetFilenames     func(chooser uintptr) uintptr // GSList* of char*
	gtkFileChooserSetCurrentFolder func(chooser uintptr, path string) bool
	gSListFree                     func(list uintptr)

	// GTK4 GFile/GListModel-based result + folder selection (need GIO).
	gtkFileChooserGetFile           func(chooser uintptr) uintptr // GFile*
	gtkFileChooserGetFiles          func(chooser uintptr) uintptr // GListModel*
	gtkFileChooserSetCurrentFolder4 func(chooser, file, err uintptr) bool
	gFileNewForPath                 func(path string) uintptr
	gFileGetPath                    func(file uintptr) uintptr // char*
	gListModelGetNItems             func(model uintptr) uint32
	gListModelGetItem               func(model uintptr, pos uint32) uintptr

	dialogInitOnce sync.Once
	dialogInitErr  error
)

// ensureDialogInit resolves the file-dialog symbols once, branching on the GTK
// version detected by ensureInit.
func ensureDialogInit() error {
	if err := ensureInit(); err != nil {
		return err
	}
	dialogInitOnce.Do(func() {
		purego.RegisterLibFunc(&gtkFileChooserNativeNew, gtkLib, "gtk_file_chooser_native_new")
		purego.RegisterLibFunc(&gtkNativeDialogRun, gtkLib, "gtk_native_dialog_run")
		purego.RegisterLibFunc(&gtkFileChooserSetSelectMultiple, gtkLib, "gtk_file_chooser_set_select_multiple")
		purego.RegisterLibFunc(&gtkFileChooserSetCurrentName, gtkLib, "gtk_file_chooser_set_current_name")
		purego.RegisterLibFunc(&gtkFileFilterNew, gtkLib, "gtk_file_filter_new")
		purego.RegisterLibFunc(&gtkFileFilterSetName, gtkLib, "gtk_file_filter_set_name")
		purego.RegisterLibFunc(&gtkFileFilterAddPattern, gtkLib, "gtk_file_filter_add_pattern")
		purego.RegisterLibFunc(&gtkFileChooserAddFilter, gtkLib, "gtk_file_chooser_add_filter")

		if gtk4 {
			purego.RegisterLibFunc(&gtkFileChooserGetFile, gtkLib, "gtk_file_chooser_get_file")
			purego.RegisterLibFunc(&gtkFileChooserGetFiles, gtkLib, "gtk_file_chooser_get_files")
			purego.RegisterLibFunc(&gtkFileChooserSetCurrentFolder4, gtkLib, "gtk_file_chooser_set_current_folder")
			gio, e := openFirst("libgio-2.0.so.0")
			if e != nil {
				dialogInitErr = e
				return
			}
			purego.RegisterLibFunc(&gFileNewForPath, gio, "g_file_new_for_path")
			purego.RegisterLibFunc(&gFileGetPath, gio, "g_file_get_path")
			purego.RegisterLibFunc(&gListModelGetNItems, gio, "g_list_model_get_n_items")
			purego.RegisterLibFunc(&gListModelGetItem, gio, "g_list_model_get_item")
			return
		}
		purego.RegisterLibFunc(&gtkFileChooserGetFilename, gtkLib, "gtk_file_chooser_get_filename")
		purego.RegisterLibFunc(&gtkFileChooserGetFilenames, gtkLib, "gtk_file_chooser_get_filenames")
		purego.RegisterLibFunc(&gtkFileChooserSetCurrentFolder, gtkLib, "gtk_file_chooser_set_current_folder")
		purego.RegisterLibFunc(&gSListFree, glibLib, "g_slist_free")
	})
	return dialogInitErr
}

// --- the WebView file-dialog methods ---------------------------------------

// OpenFile shows a modal open-file dialog and returns the chosen path, or ""
// when cancelled.
func (w *webview) OpenFile(opts FileDialogOptions) (string, error) {
	paths, err := w.showFileDialog(gtkFileChooserActionOpen, false, opts)
	return firstPath(paths), err
}

// OpenFiles shows a modal open-file dialog that allows multiple selection and
// returns the chosen paths, or nil when cancelled.
func (w *webview) OpenFiles(opts FileDialogOptions) ([]string, error) {
	return w.showFileDialog(gtkFileChooserActionOpen, true, opts)
}

// SaveFile shows a modal save-file dialog and returns the chosen path, or ""
// when cancelled.
func (w *webview) SaveFile(opts FileDialogOptions) (string, error) {
	paths, err := w.showFileDialog(gtkFileChooserActionSave, false, opts)
	return firstPath(paths), err
}

// OpenDirectory shows a modal directory chooser and returns the chosen path, or
// "" when cancelled.
func (w *webview) OpenDirectory(opts FileDialogOptions) (string, error) {
	paths, err := w.showFileDialog(gtkFileChooserActionSelectFolder, false, opts)
	return firstPath(paths), err
}

// showFileDialog runs the chooser on the UI thread and blocks the caller until
// it is dismissed (gtk_native_dialog_run is modal and spins its own loop, so it
// must run on the GTK thread).
func (w *webview) showFileDialog(action int, multi bool, opts FileDialogOptions) ([]string, error) {
	if err := ensureDialogInit(); err != nil {
		return nil, err
	}
	ch := make(chan []string, 1)
	w.Dispatch(func() {
		ch <- runFileChooser(w.window, action, multi, opts)
	})
	return <-ch, nil
}

func runFileChooser(parent uintptr, action int, multi bool, opts FileDialogOptions) []string {
	accept := "_Open"
	if action == gtkFileChooserActionSave {
		accept = "_Save"
	}
	dlg := gtkFileChooserNativeNew(opts.Title, parent, action, accept, "_Cancel")
	if dlg == 0 {
		return nil
	}
	defer gObjectUnref(dlg)

	if multi {
		gtkFileChooserSetSelectMultiple(dlg, true)
	}
	if opts.Directory != "" {
		setChooserFolder(dlg, opts.Directory)
	}
	if action == gtkFileChooserActionSave && opts.Filename != "" {
		gtkFileChooserSetCurrentName(dlg, opts.Filename)
	}
	if action != gtkFileChooserActionSelectFolder {
		applyChooserFilters(dlg, opts.Filters)
	}

	if gtkNativeDialogRun(dlg) != gtkResponseAccept {
		return nil
	}
	return chooserPaths(dlg, multi)
}

func setChooserFolder(chooser uintptr, dir string) {
	if gtk4 {
		file := gFileNewForPath(dir)
		if file == 0 {
			return
		}
		gtkFileChooserSetCurrentFolder4(chooser, file, 0)
		gObjectUnref(file)
		return
	}
	gtkFileChooserSetCurrentFolder(chooser, dir)
}

func applyChooserFilters(chooser uintptr, filters []FileFilter) {
	for _, f := range filters {
		var patterns []string
		wildcard := false
		for _, e := range f.Extensions {
			if e == "" || e == "*" {
				wildcard = true
				break
			}
			patterns = append(patterns, "*."+strings.TrimPrefix(e, "."))
		}
		filter := gtkFileFilterNew()
		name := f.Name
		if wildcard || len(patterns) == 0 {
			if name == "" {
				name = "All files"
			}
			gtkFileFilterSetName(filter, name)
			gtkFileFilterAddPattern(filter, "*")
		} else {
			if name == "" {
				name = strings.Join(patterns, ", ")
			}
			gtkFileFilterSetName(filter, name)
			for _, p := range patterns {
				gtkFileFilterAddPattern(filter, p)
			}
		}
		gtkFileChooserAddFilter(chooser, filter) // transfers ownership to the chooser
	}
}

// chooserPaths reads the selected path(s) out of a chooser after an accepted
// run, branching on the GTK version.
func chooserPaths(chooser uintptr, multi bool) []string {
	if gtk4 {
		if multi {
			return gListModelPaths(gtkFileChooserGetFiles(chooser))
		}
		file := gtkFileChooserGetFile(chooser) // transfer full
		if file == 0 {
			return nil
		}
		defer gObjectUnref(file)
		if p := gfilePath(file); p != "" {
			return []string{p}
		}
		return nil
	}
	if multi {
		return gSListPaths(gtkFileChooserGetFilenames(chooser))
	}
	cs := gtkFileChooserGetFilename(chooser) // char*, owned by caller
	if cs == 0 {
		return nil
	}
	p := cstr(cs)
	gFree(cs)
	if p == "" {
		return nil
	}
	return []string{p}
}

// gfilePath returns a GFile's local path ("" if it has none).
func gfilePath(file uintptr) string {
	cs := gFileGetPath(file) // char*, owned by caller
	if cs == 0 {
		return ""
	}
	p := cstr(cs)
	gFree(cs)
	return p
}

// gListModelPaths drains a GListModel<GFile> (GTK4 multi-select result) into
// paths, releasing the model and each item.
func gListModelPaths(model uintptr) []string {
	if model == 0 {
		return nil
	}
	defer gObjectUnref(model)
	n := gListModelGetNItems(model)
	paths := make([]string, 0, n)
	for i := uint32(0); i < n; i++ {
		file := gListModelGetItem(model, i) // transfer full (a ref)
		if file == 0 {
			continue
		}
		if p := gfilePath(file); p != "" {
			paths = append(paths, p)
		}
		gObjectUnref(file)
	}
	return paths
}

// gSListPaths drains a GSList<char*> (GTK3 multi-select result) into paths,
// freeing each string and the list.
func gSListPaths(list uintptr) []string {
	if list == 0 {
		return nil
	}
	var paths []string
	for node := list; node != 0; {
		data := *(*uintptr)(asPtr(node))
		next := *(*uintptr)(asPtr(node + unsafe.Sizeof(uintptr(0))))
		if data != 0 {
			if p := cstr(data); p != "" {
				paths = append(paths, p)
			}
			gFree(data)
		}
		node = next
	}
	gSListFree(list)
	return paths
}

// asPtr reinterprets a uintptr's bits as an unsafe.Pointer without a direct
// uintptr->Pointer conversion (keeps go vet's unsafeptr check quiet).
func asPtr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

func firstPath(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}
