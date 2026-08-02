// Filo (crgimenes/filo) language definition for GlazeEditor: a Lisp.
// Line comments with ';', double-quoted strings with backslash escapes,
// #t/#f, numbers, and symbols — special forms colored as keywords, the
// engine's builtins as builtins. An app extends the completion list with its
// own vocabulary (skirmish adds the ship contract's instruments and orders).
"use strict";

(() => {
	// The language's special forms (filo's evaluator) and core builtins
	// (filo's builtins.go). Operators are left to the tokenizer.
	const FORMS = ["cond", "def", "do", "fn", "if", "let", "set"];
	const CORE = [
		"and", "bool", "filter", "fmt", "fold", "head", "is-empty", "is-nil",
		"length", "list", "list-append", "list-concat", "map", "not", "nth",
		"number", "or", "pow", "range", "reverse", "string", "tail", "tuple",
		"type-of",
	];
	const forms = new Set(FORMS);
	const core = new Set(CORE);

	function tokenizeLine(line, state) {
		const tokens = [];
		let i = 0;
		const n = line.length;
		while (i < n) {
			const c = line[i];
			if (c === ";") {
				tokens.push({ s: i, e: n, t: "c" });
				break;
			}
			if (c === '"') {
				let j = i + 1;
				while (j < n && line[j] !== '"') j += line[j] === "\\" ? 2 : 1;
				tokens.push({ s: i, e: Math.min(j + 1, n), t: "s" });
				i = j + 1;
				continue;
			}
			if (c === "(" || c === ")" || c === "[" || c === "]") {
				tokens.push({ s: i, e: i + 1, t: "p" });
				i++;
				continue;
			}
			if (c === "#" && (line[i + 1] === "t" || line[i + 1] === "f")) {
				tokens.push({ s: i, e: i + 2, t: "n" });
				i += 2;
				continue;
			}
			// A number, including a negative one — but a lone "-" is a symbol
			// (the subtraction builtin), so the digit has to be there.
			if (/[0-9]/.test(c) || (c === "-" && /[0-9]/.test(line[i + 1] || ""))) {
				let j = i + 1;
				while (j < n && /[0-9.eE+-]/.test(line[j]) && /[0-9.eE]/.test(line[j - 1] + line[j])) j++;
				while (j > i + 1 && /[+-]/.test(line[j - 1])) j--; // do not eat a trailing operator
				tokens.push({ s: i, e: j, t: "n" });
				i = j;
				continue;
			}
			if (/[A-Za-z0-9_+*/<>=!?-]/.test(c)) {
				let j = i;
				while (j < n && /[A-Za-z0-9_+*/<>=!?-]/.test(line[j])) j++;
				const word = line.slice(i, j);
				if (forms.has(word)) tokens.push({ s: i, e: j, t: "k" });
				else if (core.has(word)) tokens.push({ s: i, e: j, t: "b" });
				i = j;
				continue;
			}
			i++;
		}
		return { tokens, state };
	}

	// indent: the previous line's indent, deepened by its net open parens —
	// the rule a hand-indented Lisp file already follows.
	function indent(prevLine, prevIndent, tabSize) {
		let net = 0;
		let inStr = false;
		for (let i = 0; i < prevLine.length; i++) {
			const c = prevLine[i];
			if (inStr) {
				if (c === "\\") i++;
				else if (c === '"') inStr = false;
				continue;
			}
			if (c === '"') inStr = true;
			else if (c === ";") break;
			else if (c === "(" || c === "[") net++;
			else if (c === ")" || c === "]") net--;
		}
		if (net > 0) return prevIndent + " ".repeat(tabSize * net);
		if (net < 0) return prevIndent.slice(0, Math.max(0, prevIndent.length + tabSize * net));
		return prevIndent;
	}

	window.GlazeEditor.languages.filo = {
		name: "filo",
		lineComment: ";",
		wordChars: "A-Za-z0-9_+*/<>=!?-",
		completions: FORMS.concat(CORE, ["#t", "#f"]),
		tokenizeLine,
		indent,
	};
})();
