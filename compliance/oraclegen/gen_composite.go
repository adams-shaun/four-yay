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
// castSteps is passed through to xanswers so a cast step's own targets are not
// scripted a second time.
func xanswersForScenario(res rules.OracleResult, sc Scenario, modes map[string]int, castSteps map[int]bool) [][]XAnswer {
	ds := append([]rules.OracleDecision(nil), res.Decisions...)
	for i := range ds {
		d := &ds[i]
		if d.Kind != "choose_n" || d.Options != 2 || len(d.Picks) != 1 ||
			pickKind(*d, 0) != "yes" || len(d.PickRefs) != 1 || d.Picks[0] != d.PickRefs[0] {
			continue
		}
		var zone string
		switch {
		case d.Picks[0] == "Yes — discard":
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
			pick := &ds[j]
			if pick.Seat != d.Seat || !compositeCardPick(*pick, zone) {
				continue
			}
			followup = true
			if len(pick.ObjectPicks) > 0 {
				d.Kind = pick.Kind
				d.GorgeKind = pick.GorgeKind
				d.Picks = make([]string, len(pick.ObjectPicks))
				d.PickRefs = append([]string(nil), pick.ObjectPicks...)
				d.PickKinds = make([]string, len(pick.ObjectPicks))
				for k, ref := range pick.ObjectPicks {
					d.Picks[k] = oraclediffRefName(ref)
					d.PickKinds[k] = "card"
					if zone == "graveyard" {
						d.PickKinds[k] = "discard"
					}
				}
				d.Max = pick.Max
				pick.Kind = "composite_election" // emitted once from the correlated picks
			} else {
				// Older transcripts still carry the selection on the follow-up.
				d.Kind = "composite_election" // no XMage yes/no dialog
			}
			break
		}
		if followup {
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
	return xanswers(ds, len(sc.Steps), modes, castSteps)
}

// compositeCardPick distinguishes the follow-up card selector from any other
// same-seat object-valued decision in the step. ObjectPicks alone is not a role:
// targets, costs, and replacement choices also record object identities.
func compositeCardPick(d rules.OracleDecision, zone string) bool {
	if d.Kind != "choose_n" && d.Kind != "mode" {
		return false
	}
	want := "card"
	if zone == "graveyard" {
		want = "discard"
	}
	if len(d.ObjectPicks) == 0 {
		return len(d.PickKinds) > 0 && d.PickKinds[0] == want
	}
	if len(d.PickKinds) != len(d.ObjectPicks) {
		return false
	}
	for _, kind := range d.PickKinds {
		if kind != want {
			return false
		}
	}
	return true
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
