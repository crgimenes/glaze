// Native file dialogs on Windows, via the Common Item Dialog COM API
// (IFileOpenDialog / IFileSaveDialog). The dialogs are application-modal: Show
// runs its own message loop, so they must run on the UI thread. Each method
// dispatches onto the UI thread (w.Dispatch) and blocks the calling goroutine
// until the user dismisses the dialog, matching the macOS NSOpenPanel backend.
//
// COM idiom: same as webview2_windows.go -- each interface is a struct whose
// first field is the vtbl pointer, the vtbl is a struct of uintptr slots in
// exact IDL order, and a method call is purego.SyscallN(vtbl.Method, this,
// args...). The guid type, the ptr/utf16/wideToString helpers, coTaskMemFree
// and ensureCOMInit are shared from webview2_windows.go.

package glaze

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

// --- CLSIDs / IIDs ---------------------------------------------------------

var (
	clsidFileOpenDialog = guid{0xDC1C5A9C, 0xE88A, 0x4DDE, [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	clsidFileSaveDialog = guid{0xC0B4E2F3, 0xBA21, 0x4773, [8]byte{0x8D, 0xBA, 0x33, 0x5E, 0xC9, 0x46, 0xEB, 0x8B}}
	iidIFileOpenDialog  = guid{0xD57C7288, 0xD4AD, 0x4768, [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidIFileSaveDialog  = guid{0x84BCCD23, 0x5FDE, 0x4CDB, [8]byte{0xAE, 0xA4, 0xAF, 0x64, 0xB8, 0x3D, 0x78, 0xAB}}
	iidIShellItem       = guid{0x43826D1E, 0xE718, 0x42EE, [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

const (
	clsctxInprocServer = 0x1

	// FILEOPENDIALOGOPTIONS bits.
	fosOverwritePrompt  = 0x00000002
	fosPickFolders      = 0x00000020
	fosForceFilesystem  = 0x00000040
	fosAllowMultiSelect = 0x00000200

	// SIGDN_FILESYSPATH for IShellItem::GetDisplayName.
	sigdnFileSysPath = 0x80058000

	// HRESULT_FROM_WIN32(ERROR_CANCELLED): the user dismissed the dialog.
	hrCancelled = 0x800704C7
)

// --- COM vtable layouts (exact IDL order; uintptr per slot) ----------------

type iModalWindowVtbl struct {
	iUnknownVtbl
	Show uintptr // HRESULT Show(HWND)
}

type iFileDialogVtbl struct {
	iModalWindowVtbl
	SetFileTypes        uintptr // (UINT, const COMDLG_FILTERSPEC*)
	SetFileTypeIndex    uintptr
	GetFileTypeIndex    uintptr
	Advise              uintptr
	Unadvise            uintptr
	SetOptions          uintptr // (FILEOPENDIALOGOPTIONS)
	GetOptions          uintptr // (FILEOPENDIALOGOPTIONS*)
	SetDefaultFolder    uintptr
	SetFolder           uintptr // (IShellItem*)
	GetFolder           uintptr
	GetCurrentSelection uintptr
	SetFileName         uintptr // (LPCWSTR)
	GetFileName         uintptr
	SetTitle            uintptr // (LPCWSTR)
	SetOkButtonLabel    uintptr
	SetFileNameLabel    uintptr
	GetResult           uintptr // (IShellItem**)
	AddPlace            uintptr
	SetDefaultExtension uintptr // (LPCWSTR)
	Close               uintptr
	SetClientGuid       uintptr
	ClearClientData     uintptr
	SetFilter           uintptr
}

type iFileOpenDialogVtbl struct {
	iFileDialogVtbl
	GetResults       uintptr // (IShellItemArray**)
	GetSelectedItems uintptr
}

type iShellItemVtbl struct {
	iUnknownVtbl
	BindToHandler  uintptr
	GetParent      uintptr
	GetDisplayName uintptr // (SIGDN, LPWSTR*)
	GetAttributes  uintptr
	Compare        uintptr
}

type iShellItemArrayVtbl struct {
	iUnknownVtbl
	BindToHandler              uintptr
	GetPropertyStore           uintptr
	GetPropertyDescriptionList uintptr
	GetAttributes              uintptr
	GetCount                   uintptr // (DWORD*)
	GetItemAt                  uintptr // (DWORD, IShellItem**)
	EnumItems                  uintptr
}

// Interface wrappers (the vtbl pointer is the object's first field).
type iFileDialog struct{ vtbl *iFileDialogVtbl }
type iFileOpenDialog struct{ vtbl *iFileOpenDialogVtbl }
type iShellItem struct{ vtbl *iShellItemVtbl }
type iShellItemArray struct{ vtbl *iShellItemArrayVtbl }

// comdlgFilterSpec mirrors COMDLG_FILTERSPEC.
type comdlgFilterSpec struct {
	pszName *uint16
	pszSpec *uint16
}

func (d *iFileDialog) Show(parent uintptr) int32 {
	r, _, _ := purego.SyscallN(d.vtbl.Show, uintptr(unsafe.Pointer(d)), parent)
	return int32(r)
}
func (d *iFileDialog) GetOptions() uint32 {
	var fos uint32
	purego.SyscallN(d.vtbl.GetOptions, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&fos)))
	return fos
}
func (d *iFileDialog) SetOptions(fos uint32) {
	purego.SyscallN(d.vtbl.SetOptions, uintptr(unsafe.Pointer(d)), uintptr(fos))
}
func (d *iFileDialog) SetTitle(s *uint16) {
	purego.SyscallN(d.vtbl.SetTitle, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(s)))
}
func (d *iFileDialog) SetFileName(s *uint16) {
	purego.SyscallN(d.vtbl.SetFileName, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(s)))
}
func (d *iFileDialog) SetFolder(si uintptr) {
	purego.SyscallN(d.vtbl.SetFolder, uintptr(unsafe.Pointer(d)), si)
}
func (d *iFileDialog) SetFileTypes(n uint32, specs *comdlgFilterSpec) {
	purego.SyscallN(d.vtbl.SetFileTypes, uintptr(unsafe.Pointer(d)), uintptr(n), uintptr(unsafe.Pointer(specs)))
}
func (d *iFileDialog) GetResult(out *uintptr) int32 {
	r, _, _ := purego.SyscallN(d.vtbl.GetResult, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(out)))
	return int32(r)
}
func (d *iFileDialog) Release() {
	purego.SyscallN(d.vtbl.Release, uintptr(unsafe.Pointer(d)))
}

func (d *iFileOpenDialog) GetResults(out *uintptr) int32 {
	r, _, _ := purego.SyscallN(d.vtbl.GetResults, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(out)))
	return int32(r)
}

func (s *iShellItem) GetDisplayName(sigdn uint32, out *uintptr) int32 {
	r, _, _ := purego.SyscallN(s.vtbl.GetDisplayName, uintptr(unsafe.Pointer(s)), uintptr(sigdn), uintptr(unsafe.Pointer(out)))
	return int32(r)
}
func (s *iShellItem) Release() {
	purego.SyscallN(s.vtbl.Release, uintptr(unsafe.Pointer(s)))
}

func (a *iShellItemArray) GetCount(out *uint32) int32 {
	r, _, _ := purego.SyscallN(a.vtbl.GetCount, uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(out)))
	return int32(r)
}
func (a *iShellItemArray) GetItemAt(i uint32, out *uintptr) int32 {
	r, _, _ := purego.SyscallN(a.vtbl.GetItemAt, uintptr(unsafe.Pointer(a)), uintptr(i), uintptr(unsafe.Pointer(out)))
	return int32(r)
}
func (a *iShellItemArray) Release() {
	purego.SyscallN(a.vtbl.Release, uintptr(unsafe.Pointer(a)))
}

// --- bound ole32 / shell32 functions ---------------------------------------

var (
	coCreateInstance            func(rclsid *guid, pUnkOuter uintptr, clsCtx uint32, riid *guid, ppv *uintptr) int32
	shCreateItemFromParsingName func(name *uint16, pbc uintptr, riid *guid, ppv *uintptr) int32

	dialogInitOnce sync.Once
	dialogInitErr  error
)

// ensureDialogInit resolves CoCreateInstance (ole32) and
// SHCreateItemFromParsingName (shell32) once. ensureCOMInit (shared) has already
// loaded ole32 + CoTaskMemFree.
func ensureDialogInit() error {
	dialogInitOnce.Do(func() {
		err := ensureCOMInit()
		if err != nil {
			dialogInitErr = err
			return
		}
		ole32, err := syscall.LoadLibrary("ole32.dll")
		if err != nil {
			dialogInitErr = fmt.Errorf("load ole32.dll: %w", err)
			return
		}
		shell32, err := syscall.LoadLibrary("shell32.dll")
		if err != nil {
			dialogInitErr = fmt.Errorf("load shell32.dll: %w", err)
			return
		}
		reg := func(fn any, dll syscall.Handle, name string) {
			if dialogInitErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(dll, name)
			if e != nil {
				dialogInitErr = fmt.Errorf("resolve %s: %w", name, e)
				return
			}
			purego.RegisterFunc(fn, addr)
		}
		reg(&coCreateInstance, ole32, "CoCreateInstance")
		reg(&shCreateItemFromParsingName, shell32, "SHCreateItemFromParsingName")
	})
	return dialogInitErr
}

// --- the WebView file-dialog methods ---------------------------------------

// OpenFile shows a modal open-file dialog and returns the chosen path, or ""
// when cancelled.
func (w *webview) OpenFile(opts FileDialogOptions) (string, error) {
	paths, err := w.showFileDialog(false, false, false, opts)
	return firstPath(paths), err
}

// OpenFiles shows a modal open-file dialog that allows multiple selection and
// returns the chosen paths, or nil when cancelled.
func (w *webview) OpenFiles(opts FileDialogOptions) ([]string, error) {
	return w.showFileDialog(false, false, true, opts)
}

// SaveFile shows a modal save-file dialog and returns the chosen path, or ""
// when cancelled.
func (w *webview) SaveFile(opts FileDialogOptions) (string, error) {
	paths, err := w.showFileDialog(true, false, false, opts)
	return firstPath(paths), err
}

// OpenDirectory shows a modal directory chooser and returns the chosen path, or
// "" when cancelled.
func (w *webview) OpenDirectory(opts FileDialogOptions) (string, error) {
	paths, err := w.showFileDialog(false, true, false, opts)
	return firstPath(paths), err
}

// showFileDialog runs the dialog on the UI thread and blocks the caller until it
// is dismissed (the Common Item Dialog's Show is application-modal and pumps its
// own loop, so it must run on the message-pump thread).
func (w *webview) showFileDialog(save, pickFolders, multi bool, opts FileDialogOptions) ([]string, error) {
	err := ensureDialogInit()
	if err != nil {
		return nil, err
	}
	type outcome struct {
		paths []string
		err   error
	}
	ch := make(chan outcome, 1)
	w.Dispatch(func() {
		paths, err := runFileDialog(w.window, save, pickFolders, multi, opts)
		ch <- outcome{paths, err}
	})
	r := <-ch
	return r.paths, r.err
}

func runFileDialog(parent uintptr, save, pickFolders, multi bool, opts FileDialogOptions) ([]string, error) {
	clsid, iid := &clsidFileOpenDialog, &iidIFileOpenDialog
	if save {
		clsid, iid = &clsidFileSaveDialog, &iidIFileSaveDialog
	}
	var pdlg uintptr
	hr := coCreateInstance(clsid, 0, clsctxInprocServer, iid, &pdlg)
	if hr < 0 || pdlg == 0 {
		return nil, fmt.Errorf("glaze: CoCreateInstance(file dialog) failed: 0x%08X", uint32(hr))
	}
	dlg := (*iFileDialog)(ptr(pdlg))
	defer dlg.Release()

	fos := dlg.GetOptions() | fosForceFilesystem
	if pickFolders {
		fos |= fosPickFolders
	}
	if multi {
		fos |= fosAllowMultiSelect
	}
	if save {
		fos |= fosOverwritePrompt
	}
	dlg.SetOptions(fos)

	if opts.Title != "" {
		dlg.SetTitle(utf16(opts.Title))
	}
	if opts.Directory != "" {
		var psi uintptr
		if shCreateItemFromParsingName(utf16(opts.Directory), 0, &iidIShellItem, &psi) >= 0 && psi != 0 {
			dlg.SetFolder(psi)
			(*iShellItem)(ptr(psi)).Release()
		}
	}
	if save && opts.Filename != "" {
		dlg.SetFileName(utf16(opts.Filename))
	}
	if !pickFolders {
		specs, keep := buildFilterSpecs(opts.Filters)
		if len(specs) > 0 {
			dlg.SetFileTypes(uint32(len(specs)), &specs[0])
			runtime.KeepAlive(keep)
		}
	}

	hr = dlg.Show(parent)
	if hr < 0 {
		if uint32(hr) == hrCancelled {
			return nil, nil
		}
		return nil, fmt.Errorf("glaze: file dialog Show failed: 0x%08X", uint32(hr))
	}

	if !save && multi {
		return openDialogResults((*iFileOpenDialog)(ptr(pdlg))), nil
	}
	var psi uintptr
	if dlg.GetResult(&psi) < 0 || psi == 0 {
		return nil, nil
	}
	defer (*iShellItem)(ptr(psi)).Release()
	p := shellItemPath(psi)
	if p != "" {
		return []string{p}, nil
	}
	return nil, nil
}

// openDialogResults reads every selected path out of an IFileOpenDialog's
// IShellItemArray result.
func openDialogResults(odlg *iFileOpenDialog) []string {
	var parray uintptr
	if odlg.GetResults(&parray) < 0 || parray == 0 {
		return nil
	}
	arr := (*iShellItemArray)(ptr(parray))
	defer arr.Release()
	var n uint32
	arr.GetCount(&n)
	paths := make([]string, 0, n)
	for i := uint32(0); i < n; i++ {
		var psi uintptr
		if arr.GetItemAt(i, &psi) >= 0 && psi != 0 {
			p := shellItemPath(psi)
			if p != "" {
				paths = append(paths, p)
			}
			(*iShellItem)(ptr(psi)).Release()
		}
	}
	return paths
}

// shellItemPath returns the filesystem path of an IShellItem (the caller still
// owns and releases the item).
func shellItemPath(si uintptr) string {
	var pw uintptr
	if (*iShellItem)(ptr(si)).GetDisplayName(sigdnFileSysPath, &pw) < 0 || pw == 0 {
		return ""
	}
	s := wideToString(pw)
	coTaskMemFree(pw)
	return s
}

// buildFilterSpecs turns FileFilters into a COMDLG_FILTERSPEC array. It returns
// the array plus the backing UTF-16 buffers, which the caller must keep alive
// across SetFileTypes (and Show, conservatively).
func buildFilterSpecs(filters []FileFilter) ([]comdlgFilterSpec, [][]uint16) {
	var specs []comdlgFilterSpec
	var keep [][]uint16
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
		spec := "*.*"
		if !wildcard && len(patterns) > 0 {
			spec = strings.Join(patterns, ";")
		}
		name := f.Name
		if name == "" {
			name = spec
		}
		nameU := utf16Slice(name)
		specU := utf16Slice(spec)
		keep = append(keep, nameU, specU)
		specs = append(specs, comdlgFilterSpec{pszName: &nameU[0], pszSpec: &specU[0]})
	}
	return specs, keep
}

// utf16Slice is utf16's slice-returning sibling: it keeps the backing array
// addressable so its first element can be embedded in a C struct.
func utf16Slice(s string) []uint16 {
	u := make([]uint16, 0, len(s)+1)
	for _, r := range s {
		if r < 0x10000 {
			u = append(u, uint16(r))
		} else {
			r -= 0x10000
			u = append(u, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		}
	}
	return append(u, 0)
}

func firstPath(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}
