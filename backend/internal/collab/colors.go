package collab

// colorWheel hands out distinguishable cursor colors, stable per client id:
// a reconnecting client keeps the same hue as long as the room is alive.
type colorWheel struct {
	palette []string
	used    map[string]string
}

func newColorWheel() *colorWheel {
	return &colorWheel{
		palette: []string{
			"#e6194b", "#3cb44b", "#4363d8", "#f58231", "#911eb4",
			"#42d4f4", "#f032e6", "#9a6324", "#800000", "#808000",
			"#000075", "#469990",
		},
		used: map[string]string{},
	}
}

func (w *colorWheel) take(id string) string {
	if c := w.used[id]; c != "" {
		return c
	}
	taken := map[string]bool{}
	for _, c := range w.used {
		taken[c] = true
	}
	for _, c := range w.palette {
		if !taken[c] {
			w.used[id] = c
			return c
		}
	}
	// Fall back to a deterministic color derived from the id hash.
	var h uint32 = 2166136261
	for i := 0; i < len(id); i++ {
		h ^= uint32(id[i])
		h *= 16777619
	}
	r := 80 + h%140
	g := 80 + (h>>8)%140
	b := 80 + (h>>16)%140
	c := "#" + hexByte(byte(r)) + hexByte(byte(g)) + hexByte(byte(b))
	w.used[id] = c
	return c
}

func hexByte(b byte) string {
	const digits = "0123456789abcdef"
	return string(digits[b>>4]) + string(digits[b&0xf])
}
