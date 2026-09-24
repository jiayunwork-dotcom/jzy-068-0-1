package engine

import "strconv"

// parseNumberLiteral mirrors spreadsheet entry semantics: a raw cell is a
// number only when its entire text parses as one.
func parseNumberLiteral(raw string) (float64, bool) {
	if raw == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
