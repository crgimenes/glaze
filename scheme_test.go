package glaze

import "testing"

func TestCallSchemeHandlerPassesThrough(t *testing.T) {
	h := func(req *SchemeRequest) *SchemeResponse {
		return &SchemeResponse{Body: []byte(req.URL), MIMEType: "text/plain"}
	}
	resp := callSchemeHandler(h, &SchemeRequest{URL: "app://x/y"})
	if resp == nil || string(resp.Body) != "app://x/y" {
		t.Fatalf("response = %+v, want body %q", resp, "app://x/y")
	}
}

func TestCallSchemeHandlerContainsPanic(t *testing.T) {
	h := func(*SchemeRequest) *SchemeResponse {
		panic("handler bug")
	}
	// A panicking handler must answer nil (the platform's "not found"), not
	// unwind into the native UI callback and kill the process.
	resp := callSchemeHandler(h, &SchemeRequest{URL: "app://x"})
	if resp != nil {
		t.Fatalf("response = %+v, want nil", resp)
	}
}
