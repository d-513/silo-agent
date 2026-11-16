// Package toolarg reads the loosely typed arguments a model (or Python) hands
// to a tool: JSON decodes every number as float64, but a Go caller may pass an
// int.
package toolarg

import "encoding/json"

// Int is args[k] as an int; 0 when it is missing or not a number.
func Int(args map[string]any, k string) int {
	switch v := args[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

// Str is args[k] as a string; "" when it is missing or not one.
func Str(args map[string]any, k string) string {
	v, _ := args[k].(string)
	return v
}

// Strings is args[k] as a list of strings: a JSON array of them, or one
// string (a model often passes a single id bare). Empty ones are dropped.
func Strings(args map[string]any, k string) []string {
	var out []string
	add := func(v any) {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	switch v := args[k].(type) {
	case []any:
		for _, x := range v {
			add(x)
		}
	case []string:
		for _, x := range v {
			add(x)
		}
	default:
		add(v)
	}
	return out
}
