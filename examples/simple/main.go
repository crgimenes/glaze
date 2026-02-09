package main

import (
	"github.com/crgimenes/go-webview"
	_ "github.com/crgimenes/go-webview/embedded"
)

func main() {
	w := webview.New(true)
	w.SetTitle("Basic Example")
	w.SetSize(480, 320, webview.HintNone)
	w.SetHtml("Thanks for using webview!")
	w.Run()
	w.Destroy()
}
