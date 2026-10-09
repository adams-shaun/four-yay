package templates

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The cause-driven self recipes (ticket g5d, Transformed / Cycled /
// LandPlayed): each names the one way a p0-only turn-1 scenario causes the
// trigger's own event. Engine support already exists (rules/trigmatch
// TransformedMatches, cycledMatches, landPlayedMatches); this is purely the
// level-B classifier and its causes.

// transformTriggerRecipe serves Mode$ Transformed (ValidCard$ Card.Self).
// The engine fires a Transformed trigger only on the transform INTO the face
// that carries it (CR 701.26; rules/trigmatch_transformed_test.go), so the
// cause is the OTHER face's Phase transform enabler -- "At the beginning of
// your first main phase, you may pay {U}. If you do, transform NICKNAME." --
// with the card placed standing on the requirement's face: a face-1 row
// stands front-up and the front face's enabler transforms it into the back;
// a face-0 row stands back-up and the back face's enabler transforms it back
// into the front. A main-phase enabler fires at turn-1's own boundary, so
// the trigger is already on the stack when the scenario's steps begin; an
// end-step one is reached by pass_to. The runner's documented fallbacks pay
// the may-pay without a script: the mana window taps the first offered
// source each ask until the pool covers the cost, and the pay ask's first
// option is then the payment. The enabler's own firing condition (Sidequest's
// four Birds) is supplied by its condition fixtures.
func transformTriggerRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) ([]triggerCause, string) {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) != 2 {
		return nil, "transformed card not in corpus (or not two-faced)"
	}
	reqFace := -1
	for i, face := range c.Faces {
		if face == f {
			reqFace = i
		}
	}
	if reqFace < 0 {
		return nil, "requirement face not on the card"
	}
	// The card stands on the requirement's face; the enabler is the other
	// face's Phase transform trigger.
	enablerFace := 1 - reqFace
	enabler := phaseTransformEnabler(c.Faces[enablerFace])
	if enabler == nil {
		return nil, "no phase transform enabler on the standing face"
	}
	step, ok := phaseStep(enabler.ParamStr(cards.PKPhase))
	if !ok {
		return nil, "transform enabler phase " + enabler.ParamStr(cards.PKPhase) + " unsupported"
	}
	lands := transformCostLands(enablerSVarCost(enabler))
	if enablerSVarCost(enabler) != "" && len(lands) == 0 {
		return nil, "transform cost " + enablerSVarCost(enabler) + " has no basic land"
	}
	graveyard := transformCostGraveyard(enablerSVarCost(enabler))
	steps := []oraclegen.Step{}
	xability := []string{}
	if step != "main1" {
		// A later-phase enabler (Ultimecia's end step) is reached by pass_to;
		// a main-phase one fired at turn-1's own boundary and is already on
		// the stack when the steps begin. The XMage rule text stays "" on
		// this step: xability is parallel to steps from here on, each prefix
		// at its own activation's index (the runner throws on an activate
		// step with an empty xmage_ability and activates whatever text a
		// misaligned entry puts at that index).
		steps = append(steps, oraclegen.Step{Op: "pass_to", Seat: 0, Step: step})
		xability = append(xability, "")
	}
	if len(graveyard) > 0 {
		// A component-bearing may-pay settles its mana half from the pool
		// only (rules/cumulative.go triggeredCostComponentsPayable: the
		// window never opens a mana-activation ask for a component cost), so
		// the cause taps every land for mana at the end-step priority before
		// the passes pool-charge the payment.
		seen := map[string]int{}
		for _, land := range lands {
			lf, ok := reg.Lookup(land)
			if !ok || len(lf.Faces) == 0 {
				return nil, "land " + land + " not in corpus"
			}
			prefixes, why := oraclegen.XMageAbility(lf.Faces[0])
			if why != "" || len(prefixes) == 0 || prefixes[0] == "" {
				return nil, "land " + land + " mana ability xmage text ambiguous"
			}
			// Setup copies of one name carry ordinal refs ("p0:Island#2"), the
			// runner's own ref spelling for the second and later copies.
			seen[land]++
			ref := "p0:" + land
			if seen[land] > 1 {
				ref = fmt.Sprintf("%s#%d", ref, seen[land])
			}
			steps = append(steps, oraclegen.Step{Op: "activate", Seat: 0, Card: ref,
				Ability: "Activate " + land + " for mana"})
			xability = append(xability, prefixes[0])
		}
	}
	steps = append(steps,
		oraclegen.Step{Op: "pass_to", Seat: 0, Decision: "priority"},
		oraclegen.Step{Op: "pass", Seat: 0},
		oraclegen.Step{Op: "pass", Seat: 1},
	)
	xability = append(xability, "", "", "")
	base := triggerCause{battlefield: lands, graveyard: graveyard, steps: steps,
		selfBackUp: reqFace == 0, selfFrontUp: reqFace == 1}
	if len(graveyard) > 0 {
		base.xability = xability
	}
	out := []triggerCause{base}
	// The enabler's own condition (IsPresent$ Bird.YouCtrl GE4 on Sidequest)
	// is checked when the enabler fires: the same fixtures the enabler's own
	// Phase row serves supply the board, and the card's cast-time token (its
	// ETB) often supplies the last Bird itself.
	for _, cond := range conditionPreludes(reg, enabler.Params, c.Faces[enablerFace].SVars) {
		out = append(out, applyPrelude(base, cond))
	}
	for _, cond := range triggerConditionFixtures(reg, c.Faces[enablerFace], enabler) {
		out = append(out, applyPrelude(base, cond))
	}
	return out, ""
}

// cycledTriggerRecipe serves Mode$ Cycled (ValidCard$ Card.Self): the cause
// activates the card's own cycling ability from its hand. The cost discard
// is the engine's tagged cycle event, so the Cycled trigger is on the stack
// beside the cycle ability itself. The step mirrors the activate template's
// served cycling activations: same cost pool spelling, and the X announce
// and target asks fall back to the runner's documented defaults (X = 0,
// first offered target).
func cycledTriggerRecipe(f *cards.Face, name string) ([]triggerCause, string) {
	for i, sa := range f.Abilities {
		if !sa.IsActivated() || !strings.HasPrefix(strings.ToLower(sa.ParamStr(cards.PKKeyword)), "cycling") {
			continue
		}
		pool, gap := activationCostIn(sa.ParamStr(cards.PKCost), "hand", name)
		if gap != "" {
			return nil, "cycling cost gap: " + gap
		}
		// The activate step carries the ability's XMage rule text at its own
		// index, exactly as every served activate step does: the runner
		// throws on an activate step with an empty xmage_ability.
		prefixes, why := oraclegen.XMageAbility(f)
		if why != "" {
			return nil, "cycling xmage text ambiguous: " + why
		}
		idx := i
		prefix, ok := prefixes[idx]
		if !ok || prefix == "" {
			return nil, "cycling xmage text ambiguous"
		}
		return []triggerCause{{
			selfInHand: true,
			hand:       []string{name},
			steps: []oraclegen.Step{{
				Op: "activate", Seat: 0, Card: "p0:" + name,
				Mana: pool, AbilityIndex: &idx,
			}},
			xability: []string{prefix},
		}}, ""
	}
	return nil, "cycled has no cycling ability"
}

// landPlayedTriggerRecipe serves Mode$ LandPlayed without an origin
// qualifier: the cause is p0's own land drop. The runner's play op submits
// the play_land offer for a basic land in p0's hand, and the engine's
// MoveZone hand->battlefield event is what the trigger matches.
func landPlayedTriggerRecipe() ([]triggerCause, string) {
	return []triggerCause{{
		hand:  []string{"Forest"},
		steps: []oraclegen.Step{{Op: "play", Seat: 0, Card: "p0:Forest"}},
	}}, ""
}

// transformCostGraveyard seeds the enabler cost's ExileFromGrave<N/Card>
// component: N cards must be in the graveyard for the may-pay to be payable
// at all (an unpayable cost offers decline only). Only the any-card filter is
// seeded; a typed one would need a typed probe.
func transformCostGraveyard(cost string) []string {
	i := strings.Index(cost, "ExileFromGrave<")
	if i < 0 {
		return nil
	}
	rest := cost[i+len("ExileFromGrave<"):]
	j := strings.IndexByte(rest, '>')
	if j < 0 {
		return nil
	}
	spec := rest[:j]
	n := spec
	if k := strings.IndexByte(spec, '/'); k >= 0 {
		n, spec = spec[:k], spec[k+1:]
	}
	count, err := strconv.Atoi(strings.TrimSpace(n))
	if err != nil || count <= 0 || count > 12 {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(spec), "Card") {
		return nil
	}
	out := make([]string, 0, count)
	for k := 0; k < count; k++ {
		out = append(out, "Grizzly Bears")
	}
	return out
}

// phaseTransformEnabler names the face's Phase trigger whose Execute body is
// a SetState Mode$ Transform -- the self-transform enabler a Transformed
// trigger waits for.
func phaseTransformEnabler(f *cards.Face) *cards.Trigger {
	for i := range f.Triggers {
		t := &f.Triggers[i]
		if t.ModeKind() != cards.TriggerPhase || t.Effect == nil {
			continue
		}
		if t.Effect.API == "SetState" && strings.EqualFold(t.Effect.ParamStr(cards.PKMode), "Transform") {
			return t
		}
	}
	return nil
}

// enablerSVarCost reads the enabler body's Cost$ value ("" when the enabler
// transforms for free, Sidequest's four Birds).
func enablerSVarCost(enabler *cards.Trigger) string {
	if enabler.Effect == nil {
		return ""
	}
	return enabler.Effect.ParamStr(cards.PKCost)
}

// transformCostLands names the basic lands that pay the enabler's may-pay
// cost: one land per pip of its colour, with the generic pips charged to the
// first coloured pip and -- since the pool must cover the cost's whole pip
// count -- topped up on the last coloured pip. The runner's mana window taps
// offered sources one ask at a time until the pool covers the cost, so extra
// pips of either colour are equivalent; the order is the fixed basic-land
// order, never a map range.
func transformCostLands(cost string) []string {
	generic := 0
	total := 0
	colours := map[string]int{}
	for _, tok := range strings.Fields(cost) {
		if i := strings.IndexByte(tok, '<'); i >= 0 {
			continue // a non-mana cost component (ExileFromGrave<8/Card>)
		}
		switch strings.ToUpper(tok) {
		case "W":
			colours["Plains"]++
			total++
		case "U":
			colours["Island"]++
			total++
		case "B":
			colours["Swamp"]++
			total++
		case "R":
			colours["Mountain"]++
			total++
		case "G":
			colours["Forest"]++
			total++
		default:
			if n, err := strconv.Atoi(tok); err == nil && n > 0 {
				generic += n
			}
		}
	}
	total += generic
	out := []string{}
	last := ""
	first := true
	for _, land := range []string{"Plains", "Island", "Swamp", "Mountain", "Forest"} {
		n := colours[land]
		if n == 0 {
			continue
		}
		if first {
			n += generic
			first = false
		}
		last = land
		for i := 0; i < n; i++ {
			out = append(out, land)
		}
	}
	for len(out) < total && last != "" {
		out = append(out, last)
	}
	return out
}
