package main

import (
	"embed"
	"fmt"
	"log"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/crgimenes/glaze"
	_ "github.com/crgimenes/glaze/embedded"
)

//go:embed ui/index.html ui/app.css ui/app.js
var uiFS embed.FS

const (
	gridWidth  = 60
	gridHeight = 40
)

// Game holds the state for Conway's Game of Life.
type Game struct {
	mu      sync.Mutex
	cells   [gridHeight][gridWidth]bool
	running bool
}

// Init fills the grid randomly with ~30% alive cells.
func (g *Game) Init() [][]bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for y := range gridHeight {
		for x := range gridWidth {
			g.cells[y][x] = rand.Float64() < 0.3
		}
	}
	return g.snapshot()
}

// Clear sets all cells to dead.
func (g *Game) Clear() [][]bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cells = [gridHeight][gridWidth]bool{}
	g.running = false
	return g.snapshot()
}

// Toggle flips the state of a single cell.
func (g *Game) Toggle(x, y int) [][]bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if y >= 0 && y < gridHeight && x >= 0 && x < gridWidth {
		g.cells[y][x] = !g.cells[y][x]
	}
	return g.snapshot()
}

// Step advances the simulation by one generation.
func (g *Game) Step() [][]bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.advance()
	return g.snapshot()
}

// SetRunning enables or disables continuous simulation.
func (g *Game) SetRunning(on bool) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.running = on
	return g.running
}

// IsRunning returns current running state.
func (g *Game) IsRunning() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

// Tick advances one generation only if running. Returns nil if paused.
func (g *Game) Tick() [][]bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.running {
		return nil
	}
	g.advance()
	return g.snapshot()
}

// GetGrid returns the current grid state.
func (g *Game) GetGrid() [][]bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.snapshot()
}

// GetSize returns width and height of the grid.
func (g *Game) GetSize() [2]int {
	return [2]int{gridWidth, gridHeight}
}

// advance computes the next generation (must be called with lock held).
func (g *Game) advance() {
	var next [gridHeight][gridWidth]bool
	for y := range gridHeight {
		for x := range gridWidth {
			n := g.neighbors(x, y)
			switch {
			case g.cells[y][x] && (n == 2 || n == 3):
				next[y][x] = true
			case !g.cells[y][x] && n == 3:
				next[y][x] = true
			}
		}
	}
	g.cells = next
}

// neighbors counts live neighbors with wrapping (toroidal grid).
func (g *Game) neighbors(x, y int) int {
	count := 0
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			ny := (y + dy + gridHeight) % gridHeight
			nx := (x + dx + gridWidth) % gridWidth
			if g.cells[ny][nx] {
				count++
			}
		}
	}
	return count
}

// snapshot returns a copy of the grid as a 2D slice for JSON serialization.
func (g *Game) snapshot() [][]bool {
	out := make([][]bool, gridHeight)
	for y := range gridHeight {
		row := make([]bool, gridWidth)
		copy(row, g.cells[y][:])
		out[y] = row
	}
	return out
}

func stageUIFiles() (string, string, error) {
	tmpDir, err := os.MkdirTemp("", "glaze-gameoflife-*")
	if err != nil {
		return "", "", fmt.Errorf("create temp dir: %w", err)
	}

	files := []string{"ui/index.html", "ui/app.css", "ui/app.js"}
	for _, name := range files {
		data, readErr := uiFS.ReadFile(name)
		if readErr != nil {
			_ = os.RemoveAll(tmpDir)
			return "", "", fmt.Errorf("read embedded file %s: %w", name, readErr)
		}

		base := filepath.Base(name)
		target := filepath.Join(tmpDir, base)
		if writeErr := os.WriteFile(target, data, 0o600); writeErr != nil {
			_ = os.RemoveAll(tmpDir)
			return "", "", fmt.Errorf("write ui file %s: %w", target, writeErr)
		}
	}

	indexPath := filepath.Join(tmpDir, "index.html")
	indexURL := (&url.URL{Scheme: "file", Path: indexPath}).String()
	return tmpDir, indexURL, nil
}

func main() {
	game := &Game{}

	w, err := glaze.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Conway's Game of Life")
	w.SetSize(800, 640, glaze.HintNone)

	if _, err := glaze.BindMethods(w, "game", game); err != nil {
		log.Fatal(err)
	}

	uiDir, indexURL, err := stageUIFiles()
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(uiDir)

	w.Navigate(indexURL)
	w.Run()
}
