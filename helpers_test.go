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
		{"", ""},
		{"ABC", "abc"},
		{"ABCDef", "abc_def"},
		{"XMLHTTPRequest", "xmlhttp_request"},
		{"a", "a"},
		{"aB", "a_b"},
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

func TestRenderHTMLNested(t *testing.T) {
	tpl := template.Must(template.New("").Parse(
		`{{define "header"}}<h1>{{.Title}}</h1>{{end}}` +
			`{{define "page"}}{{template "header" .}}<p>body</p>{{end}}`,
	))

	got, err := RenderHTML(tpl, "page", struct{ Title string }{"Test"})
	if err != nil {
		t.Fatal(err)
	}
	want := "<h1>Test</h1><p>body</p>"
	if got != want {
		t.Errorf("RenderHTML nested = %q, want %q", got, want)
	}
}

func TestRenderHTMLMissingTemplate(t *testing.T) {
	tpl := template.Must(template.New("test").Parse(`{{define "a"}}ok{{end}}`))

	_, err := RenderHTML(tpl, "missing", nil)
	if err == nil {
		t.Fatal("expected error for missing template")
	}
}

func TestRenderHTMLNilData(t *testing.T) {
	tpl := template.Must(template.New("").Parse(
		`{{define "static"}}no data needed{{end}}`,
	))

	got, err := RenderHTML(tpl, "static", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "no data needed"
	if got != want {
		t.Errorf("RenderHTML nil data = %q, want %q", got, want)
	}
}
