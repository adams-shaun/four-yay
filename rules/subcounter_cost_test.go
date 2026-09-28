package rules

// cost:SubCounter-target — the SubCounter cost's removal-target filter (the
// third field) and the "Any" counter kind.
//
// The cost token's extended spellings SubCounter<N|X/Kind/Target[/desc]> name
// WHOSE counters the payment removes (Ghave, Guru of Spores' "remove a
// +1/+1 counter from a creature you control"; Moxite Refinery's
// "remove X counters of any kind from an artifact or creature you control").
// The parser reads the third field into CostPart.Target; the cast flow asks
// the payer which matching permanent to remove from (rules/cast.go's
// subCounterAsk) and the settle removes from that object, not the source.
// Source-anchored parts (the two-field tokens, and Forge's CARDNAME/
// NICKNAME third-field spellings) keep the pre-existing source settlement,
// which is what every mana-ability carrier (the 65 AB$ Mana lines) needs.
//
// The end-to-end pins run on the REAL corpus cards: Moxite Refinery (the
// deck-gap census's X/Any carrier) and Ghave, Guru of Spores / Bolrac-Clan
// Crusher (the digit form). None of the 14 carrier files is in any repo
// deck, so the chain heads are safe by construction.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestParseCostSubCounterTargetField(t *testing.T) {
	t.Parallel()
	c := ParseCost("SubCounter<1/P1P1/Creature.YouCtrl/a creature you control>")
	if len(c.SubCounter) != 1 {
		t.Fatalf("digit form parsed %d SubCounter parts, want 1", len(c.SubCounter))
	}
	p := c.SubCounter[0]
	if p.N != 1 || p.Spec != "P1P1" {
		t.Fatalf("digit form count/kind = %d/%q, want 1/P1P1", p.N, p.Spec)
	}
	if p.Target != "Creature.YouCtrl" {
		t.Fatalf("digit form target = %q, want Creature.YouCtrl", p.Target)
	}
	if p.Desc != "a creature you control" {
		t.Fatalf("digit form desc = %q, want the prose", p.Desc)
	}

	x := ParseCost("SubCounter<X/Any/Artifact.YouCtrl;Creature.YouCtrl/an artifact or creature you control>")
	if len(x.SubCounter) != 1 {
		t.Fatalf("X form parsed %d SubCounter parts, want 1", len(x.SubCounter))
	}
	q := x.SubCounter[0]
	if !q.Announced || q.Spec != "Any" {
		t.Fatalf("X form announced/kind = %v/%q, want true/Any", q.Announced, q.Spec)
	}
	if q.Target != "Artifact.YouCtrl,Creature.YouCtrl" {
		t.Fatalf("X form target = %q, want the comma-folded alternation", q.Target)
	}

	// The two-field token stays source-anchored and unchanged.
	plain := ParseCost("SubCounter<2/LOYALTY>").SubCounter[0]
	if plain.Target != "" || plain.Spec != "LOYALTY" || plain.Announced {
		t.Fatalf("two-field form = %+v, want source-anchored LOYALTY", plain)
	}
	// Forge's payCostFromSource third-field spellings anchor the source.
	for _, tok := range []string{"SubCounter<1/DREAM/NICKNAME>", "SubCounter<1/OIL/CARDNAME>"} {
		got := ParseCost(tok).SubCounter[0]
		if !subCounterTargetsSource(got.Target) {
			t.Fatalf("%s target = %q, want the source spelling", tok, got.Target)
		}
	}
	// A space-bearing third field is a description, not a filter: the
	// removal stays source-anchored (the mana-ability carriers' shape).
	prose := ParseCost("SubCounter<1/P1P1/a creature you control>").SubCounter[0]
	if prose.Target != "" || prose.Desc != "a creature you control" {
		t.Fatalf("prose third field = target %q desc %q, want empty target + desc", prose.Target, prose.Desc)
	}
}

// subCounterConfig seeds a 2-seat game whose seat 0 carries the named corpus
// cards plus Mountains, drives to the first Main 1 (seatZeroStart pins the
// active seat to 0) and returns the engine, the Config its log replays
// against and the active seat.
func subCounterConfig(t *testing.T, seed uint64, names ...string) (*Engine, Config, state.PlayerID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	deck := make([]*cards.Card, 0, len(names))
	for _, n := range names {
		deck = append(deck, mustCorpusCard(t, reg, n))
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append(append([]*cards.Card{}, deck...), mountainDeck(t, 40-len(deck))...),
			mountainDeck(t, 40),
		}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	if e.G.Active != 0 {
		t.Fatalf("precondition: seatZeroStart should make seat 0 active, got %d", e.G.Active)
	}
	return e, cfg, 0
}

// bridgeToHand moves seat 0's copy of the named corpus card into its hand
// (the corpusCardConfig idiom) and returns the id.
func bridgeToHand(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	id := findByName(e, name, 0)
	if id == 0 {
		t.Fatalf("corpus %q not found in seat 0's zones", name)
	}
	if o := e.G.Obj(id); o.Zone != state.ZHand {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZHand})
		e.pending = nil
		e.priorityRound()
	}
	return id
}

// castBallista casts seat 0's Walking Ballista at the given X and asserts the
// precondition every later assertion rides on: the Ballista on the
// battlefield carrying exactly X +1/+1 counters (the K:etbCounter entry
// grant).
func castBallista(t *testing.T, e *Engine, p state.PlayerID, x int) state.ObjID {
	t.Helper()
	id := bridgeToHand(t, e, "Walking Ballista")
	cost := ""
	for i := 0; i < x; i++ {
		cost += "G"
	}
	addMana(t, e, p, cost+cost) // {X}{X}: X generic pips twice
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("Ballista X decision: %+v", d)
	}
	if len(d.Options) < x+1 {
		t.Fatalf("Ballista X bound too small: %d options, want at least %d", len(d.Options), x+1)
	}
	submitChoices(t, e, d.Options[x].Index)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: Ballista zone=%v, want battlefield", o)
	}
	if got := o.Counter("P1P1"); got != int32(x) {
		t.Fatalf("precondition failed: Ballista carries %d +1/+1 counters, want %d", got, x)
	}
	return id
}

// TestMoxiteRefineryAnyKindRemovesFromTheChosenArtifact is the brief's
// carrier pin end to end: the X/Any cost with an artifact-creature target
// announces the REAL X bound (the Ballista's 2 counters), removes 2 counters
// from the CHOSEN artifact (not the silent no-op on the source), and the
// Charm's X-funded mode puts X charge counters on the target artifact.
func TestMoxiteRefineryAnyKindRemovesFromTheChosenArtifact(t *testing.T) {
	t.Parallel()
	e, cfg, p := subCounterConfig(t, 47, "Moxite Refinery", "Walking Ballista")
	moxite := bridgeToHand(t, e, "Moxite Refinery")
	placeOnBattlefield(t, e, moxite)
	if got := e.G.Obj(moxite).Counter("CHARGE"); got != 0 {
		t.Fatalf("precondition failed: Moxite enters with %d charge counters, want 0", got)
	}
	ballista := castBallista(t, e, p, 3)

	addMana(t, e, p, "GG") // Moxite's {2}
	submitChoices(t, e, abilityOption(t, e, moxite, 0).Index)

	// CR 601.2b: the announced X is bounded by the largest candidate's
	// counter count -- 2, the Ballista's -- not by the source's (0), so
	// three values are offered. Pre-fix the bound read the source's
	// o.Counter("Any") = 0 and only X=0 was offered.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("X decision for the Moxite activation: %+v", d)
	}
	if len(d.Options) != 4 {
		t.Fatalf("X bound offered %d values, want 4 (X=0..3, bounded by the Ballista's 3 counters): %+v",
			len(d.Options), d.Options)
	}
	submitChoices(t, e, d.Options[2].Index) // X = 2

	// The removal's sole candidate is the Ballista (the only Artifact or
	// Creature you control with >= 2 counters of any kind), so no
	// subcounter ask is posed (strict supersets) -- the ability pushes.
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected priority after the activation settled, got %+v", d)
	}
	passOne(t, e)
	passOne(t, e)

	// Resolution: the Charm asks its mode; the Artifact mode then asks its
	// target. Remove the counters, put X charge counters on the artifact.
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the Charm mode ask at resolution, got %+v", d)
	}
	art := -1
	for _, o := range d.Options {
		if o.Label == "Put X charge counters on target artifact." {
			art = o.Index
		}
	}
	if art < 0 {
		t.Fatalf("artifact mode not offered: %+v", d.Options)
	}
	submitChoices(t, e, art)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "tgts" {
		t.Fatalf("expected the artifact-mode target ask, got %+v", d)
	}
	bIdx := -1
	for _, o := range d.Options {
		if o.Obj == ballista {
			bIdx = o.Index
		}
	}
	if bIdx < 0 {
		t.Fatalf("Ballista not offered as the mode's target: %+v", d.Options)
	}
	submitChoices(t, e, bIdx)
	passOne(t, e)
	passOne(t, e)

	// The counters came OFF the Ballista (the chosen artifact), not off the
	// Moxite source, and the mode put 2 charge counters on it. The Ballista
	// keeps its third +1/+1 counter -- it did not fall to the SBA, which is
	// what proves the removal took exactly 2.
	b := e.G.Obj(ballista)
	if got := b.Counter("P1P1"); got != 1 {
		t.Fatalf("Ballista carries %d +1/+1 counters after the removal, want 1 (3 minus the removed 2)", got)
	}
	if got := b.Counter("CHARGE"); got != 2 {
		t.Fatalf("Ballista carries %d charge counters after the mode, want 2", got)
	}
	if got := e.G.Obj(moxite).Counter("CHARGE"); got != 0 {
		t.Fatalf("Moxite source carries %d charge counters, want 0 (the removal must not land on the source)", got)
	}
	if !hasCounterChange(e, ballista, "P1P1", -2) {
		t.Fatal("no CounterChange -2 P1P1 on the Ballista: the removal never happened")
	}
	replayCheck(t, e, cfg)
}

// TestGhaveRemovesFromTheChosenCreature pins the digit form's target ask:
// with TWO eligible creatures (Ghave itself carries its 5 entry counters, the
// Ballista one) the engine poses the KChoose and removes from the CHOSEN
// creature. Pre-fix the cost read the source's counters, posed no ask, and
// silently removed from Ghave.
func TestGhaveRemovesFromTheChosenCreature(t *testing.T) {
	t.Parallel()
	e, cfg, p := subCounterConfig(t, 91, "Ghave, Guru of Spores", "Walking Ballista")
	ghave := bridgeToHand(t, e, "Ghave, Guru of Spores")
	placeOnBattlefield(t, e, ghave)
	if got := e.G.Obj(ghave).Counter("P1P1"); got != 5 {
		t.Fatalf("precondition failed: Ghave enters with %d +1/+1 counters, want 5", got)
	}
	ballista := bridgeToHand(t, e, "Walking Ballista")
	placeOnBattlefield(t, e, ballista)
	e.emit(events.Event{Kind: events.CounterChange, Obj: ballista, Counter: "P1P1", Amount: 1})
	e.pending = nil
	if got := e.G.Obj(ballista).Counter("P1P1"); got != 1 {
		t.Fatalf("precondition failed: Ballista carries %d +1/+1 counters, want 1", got)
	}

	addMana(t, e, p, "G") // Ghave's {1}
	submitChoices(t, e, abilityOption(t, e, ghave, 0).Index)

	// Two candidates -> a real KChoose over the matching creatures.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 {
		t.Fatalf("expected the counter-removal KChoose over two creatures, got %+v", d)
	}
	var opts []decision.Option
	for _, o := range d.Options {
		if o.Kind == "subcounter" {
			opts = append(opts, o)
		}
	}
	if len(opts) != 2 {
		t.Fatalf("counter-removal options = %d (%+v), want the two eligible creatures", len(opts), d.Options)
	}
	bIdx := -1
	for _, o := range opts {
		if o.Obj == ballista {
			bIdx = o.Index
		}
	}
	if bIdx < 0 {
		t.Fatalf("Ballista not offered as a removal candidate: %+v", d.Options)
	}
	submitChoices(t, e, bIdx)
	passOne(t, e)
	passOne(t, e)

	// The removal landed on the CHOSEN Ballista; the token was created.
	if got := e.G.Obj(ballista).Counter("P1P1"); got != 0 {
		t.Fatalf("Ballista carries %d +1/+1 counters, want 0 (the chosen removal target)", got)
	}
	if got := e.G.Obj(ghave).Counter("P1P1"); got != 5 {
		t.Fatalf("Ghave carries %d +1/+1 counters, want 5 (untouched)", got)
	}
	tokens := 0
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.IsToken && o.Zone == state.ZBattlefield && o.Owner == p {
			tokens++
		}
	}
	if tokens != 1 {
		t.Fatalf("saproling tokens on the battlefield = %d, want 1", tokens)
	}
	if !hasCounterChange(e, ballista, "P1P1", -1) {
		t.Fatal("no CounterChange -1 P1P1 on the Ballista: the removal never happened")
	}
	replayCheck(t, e, cfg)
}

// TestGorgonRemovalTargetIsNotTheSource pins the "offered at all" half: an
// actor whose SOURCE carries none of the counters the cost removes is
// offered because another creature you control does (pre-fix the offer gate
// read the source's counters and withheld the ability entirely). Korozda
// Gorgon is the carrier ({2}, no {T}, so no summoning-sickness lock on the
// entry turn).
func TestGorgonRemovalTargetIsNotTheSource(t *testing.T) {
	t.Parallel()
	e, cfg, _ := subCounterConfig(t, 173, "Korozda Gorgon", "Walking Ballista")
	gorgon := bridgeToHand(t, e, "Korozda Gorgon")
	placeOnBattlefield(t, e, gorgon)
	if got := e.G.Obj(gorgon).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition failed: Korozda Gorgon carries %d +1/+1 counters, want 0", got)
	}
	ballista := bridgeToHand(t, e, "Walking Ballista")
	placeOnBattlefield(t, e, ballista)
	e.emit(events.Event{Kind: events.CounterChange, Obj: ballista, Counter: "P1P1", Amount: 1})
	e.pending = nil
	e.priorityRound()
	if got := e.G.Obj(ballista).Counter("P1P1"); got != 1 {
		t.Fatalf("precondition failed: Ballista carries %d +1/+1 counters, want 1", got)
	}

	addMana(t, e, 0, "GG") // Gorgon's {2}
	submitChoices(t, e, abilityOption(t, e, gorgon, 0).Index)

	// The removal's sole candidate is the Ballista; the ability's target
	// (ValidTgts$ Creature) is chosen after the push.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the damage target ask, got %+v", d)
	}
	bIdx := -1
	for _, o := range d.Options {
		if o.Obj == ballista {
			bIdx = o.Index
		}
	}
	if bIdx < 0 {
		t.Fatalf("Ballista not offered as the damage target: %+v", d.Options)
	}
	submitChoices(t, e, bIdx)
	passOne(t, e)
	passOne(t, e)

	if !hasCounterChange(e, ballista, "P1P1", -1) {
		t.Fatal("no CounterChange -1 P1P1 on the Ballista: the removal never happened")
	}
	if got := e.G.Obj(gorgon).Counter("P1P1"); got != 0 {
		t.Fatalf("Korozda Gorgon carries %d +1/+1 counters, want 0 (never had any)", got)
	}
	replayCheck(t, e, cfg)
}

// passOne submits the "pass" option of the current pending priority
// decision (whoever holds it) once.
func passOne(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected a priority decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "pass" {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("priority decision with no pass option: %+v", d)
}

// hasCounterChange reports whether the log carries a CounterChange of kind
// against obj with the given counter kind and amount.
func hasCounterChange(e *Engine, obj state.ObjID, kind string, amount int32) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == obj && ev.Counter == kind && ev.Amount == amount {
			return true
		}
	}
	return false
}
