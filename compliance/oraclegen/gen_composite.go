package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/rules"
)

// xanswersForScenario translates a composite gorge election ("Yes — discard" /
// "Yes — put into the battlefield") into XMage's direct card selection. The
// card is taken from the verified before/after checkpoint, never guessed from
// the first library card: a spell can draw, reveal or replace that card before
// the election is answered. Other elections remain ordinary choices.
func xanswersForScenario(res rules.OracleResult, sc Scenario, modes map[string]int) [][]XAnswer {
	ds := append([]rules.OracleDecision(nil), res.Decisions...)
	for i := range ds {
		d := &ds[i]
		if d.Kind != "choose_n" || d.Options != 2 || len(d.Picks) != 1 ||
			pickKind(*d, 0) != "yes" || len(d.PickRefs) != 1 || d.Picks[0] != d.PickRefs[0] {
			continue
		}
		var zone string
		switch {
		case strings.HasPrefix(d.Picks[0], "Yes — discard"):
			zone = "graveyard"
		case strings.HasPrefix(d.Picks[0], "Yes — put into the battlefield"):
			zone = "battlefield"
		default:
			continue
		}
		// If the engine offered the subsequent object choice, use that
		// recorded pick instead of inferring it from the resulting board.
		followup := false
		for j := i + 1; j < len(ds) && ds[j].Step == d.Step; j++ {
			if ds[j].Seat == d.Seat && len(ds[j].PickKinds) > 0 &&
				(ds[j].PickKinds[0] == "discard" || ds[j].PickKinds[0] == "card") {
				followup = true
				break
			}
		}
		if followup {
			d.Kind = "composite_election" // no XMage yes/no dialog
			continue
		}
		names := movedCards(res.Snapshots, sc, d.Step, d.Seat, zone)
		if len(names) == 0 {
			continue // no observable selected card: do not invent one
		}
		d.Picks, d.PickRefs = names, append([]string(nil), names...)
		d.PickKinds = make([]string, len(names))
		for k := range d.PickKinds {
			d.PickKinds[k] = "card"
		}
		// A TargetCardInLibrary(0, MAX) choice is not completed by the
		// first pick: makeChoose needs an explicit stop after the subset.
		if zone == "battlefield" {
			d.Max = len(names) + 1
		} else {
			d.Max = len(names)
		}
	}
	return xanswers(ds, len(sc.Steps), modes)
}

// movedCards finds cards newly in the indicated destination during one step.
// For discard, the resolving spell itself also goes to the graveyard; remove
// exactly one occurrence of its name rather than mistaking it for the pick.
// If a step introduces several other objects, do not guess which one was
// chosen: the generator's ordinary fallback remains visible to the oracle.
func movedCards(snaps []rules.OracleSnapshot, sc Scenario, step, seat int, zone string) []string {
	if step < 0 || step+1 >= len(snaps) || step >= len(sc.Steps) || seat < 0 ||
		seat >= len(snaps[step].Players) || seat >= len(snaps[step+1].Players) {
		return nil
	}
	before, after := snaps[step], snaps[step+1]
	var old, next []string
	if zone == "graveyard" {
		old, next = before.Players[seat].Graveyard, after.Players[seat].Graveyard
	} else {
		for _, p := range before.Permanents {
			if p.Owner == seat {
				old = append(old, p.Ref)
			}
		}
		for _, p := range after.Permanents {
			if p.Owner == seat {
				next = append(next, p.Ref)
			}
		}
	}
	counts := make(map[string]int, len(old))
	for _, name := range old {
		counts[name]++
	}
	var added []string
	for _, name := range next {
		if counts[name] > 0 {
			counts[name]--
		} else {
			added = append(added, name)
		}
	}
	// A permanent spell enters during its resolve step too; it is not
	// the object selected by the enter trigger. Likewise an instant or
	// sorcery enters the graveyard after resolving.
	for j := step - 1; j >= 0; j-- {
		if sc.Steps[j].Op != "cast" {
			continue
		}
		spell := oraclediffRefName(sc.Steps[j].Card)
		for k, name := range added {
			if oraclediffRefName(name) == spell {
				added = append(added[:k], added[k+1:]...)
				break
			}
		}
		break
	}
	if zone == "battlefield" {
		for i, ref := range added {
			for _, p := range after.Permanents {
				if p.Ref == ref {
					added[i] = p.Name
					break
				}
			}
		}
	}
	if len(added) != 1 { // unrelated moves or an ambiguous multi-card selection
		return nil
	}
	return added
}
