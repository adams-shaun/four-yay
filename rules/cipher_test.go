package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Cipher (CR 702.99): "Then you may exile this spell card encoded on a
// creature you control. Whenever that creature deals combat damage to a
// player, its controller may cast a copy of the encoded card without paying
// its mana cost."
//
// These tests drive the real compiled corpus cards where they exercise the
// shape: Arcane Heist (the report's named carrier) and Paranoid Delusions.
// Paranoid Delusions carries the full resolve -> encode -> combat-damage copy
// e2e because its copy body has no nested Play targeting; one inline Cipher
// probe gives the copy an observable, targetless body (a draw).

// cipherProbeSrc is an inline Cipher sorcery: resolving it draws a card, so a
// copy cast later is observable through the Draw event. The keyword line is
// the bare corpus spelling ("K:Cipher").
const cipherProbeSrc = "Name:Test Cipher Probe\nManaCost:0\nTypes:Sorcery\nK:Cipher\n" +
	"A:SP$ Draw | NumCards$ 1\n" +
	"Oracle:Cipher probe.\n"

// cipherBeanSrc is a vanilla 2/2 the tests attack with.
const cipherBeanSrc = "Name:Bean\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// cipherOppInstantSrc is a targetless instant parked in seat 1's library so a
// test can move it to seat 1's graveyard: Arcane Heist's spell ability targets
// an instant or sorcery card in an OPPONENT's graveyard, and a spell with no
// legal target fizzles (CR 608.2b) before its encode tail runs.
const cipherOppInstantSrc = "Name:Opp Probe\nManaCost:0\nTypes:Instant\nA:SP$ GainLife | LifeAmount$ 1\nOracle:x\n"

// cipherBoard builds a two-seat genesis whose seat-0 deck holds cipherCard
// (when non-nil) plus the inline creature sources. Every creature is placed on
// the battlefield, not summoning sick, ready to attack; the cipher card (if
// any) is left in seat 0's library. Every placement is a logged event, so the
// whole board replays.
func cipherBoard(t *testing.T, reg *cards.Registry, cipherCard *cards.Card, creatureSrcs ...string) (*Engine, Config, state.ObjID, []state.ObjID) {
	t.Helper()
	creatureCards := make([]*cards.Card, 0, len(creatureSrcs))
	var deck0 []*cards.Card
	if cipherCard != nil {
		deck0 = append(deck0, cipherCard)
	}
	for _, src := range creatureSrcs {
		c := card(t, src)
		creatureCards = append(creatureCards, c)
		deck0 = append(deck0, c)
	}
	if len(deck0) > 40 {
		t.Fatalf("cipherBoard deck has %d named cards, exceeds 40", len(deck0))
	}
	deck0 = append(deck0, mountainDeck(t, 40-len(deck0))...)
	deck1 := append([]*cards.Card{card(t, cipherOppInstantSrc)}, mountainDeck(t, 39)...)
	cfg := Config{Seed: 73, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{deck0, deck1}}
	e := New(cfg)

	var cipherID state.ObjID
	if cipherCard != nil {
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Owner == 0 && o.Card == cipherCard {
				cipherID = o.ID
			}
		}
		if cipherID == 0 {
			t.Fatalf("cipher card %q was not dealt to seat 0", cipherCard.Faces[0].Name)
		}
	}
	var placed []state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 {
			continue
		}
		for _, c := range creatureCards {
			if o.Card == c {
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
				placed = append(placed, o.ID)
			}
		}
	}
	if len(placed) != len(creatureSrcs) {
		t.Fatalf("placed %d of %d inline creatures", len(placed), len(creatureSrcs))
	}
	return e, cfg, cipherID, placed
}

// resolveCipher performs a REAL free cast of the cipher card, then drains the
// stack through the encode ask -- the resolution path cards/kw_cipher.go's
// tail instruction rides. It no longer fakes the stack->graveyard move the
// removed trigger fired on: the encode is part of spell resolution, so the
// test must exercise that resolution to reach the ask. encodeChoice is the
// KModes answer (nil declines).
func resolveCipher(t *testing.T, e *Engine, cipherID state.ObjID, encodeChoice []int) {
	t.Helper()
	o := e.G.Obj(cipherID)
	if o == nil {
		t.Fatal("cipher card vanished")
	}
	if o.Zone != state.ZLibrary {
		t.Fatalf("precondition: cipher card in zone %v, want library", o.Zone)
	}
	base := len(e.L.Events)
	e.beginPlay(0, cipherID, true, "", false, false)
	if n := countPutOnStackFor(e, base, o.Face().Name); n != 1 {
		t.Fatalf("precondition: actual cast put %d %s on the stack, want one", n, o.Face().Name)
	}
	if e.Pending() == nil {
		// A targetless spell is cast and left on the stack awaiting its first
		// priority round; a target-bearing one already posed its target ask.
		e.priorityRound()
	}
	drainCipher(t, e, 40, encodeChoice, nil)
}

// drainCipher drives the stack answering the cipher encode ask (ResumeKind
// "cipher") with encodeChoice, the copy-cast ask (ResumeKind "play") with
// copyChoice, and every other ask with option 0 / pass. It returns whether a
// copy-cast ask was actually posed, so a test can assert the offer ran
// instead of passing vacuously when nothing happened.
func drainCipher(t *testing.T, e *Engine, limit int, encodeChoice, copyChoice []int) bool {
	t.Helper()
	sawPlay, sawEncode := false, false
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the cipher stack (depth %d)", len(e.G.Stack))
		}
		switch {
		case d.Kind == decision.KModes && d.ResumeKind == "cipher":
			sawEncode = true
			if _, ok := cipherFindOption(d, "mode"); !ok {
				t.Fatalf("cipher encode ask posed no creature option: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: encodeChoice}); err != nil {
				t.Fatalf("submit cipher encode %v: %v", encodeChoice, err)
			}
		case d.Kind == decision.KModes && d.ResumeKind == "play":
			sawPlay = true
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: copyChoice}); err != nil {
				t.Fatalf("submit cipher copy %v: %v", copyChoice, err)
			}
		case d.Kind == decision.KPriority:
			idx, ok := cipherFindOption(d, "pass")
			if !ok {
				t.Fatalf("priority decision with no pass option: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		default:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
				t.Fatalf("submit %v: %v", d.Kind, err)
			}
		}
	}
	if !e.G.Over && len(e.G.Stack) > 0 {
		t.Fatalf("cipher stack never emptied (depth %d)", len(e.G.Stack))
	}
	if encodeChoice != nil && !sawEncode {
		t.Fatal("the eligible creature was not offered as a Cipher encode choice")
	}
	return sawPlay
}

// cipherFindOption returns the index of the first option of the given kind.
func cipherFindOption(d *decision.Decision, kind string) (int, bool) {
	for _, o := range d.Options {
		if o.Kind == kind {
			return o.Index, true
		}
	}
	return 0, false
}

// cipherEncodedOn returns the encoded card ids on the creature.
func cipherEncodedOn(e *Engine, id state.ObjID) []state.ObjID {
	if o := e.G.Obj(id); o != nil {
		return o.EncodedCards
	}
	return nil
}

// cipherHasResolutionTail reports whether the card's face carries the Cipher
// resolution-tail expansion the expander mints (the __kwCipher SVar
// rules/cipher.go appends to the resolving spell's chain) -- the precondition
// every e2e test depends on (without it, the engine silently does nothing and
// a "nothing happened" assertion would pass vacuously).
func cipherHasResolutionTail(c *cards.Card) bool {
	if c == nil || len(c.Faces) == 0 {
		return false
	}
	f := c.Faces[0]
	if _, ok := f.KeywordParam("Cipher"); !ok {
		return false
	}
	return cards.ResolveSVar(f.SVars, "__kwCipher") != nil
}

// castCipherDelusions drives the compiled spell through an actual free cast,
// target choice, spell resolution and encode decision rather than faking a
// stack-to-graveyard move. The copy cast later uses the same cast transaction.
func castCipherDelusions(t *testing.T, e *Engine, id state.ObjID, encodeChoice []int) {
	t.Helper()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZLibrary || o.Face().Name != "Paranoid Delusions" {
		t.Fatalf("precondition: Paranoid Delusions %d must be in library", id)
	}
	base := len(e.L.Events)
	e.beginPlay(0, id, true, "", false, false)
	if n := countPutOnStackFor(e, base, "Paranoid Delusions"); n != 1 {
		t.Fatalf("precondition: actual cast put %d Paranoid Delusions on the stack, want one", n)
	}
	drainCipher(t, e, 40, encodeChoice, nil)
}

// dealCipherCombatDamage emits the combat-damage event EXACTLY the way
// dealCombatDamage's assignment loop does: e.damaging names the dealer and
// e.combatDamaging is set for the emit, so rules.checkCipherTriggers's gate
// (combat + this source + a player recipient) is satisfied. A direct emit of
// the same shape the combat pass produces is the established pattern for
// isolating a combat-damage trigger (combat_damage_trigger_test.go).
func dealCipherCombatDamage(t *testing.T, e *Engine, attacker state.ObjID, defender state.PlayerID, amount int32) {
	t.Helper()
	a := e.G.Obj(attacker)
	if a == nil || a.Zone != state.ZBattlefield {
		t.Fatalf("precondition: attacker %d is not on the battlefield", attacker)
	}
	e.damaging = attacker
	e.combatDamaging = true
	e.emit(events.Event{Kind: events.Damage, Player: defender, Amount: amount})
	e.combatDamaging = false
	e.damaging = 0
	e.priorityRound()
}

// countPutOnStackFor counts PutOnStack events naming face after index base.
func countPutOnStackFor(e *Engine, base int, face string) int {
	n := 0
	for i := base; i < len(e.L.Events); i++ {
		ev := e.L.Events[i]
		if ev.Kind == events.PutOnStack && ev.Text == face {
			n++
		}
	}
	return n
}

// putOnStackObjFor returns the id of the first object put on the stack under
// face after index base, or 0 if none -- the copy's own identity, so a test
// can follow it off the stack and into its resolution instead of only counting
// that something was cast.
func putOnStackObjFor(e *Engine, base int, face string) state.ObjID {
	for i := base; i < len(e.L.Events); i++ {
		ev := e.L.Events[i]
		if ev.Kind == events.PutOnStack && ev.Text == face {
			return ev.Obj
		}
	}
	return 0
}

func cipherNote(e *Engine, id state.ObjID, text string) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Obj == id && ev.Text == text {
			return true
		}
	}
	return false
}

// zoneOf is a nil-safe zone name for failure messages.
func zoneOf(o *state.Object) any {
	if o == nil {
		return "<gone>"
	}
	return o.Zone
}

// TestCipherKeywordRegistration keeps the coverage declaration pinned to the
// tested implementation: reverting either expander or registration fails.
func TestCipherKeywordRegistration(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	c := mustCorpusCard(t, reg, "Paranoid Delusions")
	if !cipherHasResolutionTail(c) {
		t.Fatal("precondition: Cipher keyword did not expand")
	}
	if !effects.Supported()["kw:Cipher"] {
		t.Fatal("Cipher is not registered as a supported keyword")
	}
}

// TestCipherArcaneHeistEncodesOntoCreature drives the REAL compiled corpus
// card Arcane Heist through the encode half: its K:Cipher line expands into a
// resolution-tail instruction, resolving the spell offers the optional
// exile-encoded, and accepting it exiles the card and records the association
// on the chosen creature. Arcane Heist's spell ability targets an instant or
// sorcery in an opponent's graveyard, so a legal target is parked there first:
// with none the spell fizzles (CR 608.2b) and never reaches its encode tail,
// which would make the assertion below vacuous.
func TestCipherArcaneHeistEncodesOntoCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	heist := mustCorpusCard(t, reg, "Arcane Heist")
	if !cipherHasResolutionTail(heist) {
		t.Fatalf("Arcane Heist's K:Cipher did not expand into a resolution tail")
	}
	e, cfg, cipherID, creatures := cipherBoard(t, reg, heist, cipherBeanSrc)
	creature := creatures[0]
	// Arcane Heist's ValidTgts$ Instant.OppOwn,Sorcery.OppOwn: put one in
	// seat 1's graveyard so the spell actually resolves.
	oppTarget := negMoveByName(t, e, 1, "Opp Probe", state.ZGraveyard)
	if o := e.G.Obj(oppTarget); o == nil || o.Zone != state.ZGraveyard || o.Owner != 1 {
		t.Fatalf("precondition: legal opponent-graveyard target missing: %+v", o)
	}

	resolveCipher(t, e, cipherID, []int{0})

	got := cipherEncodedOn(e, creature)
	if len(got) != 1 || got[0] != cipherID {
		t.Fatalf("creature %d encoded cards = %v, want [%d]", creature, got, cipherID)
	}
	co := e.G.Obj(cipherID)
	if co == nil || co.Zone != state.ZExile {
		t.Fatalf("encoded card zone = %v, want exile", zoneOf(co))
	}
	replayCheck(t, e, cfg)
}

// TestCipherCopyDeclineCastsNothing: the copy cast is a MAY -- a declined
// offer puts nothing on the stack. The trigger still fired, and its handler
// still ran, which the answered offer proves.
func TestCipherCopyDeclineCastsNothing(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, cipherID, creatures := cipherBoard(t, reg, card(t, cipherProbeSrc), cipherBeanSrc)
	creature := creatures[0]

	resolveCipher(t, e, cipherID, []int{0})
	if got := cipherEncodedOn(e, creature); len(got) != 1 {
		t.Fatalf("precondition failed: encoded cards = %v, want one", got)
	}

	base := len(e.L.Events)
	dealCipherCombatDamage(t, e, creature, 1, 2)
	if !drainCipher(t, e, 40, nil, nil) { // decline the copy
		t.Fatalf("combat damage by the encoded creature posed no copy-cast offer")
	}

	if n := countPutOnStackFor(e, base, "Test Cipher Probe"); n != 0 {
		t.Fatalf("declined copy cast put %d copies on the stack, want 0", n)
	}
	if got := cipherEncodedOn(e, creature); len(got) != 1 {
		t.Fatalf("declined copy cast changed the encoded set: %v", got)
	}
	replayCheck(t, e, cfg)
}

// TestCipherEncodeDeclineLeavesCardInGraveyard exercises the optional
// decision with a legal creature present and an empty (decline) answer.
func TestCipherEncodeDeclineLeavesCardInGraveyard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	probe := card(t, cipherProbeSrc)
	if !cipherHasResolutionTail(probe) {
		t.Fatal("precondition: Cipher keyword did not expand")
	}
	e, cfg, cipherID, creatures := cipherBoard(t, reg, probe, cipherBeanSrc)
	if o := e.G.Obj(creatures[0]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: eligible creature missing")
	}
	resolveCipher(t, e, cipherID, []int{})
	if co := e.G.Obj(cipherID); co == nil || co.Zone != state.ZGraveyard {
		t.Fatalf("declined encode card zone = %v, want graveyard", zoneOf(co))
	}
	if got := cipherEncodedOn(e, creatures[0]); len(got) != 0 {
		t.Fatalf("declined encode still linked %v", got)
	}
	replayCheck(t, e, cfg)
}

// TestCipherNoCreatureDoesNotEncode: with no creature to encode onto, the
// encode clause has no legal host, so the card stays in the graveyard and no
// creature carries an association.
func TestCipherNoCreatureDoesNotEncode(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	probe := card(t, cipherProbeSrc)
	if !cipherHasResolutionTail(probe) {
		t.Fatal("precondition: Cipher keyword did not expand")
	}
	e, cfg, cipherID, _ := cipherBoard(t, reg, probe)
	if n := len(e.G.Zone(state.ZBattlefield, 0)); n != 0 {
		t.Fatalf("precondition: seat 0 controls %d creatures, want zero", n)
	}

	resolveCipher(t, e, cipherID, nil)
	if !cipherNote(e, cipherID, "cipher encode has no creature you control") {
		t.Fatal("Cipher's no-eligible-creature handler did not run")
	}
	if co := e.G.Obj(cipherID); co == nil || co.Zone != state.ZGraveyard {
		t.Fatalf("with no creature the card zone = %v, want graveyard", zoneOf(co))
	}
	for i := range e.G.Objs {
		if len(e.G.Objs[i].EncodedCards) != 0 {
			t.Fatalf("object %d encoded %v with no creature on the board", e.G.Objs[i].ID, e.G.Objs[i].EncodedCards)
		}
	}
	replayCheck(t, e, cfg)
}

// TestCipherOpponentCreatureIsNotAnEncodeHost keeps the eligible-creature
// predicate honest: a creature on the board controlled by an opponent must
// not become a fallback option when the caster controls none.
func TestCipherOpponentCreatureIsNotAnEncodeHost(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	probe := card(t, cipherProbeSrc)
	if !cipherHasResolutionTail(probe) {
		t.Fatal("precondition: Cipher keyword did not expand")
	}
	e, cfg, cipherID, creatures := cipherBoard(t, reg, probe, cipherBeanSrc)
	creature := creatures[0]
	e.emit(events.Event{Kind: events.ControlChange, Obj: creature, Player: 1})
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: opponent must control battlefield creature: %+v", o)
	}
	resolveCipher(t, e, cipherID, nil)
	if !cipherNote(e, cipherID, "cipher encode has no creature you control") {
		t.Fatal("Cipher's opponent-controlled-creature handler did not run")
	}
	if co := e.G.Obj(cipherID); co == nil || co.Zone != state.ZGraveyard {
		t.Fatalf("no legal creature: card zone = %v, want graveyard", zoneOf(co))
	}
	if got := cipherEncodedOn(e, creature); len(got) != 0 {
		t.Fatalf("opponent creature wrongly encoded: %v", got)
	}
	replayCheck(t, e, cfg)
}

// TestCipherEncodedCardLeavingExileClearsAssociation: once the encoded card
// leaves exile (cast, blinked, moved), the association is gone -- events.Move
// prunes it -- so a later combat hit by the same creature fires nothing.
func TestCipherEncodedCardLeavingExileClearsAssociation(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, cipherID, creatures := cipherBoard(t, reg, card(t, cipherProbeSrc), cipherBeanSrc)
	creature := creatures[0]

	resolveCipher(t, e, cipherID, []int{0})
	if got := cipherEncodedOn(e, creature); len(got) != 1 {
		t.Fatalf("precondition failed: encoded cards = %v, want one", got)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: cipherID, From: state.ZExile, To: state.ZHand})
	if got := cipherEncodedOn(e, creature); len(got) != 0 {
		t.Fatalf("encoded card left exile but the association survived: %v", got)
	}

	base := len(e.L.Events)
	dealCipherCombatDamage(t, e, creature, 1, 2)
	drainCipher(t, e, 40, nil, []int{0})
	if n := countPutOnStackFor(e, base, "Test Cipher Probe"); n != 0 {
		t.Fatalf("combat damage after the encoded card left exile still cast %d copies", n)
	}
	replayCheck(t, e, cfg)
}

// TestCipherEncodedCardLeavesBattlefieldClearsAssociation: when the creature
// leaves the battlefield, its encoded links are battlefield-stint state and
// are dropped.
func TestCipherEncodedCardLeavesBattlefieldClearsAssociation(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, cipherID, creatures := cipherBoard(t, reg, card(t, cipherProbeSrc), cipherBeanSrc)
	creature := creatures[0]

	resolveCipher(t, e, cipherID, []int{0})
	if got := cipherEncodedOn(e, creature); len(got) != 1 {
		t.Fatalf("precondition failed: encoded cards = %v, want one", got)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: creature, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := cipherEncodedOn(e, creature); len(got) != 0 {
		t.Fatalf("creature left the battlefield but kept encoded cards: %v", got)
	}
	if co := e.G.Obj(cipherID); co == nil || co.Zone != state.ZExile {
		t.Fatalf("encoded card zone = %v, want exile", zoneOf(co))
	}
	replayCheck(t, e, cfg)
}
