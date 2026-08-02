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

    // Typing must reach the app: setRangeText + an input event is exactly what
    // a keystroke does, and onChange is the editor's entire output channel. The
    // first version shipped calling an emitChange that did not exist — every
    // keystroke threw inside the event handler, silently — and this scenario
    // missed it because it never typed.
    var changed = '';
    fe.opts.onChange = v => { changed = v; };
    fe.ta.setRangeText('; typed', 0, 0, 'end');
    fe.ta.dispatchEvent(new Event('input'));
    if (!changed.startsWith('; typed')) { window.done('onChange never heard the keystroke'); return; }

    // Geometry: the row height must be the REAL row, not the font's content
    // box. Reading the inline probe's rect gave 15px against an 18.84px row,
    // which drew the completion list a quarter-line too high for every line
    // down the file and scrolled the caret to an offset between rows.
    // Two extra lines add exactly two rows, whatever the renderer does at the
    // ends — a difference is immune to the trailing newline and to how many
    // text nodes the highlighter happens to emit.
    var codeEl = document.querySelectorAll('.ge-hl code')[0];
    fe.setValue('a');
    var h1 = codeEl.getBoundingClientRect().height;
    fe.setValue('a\nb\nc');
    var h3 = codeEl.getBoundingClientRect().height;
    var realRow = (h3 - h1) / 2;
    if (Math.abs(fe.lineH - realRow) > 0.5) {
      window.done('row height is ' + fe.lineH + ' but rows are ' + realRow + ' apart'); return;
    }

    // One character is enough to offer completions: a two-character rule is
    // invisible to the user and reads as "it does not always appear".
    fe.setValue('');
    fe.ta.value = '(f';
    fe.ta.setSelectionRange(2, 2);
    fe.refreshComp(false);
    if (!fe.compOpen) { window.done('one character offered nothing'); return; }
    // And it is placed on the line BELOW the caret's own row.
    var top = parseFloat(fe.comp.style.top);
    if (Math.abs(top - (fe.padT + realRow)) > 1.5) {
      window.done('completion list at ' + top + ', want ' + (fe.padT + realRow)); return;
    }
    fe.closeComp();

    // The caret and the selection are DRAWN, and they must land on the text's
    // own row — the textarea's native ones sit a half-leading above it, which
    // is what crg saw as a caret off its line and a selection that did not
    // match the line height.
    fe.setValue('aaaa\nbbbb\ncccc\ndddd');
    fe.ta.focus();
    var caretAt = fe.ta.value.indexOf('cccc') + 2;   // line 3, column 2
    fe.ta.setSelectionRange(caretAt, caretAt);
    fe.paintCaret();
    if (fe.caret.hidden) { window.done('the caret is not drawn while focused'); return; }
    var wantTop = fe.padT + 2 * realRow, wantLeft = fe.padL + 2 * fe.charW;
    var gotTop = parseFloat(fe.caret.style.top), gotLeft = parseFloat(fe.caret.style.left);
    if (Math.abs(gotTop - wantTop) > 0.5 || Math.abs(gotLeft - wantLeft) > 0.5) {
      window.done('caret at ' + gotLeft + ',' + gotTop + ' want ' + wantLeft + ',' + wantTop); return;
    }
    if (Math.abs(parseFloat(fe.caret.style.height) - realRow) > 0.5) {
      window.done('the caret is ' + fe.caret.style.height + ' tall on a ' + realRow + 'px row'); return;
    }

    // A selection spanning three lines draws one band per line, each a full
    // row tall and starting on the row's own top.
    fe.ta.setSelectionRange(fe.ta.value.indexOf('bbbb'), fe.ta.value.indexOf('dddd') + 2);
    fe.paintCaret();
    var bands = fe.sel.children;
    if (bands.length !== 3) { window.done('three selected lines drew ' + bands.length + ' bands'); return; }
    if (!fe.caret.hidden) { window.done('the caret is drawn over a range selection'); return; }
    if (Math.abs(parseFloat(bands[0].style.top) - (fe.padT + realRow)) > 0.5) {
      window.done('the first band is at ' + bands[0].style.top + ', off its row'); return;
    }
    if (Math.abs(parseFloat(bands[0].style.height) - realRow) > 0.5) {
      window.done('a band is ' + bands[0].style.height + ' on a ' + realRow + 'px row'); return;
    }
    fe.ta.setSelectionRange(0, 0);
    fe.paintCaret();
    if (fe.sel.children.length !== 0) { window.done('bands survived the selection being cleared'); return; }

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
