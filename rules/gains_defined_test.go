package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The GainsAbilitiesOfDefined$ spelling of the has-all-activated-abilities
// grant, pinned on the real corpus carriers at both of its delivery routes:
//
//   - Kasmina, Enigma Sage's printed
//     S:Mode$ Continuous | Affected$ Planeswalker.Other+YouCtrl |
//       GainsAbilitiesOfDefined$ Self | GainsValidAbilities$ Activated.Loyalty
//     (the direct static scanner, rules/layers.go's staticEffects);
//   - Quicksilver Elemental's {U} ability, whose SVar STSteal is an
//     DB$ Effect | StaticAbilities$ body carrying
//     Affected$ Card.EffectSource | GainsAbilitiesOfDefined$ RememberedLKI
//     (effects/misc.go's effEffect registration).
//
// The Defined set is resolved by effects.GainedFacesOfDefined (shared by both
// routes), so every assertion runs through the engine's own offer, activation
// and replay paths against the compiled carriers. Target cards are AUTHORED
// fixtures, never corpus .txt (the licensing rule).

// gainsDefinedWalker is the authored planeswalker the Kasmina test grants
// onto: a walker with NO activated ability of its own, so any loyalty ability
// offered on it can only have come through the GainsAbilitiesOfDefined$
// grant.
func gainsDefinedWalker() string {
	return "Name:Test Walker\nManaCost:2 U U\nTypes:Legendary Planeswalker Testwalker\nLoyalty:4\nOracle:x\n"
}

// gainsDefinedDrainer answers the pending KArrange ask a Scry resolution
// poses (keeping the scried card on top by choosing it for pile A) and passes
// priority otherwise, until the stack is empty -- passUntilStackEmpty's
// sibling that tolerates the one mid-resolution arrange decision.
func gainsDefinedDrainer(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		if len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack depth %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KArrange:
			if len(d.Options) == 0 {
				t.Fatalf("empty arrange decision: %+v", d)
			}
			submitChoices(t, e, d.Options[0].Index)
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, idx)
		default:
			t.Fatalf("unexpected decision %v while draining: %+v", d.Kind, d)
		}
	}
	t.Fatalf("stack never emptied within the drain budget")
}

// gainsDefinedDrive is the lifetime driver: it answers the decisions the
// no-play drive to the next turn poses (pass, empty attackers/blockers, the
// cleanup discard) and fails loudly on anything else, printing the decision.
func gainsDefinedDrive(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step) {
	t.Helper()
	for i := 0; i < 8000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before reaching turn %d seat %d step %s", turn, active, step)
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatal("no decision pending")
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, idx)
		case decision.KAttackers, decision.KBlockers:
			submitChoices(t, e)
		case decision.KArrange:
			submitChoices(t, e, d.Options[0].Index)
		case decision.KChoose:
			// A cleanup-step discard down to the hand-size limit (CR 514.1):
			// answer with the maximum the decision allows, first cards first.
			if d.Max <= 0 || d.Max > len(d.Options) {
				t.Fatalf("unexpected KChoose while driving: %+v", d)
			}
			choices := make([]int, 0, d.Max)
			for _, o := range d.Options {
				if len(choices) == d.Max {
					break
				}
				choices = append(choices, o.Index)
			}
			submitChoices(t, e, choices...)
		default:
			t.Fatalf("unexpected decision %v while driving: %+v", d.Kind, d)
		}
	}
	t.Fatalf("did not reach turn %d seat %d step %s", turn, active, step)
}

// gainsDefinedUnimplementedNotes counts unimplemented-API style Notes in the
// log: the grant routes must not degrade to a loud "continuous effect
// Continuous unimplemented" fallback (a test that only checks for an absence
// of offers would otherwise pass with the whole registration unregistered).
func gainsDefinedUnimplementedNotes(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented") {
			n++
		}
	}
	return n
}

// TestKasminaGainsSelfDefinedLoyaltyAbility pins the DIRECT route: Kasmina's
// printed `GainsAbilitiesOfDefined$ Self` static makes each other planeswalker
// you control gain Kasmina's own loyalty abilities, offered with Kasmina as
// the GainedSource anchor and resolvable through GainedAbilityPush.
func TestKasminaGainsSelfDefinedLoyaltyAbility(t *testing.T) {
	t.Parallel()
	kasmina := tokenReplCorpusCard(t, "Kasmina, Enigma Sage")
	walker := card(t, gainsDefinedWalker())
	e, cfg := tokenReplGame(t, 9109, kasmina, walker)
	kasminaID := moveSeededCard(t, e, 0, kasmina, state.ZBattlefield)
	walkerID := moveSeededCard(t, e, 0, walker, state.ZBattlefield)
	e.pending = nil
	e.priorityRound()

	// Preconditions: both walkers on the battlefield; the authored walker has
	// no printed activated ability and holds loyalty > 0 so a [+2] is
	// payable; Kasmina's face carries the loyalty abilities to gain; and no
	// unimplemented-API Note degraded the grant to a fallback.
	for id, name := range map[state.ObjID]string{kasminaID: "Kasmina", walkerID: "Test Walker"} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("%s = %+v, want on the battlefield", name, o)
		}
	}
	wk := e.G.Obj(walkerID)
	if wk.Face() == nil {
		t.Fatal("Test Walker has no face")
	}
	for _, ab := range wk.Face().Abilities {
		if ab != nil && ab.Kind == "AB" {
			t.Fatalf("precondition broken: Test Walker carries a printed activated ability %+v", ab)
		}
	}
	if got := wk.Counter("LOYALTY"); got != 4 {
		t.Fatalf("Test Walker loyalty = %d, want 4 (the [+2] must be payable)", got)
	}
	kasminaFace := e.G.Obj(kasminaID).Face()
	if kasminaFace == nil {
		t.Fatal("Kasmina has no face")
	}
	nAb := 0
	for _, ab := range kasminaFace.Abilities {
		if ab != nil && ab.Kind == "AB" {
			nAb++
		}
	}
	if nAb != 3 {
		t.Fatalf("Kasmina face has %d activated abilities, want the 3 loyalty lines", nAb)
	}
	if n := gainsDefinedUnimplementedNotes(e); n != 0 {
		t.Fatalf("%d unimplemented-API Notes in the log; the grant degraded instead of registering", n)
	}

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority", d)
	}
	// Kasmina's own printed loyalty ability is offered as before (Ability 0,
	// the [+2] Scry), with no gained anchor.
	printed := false
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == kasminaID && o.GainedSource == 0 && o.Ability == 0 {
			printed = true
		}
	}
	if !printed {
		t.Fatalf("Kasmina is not offered its own printed loyalty ability: %+v", d.Options)
	}
	// The other walker is offered the gained [+2] (GainedIdx 0 on Kasmina's
	// face), anchored on Kasmina; every loyalty option it is offered is a
	// GAINED one, and none is a printed ability of its own.
	scryOpt, scryFound := decision.Option{}, false
	for _, o := range d.Options {
		if o.Obj != walkerID || o.Kind != "ability" {
			continue
		}
		if o.GainedSource == 0 {
			t.Fatalf("Test Walker offered a printed ability %+v though its face carries none", o)
		}
		if o.GainedSource != kasminaID {
			t.Fatalf("gained option %+v anchored on %d, want Kasmina (%d)", o, o.GainedSource, kasminaID)
		}
		if o.GainedIdx == 0 {
			scryOpt, scryFound = o, true
		}
	}
	if !scryFound {
		t.Fatalf("Test Walker is not offered the gained [+2] Scry: %+v", d.Options)
	}

	// Resolution: activating the gained [+2] adds 2 loyalty to Test Walker
	// (the recipient pays loyalty, not Kasmina) and poses the Scry 1 ask,
	// which the drain answers keeping the card on top.
	submitChoices(t, e, scryOpt.Index)
	gainsDefinedDrainer(t, e, 30)
	if got := e.G.Obj(walkerID).Counter("LOYALTY"); got != 6 {
		t.Fatalf("Test Walker loyalty after the gained [+2] = %d, want 6", got)
	}
	if got := e.G.Obj(kasminaID).Counter("LOYALTY"); got != 2 {
		t.Fatalf("Kasmina loyalty = %d, want 2 (the recipient pays the loyalty cost)", got)
	}
	if n := countKind(e.L.Events, events.GainedAbilityPush, walkerID); n != 1 {
		t.Fatalf("GainedAbilityPush count on Test Walker = %d, want 1", n)
	}
	replayCheck(t, e, cfg)
}

// gainsDefinedTarget is the authored creature Quicksilver steals from: one
// observable non-mana activated ability, "{T}: Draw a card."
func gainsDefinedTarget() string {
	return "Name:Gains Draw Target\nManaCost:1 U\nTypes:Creature Human Wizard\nPT:1/1\n" +
		"A:AB$ Draw | Cost$ T | NumCards$ 1 | SpellDescription$ Draw a card.\nOracle:x\n"
}

// gainsDefinedMixedCarrier deliberately combines both activated-half spellings:
// the ordinary filter names every controlled artifact, while Defined$ Self
// adds the carrier itself. The foreign artifact below is reachable only from
// the ordinary spelling, so replacing instead of appending the Defined faces
// loses its Draw activation.
func gainsDefinedMixedCarrier() string {
	return "Name:Mixed Gains Carrier\nTypes:Artifact\n" +
		"S:Mode$ Continuous | Affected$ Card.Self | EffectZone$ Battlefield | GainsAbilitiesOf$ Artifact.YouCtrl | GainsAbilitiesOfDefined$ Self\nOracle:x\n"
}

// gainsDefinedForeignArtifact is selectable only by the card-filter half of
// the mixed static. Its costless draw is immediately activatable, keeping the
// regression focused on face collection rather than summoning sickness.
func gainsDefinedForeignArtifact() string {
	return "Name:Mixed Foreign Artifact\nTypes:Artifact\n" +
		"A:AB$ Draw | NumCards$ 1 | SpellDescription$ Draw a card.\nOracle:x\n"
}

// TestMixedGainsAbilitySpellingsAppendFaces guards a static carrying both
// GainsAbilitiesOf$ and GainsAbilitiesOfDefined$: both collections remain in
// deterministic parameter order, and the card-filter-only foreign activation
// is offered and resolves through the ordinary gained-ability path.
func TestMixedGainsAbilitySpellingsAppendFaces(t *testing.T) {
	t.Parallel()
	carrier := card(t, gainsDefinedMixedCarrier())
	foreign := card(t, gainsDefinedForeignArtifact())
	e, cfg := tokenReplGame(t, 9111, carrier, foreign)
	carrierID := moveSeededCard(t, e, 0, carrier, state.ZBattlefield)
	foreignID := moveSeededCard(t, e, 0, foreign, state.ZBattlefield)
	e.pending = nil
	e.priorityRound()

	// The direct filter must have a foreign artifact to select, and the
	// Defined spelling must resolve Self to the static host.
	for id, name := range map[state.ObjID]string{carrierID: "carrier", foreignID: "foreign"} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("%s = %+v, want on the battlefield", name, o)
		}
	}
	var gained []state.ObjID
	for _, ce := range e.active() {
		if ce.Source != carrierID || ce.Layer != LAbilities {
			continue
		}
		for _, gf := range ce.GainedFaces {
			gained = append(gained, gf.Obj)
		}
	}
	want := []state.ObjID{carrierID, foreignID, carrierID}
	if len(gained) != len(want) {
		t.Fatalf("GainedFaces = %v, want %v (both spellings appended)", gained, want)
	}
	for i := range want {
		if gained[i] != want[i] {
			t.Fatalf("GainedFaces = %v, want %v (deterministic filter then Defined order)", gained, want)
		}
	}

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority", d)
	}
	opt, found := decision.Option{}, false
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == carrierID && o.GainedSource == foreignID && o.GainedIdx == 0 {
			opt, found = o, true
		}
	}
	if !found {
		t.Fatalf("carrier offers no card-filter gained ability from foreign artifact: %+v", d.Options)
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("mixed grant draw: hand %d -> %d, want +1", handBefore, got)
	}
	replayCheck(t, e, cfg)
}

// TestQuicksilverGainsRememberedDefinedAbility pins the EFFECT-DELIVERED
// route: Quicksilver Elemental's {U} ability targets a creature and its
// STSteal static (DB$ Effect | StaticAbilities$) grants Quicksilver all
// activated abilities of the RememberedLKI target until end of turn.
func TestQuicksilverGainsRememberedDefinedAbility(t *testing.T) {
	t.Parallel()
	quick := tokenReplCorpusCard(t, "Quicksilver Elemental")
	target := card(t, gainsDefinedTarget())
	e, cfg := tokenReplGame(t, 9110, quick, target)
	quickID := moveSeededCard(t, e, 0, quick, state.ZBattlefield)
	targetID := moveSeededCard(t, e, 0, target, state.ZBattlefield)
	e.pending = nil
	e.priorityRound()

	// Preconditions: the target is on the battlefield carrying its own printed
	// activated ability, and Quicksilver is a creature that can activate.
	if o := e.G.Obj(targetID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("target = %+v, want on the battlefield", o)
	}
	if q := e.G.Obj(quickID); q == nil || q.Zone != state.ZBattlefield || q.Face() == nil ||
		len(q.Face().Abilities) == 0 {
		t.Fatalf("Quicksilver = %+v, want on the battlefield with its {U} ability", q)
	}
	// The gained ability costs {T}, so drive past summoning sickness first.
	driveToStep(t, e, 3, 0, state.StepMain1)

	// Activate the real {U} ability against the target.
	addMana(t, e, 0, "U")
	opt := abilityOption(t, e, quickID, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision after activating Quicksilver: %+v", d)
	}
	tidx := -1
	for _, o := range d.Options {
		if o.Kind == "permanent" && o.Obj == targetID {
			tidx = o.Index
		}
	}
	if tidx < 0 {
		t.Fatalf("target decision does not offer the creature: %+v", d.Options)
	}
	submitChoices(t, e, tidx)
	passUntilStackEmpty(t, e, 20)

	// The Effect's registration is live and captures the target BOTH in its
	// remembered set (RememberLKI$ Targeted) and as a GainedFace carrying the
	// target's live compiled face -- the pointer identity GainedAbilityPush
	// replay and the owning-face SVar recovery depend on.
	live := false
	remembered := false
	for _, ce := range e.active() {
		if ce.Source != quickID {
			continue
		}
		for _, id := range ce.Remembered {
			if id == targetID {
				remembered = true
			}
		}
		for _, gf := range ce.GainedFaces {
			if gf.Obj == targetID && gf.Face != nil && gf.Face.Name == "Gains Draw Target" {
				live = true
			}
		}
	}
	if !remembered {
		t.Fatal("the Effect did not remember the targeted creature")
	}
	if !live {
		t.Fatal("no live continuous registration grants Quicksilver the target's face")
	}
	if n := gainsDefinedUnimplementedNotes(e); n != 0 {
		t.Fatalf("%d unimplemented-API Notes after the activation; the Effect route degraded", n)
	}

	// Quicksilver now offers the foreign ability, anchored on the target.
	if d := e.Pending(); d == nil {
		e.priorityRound()
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want priority", d)
	}
	gopt, found := decision.Option{}, false
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == quickID && o.GainedSource == targetID && o.GainedIdx == 0 {
			gopt, found = o, true
		}
	}
	if !found {
		t.Fatalf("Quicksilver offers no gained ability anchored on the target: %+v", d.Options)
	}

	// Resolution: activating the gained draw taps Quicksilver and draws.
	handBefore := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, gopt.Index)
	passUntilStackEmpty(t, e, 20)
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("gained draw: hand %d -> %d, want +1", handBefore, got)
	}
	if n := countKind(e.L.Events, events.GainedAbilityPush, quickID); n != 1 {
		t.Fatalf("GainedAbilityPush count on Quicksilver = %d, want 1", n)
	}
	replayCheck(t, e, cfg)

	// Lifetime: the Effect is Duration$ UntilHostLeavesPlayOrEOT, so the
	// grant expires at end of turn; on the next turn's main phase neither a
	// registration nor an offer remains.
	gainsDefinedDrive(t, e, 4, 1, state.StepMain1)
	for _, ce := range e.active() {
		if ce.Source != quickID {
			continue
		}
		for _, gf := range ce.GainedFaces {
			if gf.Obj == targetID {
				t.Fatalf("the grant outlived its UntilHostLeavesPlayOrEOT lifetime: %+v", ce)
			}
		}
	}
	idx := -1
	if d := e.Pending(); d != nil && d.Kind == decision.KPriority {
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
	}
	if idx >= 0 {
		submitChoices(t, e, idx)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("pending after passing seat 1 = %+v, want seat 0 priority", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == quickID && o.GainedSource != 0 {
			t.Fatalf("Quicksilver still offers a gained ability after the grant expired: %+v", o)
		}
	}
	replayCheck(t, e, cfg)
}
