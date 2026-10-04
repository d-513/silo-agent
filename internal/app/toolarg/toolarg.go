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
