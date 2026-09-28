// The CastSa Spell.ManaFromArtifact cast-provenance predicate (task
// mayplay-mfa) — "if mana from an artifact was spent to cast it" (Shadow the
// Hedgehog's Chaos Control) — was read nowhere: the tag table held only
// Treasure/Cave/Desert, so the predicate failed closed on every cast and the
// Chaos Control grant was inert. The fix records an Artifact producer tag on
// the SAME per-unit ManaAdd encoding the three sibling spend spellings use
// (state.TypedManaTags gained Artifact, so a mana rock that prints none of
// the three earlier tags tags Artifact<colour>), which makes the tag readable
// at every site the row names:
//
//   - the ValidLKI$ replacement match (replacement.go -> castSaAdmits),
//   - the layer Affected$ match (layers.go matchesWithTypes ->
//     castProvenanceAdmitsWindow -> castSaAdmits),
//   - Count$ThisTurnCast_ (stack.go spellsCastThisTurnMatching), and
//   - castSaAdmits itself.
//
// The test drives the real corpus card (Shadow the Hedgehog) for the spec
// text and the end-to-end layer-6 keyword grant, and pairs every positive
// assertion with a plain-mana cast that must stay fail-closed, so the two
// compared values really differ.

package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestCastSaManaFromArtifactAllSites(t *testing.T) {
	t.Parallel()
	shadow := corpusAlternativeCard(t, "Shadow the Hedgehog")
	if len(shadow.Faces) == 0 {
		t.Fatal("Shadow the Hedgehog has no face")
	}
	// The real corpus static is what the layer Affected$ site matches; assert
	// it carries the predicate and the Stack active zone before leaning on it.
	var affected string
	for _, st := range shadow.Faces[0].Statics {
		if strings.Contains(st.Params["Affected"], "CastSa Spell.ManaFromArtifact") {
			affected = st.Params["Affected"]
			if got := st.Params["AffectedZone"]; got != "Stack" {
				t.Fatalf("Shadow's Chaos Control static AffectedZone = %q, want Stack", got)
			}
		}
	}
	if affected == "" {
		t.Fatalf("Shadow the Hedgehog's static does not carry CastSa Spell.ManaFromArtifact: %+v", shadow.Faces[0].Statics)
	}

	blank := card(t, "Name:Blank\nManaCost:C\nTypes:Sorcery\nOracle:x\n")
	e := handEngine(t, shadow, blank)
	shadowID := e.G.Zone(state.ZHand, 0)[0]
	spell := e.G.Zone(state.ZHand, 0)[1]
	placeOnBattlefield(t, e, shadowID)
	if o := e.G.Obj(shadowID); o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Shadow is on %s, want the battlefield", o.Zone)
	}

	// The artifact-paid cast: the push first, then the payment's tagged
	// negative ManaAdd below it in log order (the spend window walks backward
	// from the end to the push).
	o := e.G.Obj(spell)
	o.Zone = state.ZStack
	e.emit(events.Event{Kind: events.PutOnStack, Obj: spell, From: state.ZHand, To: state.ZStack, Player: 0})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "ArtifactC", Amount: -1})
	// Preconditions: the predicate's two inputs are really present and
	// really differ from the plain case.
	if !e.WasCastByYou(spell, 0) {
		t.Fatal("precondition: the PutOnStack did not record seat 0 as the caster")
	}
	if tag, _, ok := state.TypedManaCounter("ArtifactC"); !ok || tag != state.TypedArtifact {
		t.Fatalf("precondition: ArtifactC is not the artifact tag (tag=%d ok=%v)", tag, ok)
	}
	if tag, _, ok := state.TypedManaCounter("C"); ok {
		t.Fatalf("precondition: a plain counter parsed as typed tag %d; the two cases must differ", tag)
	}

	// 1. castSaAdmits itself.
	spec, admitted := e.castSaAdmits("Card.CastSa Spell.ManaFromArtifact", spell)
	if !admitted || spec == "" || strings.Contains(spec, "ManaFromArtifact") {
		t.Fatalf("castSaAdmits = %q admitted=%v, want the artifact-paid cast admitted and the token stripped", spec, admitted)
	}
	// 2. The combined layer entry point.
	layerSpec, ok := e.castProvenanceAdmits("Card.CastSa Spell.ManaFromArtifact", spell, 0)
	if !ok || layerSpec == "" || strings.Contains(layerSpec, "ManaFromArtifact") {
		t.Fatalf("castProvenanceAdmits = %q ok=%v, want admitted", layerSpec, ok)
	}
	// 3. The layer Affected$ match, on the real corpus spec.
	ce := ContinuousEffect{Source: shadowID, Controller: 0, Affects: affected}
	if !e.matchesWithTypes(&ce, spell, nil, state.ZStack) {
		t.Fatal("matchesWithTypes rejected the artifact-paid cast for Shadow's real Affected$ spec")
	}
	// 4. Count$ThisTurnCast_.
	counted := e.spellsCastThisTurnMatching(0, "Card.CastSa Spell.ManaFromArtifact", 0)
	if len(counted) != 1 || counted[0] != spell {
		t.Fatalf("spellsCastThisTurnMatching = %v, want [%d]", counted, spell)
	}
	// 5. The ValidLKI$ replacement match (the row's first site).
	move := events.Event{Kind: events.MoveZone, Obj: spell, From: state.ZStack, To: state.ZGraveyard}
	repl := cards.Repl{Event: "Moved", Params: map[string]string{
		"Origin": "Stack", "Destination": "Graveyard", "ValidLKI": "Card.CastSa Spell.ManaFromArtifact",
	}}
	if !e.replacementMatches(repl, spell, move) {
		t.Fatal("replacementMatches rejected the artifact-paid cast for its ValidLKI spec")
	}
	// End to end: Shadow's Chaos Control really grants Split second through
	// the layer walk to the artifact-paid spell.
	if !hasSplitSecond(e.derivedWith(spell, state.ZStack).Keywords) {
		t.Fatalf("Shadow did not grant Split second to the artifact-paid cast: %v", e.derivedWith(spell, state.ZStack).Keywords)
	}

	// The plain-paid cast: every one of the five sites must stay fail-closed,
	// which is what proves the positive route above was not vacuous.
	blank2 := card(t, "Name:Blank2\nManaCost:C\nTypes:Sorcery\nOracle:x\n")
	o2 := e.G.AddObject(blank2, 0)
	o2.Zone = state.ZStack
	e.emit(events.Event{Kind: events.PutOnStack, Obj: o2.ID, From: state.ZHand, To: state.ZStack, Player: 0})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: -1})
	if !e.WasCastByYou(o2.ID, 0) {
		t.Fatal("precondition: the plain cast was not recorded as seat 0's")
	}
	if _, admitted := e.castSaAdmits("Card.CastSa Spell.ManaFromArtifact", o2.ID); admitted {
		t.Fatal("castSaAdmits admitted a plain-paid cast")
	}
	if _, ok := e.castProvenanceAdmits("Card.CastSa Spell.ManaFromArtifact", o2.ID, 0); ok {
		t.Fatal("castProvenanceAdmits admitted a plain-paid cast")
	}
	if e.matchesWithTypes(&ce, o2.ID, nil, state.ZStack) {
		t.Fatal("matchesWithTypes admitted a plain-paid cast")
	}
	if got := e.spellsCastThisTurnMatching(0, "Card.CastSa Spell.ManaFromArtifact", 0); len(got) != 1 {
		t.Fatalf("spellsCastThisTurnMatching = %v, want only the artifact-paid cast", got)
	}
	move2 := events.Event{Kind: events.MoveZone, Obj: o2.ID, From: state.ZStack, To: state.ZGraveyard}
	if e.replacementMatches(repl, o2.ID, move2) {
		t.Fatal("replacementMatches admitted a plain-paid cast")
	}
	if hasSplitSecond(e.derivedWith(o2.ID, state.ZStack).Keywords) {
		t.Fatal("Shadow granted Split second to a plain-paid cast")
	}
}

func hasSplitSecond(keywords []string) bool {
	for _, k := range keywords {
		if strings.EqualFold(cardsKeywordHead(k), "Split second") {
			return true
		}
	}
	return false
}

// TestManaProducerTagsArtifact pins the PRODUCER half end to end on a real
// corpus mana rock: the other leaf here seeds the pool with a hand-written
// "ArtifactC" counter, so the effect-side tagging (ManaProducerTag reading
// the producing permanent's Face().Types through state.TypedManaTags) could
// break without failing it. Sol Ring's printed {T}: Add {C}{C} is activated
// through the real option wheel and the produced units are asserted to sit in
// the Artifact tally.
func TestManaProducerTagsArtifact(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Sol Ring"))
	ring := e.G.Zone(state.ZHand, 0)[0]
	placeOnBattlefield(t, e, ring)
	if o := e.G.Obj(ring); o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: Sol Ring zone=%s tapped=%v, want an untapped battlefield artifact", o.Zone, o.Tapped)
	}
	if !faceHasType(e.G.Obj(ring), "Artifact") {
		t.Fatal("precondition: Sol Ring's face is not an Artifact")
	}
	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision")
	}
	act := -1
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == ring {
			act = o.Index
		}
	}
	if act < 0 {
		t.Fatalf("Sol Ring's mana ability is not offered: %+v", d.Options)
	}
	submitChoices(t, e, act)
	if got := e.G.Players[0].TypedMana[state.TypedArtifact][state.MC]; got != 2 {
		t.Fatalf("Artifact typed tally MC = %d, want 2 (effMana must tag Sol Ring's units from its Artifact type)", got)
	}
	if got := e.G.Players[0].TypedMana[state.TypedTreasure][state.MC]; got != 0 {
		t.Fatalf("Treasure typed tally MC = %d, want 0 (an artifact that prints no Treasure subtype must not tag Treasure)", got)
	}
}
