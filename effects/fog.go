package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Fog", effFog) }

// effFog implements the CR 701.14a-style "prevent all combat damage that
// would be dealt this turn" effect (Fog, Constant Mists, Holy Day -- 15
// corpus files carry the API). It registers one continuous effect whose
// Restriction mode "PreventCombatDamage" the turn structure's combat-damage
// step consults (rules/combat.go's dealCombatDamage): while it is active,
// every combat-damage assignment of that step is skipped and a Note says so.
//
// Lifetime: the ordinary UntilEOT discipline -- the effect expires in the
// EndOfTurnCleanup of the turn it was cast in (CR 514.2), which is exactly
// "this turn" for an instant/sorcery source; the effectUntilEOT helper is the
// same one an Effect's Duration$ routes through, so a corpus Fog line always
// reads as until-end-of-turn here (Fog effects on PERMANENT sources would be
// a different shape -- the corpus has none; the Duration$ of the SP line is
// what the source's own spell shape decides, and no Fog SP line carries one).
//
// The consultation point lives in rules because "would be dealt" is a
// combat-step decision, not an effect; the effect only registers the fact.
// Prevention here is unconditional and total for the turn -- the corpus's Fog
// lines carry no exceptions (no "except combat damage from X"), so the
// register-and-consult split loses nothing.
func effFog(h Host, c *Ctx, sa *cards.SA) {
	h.AddContinuous(state.ContinuousEffect{
		Source:      c.Source,
		Controller:  c.Controller,
		UntilEOT:    effectUntilEOT(h, c.Source, sa.ParamStr(cards.PKDuration)),
		Restriction: "PreventCombatDamage",
		Duration:    "UntilEOT",
	})
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
		Text: "prevents all combat damage this turn"})
}
