package webview

import (
	"errors"
	"html/template"
	"testing"
	"unsafe"
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

type bindMethodsWebViewStub struct {
	bound     map[string]any
	failOn    string
	bindCalls int
}

func (s *bindMethodsWebViewStub) Run() {}

func (s *bindMethodsWebViewStub) Terminate() {}

func (s *bindMethodsWebViewStub) Dispatch(_ func()) {}

func (s *bindMethodsWebViewStub) Destroy() {}

func (s *bindMethodsWebViewStub) Window() unsafe.Pointer { return nil }

func (s *bindMethodsWebViewStub) SetTitle(_ string) {}

func (s *bindMethodsWebViewStub) SetSize(_, _ int, _ Hint) {}

func (s *bindMethodsWebViewStub) Navigate(_ string) {}

func (s *bindMethodsWebViewStub) SetHtml(_ string) {}

func (s *bindMethodsWebViewStub) Init(_ string) {}

func (s *bindMethodsWebViewStub) Eval(_ string) {}

func (s *bindMethodsWebViewStub) Bind(name string, f any) error {
	s.bindCalls++
	if name == s.failOn {
		return errors.New("bind failure")
	}
	if s.bound == nil {
		s.bound = make(map[string]any)
	}
	s.bound[name] = f
	return nil
}

func (s *bindMethodsWebViewStub) Unbind(_ string) error { return nil }

type bindMethodsService struct{}

func (bindMethodsService) GetUserByID(_ int) int { return 1 }

func (bindMethodsService) Ping() {}

func (bindMethodsService) hidden() {}

func TestBindMethods(t *testing.T) {
	w := &bindMethodsWebViewStub{}
	names, err := BindMethods(w, "api", bindMethodsService{})
	if err != nil {
		t.Fatalf("BindMethods() unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("BindMethods() names len = %d, want 2", len(names))
	}
	if names[0] != "api_get_user_by_id" {
		t.Fatalf("BindMethods() names[0] = %q, want %q", names[0], "api_get_user_by_id")
	}
	if names[1] != "api_ping" {
		t.Fatalf("BindMethods() names[1] = %q, want %q", names[1], "api_ping")
	}
	if _, ok := w.bound["api_hidden"]; ok {
		t.Fatal("BindMethods() bound unexported method")
	}
	if _, ok := w.bound["api_get_user_by_id"]; !ok {
		t.Fatal("BindMethods() missing binding for api_get_user_by_id")
	}
	if _, ok := w.bound["api_ping"]; !ok {
		t.Fatal("BindMethods() missing binding for api_ping")
	}
}

func TestBindMethodsNilWebView(t *testing.T) {
	_, err := BindMethods(nil, "api", bindMethodsService{})
	if err == nil {
		t.Fatal("BindMethods() expected error for nil WebView")
	}
}

func TestBindMethodsNilObject(t *testing.T) {
	w := &bindMethodsWebViewStub{}
	_, err := BindMethods(w, "api", nil)
	if err == nil {
		t.Fatal("BindMethods() expected error for nil object")
	}
}

func TestBindMethodsNilPointerObject(t *testing.T) {
	w := &bindMethodsWebViewStub{}
	var service *bindMethodsService
	_, err := BindMethods(w, "api", service)
	if err == nil {
		t.Fatal("BindMethods() expected error for nil pointer object")
	}
}

func TestBindMethodsBindError(t *testing.T) {
	w := &bindMethodsWebViewStub{failOn: "api_ping"}
	names, err := BindMethods(w, "api", bindMethodsService{})
	if err == nil {
		t.Fatal("BindMethods() expected bind error")
	}
	if len(names) != 1 {
		t.Fatalf("BindMethods() names len = %d, want 1", len(names))
	}
	if names[0] != "api_get_user_by_id" {
		t.Fatalf("BindMethods() names[0] = %q, want %q", names[0], "api_get_user_by_id")
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
