package core

import "strings"

// ParseBool parses common truthy/falsey strings. If unknown, returns def.
// Accepted true: 1, true, yes, y, on
// Accepted false: 0, false, no, n, off
func ParseBool(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	}
	return def
}
