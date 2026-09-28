package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kw:Offspring (CR 702.175) pinned end to end on the REAL corpus carriers:
//
//   - the PRINTED form on Agate Instigator (K:Offspring:1 R, the Family
//     Matters deck's carrier): an optional ADDITIONAL cost offered beside the
//     plain cast, recorded as FlagOffspringPaid, and the ETB trigger mints a
//     1/1 token copy when it was paid;
//   - the GRANTED form on Zinnia, Valley's Voice's exact static
//     (`Affected$ Creature.wasCastByYou | AffectedZone$ Stack |
//     AddKeyword$ Offspring:2`): a plain creature spell cast while Zinnia is
//     on the battlefield is offered the granted {2} offspring cost and mints
//     the 1/1 copy. The granted form is the ticket's hard requirement —
//     a printed-only test does not close it — and it exercises THREE dead
//     layers at once: the wasCastByYou predicate at the pre-push offer
//     window, the layer-6 granted keyword reaching the cast offer, and the
//     granted keyword's synthesised ETB trigger.
//
// The decks are built from compiled corpus cards only, so no Forge script
// text is committed. Agate Instigator and Zinnia are in no legacy golden
// deck, so no chain head depends on this work.

// offspringEngine deals seat 0 a 40-card deck led by headline, then Forests
// and Grizzly Bears; the opponent's deck is all Forests. Seat 0 is advanced
// to Main1.
func offspringEngine(t *testing.T, headline *cards.Card) (*Engine, Config, *cards.Registry) {
	t.Helper()
	reg := searchTestRegistry(t)
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	forest := searchCorpusCard(t, reg, "Forest")
	deck := []*cards.Card{headline}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: 4242, Names: []string{"offspring", "opp"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg, reg
}

// offspringOption returns the "cast" option casting id in the given mode, or
// fails when it is absent.
func offspringOption(t *testing.T, e *Engine, id state.ObjID, mode string) decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == mode {
			return o
		}
	}
	t.Fatalf("no %q cast option for %d: %+v", mode, id, d.Options)
	return decision.Option{}
}

// offspringCastInfo reports whether the log carries a pay-time CastInfo for
// obj whose flag list names offspringpaid.
func offspringCastInfo(e *Engine, obj state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind != events.CastInfo || ev.Obj != obj {
			continue
		}
		for _, part := range splitCSV(ev.Counter) {
			if part == "offspringpaid" {
				return true
			}
		}
	}
	return false
}

// offspringTokens returns every token copy of src on seat 0's battlefield:
// same printed name, IsToken, still on the battlefield and not src itself.
func offspringTokens(t *testing.T, e *Engine, src state.ObjID) []state.ObjID {
	t.Helper()
	o := e.G.Obj(src)
	if o == nil || o.Face() == nil {
		t.Fatalf("source %d does not resolve", src)
	}
	name := o.Face().Name
	var out []state.ObjID
	for _, cid := range e.G.Zone(state.ZBattlefield, 0) {
		if cid == src {
			continue
		}
		c := e.G.Obj(cid)
		if c != nil && c.Face() != nil && c.IsToken && c.Face().Name == name {
			out = append(out, cid)
		}
	}
	return out
}

// TestOffspringPrintedPayMintsOneOneTokenCopy pins Agate Instigator's printed
// K:Offspring:1 R: the additional cost is offered, paying it stamps
// FlagOffspringPaid, and the ETB trigger mints exactly one 1/1 token copy.
func TestOffspringPrintedPayMintsOneOneTokenCopy(t *testing.T) {
	t.Parallel()
	agate := searchCorpusCard(t, searchTestRegistry(t), "Agate Instigator")
	e, cfg, _ := offspringEngine(t, agate)
	hero := searchMoveByName(t, e, "Agate Instigator", state.ZHand)
	// The printed form must actually carry the keyword; otherwise every
	// assertion below is vacuous.
	if !e.HasKeyword(hero, "Offspring") {
		t.Fatal("precondition: Agate Instigator must have Offspring")
	}
	// Base {1}{R} plus the {1}{R} offspring additional cost.
	addMana(t, e, 0, "RRCCCC")

	// Both the plain and offspring options are offered (the additional cost
	// is optional, so the plain cast must remain).
	offspringOption(t, e, hero, "")
	opt := offspringOption(t, e, hero, "offspring")
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)

	if !offspringCastInfo(e, hero) {
		t.Fatal("no pay-time CastInfo carrying offspringpaid")
	}
	if !e.G.Obj(hero).OffspringPaid {
		t.Fatal("Object.OffspringPaid not folded from the CastInfo")
	}
	toks := offspringTokens(t, e, hero)
	if len(toks) != 1 {
		t.Fatalf("token copies = %d, want 1", len(toks))
	}
	if p, tg := e.Power(toks[0]), e.Toughness(toks[0]); p != 1 || tg != 1 {
		t.Fatalf("offspring copy is %d/%d, want 1/1", p, tg)
	}
	if e.G.Obj(hero).Zone != state.ZBattlefield {
		t.Fatalf("the cast creature left the battlefield: zone %v", e.G.Obj(hero).Zone)
	}
	replayCheck(t, e, cfg)
}

// TestOffspringPrintedPlainCastMintsNothing pins the optionality: the plain
// cast (no offspring option taken) leaves OffspringPaid false and mints no
// token copy.
func TestOffspringPrintedPlainCastMintsNothing(t *testing.T) {
	t.Parallel()
	agate := searchCorpusCard(t, searchTestRegistry(t), "Agate Instigator")
	e, cfg, _ := offspringEngine(t, agate)
	hero := searchMoveByName(t, e, "Agate Instigator", state.ZHand)
	addMana(t, e, 0, "RRCCCC")

	plain := offspringOption(t, e, hero, "")
	submitChoices(t, e, plain.Index)
	passUntilStackEmpty(t, e, 20)

	if e.G.Obj(hero).OffspringPaid {
		t.Fatal("plain cast set OffspringPaid")
	}
	if toks := offspringTokens(t, e, hero); len(toks) != 0 {
		t.Fatalf("plain cast minted %d token copies, want 0", len(toks))
	}
	replayCheck(t, e, cfg)
}

// TestOffspringGrantedByZinniaReachesTheCast is the ticket's hard
// requirement: Zinnia, Valley's Voice's exact static
// (`Affected$ Creature.wasCastByYou | AffectedZone$ Stack | AddKeyword$
// Offspring:2`) must make a plain creature spell cast while she is on the
// battlefield offer the GRANTED {2} offspring cost, and paying it must mint
// the 1/1 token copy. It fails at three earlier layers if any is dead: the
// granted keyword list is empty (wasCastByYou matches nobody), the offspring
// option is never offered (the layer-6 grant does not reach the cast), or no
// token appears (the synthesised ETB trigger).
func TestOffspringGrantedByZinniaReachesTheCast(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	zinnia := searchCorpusCard(t, reg, "Zinnia, Valley's Voice")
	e, cfg, _ := offspringEngine(t, zinnia)
	// Zinnia is CAST from hand, not placed eventlessly: the whole sequence
	// then rides the log, so replayCheck below proves a log-only replay
	// rebuilds the grant and mints the same copy. Her {U}{R}{W} plus the
	// later {G} base and {2} granted offspring cost are floated up front.
	zinniaID := searchMoveByName(t, e, "Zinnia, Valley's Voice", state.ZHand)
	addMana(t, e, 0, "URWCC")
	submitChoices(t, e, offspringOption(t, e, zinniaID, "").Index)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(zinniaID); z == nil || z.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Zinnia must be on the battlefield after her cast, got %v", z)
	}
	// {1}{G} base plus the granted {2}.
	addMana(t, e, 0, "GCCCCC")

	hero := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	// PRECONDITION, asserted at the layer the fix lives in: the granted
	// Offspring:2 reaches the stack-zone derived keyword list. Without this
	// the offer assertion below could pass for the wrong reason, and with it
	// false the test names the dead layer precisely.
	kw := e.derivedWith(hero, state.ZStack).Keywords
	found := false
	for _, k := range kw {
		if cards.KeywordHead(k) == "Offspring" && k == "Offspring:2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Zinnia's granted Offspring:2 does not reach the cast: derived keywords %v", kw)
	}
	if e.G.Obj(hero).Face().HasKeyword("Offspring") {
		t.Fatal("Grizzly Bears prints Offspring; the granted path is not under test")
	}

	plain := offspringOption(t, e, hero, "")
	opt := offspringOption(t, e, hero, "offspring")
	if opt.Index == plain.Index {
		t.Fatal("the offspring option aliases the plain cast")
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)

	if !offspringCastInfo(e, hero) {
		t.Fatal("granted offspring cast carries no offspringpaid CastInfo")
	}
	toks := offspringTokens(t, e, hero)
	if len(toks) != 1 {
		t.Fatalf("granted offspring minted %d token copies, want 1", len(toks))
	}
	if p, tg := e.Power(toks[0]), e.Toughness(toks[0]); p != 1 || tg != 1 {
		t.Fatalf("granted offspring copy is %d/%d, want 1/1", p, tg)
	}
	replayCheck(t, e, cfg)
}

// TestOffspringGrantedPlainCastMintsNothing pins the granted form's
// optionality: without taking the offspring option the granted keyword stamps
// nothing and mints nothing.
func TestOffspringGrantedPlainCastMintsNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	zinnia := searchCorpusCard(t, reg, "Zinnia, Valley's Voice")
	e, cfg, _ := offspringEngine(t, zinnia)
	zinniaID := searchMoveByName(t, e, "Zinnia, Valley's Voice", state.ZHand)
	addMana(t, e, 0, "URWCC")
	submitChoices(t, e, offspringOption(t, e, zinniaID, "").Index)
	passUntilStackEmpty(t, e, 20)
	addMana(t, e, 0, "GCCCCC")

	hero := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	plain := offspringOption(t, e, hero, "")
	submitChoices(t, e, plain.Index)
	passUntilStackEmpty(t, e, 20)

	if e.G.Obj(hero).OffspringPaid {
		t.Fatal("plain cast under the grant set OffspringPaid")
	}
	if toks := offspringTokens(t, e, hero); len(toks) != 0 {
		t.Fatalf("plain cast under the grant minted %d token copies, want 0", len(toks))
	}
	replayCheck(t, e, cfg)
}
