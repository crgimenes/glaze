# Contributing to Glaze

Thanks for your interest. Glaze is a small, opinionated project, and two rules
define it. A change that respects them is welcome; a change that fights them will
be declined no matter how well written. Please read these first; they save
everyone time.

## 1. No CGo. Ever.

This isn't a preference, it's the point of the project. Glaze binds the OS
WebView through [purego](https://github.com/ebitengine/purego) precisely so there
is no C compiler in the loop. That is what gives you cross-compilation to every
desktop from one machine, `CGO_ENABLED=0` reproducible builds, and a `go install`
that works with no toolchain to set up.

Concretely, a contribution must:

- Use **no `import "C"`** and pull in **no dependency that needs cgo**. `purego`,
  `syscall` / `golang.org/x/sys`, and the standard library are the tools; native
  APIs are reached via `dlopen` / `LoadLibrary` / the Objective-C runtime / COM,
  never a C shim.
- Keep **`CGO_ENABLED=0 go build` green for every `GOOS/GOARCH`** glaze targets
  (see the cross-build in the checklist). Nothing is bundled: no
  `.dll`/`.dylib`/`.so` ships with the binary; it loads the WebView the OS already
  has.

A feature that can only be built with cgo does not belong in glaze. That's a
boundary, not a TODO.

## 2. YAGNI: build the least that works.

The best code is the code never written. Before adding anything, stop at the first
rung that holds: does it need to exist at all? is it already here? does the stdlib
do it? does a platform API cover it? can it be a few lines? Only then write the
minimum.

- **No speculative features, no unrequested abstraction.** No interface with one
  implementation, no config knob nobody sets, no "we might need it later." Inline
  it until a second real case exists.
- **No new dependency** the stdlib, a syscall, or a few lines can cover. Glaze's
  only dependency is purego; keep it that way.
- **Deletion over addition, boring over clever.** The shortest change that
  correctly solves the problem wins.

Never minimize away the things that matter: input validation, error handling that
prevents data loss, security, bounded execution, and one runnable test for
non-trivial logic.

## Scope: what glaze is, and is not

Glaze is a **desktop** WebView binding for **Windows, macOS, and Linux**. That's
the whole surface.

Out of scope for this repository (these keep coming up; the answer is the same):

- **Mobile (iOS / Android).** Mobile needs JNI / a native app shell / the
  `gomobile` toolchain; that means cgo and a C toolchain, the exact things glaze
  exists to avoid. A mobile port is a different project with a different core; it
  can live happily on its own, just not merged in here.
- **A plugin system / marketplace.** Glaze is a Go library; "plugins" are just
  packages you import and compose; Go already gives you that. A formal registry
  is scope glaze doesn't want.
- **A GUI toolkit.** Build the UI with HTML/CSS/JS in the WebView, or use a real
  toolkit.

Standalone, window-independent OS bindings (clipboard, notifications,
single-instance, open-url, ...) live in the sibling
[native](https://github.com/crgimenes/native) project, not here. If your idea is
one of those, that's where it goes.

## Platforms: all three, or honest about it

A feature should work on macOS, Windows, and Linux. Where a platform genuinely
can't do something cheaply and safely, return a clear error (e.g.
`ErrUnsupported`) and say so in the docs.
"Works on two, honestly unsupported on the third" is acceptable; "pretends to
work" is not.

## Code style

Go, `gofmt`-formatted, **US English everywhere**: comments, identifiers, docs. A
few house rules the linters don't enforce:

- **No inline `if`-init.** Assign on its own line, then `if`. Not
  `if x, err := f(); err != nil { ... }`.
- **No `else` after a terminal branch**; return early instead. Prefer `switch`
  over a long `else if` chain. Keep it flat.
- **Comments explain _why_, not _what_.** Don't restate the code; note the intent,
  the gotcha, the reason it's there.

## The cgo-free discipline (the part that bites)

cgo's pointer rules still apply conceptually. Learned the hard way porting the
three backends:

- **No Go pointer is ever held by native code.** The GC moves memory. Pass an
  integer id and resolve it through a package-level `map[id]*T`; keep callback
  trampolines and COM vtables in a package global so they stay alive and
  unmovable.
- **Struct-by-value is architecture-specific.** A 16-byte struct goes by hidden
  reference on amd64 but packs into registers on arm64, so it needs
  `*_amd64.go` / `*_arm64.go`. It **compiles clean and breaks at runtime**;
  cross-compilation won't catch it.
- **Thread affinity.** Cocoa / GTK / COM are pinned to a thread; use
  `runtime.LockOSThread` and route cross-thread work through `Dispatch`. On macOS,
  an `NSAutoreleasePool` must be created and drained on the same locked thread.
- **Windows has no `Dlopen`**; resolve symbols with `syscall.LoadLibrary` +
  `GetProcAddress` + `purego.RegisterFunc`. Never load two conflicting versions of
  one library in a process (the GTK3/GTK4 `gtk_init` crash).

## Before you open a PR

Run the checks locally; CI runs the same on macOS, Windows, and Linux, and a PR
isn't done until they're green:

```sh
go fix ./...            # modernizations; the step people forget
gofmt -l .              # must print nothing
go vet ./...
go test -short ./...    # -short skips the real-window GUI tests

# golangci-lint must be clean on each GOOS:
for os in darwin linux windows; do GOOS=$os golangci-lint run ./...; done

# cross-compile every target; ABI bugs surface here, not in a single build:
for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
  GOOS=${t%/*} GOARCH=${t#*/} go build ./... || echo "FAIL $t"
done

cd examples && go build ./... && go vet ./...   # examples are a separate module
```

If you touch FFI, keep `gosec ./...` clean too; audited `unsafe` / `uintptr`
uses are marked `// #nosec Gxxx` (the hash form), never a bare `//nosec`.

## Testing reality

ABI bugs don't appear in cross-compilation; you have to run on the target, which
is what CI is for. The GUI tests **skip themselves** when the system WebView can't
run (headless, or the libraries aren't installed), so the suite stays green on a
minimal box instead of failing. Add a test for non-trivial logic and keep it able
to run headless where you can.

## Proposing a change

- **Docs, typos, small fixes:** open a PR directly. Documentation improvements are
  genuinely welcome.
- **Anything larger (a new feature, a new API, a new backend):** open an issue
  first so we can agree it fits the scope and the two rules above *before* you
  write it. That spares you from building something that can't be merged.

Small, focused PRs review fastest. Thanks for helping keep glaze small and
cgo-free.
