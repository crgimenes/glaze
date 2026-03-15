package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"math"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"syscall"

	"github.com/crgimenes/glaze"
	_ "github.com/crgimenes/glaze/embedded"
)

//go:embed ui/index.html ui/app.css ui/app.js
var uiFS embed.FS

type renderParams struct {
	fractalType   string
	colorScheme   string
	width         int
	height        int
	maxIterations int
	zoom          float64
	centerX       float64
	centerY       float64
	juliaCX       float64
	juliaCY       float64
}

func parseRenderParams(r *http.Request) (renderParams, error) {
	query := r.URL.Query()

	width, err := parseIntParam(query.Get("width"), 1, 1920)
	if err != nil {
		return renderParams{}, fmt.Errorf("invalid width: %w", err)
	}

	height, err := parseIntParam(query.Get("height"), 1, 1080)
	if err != nil {
		return renderParams{}, fmt.Errorf("invalid height: %w", err)
	}

	maxIterations, err := parseIntParam(query.Get("iterations"), 10, 4000)
	if err != nil {
		return renderParams{}, fmt.Errorf("invalid iterations: %w", err)
	}

	zoom, err := parseFloatParam(query.Get("zoom"), 0.000001, 1e12)
	if err != nil {
		return renderParams{}, fmt.Errorf("invalid zoom: %w", err)
	}

	centerX, err := parseFloatParam(query.Get("centerX"), -4, 4)
	if err != nil {
		return renderParams{}, fmt.Errorf("invalid centerX: %w", err)
	}

	centerY, err := parseFloatParam(query.Get("centerY"), -4, 4)
	if err != nil {
		return renderParams{}, fmt.Errorf("invalid centerY: %w", err)
	}

	juliaCX, err := parseFloatParam(query.Get("juliaCX"), -2, 2)
	if err != nil {
		return renderParams{}, fmt.Errorf("invalid juliaCX: %w", err)
	}

	juliaCY, err := parseFloatParam(query.Get("juliaCY"), -2, 2)
	if err != nil {
		return renderParams{}, fmt.Errorf("invalid juliaCY: %w", err)
	}

	fractalType := query.Get("fractal")
	if fractalType == "" {
		fractalType = "mandelbrot"
	}

	switch fractalType {
	case "mandelbrot", "julia":
	default:
		return renderParams{}, fmt.Errorf("invalid fractal type: %q", fractalType)
	}

	colorScheme := query.Get("color")
	if colorScheme == "" {
		colorScheme = "blue-gold"
	}

	switch colorScheme {
	case "blue-gold", "grayscale", "psychedelic":
	default:
		return renderParams{}, fmt.Errorf("invalid color scheme: %q", colorScheme)
	}

	return renderParams{
		fractalType:   fractalType,
		colorScheme:   colorScheme,
		width:         width,
		height:        height,
		maxIterations: maxIterations,
		zoom:          zoom,
		centerX:       centerX,
		centerY:       centerY,
		juliaCX:       juliaCX,
		juliaCY:       juliaCY,
	}, nil
}

func parseIntParam(raw string, minimum int, maximum int) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if value < minimum || value > maximum {
		return 0, fmt.Errorf("must be between %d and %d", minimum, maximum)
	}
	return value, nil
}

func parseFloatParam(raw string, minimum float64, maximum float64) (float64, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, err
	}
	if value < minimum || value > maximum {
		return 0, fmt.Errorf("must be between %g and %g", minimum, maximum)
	}
	return value, nil
}

func renderFractal(ctx context.Context, params renderParams) ([]byte, bool) {
	pixels := make([]byte, params.width*params.height*4)
	workers := runtime.GOMAXPROCS(0)
	if workers > params.height {
		workers = params.height
	}

	rows := make(chan int, params.height)
	results := make(chan bool, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- renderRows(ctx, params, pixels, rows)
		}()
	}

	for py := range params.height {
		if ctx.Err() != nil {
			close(rows)
			wg.Wait()
			close(results)
			return nil, false
		}
		rows <- py
	}
	close(rows)
	wg.Wait()
	close(results)
	for ok := range results {
		if !ok {
			return nil, false
		}
	}

	return pixels, true
}

func renderRows(ctx context.Context, params renderParams, pixels []byte, rows <-chan int) bool {
	widthHalf := float64(params.width) / 2
	heightHalf := float64(params.height) / 2
	scale := 4.0 / (float64(params.width) * params.zoom)

	for py := range rows {
		if ctx.Err() != nil {
			return false
		}
		y0 := (float64(py)-heightHalf)*scale + params.centerY
		rowOffset := py * params.width * 4
		for px := range params.width {
			x0 := (float64(px)-widthHalf)*scale + params.centerX
			iter := iterationsAt(params, x0, y0)
			r, g, b := colorForIteration(iter, params.maxIterations, params.colorScheme)
			pixelOffset := rowOffset + px*4
			pixels[pixelOffset] = r
			pixels[pixelOffset+1] = g
			pixels[pixelOffset+2] = b
			pixels[pixelOffset+3] = 255
		}
	}

	return true
}

func iterationsAt(params renderParams, x0 float64, y0 float64) int {
	if params.fractalType == "julia" {
		return juliaIterations(x0, y0, params.juliaCX, params.juliaCY, params.maxIterations)
	}
	return mandelbrotIterations(x0, y0, params.maxIterations)
}

func mandelbrotIterations(x0 float64, y0 float64, maxIterations int) int {
	x := 0.0
	y := 0.0
	iterations := 0
	for x*x+y*y <= 4 && iterations < maxIterations {
		nextX := x*x - y*y + x0
		y = 2*x*y + y0
		x = nextX
		iterations++
	}
	return iterations
}

func juliaIterations(x float64, y float64, cx float64, cy float64, maxIterations int) int {
	iterations := 0
	for x*x+y*y <= 4 && iterations < maxIterations {
		nextX := x*x - y*y + cx
		y = 2*x*y + cy
		x = nextX
		iterations++
	}
	return iterations
}

func colorForIteration(iterations int, maxIterations int, colorScheme string) (byte, byte, byte) {
	if iterations >= maxIterations {
		return 0, 0, 0
	}

	t := float64(iterations) / float64(maxIterations)
	switch colorScheme {
	case "grayscale":
		shade := byte(math.Round(255 * t))
		return shade, shade, shade
	case "psychedelic":
		r := byte(math.Round(128 * (1 + math.Sin(float64(iterations)*0.1))))
		g := byte(math.Round(128 * (1 + math.Sin(float64(iterations)*0.2))))
		b := byte(math.Round(128 * (1 + math.Sin(float64(iterations)*0.3))))
		return r, g, b
	default:
		r := byte(math.Round(9 * (1 - t) * t * t * t * 255))
		g := byte(math.Round(15 * (1 - t) * (1 - t) * t * t * 255))
		b := byte(math.Round(8.5 * (1 - t) * (1 - t) * (1 - t) * t * 255))
		return r, g, b
	}
}

func renderHandler(w http.ResponseWriter, r *http.Request) {
	params, err := parseRenderParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pixels, ok := renderFractal(r.Context(), params)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, err = w.Write(pixels)
	if err != nil {
		if isExpectedDisconnect(err) {
			return
		}
		log.Printf("render write failed: %v", err)
	}
}

func isExpectedDisconnect(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}

	return false
}

// startServer starts a local HTTP server on a random loopback port
// serving the embedded UI files. Returns the base URL.
func startServer() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}

	sub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		return "", fmt.Errorf("sub fs: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/render", renderHandler)
	mux.Handle("/", http.FileServer(http.FS(sub)))

	go func() {
		srv := &http.Server{Handler: mux}
		_ = srv.Serve(ln)
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return fmt.Sprintf("http://127.0.0.1:%d", addr.Port), nil
}

func main() {
	w, err := glaze.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Mandelbrot and Julia Sets")
	w.SetSize(1024, 768, glaze.HintNone)

	baseURL, err := startServer()
	if err != nil {
		log.Fatal(err)
	}

	w.Navigate(baseURL)
	w.Run()
}
