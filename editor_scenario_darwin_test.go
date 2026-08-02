package glaze

import (
	"testing"
	"time"

	"github.com/crgimenes/glaze/editor"
)

// editorScenario runs the editor package inside a real WKWebView — the only
// truthful place to test it, since its whole body is JS the Go tests cannot
// reach. It asserts the three faces of the component: tokenization produces
// the documented classes for both shipped languages, the completion engine
// surfaces a language builtin AND an app-supplied word for a prefix, and an
// error mark lands on the gutter and the line.
func editorScenario() string {
	js, err := editor.JS("filo", "sql")
	if err != nil {
		return "bundle: " + err.Error()
	}
	w, err := New(false)
	if err != nil {
		return "new error: " + err.Error()
	}
	defer w.Destroy()
	w.SetSize(700, 500, HintNone)

	done := make(chan string, 1)
	_ = w.Bind("done", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Terminate()
	})
	time.AfterFunc(15*time.Second, w.Terminate)

	page := `<!DOCTYPE html><html><head><style>` + string(editor.CSS()) + `</style></head><body>
<div id="fe" style="height:12em"></div><div id="se" style="height:8em"></div>
<script>` + string(js) + `</script>
<script>
window.addEventListener('load', function () {
  try {
    var fe = new GlazeEditor(document.getElementById('fe'),
      {language: 'filo', completions: ['seek', 'self-x', 'self-hull']});
    fe.setValue('; hunt\n(def n 42)\n(fire "at" n)');
    var html = document.querySelectorAll('.ge-hl code')[0].innerHTML;
    var miss = ['ge-t-c', 'ge-t-k', 'ge-t-n', 'ge-t-s', 'ge-t-p'].filter(c => html.indexOf(c) < 0);
    if (miss.length) { window.done('filo tokens missing: ' + miss.join(',')); return; }

    fe.ta.value = '(se';
    fe.ta.setSelectionRange(3, 3);
    fe.refreshComp(true);
    var got = fe.compList.join(',');
    if (got.indexOf('seek') < 0 || got.indexOf('set') < 0) {
      window.done('completions for "se" lack seek/set: ' + got); return;
    }

    fe.setValue('(def a 1)\n(broken');
    fe.setErrors([{line: 2, msg: 'unbalanced'}]);
    var nums = document.querySelectorAll('.ge-nums')[0].innerHTML;
    var code = document.querySelectorAll('.ge-hl code')[0].innerHTML;
    if (nums.indexOf('ge-num-err') < 0 || code.indexOf('ge-line-err') < 0) {
      window.done('error mark did not land'); return;
    }

    var se = new GlazeEditor(document.getElementById('se'), {language: 'sql'});
    se.setValue("SELECT count(*) FROM t -- all\n/* block\nstill */ WHERE x = 'v' AND y > 42");
    var sh = document.querySelectorAll('.ge-hl code')[1].innerHTML;
    var smiss = ['ge-t-k', 'ge-t-c', 'ge-t-s', 'ge-t-n'].filter(c => sh.indexOf(c) < 0);
    if (smiss.length) { window.done('sql tokens missing: ' + smiss.join(',')); return; }
    // The block comment must carry across the line break — that is what the
    // tokenizer state exists for.
    if (sh.split('ge-t-c').length < 4) { window.done('sql block comment did not span lines'); return; }

    window.done('editor-ok');
  } catch (e) { window.done('ERR:' + e); }
});
</script></body></html>`
	w.SetHtml(page)
	w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

func TestEditorInsideARealWebView(t *testing.T) {
	const want = "editor-ok"
	got, _ := resEditor.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("editor scenario: got %q, want %q", got, want)
	}
}
