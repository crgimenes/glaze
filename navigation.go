package glaze

// NavigationKind says how a main-frame navigation ended.
type NavigationKind int

const (
	NavigationFinished NavigationKind = iota
	NavigationFailed
)

// NavigationEvent reports the end of a main-frame navigation. Err is set only
// for NavigationFailed; URL is empty when the engine does not know it.
type NavigationEvent struct {
	Kind NavigationKind
	URL  string
	Err  error
}
