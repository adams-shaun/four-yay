package cards

import "sort"

// Coverage answers "how much of the corpus can this engine actually play?".
// It is the project's progress metric and the input to deck-build validation.
type Coverage struct {
	Cards     int
	Supported int
	// Missing counts, per unimplemented primitive, how many cards it blocks.
	Missing map[string]int
}

type MissingPrimitive struct {
	Name  string
	Cards int
}

// Unsupported lists the primitives a card needs that the engine lacks --
// its Primitives plus its ValueHeads (the "count:" heads its referenced
// value SVars read). An empty result means the card is playable.
func (r *Registry) Unsupported(c *Card, supported map[string]bool) []string {
	var out []string
	for _, p := range c.Primitives() {
		if !supported[p] {
			out = append(out, p)
		}
	}
	// The count-expression heads the card's referenced value SVars read
	// (ValueHeads): a gate or amount over a head the evaluator does not
	// model can never be read, so the card is not fully playable either.
	for _, p := range c.ValueHeads() {
		if !supported[p] {
			out = append(out, p)
		}
	}
	return out
}

// named reports whether c parsed to an actual card identity. A zero-byte or
// otherwise empty script parses without error into a Card holding one Face
// with every field at its zero value: no name, no abilities, and therefore
// no entries from Primitives/Unsupported. Coverage must not let that read as
// "playable" — it isn't a card at all, just an artifact of a bad file — so
// such cards are excluded from Coverage entirely rather than silently
// inflating the Supported count and the headline playable-card percentage.
func (c *Card) named() bool {
	for _, f := range c.Faces {
		if f.Name != "" {
			return true
		}
	}
	return false
}

func (r *Registry) Coverage(supported map[string]bool) Coverage {
	cv := Coverage{Missing: map[string]int{}}
	for _, c := range r.cards {
		if !c.named() {
			continue
		}
		cv.Cards++
		miss := r.Unsupported(c, supported)
		if len(miss) == 0 {
			cv.Supported++
			continue
		}
		for _, m := range miss {
			cv.Missing[m]++
		}
	}
	return cv
}

// TopMissing ranks unimplemented primitives by how many cards each unlocks.
// Ties break on name so the report is stable run to run.
func (cv Coverage) TopMissing(n int) []MissingPrimitive {
	out := make([]MissingPrimitive, 0, len(cv.Missing))
	for k, v := range cv.Missing {
		out = append(out, MissingPrimitive{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Cards != out[j].Cards {
			return out[i].Cards > out[j].Cards
		}
		return out[i].Name < out[j].Name
	})
	if n < len(out) {
		out = out[:n]
	}
	return out
}
