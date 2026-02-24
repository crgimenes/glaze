package webview

import (
	"bytes"
	"fmt"
	"html/template"
	"reflect"
	"strings"
	"unicode"
)

// BindMethods binds all exported methods of obj as JavaScript functions.
// Each method is exposed as window.{prefix}_{MethodName}(args...).
// Methods must follow the same signature rules as Bind:
//   - Return either nothing, a value, an error, or (value, error).
//
// Returns the list of bound function names and the first error encountered.
func BindMethods(w WebView, prefix string, obj any) ([]string, error) {
	v := reflect.ValueOf(obj)
	t := v.Type()

	var bound []string
	for i := range t.NumMethod() {
		method := t.Method(i)

		// Skip unexported methods.
		if !method.IsExported() {
			continue
		}

		// Build the JS function name: {prefix}_{snake_case_method}.
		name := prefix + "_" + camelToSnake(method.Name)

		fn := v.Method(i).Interface()
		if err := w.Bind(name, fn); err != nil {
			return bound, fmt.Errorf("binding %s: %w", name, err)
		}
		bound = append(bound, name)
	}
	return bound, nil
}

// camelToSnake converts a CamelCase name to snake_case for JavaScript.
// Example: "GetUserByID" -> "get_user_by_id"
func camelToSnake(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)

	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			// Insert underscore before uppercase runs, but not at the start.
			if i > 0 {
				prev := runes[i-1]
				// Don't insert underscore between consecutive uppercase
				// unless the next char is lowercase (e.g., "ID" stays together
				// but "IDa" → "i_da" boundary).
				if unicode.IsLower(prev) {
					b.WriteRune('_')
				} else if i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
					b.WriteRune('_')
				}
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// RenderHTML executes a named template to a string, suitable for SetHtml().
// This allows reusing Go html/template definitions without an HTTP server.
func RenderHTML(tpl *template.Template, name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("render %s: %w", name, err)
	}
	return buf.String(), nil
}
