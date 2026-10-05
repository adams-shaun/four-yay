package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// kw:Bargain (CR 702.166) pinned on the REAL corpus carriers:
//
//   - Candy Grapple (K:Bargain, SVar:X:Count$Bargained.5.3): the plain cast
//     pumps -3/-3 and the bargained cast -5/-5, so the Count$Bargained head
//     is proved in both branches and the sacrifice is proved to settle.
//   - Hamlet Glutton (the Spell.Bargain ReduceCost static): the bargained
//     option is offered {2} cheaper AND the reduction is charged, so the
//     offer and the charge agree on the discount.
//   - the eligibility filter: an artifact and an enchantment are offered, a
//     bare creature is not.
//   - the bare Condition$ Bargain gate on Torch the Tower's DBScry.
//
// Bargain is an optional additional cost offered beside the plain cast, so
// declining is the plain cast and paying sacrifices one permanent. No legacy
// golden deck contains a Bargain carrier, so no chain head depends on this
// work.

func bargainEngine(t *testing.T, headline *cards.Card) (*Engine, Config, *cards.Registry) {
	t.Helper()
	reg := searchTestRegistry(t)
	bears := searchCorpusCard(t, reg, "Grizzly Bears")
	swamp := searchCorpusCard(t, reg, "Swamp")
	// Seat 0 carries one Sol Ring (an artifact) and one Pacifism (an
	// enchantment) so the eligibility and sacrifice tests can seed them; the
	// headline is the spell under test.
	deck := []*cards.Card{headline, searchCorpusCard(t, reg, "Sol Ring"), searchCorpusCard(t, reg, "Propaganda")}
	for i := 0; i < 8; i++ {
		deck = append(deck, swamp)
	}
	for len(deck) < 40 {
		deck = append(deck, bears)
	}
	// Seat 1 carries one Colossal Dreadmaw (a 6/6 that survives both pumps)
	// plus Siege Mastodons.
	opp := []*cards.Card{searchCorpusCard(t, reg, "Colossal Dreadmaw")}
	for len(opp) < 40 {
		opp = append(opp, searchCorpusCard(t, reg, "Siege Mastodon"))
	}
	cfg := seatZeroStart(Config{Seed: 90210, Names: []string{"bargainer", "opp"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg, reg
}

// bargainCastMode returns the "cast" option casting id in the given mode, or
// fails when it is absent.
func bargainCastMode(t *testing.T, e *Engine, id state.ObjID, mode string) decision.Option {
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

func bargainHasCastMode(e *Engine, id state.ObjID, mode string) bool {
	d := e.Pending()
	if d == nil {
		return false
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == mode {
			return true
		}
	}
	return false
}

// bargainAnswer submits the bargain sacrifice of want from the pending ask,
// asserting the ask is a genuine bargain election and that want is offered.
func bargainAnswer(t *testing.T, e *Engine, want state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "bargain" {
		t.Fatalf("expected a bargain sacrifice ask, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == want {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("bargain sacrifice %d not offered: %+v", want, d.Options)
}

// TestBargainCandyGrapplePaysForFiveMinusFive proves both branches of the
// Count$Bargained head and that the sacrifice settles: the plain cast gives
// -3/-3, the bargained cast sacrifices the chosen artifact and gives -5/-5.
func TestBargainCandyGrapplePaysForFiveMinusFive(t *testing.T) {
	t.Parallel()
	for _, pay := range []bool{false, true} {
		name := "decline"
		if pay {
			name = "pay"
		}
		t.Run(name, func(t *testing.T) {
			spell := searchCorpusCard(t, searchTestRegistry(t), "Candy Grapple")
			e, cfg, reg := bargainEngine(t, spell)
			target := moveSeededCard(t, e, 1, searchCorpusCard(t, reg, "Colossal Dreadmaw"), state.ZBattlefield)
			sol := seedBattlefield(t, e, reg, "Sol Ring")
			if e.G.Obj(target).Zone != state.ZBattlefield || e.G.Obj(sol).Zone != state.ZBattlefield {
				t.Fatal("precondition: target and artifact must be on the battlefield")
			}
			// The target must survive both pumps.
			if e.Toughness(target) < 6 {
				t.Fatalf("precondition: target toughness %d must survive -5", e.Toughness(target))
			}
			if e.G.Obj(sol).Face() == nil || !e.G.Obj(sol).Face().IsArtifact() {
				t.Fatal("precondition: Sol Ring must be an artifact")
			}
			candy := searchMoveByName(t, e, "Candy Grapple", state.ZHand)
			if e.G.Obj(candy).Zone != state.ZHand || !e.HasKeyword(candy, "Bargain") {
				t.Fatal("precondition: Candy Grapple in hand with Bargain")
			}
			addMana(t, e, 0, "BB")
			if !bargainHasCastMode(e, candy, "bargained") {
				t.Fatalf("bargained cast option missing: %+v", e.Pending().Options)
			}
			mode := ""
			if pay {
				mode = "bargained"
			}
			submitChoices(t, e, bargainCastMode(t, e, candy, mode).Index)
			if pay {
				bargainAnswer(t, e, sol)
			}
			d := e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("missing target ask: %+v", d)
			}
			found := false
			for _, o := range d.Options {
				if o.Obj == target {
					submitChoices(t, e, o.Index)
					found = true
					break
				}
			}
			if !found {
				t.Fatal("creature target not offered")
			}
			passUntilStackEmpty(t, e, 60)
			wantTough := int32(3)
			if pay {
				wantTough = 1
				if e.G.Obj(sol).Zone != state.ZGraveyard {
					t.Fatalf("bargain sacrifice not settled: Sol Ring zone %v", e.G.Obj(sol).Zone)
				}
			} else if e.G.Obj(sol).Zone != state.ZBattlefield {
				t.Fatal("declined bargain sacrificed the artifact")
			}
			o := e.G.Obj(target)
			if o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("target left the battlefield: %+v", o)
			}
			if got := e.Toughness(target); got != wantTough {
				t.Fatalf("target toughness = %d, want %d", got, wantTough)
			}
			if got := e.Power(target); got != wantTough {
				t.Fatalf("target power = %d, want %d", got, wantTough)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestBargainEligibilityOffersArtifactEnchantmentTokenOnly proves the offer's
// candidate set: with an artifact, an enchantment and a bare creature on the
// battlefield, the ask offers the artifact and enchantment but not the
// creature.
func TestBargainEligibilityOffersArtifactEnchantmentTokenOnly(t *testing.T) {
	t.Parallel()
	spell := searchCorpusCard(t, searchTestRegistry(t), "Candy Grapple")
	e, _, reg := bargainEngine(t, spell)
	sol := seedBattlefield(t, e, reg, "Sol Ring")
	enchant := seedBattlefield(t, e, reg, "Propaganda")
	bears := seedBattlefield(t, e, reg, "Grizzly Bears")
	o := e.G.Obj(enchant)
	if o == nil || o.Face() == nil || !o.Face().IsEnchantment() {
		t.Fatalf("precondition: %d must be an enchantment", enchant)
	}
	if o := e.G.Obj(bears); o == nil || o.Face() == nil || o.Face().IsEnchantment() || o.Face().IsArtifact() {
		t.Fatal("precondition: Grizzly Bears must be neither artifact nor enchantment")
	}
	candy := searchMoveByName(t, e, "Candy Grapple", state.ZHand)
	if e.G.Obj(candy).Zone != state.ZHand {
		t.Fatal("precondition: Candy Grapple must be in hand")
	}
	addMana(t, e, 0, "BB")
	submitChoices(t, e, bargainCastMode(t, e, candy, "bargained").Index)
	d := e.Pending()
	if d == nil || len(d.Options) == 0 || d.Options[0].Kind != "bargain" {
		t.Fatalf("missing bargain ask: %+v", d)
	}
	seen := map[state.ObjID]bool{}
	for _, opt := range d.Options {
		seen[opt.Obj] = true
	}
	if !seen[sol] {
		t.Fatal("artifact not offered for bargain")
	}
	if !seen[enchant] {
		t.Fatal("enchantment not offered for bargain")
	}
	if seen[bears] {
		t.Fatal("bare creature offered for bargain")
	}
}

// TestBargainHamletGluttonReducesCostByTwo proves the Spell.Bargain cost
// static: the bargained option is offered with {2} less mana than the plain
// cast, and the bargained cast then resolves. The plain option still requires
// the full cost.
func TestBargainHamletGluttonReducesCostByTwo(t *testing.T) {
	t.Parallel()
	spell := searchCorpusCard(t, searchTestRegistry(t), "Hamlet Glutton")
	e, cfg, reg := bargainEngine(t, spell)
	sol := seedBattlefield(t, e, reg, "Sol Ring")
	glutton := searchMoveByName(t, e, "Hamlet Glutton", state.ZHand)
	if e.G.Obj(glutton).Zone != state.ZHand {
		t.Fatal("precondition: Hamlet Glutton must be in hand")
	}
	if !e.HasKeyword(glutton, "Bargain") {
		t.Fatal("precondition: Hamlet Glutton must carry Bargain")
	}
	// Exactly the reduced cost {3}{G}{G}: 2 green + 3 generic, the printed
	// {5}{G}{G} minus 2. The pool is exactly the reduced cost, so the full
	// cost cannot be paid from it.
	addMana(t, e, 0, "GGCCC")
	if !bargainHasCastMode(e, glutton, "bargained") {
		t.Fatalf("bargained option missing at the reduced cost: %+v", e.Pending().Options)
	}
	// The plain cast needs the full {5}{G}{G} = 7 mana; the pool is exactly
	// the reduced 5, so it must NOT be offerable, proving the discount is
	// real and not a general cost reduction.
	if bargainHasCastMode(e, glutton, "") {
		t.Fatal("plain cast offered on only the reduced pool")
	}
	submitChoices(t, e, bargainCastMode(t, e, glutton, "bargained").Index)
	bargainAnswer(t, e, sol)
	passUntilStackEmpty(t, e, 60)
	if e.G.Obj(glutton).Zone != state.ZBattlefield {
		t.Fatalf("Hamlet Glutton did not resolve onto the battlefield: zone %v", e.G.Obj(glutton).Zone)
	}
	if e.G.Obj(sol).Zone != state.ZGraveyard {
		t.Fatal("bargain sacrifice not settled")
	}
	replayCheck(t, e, cfg)
}

// TestBargainConditionGateScriesOnlyWhenBargained proves the bare
// Condition$ Bargain gate on Torch the Tower's DBScry: scrying happens only
// when the cast was bargained, and the Count$Bargain.3.2 damage head reads 3
// in that branch.
func TestBargainConditionGateScriesOnlyWhenBargained(t *testing.T) {
	t.Parallel()
	for _, pay := range []bool{false, true} {
		name := "decline"
		if pay {
			name = "pay"
		}
		t.Run(name, func(t *testing.T) {
			spell := searchCorpusCard(t, searchTestRegistry(t), "Torch the Tower")
			e, cfg, reg := bargainEngine(t, spell)
			target := moveSeededCard(t, e, 1, searchCorpusCard(t, reg, "Colossal Dreadmaw"), state.ZBattlefield)
			sol := seedBattlefield(t, e, reg, "Sol Ring")
			if e.G.Obj(target).Zone != state.ZBattlefield || e.G.Obj(sol).Zone != state.ZBattlefield {
				t.Fatal("precondition: target and artifact must be on the battlefield")
			}
			if e.Toughness(target) < 4 {
				t.Fatalf("precondition: target must survive 3 damage, toughness %d", e.Toughness(target))
			}
			torch := searchMoveByName(t, e, "Torch the Tower", state.ZHand)
			if e.G.Obj(torch).Zone != state.ZHand || !e.HasKeyword(torch, "Bargain") {
				t.Fatal("precondition: Torch the Tower in hand with Bargain")
			}
			addMana(t, e, 0, "R")
			mode := ""
			if pay {
				mode = "bargained"
			}
			submitChoices(t, e, bargainCastMode(t, e, torch, mode).Index)
			if pay {
				bargainAnswer(t, e, sol)
			}
			d := e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("missing target ask: %+v", d)
			}
			for _, o := range d.Options {
				if o.Obj == target {
					submitChoices(t, e, o.Index)
					break
				}
			}
			// Pass priority to resolve the spell, answering the scry ask when
			// the bargained branch poses it. Track that the ask really
			// appeared, so the unbargained branch's "no scry" assertion is
			// not vacuous.
			sawScry := false
			for i := 0; i < 60 && !e.G.Over && len(e.G.Stack) > 0; i++ {
				d = e.Pending()
				if d == nil {
					t.Fatal("no decision draining the stack")
				}
				switch d.Kind {
				case decision.KArrange:
					sawScry = true
					idx := make([]int, len(d.Options))
					for j := range d.Options {
						idx[j] = d.Options[j].Index
					}
					submitChoices(t, e, idx...)
				case decision.KPriority:
					passed := false
					for _, o := range d.Options {
						if o.Kind == "pass" {
							submitChoices(t, e, o.Index)
							passed = true
							break
						}
					}
					if !passed {
						t.Fatalf("priority ask with no pass option: %+v", d)
					}
				default:
					t.Fatalf("unexpected mid-resolution ask %v: %+v", d.Kind, d)
				}
			}
			if sawScry != pay {
				t.Fatalf("scry posed = %v, want %v (Condition$ Bargain gate)", sawScry, pay)
			}
			wantDamage := int32(2)
			if pay {
				wantDamage = 3
			}
			if got := e.G.Obj(target).Damage; got != wantDamage {
				t.Fatalf("damage = %d, want %d", got, wantDamage)
			}
			replayCheck(t, e, cfg)
		})
	}
}
