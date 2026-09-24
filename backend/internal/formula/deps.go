package formula

// ExtractDeps parses a formula and returns the distinct list of cells and
// ranges it reads, resolved against ownerSheet for unqualified references.
func ExtractDeps(ast Node, ownerSheet string) []Ref {
	var refs []Ref
	WalkRefs(ast, func(rn *RefNode) {
		if rn.Ref.IsError {
			return
		}
		r := rn.Ref
		if r.Sheet == "" {
			r.Sheet = ownerSheet
		}
		refs = append(refs, r)
	})
	return dedupeRefs(refs)
}

func dedupeRefs(refs []Ref) []Ref {
	seen := make(map[Ref]bool, len(refs))
	out := refs[:0]
	for _, r := range refs {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}
