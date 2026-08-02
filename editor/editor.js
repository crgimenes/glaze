// GlazeEditor: a small, dependency-free code editor for glaze WebViews.
//
// The technique is the classic transparent-textarea overlay: a <textarea>
// (which supplies the caret, selection, native editing and the undo stack)
// sits on top of a <pre> that draws the same text with syntax colors, kept in
// scroll lockstep. A gutter column draws line numbers. Everything is plain
// DOM — no contenteditable, no framework, no network.
//
// Languages are pluggable: a definition registers itself under
// GlazeEditor.languages and provides a per-line tokenizer (threading an opaque
// state, so block comments and multi-line strings can carry across lines),
// completions, word characters and an optional auto-indent rule.
//
//   const ed = new GlazeEditor(hostElement, {
//     language: "filo",            // a key of GlazeEditor.languages
//     value: "(fire 1 2)",
//     completions: ["self-x"],     // app-specific additions to the language's
//     onChange: v => {...},
//   });
//   ed.getValue(); ed.setValue(s); ed.focus();
//   ed.setErrors([{line: 3, msg: "unbalanced paren"}]); ed.clearErrors();
//
// Token classes drawn by editor.css: ge-t-k keyword, ge-t-b builtin,
// ge-t-s string, ge-t-n number/constant, ge-t-c comment, ge-t-p punctuation.
"use strict";

class GlazeEditor {
	constructor(host, opts = {}) {
		this.host = host;
		this.opts = opts;
		this.tabSize = opts.tabSize || 2;
		this.lang = GlazeEditor.languages[opts.language] || null;
		this.extra = (opts.completions || []).slice();
		this.errors = new Map(); // line (1-based) -> message
		this.matchSet = null; // absolute offsets of the matched bracket pair
		this.matchKey = "";

		host.classList.add("ge");
		// The caret and the selection are the textarea's own, NATIVE ones.
		// A first version drew both over the text with (line, column)
		// arithmetic, because a textarea paints them on the font's content box
		// aligned to the top of the line box while glyphs sit centred in it —
		// a half-leading offset. But everything drawn by arithmetic drifted
		// somewhere the arithmetic did not reach (long documents, mouse drags),
		// and crg was right to call it chasing our own tail. The browser
		// draws its caret on the real glyph geometry on every platform; the
		// job here is only to make the half-leading invisible, which the
		// line-height in editor.css does.
		host.innerHTML =
			'<div class="ge-gutter"><div class="ge-nums">1</div></div>' +
			'<div class="ge-body">' +
			'<pre class="ge-hl" aria-hidden="true"><code></code></pre>' +
			'<textarea class="ge-ta" spellcheck="false" autocapitalize="off" ' +
			'autocomplete="off" autocorrect="off" wrap="off"></textarea>' +
			'<div class="ge-comp" hidden></div>' +
			"</div>";
		this.nums = host.querySelector(".ge-nums");
		this.pre = host.querySelector(".ge-hl");
		this.code = this.pre.querySelector("code");
		this.ta = host.querySelector(".ge-ta");
		this.comp = host.querySelector(".ge-comp");
		this.compList = [];
		this.compIdx = 0;
		this.compOpen = false;

		this.ta.value = opts.value || "";
		// Same rule as setValue: WebKit parks the caret at the END of an
		// assigned value, and a document that opens on its last line answers
		// ArrowDown with nothing. Born at the top, like the file just opened.
		this.ta.setSelectionRange(0, 0);
		this.ta.addEventListener("input", () => {
			this.render();
			this.refreshComp(false);
			this.emitChange();
		});
		this.ta.addEventListener("scroll", () => this.syncScroll());
		this.ta.addEventListener("keydown", e => this.onKey(e));
		this.ta.addEventListener("click", () => this.closeComp());
		// Bracket matching follows selectionchange, which fires on EVERY caret
		// move from any source: key auto-repeat, mouse drag, cmd-arrows,
		// programmatic. (keyup does not: a held arrow repeats keydown and
		// fires keyup once, at release.) Coalesced to a frame.
		this.caretRaf = 0;
		this.onSelChange = () => {
			if (document.activeElement === this.ta) this.scheduleCaret();
		};
		document.addEventListener("selectionchange", this.onSelChange);
		// A click on a completion item must win over the blur it causes.
		this.ta.addEventListener("blur", () => {
			setTimeout(() => this.closeComp(), 150);
		});

		this.measure();
		this.render();
	}

	// ---- geometry -----------------------------------------------------------

	// measure caches the character cell of the (monospace) font: completions
	// are positioned arithmetically from (line, column) instead of via a mirror
	// element, which only works because code fonts are monospace — that is the
	// deal a code editor gets to make.
	//
	// The ROW height is MEASURED — two extra lines add exactly two rows — not
	// read from computed style. Both shortcuts lied in turn: an inline probe's
	// rect is the font's content box (15px against a real 18.84px row), and
	// the computed line-height is only a request the engine may overrule (asked
	// 16.25px, got 17px rows, the font's natural height). The renderer is the
	// only honest source for what a row is.
	measure() {
		const one = document.createElement("span");
		const three = document.createElement("span");
		one.textContent = "X";
		three.textContent = "X\nX\nX";
		one.style.visibility = "hidden";
		three.style.visibility = "hidden";
		this.code.append(one, three);
		this.charW = one.getBoundingClientRect().width || 8;
		this.lineH = (three.getBoundingClientRect().height - one.getBoundingClientRect().height) / 2;
		one.remove();
		three.remove();

		const cs = getComputedStyle(this.ta);
		if (!(this.lineH > 0)) {
			// A hidden host measures zero; the font-size ratio is the least
			// wrong stand-in until someone calls measure() on a visible one.
			this.lineH = (parseFloat(cs.fontSize) || 13) * 1.3;
		}
		this.padL = parseFloat(cs.paddingLeft) || 0;
		this.padT = parseFloat(cs.paddingTop) || 0;
	}

	syncScroll() {
		this.pre.scrollTop = this.ta.scrollTop;
		this.pre.scrollLeft = this.ta.scrollLeft;
		this.nums.style.transform = "translateY(" + -this.ta.scrollTop + "px)";
	}

	scrollCaretIntoView() {
		const { line } = this.caretLineCol();
		const top = line * this.lineH;
		const bottom = top + this.lineH;
		const view = this.ta.clientHeight;
		if (top < this.ta.scrollTop) this.ta.scrollTop = top;
		else if (bottom > this.ta.scrollTop + view) this.ta.scrollTop = bottom - view + this.padT;
		this.syncScroll();
	}

	caretLineCol() {
		const before = this.ta.value.slice(0, this.ta.selectionStart);
		const nl = before.lastIndexOf("\n");
		return { line: (before.match(/\n/g) || []).length, col: before.length - nl - 1 };
	}

	// ---- rendering ----------------------------------------------------------

	esc(s) {
		return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
	}

	span(txt, cls) {
		if (!txt) return "";
		return cls ? '<span class="' + cls + '">' + this.esc(txt) + "</span>" : this.esc(txt);
	}

	// emitSeg draws one token's text, additionally wrapping any character that
	// belongs to the matched bracket pair.
	emitSeg(txt, start, cls) {
		const marks = this.matchSet;
		if (!marks) return this.span(txt, cls);
		let html = "";
		let last = 0;
		for (const p of marks) {
			const i = p - start;
			if (i < 0 || i >= txt.length) continue;
			html += this.span(txt.slice(last, i), cls);
			html += '<span class="ge-match">' + this.esc(txt[i]) + "</span>";
			last = i + 1;
		}
		return html + this.span(txt.slice(last), cls);
	}

	renderLine(line, tokens, base) {
		let html = "";
		let at = 0;
		for (const t of tokens) {
			if (t.s > at) html += this.emitSeg(line.slice(at, t.s), base + at, "");
			html += this.emitSeg(line.slice(t.s, t.e), base + t.s, t.t ? "ge-t-" + t.t : "");
			at = t.e;
		}
		return html + this.emitSeg(line.slice(at), base + at, "");
	}

	render() {
		const text = this.ta.value;
		const lines = text.split("\n");

		let nums = "";
		for (let i = 1; i <= lines.length; i++) {
			nums += this.errors.has(i) ? '<span class="ge-num-err">' + i + "</span>\n" : i + "\n";
		}
		this.nums.innerHTML = nums;

		let html = "";
		let state = this.lang && this.lang.startState ? this.lang.startState() : null;
		let off = 0;
		for (let li = 0; li < lines.length; li++) {
			const line = lines[li];
			let body;
			if (this.lang) {
				const res = this.lang.tokenizeLine(line, state);
				state = res.state;
				body = this.renderLine(line, res.tokens, off);
			} else {
				body = this.emitSeg(line, off, "");
			}
			const msg = this.errors.get(li + 1);
			if (msg !== undefined) {
				body = '<span class="ge-line-err" title="' + this.esc(msg) + '">' + (body || " ") + "</span>";
			}
			html += body + "\n";
			off += line.length + 1;
		}
		this.code.innerHTML = html;
		this.syncScroll();
	}

	// ---- bracket matching ---------------------------------------------------

	// scheduleCaret coalesces however many selectionchange events a frame
	// brings (a fast drag delivers several) into one match pass.
	scheduleCaret() {
		if (this.caretRaf) return;
		this.caretRaf = requestAnimationFrame(() => {
			this.caretRaf = 0;
			this.caretMoved();
		});
	}

	caretMoved() {
		const pair = this.findPair(this.ta.value, this.ta.selectionStart);
		const key = pair ? pair.join(",") : "";
		if (key === this.matchKey) return;
		this.matchKey = key;
		this.matchSet = pair;
		this.render();
	}

	// findPair matches ()[]{} by raw scan. It does not consult the tokenizer,
	// so a bracket inside a string can fool it — a cosmetic nit, accepted for
	// the simplicity; the scan is capped so a pathological file cannot stall
	// the caret.
	findPair(text, pos) {
		const OPEN = "([{";
		const CLOSE = ")]}";
		const at = i => text[i] || "";
		let i = -1;
		let ch = "";
		if (OPEN.includes(at(pos - 1)) || CLOSE.includes(at(pos - 1))) {
			i = pos - 1;
			ch = at(pos - 1);
		} else if (OPEN.includes(at(pos)) || CLOSE.includes(at(pos))) {
			i = pos;
			ch = at(pos);
		} else {
			return null;
		}
		const open = OPEN.includes(ch);
		const mate = open ? CLOSE[OPEN.indexOf(ch)] : OPEN[CLOSE.indexOf(ch)];
		const dir = open ? 1 : -1;
		let depth = 0;
		const limit = 20000;
		for (let j = i, n = 0; j >= 0 && j < text.length && n < limit; j += dir, n++) {
			const c = text[j];
			if (c === ch) depth++;
			else if (c === mate) {
				depth--;
				if (depth === 0) return j < i ? [j, i] : [i, j];
			}
		}
		return null;
	}

	// ---- keys ---------------------------------------------------------------

	onKey(e) {
		if (this.compOpen) {
			if (e.key === "ArrowDown" || e.key === "ArrowUp") {
				e.preventDefault();
				const d = e.key === "ArrowDown" ? 1 : -1;
				this.compIdx = (this.compIdx + d + this.compList.length) % this.compList.length;
				this.paintComp();
				return;
			}
			if (e.key === "Enter" || e.key === "Tab") {
				e.preventDefault();
				this.accept(this.compList[this.compIdx]);
				return;
			}
			if (e.key === "Escape") {
				e.preventDefault();
				this.closeComp();
				return;
			}
		}
		if (e.key === " " && e.ctrlKey) {
			e.preventDefault();
			this.refreshComp(true);
			return;
		}
		if (e.key === "Tab") {
			e.preventDefault();
			this.indentKey(e.shiftKey);
			return;
		}
		if (e.key === "Enter") {
			e.preventDefault();
			this.enterKey();
			return;
		}
		if (e.key === "/" && (e.metaKey || e.ctrlKey)) {
			e.preventDefault();
			this.toggleComment();
		}
	}

	enterKey() {
		const text = this.ta.value;
		const pos = this.ta.selectionStart;
		const ls = text.lastIndexOf("\n", pos - 1) + 1;
		const prev = text.slice(ls, pos);
		let indent = (prev.match(/^[ \t]*/) || [""])[0];
		if (this.lang && this.lang.indent) indent = this.lang.indent(prev, indent, this.tabSize);
		this.edit("\n" + indent, pos, this.ta.selectionEnd);
	}

	// indentKey shifts the selected block (or inserts an indent at the caret).
	indentKey(dedent) {
		const unit = " ".repeat(this.tabSize);
		const text = this.ta.value;
		let { selectionStart: a, selectionEnd: b } = this.ta;
		if (a === b && !dedent) {
			this.edit(unit, a, b);
			return;
		}
		const start = text.lastIndexOf("\n", a - 1) + 1;
		let end = text.indexOf("\n", b);
		if (end === -1) end = text.length;
		const block = text.slice(start, end);
		const out = block
			.split("\n")
			.map(l => (dedent ? l.replace(new RegExp("^ {1," + this.tabSize + "}"), "") : l === "" ? l : unit + l))
			.join("\n");
		this.ta.setSelectionRange(start, end);
		this.edit(out, start, end);
		this.ta.setSelectionRange(start, start + out.length);
	}

	toggleComment() {
		const cc = this.lang && this.lang.lineComment;
		if (!cc) return;
		const text = this.ta.value;
		let { selectionStart: a, selectionEnd: b } = this.ta;
		const start = text.lastIndexOf("\n", a - 1) + 1;
		let end = text.indexOf("\n", b);
		if (end === -1) end = text.length;
		const linesIn = text.slice(start, end).split("\n");
		const active = linesIn.filter(l => l.trim() !== "");
		const allOn = active.length > 0 && active.every(l => l.trimStart().startsWith(cc));
		const out = linesIn
			.map(l => {
				if (l.trim() === "") return l;
				if (allOn) return l.replace(new RegExp("^(\\s*)" + cc.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + " ?"), "$1");
				return cc + " " + l;
			})
			.join("\n");
		this.ta.setSelectionRange(start, end);
		this.edit(out, start, end);
		this.ta.setSelectionRange(start, start + out.length);
	}

	// edit routes every programmatic change through setRangeText so the
	// textarea's native undo stack keeps working — the whole reason the input
	// layer is a textarea and not something cleverer.
	edit(insert, from, to) {
		this.ta.setRangeText(insert, from, to, "end");
		this.render();
		this.scrollCaretIntoView();
		this.caretMoved();
		this.emitChange();
	}

	// ---- completion ---------------------------------------------------------

	wordRe() {
		const cls = (this.lang && this.lang.wordChars) || "A-Za-z0-9_";
		return new RegExp("[" + cls + "]+$");
	}

	currentWord() {
		const pos = this.ta.selectionStart;
		const before = this.ta.value.slice(0, pos);
		const m = before.match(this.wordRe());
		if (!m) return { word: "", start: pos };
		return { word: m[0], start: pos - m[0].length };
	}

	candidates() {
		const set = new Set();
		if (this.lang && this.lang.completions) for (const c of this.lang.completions) set.add(c);
		for (const c of this.extra) set.add(c);
		// The document itself is a source: a symbol the program defined is a
		// symbol worth completing.
		const cls = (this.lang && this.lang.wordChars) || "A-Za-z0-9_";
		const words = this.ta.value.match(new RegExp("[" + cls + "]{3,}", "g")) || [];
		for (const w of words) set.add(w);
		return set;
	}

	refreshComp(forced) {
		const { word } = this.currentWord();
		if (!forced && word.length < 1) {
			this.closeComp();
			return;
		}
		const low = word.toLowerCase();
		const list = [];
		for (const c of this.candidates()) {
			if (c.toLowerCase().startsWith(low) && c !== word) list.push(c);
			if (list.length >= 40) break;
		}
		list.sort();
		if (list.length === 0) {
			this.closeComp();
			return;
		}
		this.compList = list;
		this.compIdx = 0;
		this.compOpen = true;
		this.paintComp();
	}

	paintComp() {
		const { start } = this.currentWord();
		const before = this.ta.value.slice(0, start);
		const nl = before.lastIndexOf("\n");
		const line = (before.match(/\n/g) || []).length;
		const col = before.length - nl - 1;
		this.comp.style.left = this.padL + col * this.charW - this.ta.scrollLeft + "px";
		this.comp.style.top = this.padT + (line + 1) * this.lineH - this.ta.scrollTop + "px";
		this.comp.innerHTML = this.compList
			.map((c, i) => '<div class="ge-comp-it' + (i === this.compIdx ? " ge-comp-sel" : "") + '">' + this.esc(c) + "</div>")
			.join("");
		this.comp.hidden = false;
		const sel = this.comp.children[this.compIdx];
		if (sel) sel.scrollIntoView({ block: "nearest" });
		for (let i = 0; i < this.comp.children.length; i++) {
			const el = this.comp.children[i];
			el.onmousedown = e => {
				e.preventDefault(); // keep the textarea focused
				this.accept(this.compList[i]);
			};
		}
	}

	accept(item) {
		if (!item) return;
		const { start } = this.currentWord();
		this.closeComp();
		this.edit(item, start, this.ta.selectionStart);
	}

	closeComp() {
		this.compOpen = false;
		this.comp.hidden = true;
	}

	// emitChange reports the buffer to the app. Every input path ends here —
	// typing (the input listener) and programmatic edits (edit) alike.
	emitChange() {
		if (this.opts.onChange) this.opts.onChange(this.getValue());
	}

	// ---- public API ---------------------------------------------------------

	getValue() {
		return this.ta.value;
	}

	setValue(v) {
		this.ta.value = v;
		// WebKit leaves the caret at the END of an assigned value, and a caret
		// born on the last line answers ArrowDown with nothing — crg opened
		// the editor, pressed it, and reasonably concluded keys were broken.
		// A freshly opened document starts at its beginning.
		this.ta.setSelectionRange(0, 0);
		this.ta.scrollTop = 0;
		this.ta.scrollLeft = 0;
		this.errors.clear();
		this.closeComp();
		this.render();
	}

	setCompletions(list) {
		this.extra = (list || []).slice();
	}

	setErrors(list) {
		this.errors.clear();
		for (const e of list || []) this.errors.set(e.line, e.msg || "");
		this.render();
	}

	clearErrors() {
		this.errors.clear();
		this.render();
	}

	focus() {
		this.ta.focus();
	}

	destroy() {
		document.removeEventListener("selectionchange", this.onSelChange);
		cancelAnimationFrame(this.caretRaf);
		this.host.innerHTML = "";
		this.host.classList.remove("ge");
	}
}

GlazeEditor.languages = {};
window.GlazeEditor = GlazeEditor;
