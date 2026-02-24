package webview

import (
	"html/template"
	"testing"
)

func TestCamelToSnake(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"GetUser", "get_user"},
		{"GetUserByID", "get_user_by_id"},
		{"ID", "id"},
		{"HTMLParser", "html_parser"},
		{"Simple", "simple"},
		{"A", "a"},
		{"getUser", "get_user"},
		{"QueryRow", "query_row"},
		{"QueryRowRW", "query_row_rw"},
		{"CheckpointWAL", "checkpoint_wal"},
		{"Close", "close"},
		{"Exec", "exec"},
		{"BeginTransaction", "begin_transaction"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := camelToSnake(tt.input)
			if got != tt.want {
				t.Errorf("camelToSnake(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRenderHTML(t *testing.T) {
	tpl := template.Must(template.New("test").Parse(
		`{{define "hello"}}Hello, {{.Name}}!{{end}}`,
	))

	got, err := RenderHTML(tpl, "hello", struct{ Name string }{"World"})
	if err != nil {
		t.Fatal(err)
	}
	want := "Hello, World!"
	if got != want {
		t.Errorf("RenderHTML = %q, want %q", got, want)
	}
}

func TestRenderHTMLMissingTemplate(t *testing.T) {
	tpl := template.Must(template.New("test").Parse(`{{define "a"}}ok{{end}}`))

	_, err := RenderHTML(tpl, "missing", nil)
	if err == nil {
		t.Fatal("expected error for missing template")
	}
}
