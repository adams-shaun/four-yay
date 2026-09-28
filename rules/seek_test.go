package rules

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The api:Seek end-to-end leaf on a real compiled Seek SA: activation moves
// the sought cards library -> hand and emits exactly ONE events.Seek marker
// for the whole Num$ 3 action, which is what turns Vexyr, Ich-Tekik's Heir's
// "Whenever you seek one or more cards" from dormant groundwork into a live
// trigger -- exactly one Golem token, not three. A no-match seek emits
// neither the marker nor the trigger. Replay-verified throughout.
//
// The carrier is an authored fixture artifact (never a corpus .txt, per the
// licensing rule) with `A:AB$ Seek`, so the SA is compiled by the real cards
// parser and resolves through the engine's own AB activation path.

// seekRelicSrc is a tap-for-seek artifact whose seek filter is the parameter.
func seekRelicSrc(typeSpec string, num int) string {
	return "Name:Seek Relic\nTypes:Artifact\n" +
		"A:AB$ Seek | Cost$ T | Type$ " + typeSpec + " | Num$ " + strconv.Itoa(num) +
		" | SpellDescription$ Seek.\nOracle:x\n"
}

func seekMarkerCount(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Seek {
			n++
		}
	}
	return n
}

// seekEligibleInLibrary counts seat p's library cards matching a Type$ spec
// through a real card-face check, for the test's own precondition.
func seekEligibleInLibrary(t *testing.T, e *Engine, p state.PlayerID, typeName string) int {
	t.Helper()
	n := 0
	for _, id := range e.G.Zone(state.ZLibrary, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			for _, ty := range o.Face().Types {
				if ty == typeName {
					n++
					break
				}
			}
		}
	}
	return n
}

func TestVexyrRealSeekEmitsOneMarkerForMultiCardSeek(t *testing.T) {
	t.Parallel()
	vexyr := tokenReplCorpusCard(t, "Vexyr, Ich-Tekik's Heir")
	relic := cardByName(t, seekRelicSrc("Creature", 3))
	bear := cardByName(t, "Name:Grizzly Bears\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	seat0 := []*cards.Card{vexyr, relic}
	for i := 0; i < 15; i++ {
		seat0 = append(seat0, bear)
	}
	e, cfg := tokenReplGame(t, 101, seat0...)
	vexyrID := moveSeededCard(t, e, 0, vexyr, state.ZBattlefield)
	relicID := moveSeededCard(t, e, 0, relic, state.ZBattlefield)

	eligible := seekEligibleInLibrary(t, e, 0, "Creature")
	if eligible < 3 {
		t.Fatalf("precondition: only %d eligible creatures in seat 0's library, need 3", eligible)
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))

	// Activate the Seek Relic's only ability and drain.
	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, relicID, 0).Index)
	passUntilStackEmpty(t, e, 40)

	handAfter := len(e.G.Zone(state.ZHand, 0))
	if handAfter-handBefore != 3 {
		t.Fatalf("seek moved %d cards, want exactly 3", handAfter-handBefore)
	}
	if got := seekMarkerCount(e); got != 1 {
		t.Fatalf("Num$ 3 seek emitted %d Seek markers, want exactly 1", got)
	}
	if got := triggerPushesFor(e, vexyrID); got != 1 {
		t.Fatalf("Vexyr had %d trigger pushes, want exactly 1 for one seek action", got)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Phyrexian Golem Token"); got != 1 {
		t.Fatalf("Vexyr created %d Golem tokens, want exactly 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestSignatureSpellsRealSeekImprintsHandForItsExileContinuation is the real
// corpus ImprintFound$ carrier end to end: Signature Spells' ETB seeks two
// mana-value-3 instant/sorcery cards into hand and its chained
// `Defined$ Imprinted | Origin$ Hand | Destination$ Exile` body must exile
// exactly those two. Before the SeekFound association the continuation
// resolved to an empty set and both cards stayed in hand.
func TestSignatureSpellsRealSeekImprintsHandForItsExileContinuation(t *testing.T) {
	t.Parallel()
	spells := tokenReplCorpusCard(t, "Signature Spells")
	// Authored cmc-3 instants/sorceries (never a corpus .txt): ManaCost 2R is
	// mana value 3, so they satisfy Card.Instant/Sorcery+cmcEQ3.
	bolt := cardByName(t, "Name:Test Bolt\nManaCost:2 R\nTypes:Instant\nOracle:x\n")
	ritual := cardByName(t, "Name:Test Ritual\nManaCost:2 R\nTypes:Sorcery\nOracle:x\n")
	seat0 := []*cards.Card{spells, bolt, bolt, bolt, ritual}
	e, cfg := tokenReplGame(t, 107, seat0...)
	// Precondition: at least two cmc-3 instants/sorceries sit in the library.
	eligible := 0
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		if o.Face().ManaValue() == 3 && (seekFaceHasType(o.Face().Types, "Instant") || seekFaceHasType(o.Face().Types, "Sorcery")) {
			eligible++
		}
	}
	if eligible < 2 {
		t.Fatalf("precondition: only %d eligible cmc-3 spells in library, need 2", eligible)
	}
	// Count the cmc-3 instants/sorceries already in hand (drawn in the opening
	// hand): the continuation must move exactly two of them to exile, so the
	// hand count must drop by exactly two. Counting the delta keeps the
	// assertion honest when the opening hand already held a copy.
	seekSpellInHand := func() int {
		n := 0
		for _, id := range e.G.Zone(state.ZHand, 0) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().ManaValue() == 3 &&
				(seekFaceHasType(o.Face().Types, "Instant") || seekFaceHasType(o.Face().Types, "Sorcery")) {
				n++
			}
		}
		return n
	}
	handBefore := seekSpellInHand()
	moveSeededCard(t, e, 0, spells, state.ZBattlefield)
	addMana(t, e, 0, "") // re-ask priority so the ETB trigger queues
	passUntilStackEmpty(t, e, 60)

	var exiled []state.ObjID
	for _, id := range e.G.Zone(state.ZExile, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && (seekFaceHasType(o.Face().Types, "Instant") || seekFaceHasType(o.Face().Types, "Sorcery")) {
			exiled = append(exiled, id)
		}
	}
	if len(exiled) != 2 {
		t.Fatalf("Signature Spells exiled %d sought spells, want exactly 2", len(exiled))
	}
	if got := seekSpellInHand(); got != handBefore {
		t.Fatalf("hand cmc-3 spells = %d after the resolution, want %d: the seek adds two and the continuation must remove the same two (without it the hand would hold four)", got, handBefore)
	}
	replayCheck(t, e, cfg)
}

func seekFaceHasType(types []string, want string) bool {
	for _, ty := range types {
		if ty == want {
			return true
		}
	}
	return false
}

func TestVexyrRealSeekWithNoMatchEmitsNeitherMarkerNorTrigger(t *testing.T) {
	t.Parallel()
	vexyr := tokenReplCorpusCard(t, "Vexyr, Ich-Tekik's Heir")
	relic := cardByName(t, seekRelicSrc("Enchantment", 3))
	bear := cardByName(t, "Name:Grizzly Bears\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	seat0 := []*cards.Card{vexyr, relic}
	for i := 0; i < 15; i++ {
		seat0 = append(seat0, bear)
	}
	e, cfg := tokenReplGame(t, 103, seat0...)
	vexyrID := moveSeededCard(t, e, 0, vexyr, state.ZBattlefield)
	relicID := moveSeededCard(t, e, 0, relic, state.ZBattlefield)

	// Precondition: the seek filter genuinely has no match, so the no-marker
	// assertion below cannot pass because the seek never ran.
	if n := seekEligibleInLibrary(t, e, 0, "Enchantment"); n != 0 {
		t.Fatalf("precondition: %d enchantments in library, the no-match seek is not a no-match", n)
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))

	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, relicID, 0).Index)
	passUntilStackEmpty(t, e, 40)

	if got := len(e.G.Zone(state.ZHand, 0)) - handBefore; got != 0 {
		t.Fatalf("no-match seek moved %d cards, want 0", got)
	}
	// The handler MUST have run: without the registration the resolver emits
	// "unimplemented API Seek" and the no-marker/no-trigger assertions below
	// would pass vacuously.
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API Seek") {
			t.Fatalf("precondition/handler: Seek API not registered: %q", ev.Text)
		}
	}
	if got := seekMarkerCount(e); got != 0 {
		t.Fatalf("no-match seek emitted %d Seek markers, want 0", got)
	}
	if got := triggerPushesFor(e, vexyrID); got != 0 {
		t.Fatalf("Vexyr had %d trigger pushes on a no-match seek, want 0", got)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Phyrexian Golem Token"); got != 0 {
		t.Fatalf("Vexyr created %d Golem tokens on a no-match seek, want 0", got)
	}
	replayCheck(t, e, cfg)
}
