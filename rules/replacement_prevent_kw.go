package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The GRANTED "Prevent all [combat] damage that would be dealt to / by
// CARDNAME." keyword (CR 615.1: a prevention effect is a replacement of the
// damage event).
//
// A PRINTED K:Prevent line is expanded cards-side into a bodyless DamageDone
// `Prevent$ True` replacement on the face (cards/kw_prevent.go), which the
// face-Repl scan collects. A grant never reaches that expansion: a Pump's
// KW$, a static's AddKeyword$ and an Animate's Keywords$ only put the
// sentence on the affected object's DERIVED keyword list, where nothing read
// it, so the prevention silently never applied (Fleeting Flight: the counter
// and flying landed, the combat damage too). This is the bloodthirst/sunburst
// device (rules/replacement_etb.go) for damage: the sentence on the derived
// list is one more DamageDone replacement, collected after the face scan.
//
// All six corpus wordings are read -- {combat, any} x {to, by, to and by} --
// although only three of them are ever printed, so the three "dealt by"-only
// wordings exist as grants alone.
//
// Lifetime is the grant's own: the keyword is on the derived list exactly
// while its continuous effect is active, so an until-end-of-turn pump stops
// preventing at cleanup with no registration to expire.

// preventKW is one granted sentence: its precompiled head and the directions
// and combat scoping it words.
type preventKW struct {
	head   kwHead
	to, by bool
	combat bool
	// printable marks the three wordings cards/kw_prevent.go expands when
	// they are PRINTED on a face (its registerKeyword list): a face carrying
	// one already holds the expansion as its own R: lines.
	printable bool
}

var preventKWs = []preventKW{
	{head: newKWHead("Prevent all combat damage that would be dealt to CARDNAME."), to: true, combat: true, printable: true},
	{head: newKWHead("Prevent all combat damage that would be dealt by CARDNAME."), by: true, combat: true},
	{head: newKWHead("Prevent all combat damage that would be dealt to and dealt by CARDNAME."), to: true, by: true, combat: true, printable: true},
	{head: newKWHead("Prevent all damage that would be dealt to CARDNAME."), to: true, printable: true},
	{head: newKWHead("Prevent all damage that would be dealt by CARDNAME."), by: true},
	{head: newKWHead("Prevent all damage that would be dealt to and dealt by CARDNAME."), to: true, by: true},
}

// The four synthetic lines, package-level so a match's repl POINTER is stable
// across collections: the CR 616.1 competition identifies an already-applied
// printed replacement by (source id, repl pointer). They are the exact lines
// kwPrevent mints, so damageReplacementMatches and the Prevent$ arm of
// applyNonMoveReplacements treat a grant and a printed line identically.
var (
	preventToAny    = preventKWRepl("ValidTarget", false)
	preventToCombat = preventKWRepl("ValidTarget", true)
	preventByAny    = preventKWRepl("ValidSource", false)
	preventByCombat = preventKWRepl("ValidSource", true)
)

func preventKWRepl(gate string, combat bool) *cards.Repl {
	p := map[string]string{gate: "Card.Self", "Prevent": "True", "Keyword": "Prevent"}
	if combat {
		p["IsCombat"] = "True"
	}
	return &cards.Repl{Event: "DamageDone", Params: p}
}

// grantedPreventMatches returns the prevention replacements the GRANTED
// sentences contribute to this Damage event: the "dealt to" half read off the
// recipient object, the "dealt by" half off the damage source. A sentence the
// object's own face prints is skipped -- its expansion is already a face Repl
// and collecting both would only pose a pointless order choice between two
// full preventions. Applicability (the combat scoping, the source/recipient
// identity, the active-zone gate) is left to the ordinary matcher, exactly as
// for a printed line.
func (e *Engine) grantedPreventMatches(ev events.Event) []replMatch {
	if ev.Kind != events.Damage {
		return nil
	}
	var out []replMatch
	add := func(id state.ObjID, to bool) {
		if id == 0 {
			return
		}
		var anyDmg, combat bool
		for i := range preventKWs {
			k := &preventKWs[i]
			if (to && !k.to) || (!to && !k.by) || !e.hasKeywordH(id, k.head) {
				continue
			}
			if o := e.G.Obj(id); o == nil || o.Face() == nil || (k.printable && o.Face().HasKeyword(k.head.S)) {
				continue
			}
			if k.combat {
				combat = true
			} else {
				anyDmg = true
			}
		}
		// The unscoped sentence subsumes the combat one: one match per
		// direction, never two that would compete with each other.
		var r *cards.Repl
		switch {
		case anyDmg && to:
			r = preventToAny
		case anyDmg:
			r = preventByAny
		case combat && to:
			r = preventToCombat
		case combat:
			r = preventByCombat
		default:
			return
		}
		if e.replacementMatches(*r, id, ev) {
			out = append(out, replMatch{id: id, repl: r})
		}
	}
	add(ev.Obj, true)
	add(e.damaging, false)
	return out
}
