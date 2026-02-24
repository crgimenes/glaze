package main

import (
	"log"

	"github.com/crgimenes/go-webview"
	_ "github.com/crgimenes/go-webview/embedded"
)

func main() {
	w, err := webview.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Basic Example")
	w.SetSize(480, 320, webview.HintNone)
	w.SetHtml("Thanks for using webview!")
	w.Run()
}
