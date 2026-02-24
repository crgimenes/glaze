package main

import (
	"log"

	"github.com/crgimenes/glaze"
	_ "github.com/crgimenes/glaze/embedded"
)

func main() {
	w, err := glaze.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Basic Example")
	w.SetSize(480, 320, glaze.HintNone)
	w.SetHtml("Thanks for using webview!")
	w.Run()
}
