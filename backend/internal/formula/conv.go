package formula

import (
	"strconv"
)

func strconvParse(s string) (float64, error) { return strconv.ParseFloat(s, 64) }
func strconvFormatInt(i int64) string        { return strconv.FormatInt(i, 10) }
func strconvFormatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
