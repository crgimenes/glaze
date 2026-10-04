package glaze

// NavigationKind says how a main-frame navigation ended.
type NavigationKind int

const (
	NavigationFinished NavigationKind = iota
	NavigationFailed
)

// ScriptDialogKind is which of a page's script dialogs is asked for.
type ScriptDialogKind int

const (
	ScriptAlert ScriptDialogKind = iota
	ScriptConfirm
	ScriptPrompt
)

// ScriptDialog is a page's alert(), confirm() or prompt(): Message is what the
// page says, Text the prompt's default answer.
type ScriptDialog struct {
	Kind    ScriptDialogKind
	Message string
	Text    string
}

// NavigationEvent reports the end of a main-frame navigation. Err is set only
// for NavigationFailed; URL is empty when the engine does not know it. TLS
// marks a failure of the secure connection, such as an untrusted, expired or
// mismatched certificate. A navigation cancelled because another replaced it
// is not a failure and is not reported.
type NavigationEvent struct {
	Kind NavigationKind
	URL  string
	Err  error
	TLS  bool
}
