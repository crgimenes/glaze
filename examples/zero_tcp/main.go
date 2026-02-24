package main

import (
	"embed"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/crgimenes/glaze"
	_ "github.com/crgimenes/glaze/embedded"
)

//go:embed ui/index.html ui/app.css ui/app.js
var uiFS embed.FS

type Task struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
}

type TaskService struct {
	mu     sync.Mutex
	nextID int
	items  []Task
}

func (s *TaskService) Add(text string) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	text = strings.TrimSpace(text)
	if text == "" {
		return s.snapshot(), nil
	}

	s.nextID++
	s.items = append(s.items, Task{
		ID:   s.nextID,
		Text: text,
	})
	return s.snapshot(), nil
}

func (s *TaskService) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot()
}

func (s *TaskService) Toggle(id int) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].Done = !s.items[i].Done
			break
		}
	}
	return s.snapshot()
}

func (s *TaskService) Delete(id int) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			break
		}
	}
	return s.snapshot()
}

func (s *TaskService) snapshot() []Task {
	out := make([]Task, len(s.items))
	copy(out, s.items)
	return out
}

func stageUIFiles() (string, string, error) {
	tmpDir, err := os.MkdirTemp("", "glaze-zero-tcp-*")
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
	service := &TaskService{}

	w, err := glaze.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Glaze - Zero TCP")
	w.SetSize(760, 560, glaze.HintNone)

	if _, err := glaze.BindMethods(w, "tasks", service); err != nil {
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
