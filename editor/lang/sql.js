// SQL language definition for GlazeEditor, aimed at PostgreSQL (keikiban) but
// generic enough for any dialect: case-insensitive keywords, '...' strings
// with '' escapes, "..." quoted identifiers, -- line comments and /* */ block
// comments that carry across lines (the tokenizer state).
"use strict";

(() => {
	const KEYWORDS = [
		"add", "all", "alter", "analyze", "and", "as", "asc", "begin", "between",
		"by", "cascade", "case", "check", "column", "commit", "constraint",
		"create", "cross", "default", "delete", "desc", "distinct", "drop",
		"else", "end", "except", "exists", "explain", "foreign", "from", "full",
		"grant", "group", "having", "if", "in", "index", "inner", "insert",
		"intersect", "into", "is", "join", "key", "left", "like", "limit",
		"not", "null", "offset", "on", "or", "order", "outer", "primary",
		"references", "returning", "revoke", "right", "rollback", "select",
		"set", "table", "then", "transaction", "union", "unique", "update",
		"using", "vacuum", "values", "view", "when", "where", "with",
	];
	const TYPES = [
		"bigint", "boolean", "bytea", "char", "date", "decimal", "double",
		"integer", "interval", "json", "jsonb", "numeric", "real", "serial",
		"smallint", "text", "time", "timestamp", "timestamptz", "uuid",
		"varchar",
	];
	const FUNCS = [
		"avg", "coalesce", "count", "current_date", "current_timestamp",
		"greatest", "least", "lower", "max", "min", "now", "nullif", "sum",
		"upper",
	];
	const kw = new Set(KEYWORDS);
	const ty = new Set(TYPES);
	const fn = new Set(FUNCS);

	function tokenizeLine(line, state) {
		const tokens = [];
		let inBlock = state ? state.inBlock : false;
		let i = 0;
		const n = line.length;
		while (i < n) {
			if (inBlock) {
				const end = line.indexOf("*/", i);
				if (end === -1) {
					tokens.push({ s: i, e: n, t: "c" });
					i = n;
					break;
				}
				tokens.push({ s: i, e: end + 2, t: "c" });
				i = end + 2;
				inBlock = false;
				continue;
			}
			const c = line[i];
			if (c === "-" && line[i + 1] === "-") {
				tokens.push({ s: i, e: n, t: "c" });
				break;
			}
			if (c === "/" && line[i + 1] === "*") {
				inBlock = true;
				continue; // the block branch above consumes it
			}
			if (c === "'") {
				let j = i + 1;
				while (j < n) {
					if (line[j] === "'" && line[j + 1] === "'") j += 2;
					else if (line[j] === "'") break;
					else j++;
				}
				tokens.push({ s: i, e: Math.min(j + 1, n), t: "s" });
				i = j + 1;
				continue;
			}
			if (c === '"') {
				let j = line.indexOf('"', i + 1);
				if (j === -1) j = n - 1;
				tokens.push({ s: i, e: j + 1, t: "b" });
				i = j + 1;
				continue;
			}
			if (/[0-9]/.test(c)) {
				let j = i + 1;
				while (j < n && /[0-9.]/.test(line[j])) j++;
				tokens.push({ s: i, e: j, t: "n" });
				i = j;
				continue;
			}
			if (/[A-Za-z_]/.test(c)) {
				let j = i;
				while (j < n && /[A-Za-z0-9_]/.test(line[j])) j++;
				const w = line.slice(i, j).toLowerCase();
				if (kw.has(w)) tokens.push({ s: i, e: j, t: "k" });
				else if (ty.has(w) || fn.has(w)) tokens.push({ s: i, e: j, t: "b" });
				i = j;
				continue;
			}
			if ("()[],;".includes(c)) {
				tokens.push({ s: i, e: i + 1, t: "p" });
				i++;
				continue;
			}
			i++;
		}
		return { tokens, state: { inBlock } };
	}

	window.GlazeEditor.languages.sql = {
		name: "sql",
		lineComment: "--",
		wordChars: "A-Za-z0-9_",
		completions: KEYWORDS.map(k => k.toUpperCase()).concat(TYPES, FUNCS),
		startState: () => ({ inBlock: false }),
		tokenizeLine,
	};
})();
