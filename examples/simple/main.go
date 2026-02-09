package main

import (
	"github.com/abemedia/go-webview"
	_ "github.com/abemedia/go-webview/embedded"
)

func main() {
	w := webview.New(true)
	w.SetTitle("Basic Example")
	w.SetSize(480, 320, webview.HintNone)
	w.SetHtml("Thanks for using webview!")
	w.Run()
	w.Destroy()
}
