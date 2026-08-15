module github.com/crgimenes/glaze/examples

go 1.26.5

require (
	github.com/crgimenes/devengine v0.0.10
	github.com/crgimenes/filo v0.0.16
	github.com/crgimenes/glaze v0.0.18
	github.com/crgimenes/native v0.1.7
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ebitengine/purego v0.10.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.23 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	modernc.org/libc v1.74.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.54.0 // indirect
)

// Examples always build against the glaze in this repo, not a published tag.
replace github.com/crgimenes/glaze => ../
