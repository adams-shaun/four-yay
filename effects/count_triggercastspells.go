package effects

import "strings"

// evalTriggerCurrentCastSpellsOK resolves the
// "TriggerObjectsCurrentCastSpells$Valid <spec>[/Op]" head: Forge's snapshot
// of the spells cast this turn AT THE MOMENT a SpellCast trigger fired, the
// triggering spell included, filtered by <spec>. Thousand-Year Storm reads it
// with /Minus.1 ("for each other instant and sorcery spell you've cast before
// it this turn") and Sentinel Tower bare ("1 plus the number ... cast before
// that spell"), so the count is inclusive of the triggering cast.
//
// The answer is derived from the event log (EachSpellCastThisTurnMatching,
// newest cast first), so a replay derives the same number, and spells cast
// before the trigger's source entered count too -- the log, not the source,
// remembers them. The snapshot is the casts up to and including the
// triggering spell's own PutOnStack (Ctx.TriggerCard): a spell cast in
// response to the trigger is newer than that push and is not counted. When
// the triggering spell is not among the matches (no trigger binding) every
// matching cast this turn counts.
//
// ok is false for a body without the Valid argument, so an unknown shape stays
// unevaluated rather than a silent zero.
func evalTriggerCurrentCastSpellsOK(h Host, c *Ctx, body string) (int32, bool) {
	spec, op, hasOp := strings.Cut(body, "/")
	spec, ok := strings.CutPrefix(strings.TrimSpace(spec), "Valid ")
	if !ok {
		return 0, false
	}
	spec = strings.TrimSpace(spec)
	ids := h.EachSpellCastThisTurnMatching(c.Controller, spec, 0)
	start := 0
	if c.TriggerCard != 0 {
		for i, id := range ids {
			if id == c.TriggerCard {
				start = i
				break
			}
		}
	}
	n := int32(len(ids) - start)
	if hasOp {
		n = applyCountOp(n, strings.TrimSpace(op))
	}
	return n, true
}
