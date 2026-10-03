package effects

// RollRide is a resolution's DB$ RollDice publications (Ctx.LastRoll,
// Ctx.LastRollName, Ctx.RollPubs) as they stood when a mid-resolution ask
// suspended it: the legacy resume rebuilds a fresh Ctx, and without the ride
// a sub after the ask read the roll as unpublished (Numbing Jellyfish's
// "target player mills X cards, where X is the result" milled nothing once
// its target ask suspended the trigger). The resolution kernel's path keeps
// the one Ctx and never loses them; the ride holds legacy to the same
// answer. Immutable once captured: both ends copy.
type RollRide struct {
	Last int32
	Name string
	Pubs []RollPub
}

// RollRideOf captures c's roll publications for a resume point (nil c: none).
func RollRideOf(c *Ctx) RollRide {
	if c == nil {
		return RollRide{}
	}
	return RollRide{Last: c.Roll.Last, Name: c.Roll.LastName, Pubs: append([]RollPub(nil), c.Roll.Pubs...)}
}

// ResumeRollRide restores a captured ride onto a rebuilt Ctx, binding X
// exactly as the original publication did (a publication named X is the
// resolution's X).
func (c *Ctx) ResumeRollRide(r RollRide) {
	if r.Name == "" && len(r.Pubs) == 0 {
		return
	}
	c.Roll.Last, c.Roll.LastName = r.Last, r.Name
	c.Roll.Pubs = append([]RollPub(nil), r.Pubs...)
	for _, p := range r.Pubs {
		if p.Name == "X" {
			c.X = p.Value
		}
	}
}
