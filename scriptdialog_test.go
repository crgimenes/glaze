//go:build darwin || linux

package glaze

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var resScriptDialog atomic.Value // string

// scriptDialogScenario loads a page that calls alert, confirm and prompt twice
// over; OnScriptDialog answers OK the first time and cancels the second, and
// the page reports what it got back.
func scriptDialogScenario() string {
	results := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/r" {
			results <- r.URL.Query().Get("v")
			return
		}
		_, _ = fmt.Fprint(rw, `<!DOCTYPE html><body><script>
var got = [];
alert("hi");
got.push("confirm=" + confirm("sure?"));
got.push("prompt=" + prompt("name?", "def"));
got.push("confirm=" + confirm("again?"));
got.push("prompt=" + prompt("again?", ""));
new Image().src = "/r?v=" + encodeURIComponent(got.join(" "));
</script></body>`)
	}))
	defer srv.Close()

	var asked []string
	calls := 0
	w, err := NewWithOptions(Options{NoBridge: true, OnScriptDialog: func(d ScriptDialog) (bool, string) {
		calls++
		asked = append(asked, fmt.Sprintf("%d:%q:%q", d.Kind, d.Message, d.Text))
		return calls <= 3, "answer"
	}})
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	page := "TIMEOUT"
	go func() {
		select {
		case page = <-results:
		case <-time.After(10 * time.Second):
		}
		w.Dispatch(w.Terminate)
	}()
	w.Navigate(srv.URL)
	w.Run()
	return "asked " + strings.Join(asked, " ") + " | page " + page
}

func TestOnScriptDialog(t *testing.T) {
	got, _ := resScriptDialog.Load().(string)
	requireGUI(t, got)
	want := `asked 0:"hi":"" 1:"sure?":"" 2:"name?":"def" 1:"again?":"" 2:"again?":""` +
		` | page confirm=true prompt=answer confirm=false prompt=null`
	if got != want {
		t.Fatalf("script dialogs:\n got %s\nwant %s", got, want)
	}
}
