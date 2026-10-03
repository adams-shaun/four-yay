package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The unless-cost and sacrifice-choice behaviour tests on real corpus cards:
// a player-targeted Sacrifice asks its player (CR 701.21a), Amount$ sizes the
// ask, and every UnlessCost$ — not only Counter's and CopySpellAbility's —
// poses its pay decision, charges the cost through the engine's payment path,
// and applies the UnlessSwitched$ orientation.

// answerSacrifice submits, against the pending sacrifice KChoose, the option
// carrying the named permanent (or no option at all when name is "" — the
// Optional$ decline).
func answerSacrifice(t *testing.T, e *Engine, name string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pending decision = %+v, want the sacrifice KChoose", d)
	}
	if name == "" {
		submitChoices(t, e)
		return
	}
	for _, o := range d.Options {
		if o.Obj != 0 {
			if o2 := e.G.Obj(o.Obj); o2 != nil && o2.Face() != nil && o2.Face().Name == name {
				submitChoices(t, e, o.Index)
				return
			}
		}
	}
	t.Fatalf("no %q option in the sacrifice ask %+v", name, d.Options)
}

// answerUnlessPay submits the pending unless-pay KModes: index 0 pays,
// anything else declines.
func answerUnlessPay(t *testing.T, e *Engine, pay bool) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "unless_pay" {
		t.Fatalf("pending decision = %+v, want the unless-pay KModes", d)
	}
	idx := len(d.Options) - 1
	if pay {
		idx = 0
	}
	submitChoices(t, e, idx)
}

// TestManaUnlessCostThomil drives the corpus's activated-mana carrier. Mana
// abilities are off-stack, so this proves their UnlessCost$ still asks and
// charges rather than taking effects.Resolve's stack-resume shortcut.
func TestManaUnlessCostThomil(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 740)
	thomil := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Thomil, the Destroyer"))
	keeper := onBoard(t, e, 0, "Name:Keeper\nTypes:Creature\nPT:1/1\nOracle:x\n")
	victim := onBoard(t, e, 0, "Name:Victim\nTypes:Creature\nPT:1/1\nOracle:x\n")
	o := e.G.Obj(thomil)
	var mana *cards.SA
	for _, sa := range o.Face().ManaAbilities() {
		if sa.Params["UnlessCost"] != "" {
			mana = sa
			break
		}
	}
	if mana == nil {
		t.Fatal("Thomil mana ability with UnlessCost missing from corpus")
	}
	e.resolveManaAbility(0, thomil, mana, false)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "mana_unless" {
		t.Fatalf("pending = %+v, want off-stack mana unless-pay decision", d)
	}
	submitChoices(t, e, 0)
	// Sac<1/Creature> has two legal payers' permanents. The nested cost
	// decision must let the payer choose Victim rather than silently taking
	// Keeper, the first battlefield object in zone order.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("pending = %+v, want the Sac<1/Creature> payment choice", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == victim {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("Victim %d absent from payment options %+v", victim, d.Options)
	}
	submitChoices(t, e, pick)
	if e.G.Obj(victim).Zone != state.ZGraveyard {
		t.Fatalf("chosen sacrifice cost zone = %v, want graveyard", e.G.Obj(victim).Zone)
	}
	if e.G.Obj(keeper).Zone != state.ZBattlefield {
		t.Fatalf("unchosen first creature zone = %v, want battlefield", e.G.Obj(keeper).Zone)
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 3 {
		t.Fatalf("black mana = %d, want 3 after paid switched unless", got)
	}
}

// TestSacrificeHonoursAmountTwo drives Barter in Blood's real "Each player
// sacrifices two creatures": with three creatures apiece the ask is exactly
// two (Min == Max == 2 over the three eligible), and each seat buries exactly
// the two it named, keeping the third.
func TestSacrificeHonoursAmountTwo(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Barter in Blood"))
	kept0 := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	a := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Goblin Piledriver"))
	b := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Goblin Piledriver"))
	kept1 := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	c := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	d := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	addMana(t, e, 0, "BBBB")
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, handIDsByFace(e)["Barter in Blood"]))
	passUntilNonPriority(t, e, 8)

	// Seat 0's ask: exactly two of the three.
	ans0 := e.Pending()
	if ans0 == nil || ans0.Kind != decision.KChoose || ans0.Min != 2 || ans0.Max != 2 || len(ans0.Options) != 3 {
		t.Fatalf("seat 0 sacrifice ask = %+v, want Min==Max==2 over 3 creatures", ans0)
	}
	submitChoices(t, e, optionsNotKept(t, e, ans0, kept0)...)
	// Seat 1's ask: the same shape, its own battlefield.
	ans1 := e.Pending()
	if ans1 == nil || ans1.Kind != decision.KChoose || ans1.Player != 1 || ans1.Min != 2 || ans1.Max != 2 || len(ans1.Options) != 3 {
		t.Fatalf("seat 1 sacrifice ask = %+v, want Min==Max==2 over seat 1's 3 creatures", ans1)
	}
	submitChoices(t, e, optionsNotKept(t, e, ans1, kept1)...)
	if got := e.Pending(); got != nil && got.Kind == decision.KChoose {
		t.Fatalf("unexpected third sacrifice ask: %+v", got)
	}
	// Seat 0 buried exactly two of its three: whatever went to the graveyard,
	// the one it kept is still on the battlefield and a third was NOT buried.
	if e.G.Obj(kept0).Zone != state.ZBattlefield {
		t.Fatalf("seat 0 kept creature zone = %v, want battlefield (only 2 of 3 sacrificed)", e.G.Obj(kept0).Zone)
	}
	if e.G.Obj(kept1).Zone != state.ZBattlefield {
		t.Fatalf("seat 1 kept creature zone = %v, want battlefield", e.G.Obj(kept1).Zone)
	}
	buried := 0
	for _, id := range []state.ObjID{kept0, a, b} {
		if e.G.Obj(id).Zone == state.ZGraveyard {
			buried++
		}
	}
	if buried != 2 {
		t.Fatalf("seat 0 buried %d creatures, want exactly 2", buried)
	}
	buried = 0
	for _, id := range []state.ObjID{kept1, c, d} {
		if e.G.Obj(id).Zone == state.ZGraveyard {
			buried++
		}
	}
	if buried != 2 {
		t.Fatalf("seat 1 buried %d creatures, want exactly 2", buried)
	}
}

// TestGiantOpportunityStrictOptionalSacrifice drives Giant Opportunity's real
// Optional$ True | Amount$ 2 | StrictAmount$ True carrier. Its first decision
// is decline-or-sacrifice-two; accepting then permits only an EXACT two-Food
// KChoose, never the old illegal one-Food partial sacrifice.
func TestGiantOpportunityStrictOptionalSacrifice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	giant := mustCorpusCard(t, reg, "Giant Opportunity")
	food := card(t, "Name:Food\nTypes:Artifact Food\nOracle:x\n")
	e := handEngine(t, giant)
	first := onBoardCard(t, e, 0, food)
	second := onBoardCard(t, e, 0, food)
	third := onBoardCard(t, e, 0, food)
	addMana(t, e, 0, "GGG")
	e.askPriority(0)
	submitChoices(t, e, passToCast(t, e, handIDsByFace(e)["Giant Opportunity"]))

	d := passUntilNonPriority(t, e, 8)
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "sacrifice_optional" || d.Min != 1 || d.Max != 1 {
		t.Fatalf("first sacrifice decision = %+v, want strict optional KModes", d)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "mode" || d.Options[1].Kind != "mode" {
		t.Fatalf("strict optional options = %+v, want sacrifice/decline", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index) // elect to sacrifice exactly two

	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" || d.Min != 2 || d.Max != 2 || len(d.Options) != 3 {
		t.Fatalf("exact sacrifice decision = %+v, want 2..2 over three Foods", d)
	}
	// One Food is not a legal answer to StrictAmount$: Validate must reject it
	// while retaining the pending exact-batch decision.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err == nil {
		t.Fatal("one-Food answer to StrictAmount$ 2 was accepted")
	}
	if got := e.Pending(); got == nil || got.Min != 2 || got.Max != 2 {
		t.Fatalf("invalid partial answer changed pending decision to %+v", got)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	if e.G.Obj(first).Zone != state.ZGraveyard || e.G.Obj(second).Zone != state.ZGraveyard {
		t.Fatalf("chosen Foods zones = %v, %v; want graveyard", e.G.Obj(first).Zone, e.G.Obj(second).Zone)
	}
	if e.G.Obj(third).Zone != state.ZBattlefield {
		t.Fatalf("unchosen Food zone = %v, want battlefield", e.G.Obj(third).Zone)
	}
}

// counterTotal reads the total N of one counter kind on an object.
func counterTotal(o *state.Object, kind string) int32 {
	n := int32(0)
	for _, c := range o.Counters {
		if c.Kind == kind {
			n += c.N
		}
	}
	return n
}

// TestTapUnlessCostHallowedFountain drives the dual land's ETB replacement
// ("you may pay 2 life. If you don't, it enters tapped"): a DB$ Tap with
// UnlessCost$ PayLife<2> and UnlessPayer$ You, resolved through the shared
// gate inside a replacement body. Paying keeps it untapped; declining taps it.
func TestTapUnlessCostHallowedFountain(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	fountain := mustCorpusCard(t, reg, "Hallowed Fountain")
	for _, tc := range []struct {
		name     string
		pay      bool
		wantTaps bool
	}{{"pays", true, false}, {"declines", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			e := handEngine(t, fountain)
			e.askPriority(0)
			id := handIDsByFace(e)["Hallowed Fountain"]
			submitChoices(t, e, passToPlay(t, e, id))
			answerUnlessPay(t, e, tc.pay)
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("fountain zone = %v, want the battlefield", o)
			} else if o.Tapped != tc.wantTaps {
				t.Fatalf("fountain tapped = %t, want %t", o.Tapped, tc.wantTaps)
			}
			wantLife := int32(20)
			if tc.pay {
				wantLife = 18
			}
			if got := e.G.Players[0].Life; got != wantLife {
				t.Fatalf("life = %d, want %d", got, wantLife)
			}
		})
	}
}

// TestDrawUnlessCostWitchsMark drives the sorcery's real switched unless-cost
// ("You may discard a card. If you do, draw two cards."): a SP$ Draw with
// UnlessCost$ Discard<1/Card>, UnlessSwitched$ True and a chained DBToken that
// runs whether the cost was paid or not (Forge's UnlessResolveSubs default
// 'Always'). Paying discards a card through the engine's payment path and
// draws two; declining does neither — and the chained token sub runs either
// way.
func TestDrawUnlessCostWitchsMark(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	mark := mustCorpusCard(t, reg, "Witch's Mark")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	for _, tc := range []struct {
		name      string
		pay       bool
		wantDraws int
		wantDisc  bool
	}{{"pays", true, 2, true}, {"declines", false, 0, false}} {
		t.Run(tc.name, func(t *testing.T) {
			e := handEngine(t, mark, bear, bear)
			addMana(t, e, 0, "RR")
			e.askPriority(0)
			id := handIDsByFace(e)["Witch's Mark"]
			// The token sub's target ask (Min 0) may come before payment;
			// answer it with nothing. The cast flow's payment follows.
			submitChoices(t, e, passToCast(t, e, id))
			for i := 0; i < 6 && e.Pending() != nil; i++ {
				d := e.Pending()
				switch d.Kind {
				case decision.KModes:
					if d.ResumeKind != "unless_pay" {
						t.Fatalf("unexpected KModes ask: %+v", d)
					}
					answerUnlessPay(t, e, tc.pay)
				case decision.KChoose:
					submitChoices(t, e) // the optional token target: none
				case decision.KPriority:
					return // the spell resolved; the rest is the priority round
				default:
					t.Fatalf("unexpected decision %s: %+v", d.Kind, d)
				}
			}
			draws := 0
			disc := false
			for _, ev := range e.L.Events {
				if ev.Kind == events.Draw && ev.Player == 0 {
					draws++
				}
				if events.IsDiscard(ev) && ev.Player == 0 {
					disc = true
				}
			}
			if draws != tc.wantDraws {
				t.Fatalf("draws = %d, want %d", draws, tc.wantDraws)
			}
			if disc != tc.wantDisc {
				t.Fatalf("discarded = %t, want %t", disc, tc.wantDisc)
			}
		})
	}
}

// TestUnswitchedUnlessPayPreventsTheEffect drives a fixture burn spell with
// the DEFAULT (unswitched) orientation — "deals 3 damage unless its
// controller pays 4 life" — through the shared gate: paying prevents the
// damage, declining lets it through. This is the orientation the Counter
// family already had; the point is that the same gate now serves a
// DealDamage, whose UnlessCost$ used to be ignored entirely.
func TestUnswitchedUnlessPayPreventsTheEffect(t *testing.T) {
	t.Parallel()
	fixture := card(t, "Name:Test Salvo\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 3 | UnlessCost$ PayLife<4> | UnlessPayer$ TargetedController\nOracle:x\n")
	bear := card(t, "Name:Target Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	for _, tc := range []struct {
		name       string
		pay        bool
		wantDamage bool
	}{{"pays", true, false}, {"declines", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			e := handEngine(t, fixture)
			bearID := onBoardCard(t, e, 0, bear)
			addMana(t, e, 0, "RR")
			e.askPriority(0)
			id := handIDsByFace(e)["Test Salvo"]
			submitChoices(t, e, passToCast(t, e, id))
			// Target the bear on seat 0 (the payer is its controller, seat 0).
			tgt := passUntilNonPriority(t, e, 8)
			if tgt == nil || tgt.Kind != decision.KTarget {
				t.Fatalf("pending decision = %+v, want the target ask", tgt)
			}
			idx := -1
			for _, o := range tgt.Options {
				if o.Obj == bearID {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("the bear is not offered as a target: %+v", tgt.Options)
			}
			submitChoices(t, e, idx)
			// The resolution runs once both seats pass; drain the priority
			// rounds until the unless-pay ask is pending.
			for i := 0; i < 8; i++ {
				p := e.Pending()
				if p == nil {
					t.Fatal("no decision pending after the target answer")
				}
				if p.Kind == decision.KModes && p.ResumeKind == "unless_pay" {
					break
				}
				if p.Kind != decision.KPriority {
					t.Fatalf("pending decision = %+v, want priority or the unless-pay ask", p)
				}
				passIdx := -1
				for _, o := range p.Options {
					if o.Kind == "pass" {
						passIdx = o.Index
					}
				}
				if passIdx < 0 {
					t.Fatalf("priority decision with no pass option: %+v", p)
				}
				if err := e.Submit(decision.Intent{Seq: p.Seq, Player: p.Player, Choices: []int{passIdx}}); err != nil {
					t.Fatalf("submit pass: %v", err)
				}
			}
			answerUnlessPay(t, e, tc.pay)
			dmg := int32(0)
			for _, ev := range e.L.Events {
				if ev.Kind == events.Damage && ev.Obj == bearID {
					dmg += ev.Amount
				}
			}
			if tc.wantDamage && dmg != 3 {
				t.Fatalf("damage dealt = %d, want 3", dmg)
			}
			if !tc.wantDamage && dmg != 0 {
				t.Fatalf("damage dealt = %d, want 0 (the pay prevented the effect)", dmg)
			}
			if tc.pay {
				if got := e.G.Players[0].Life; got != 16 {
					t.Fatalf("payer life = %d, want 16", got)
				}
			}
		})
	}
}

// passToPlay walks the priority passes until a play_land option for obj is
// offered and returns its index (the land-drop shape of passToCast).
func passToPlay(t *testing.T, e *Engine, obj state.ObjID) int {
	t.Helper()
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("expected a priority decision while seeking the land drop of %d, got %+v", obj, d)
		}
		for _, o := range d.Options {
			if o.Kind == "play_land" && o.Obj == obj {
				return o.Index
			}
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			t.Fatalf("priority decision with no pass and no land drop of %d: %+v", obj, d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatalf("never got to the land drop of %d", obj)
	return -1
}

// optionsNotKept returns the indexes of the ask's options whose object is not
// the keeper — the two creatures the ask must bury in TestSacrificeHonoursAmountTwo.
func optionsNotKept(t *testing.T, e *Engine, d *decision.Decision, keeper state.ObjID) []int {
	t.Helper()
	var out []int
	for _, o := range d.Options {
		if o.Obj != keeper {
			out = append(out, o.Index)
		}
	}
	if len(out) != len(d.Options)-1 {
		t.Fatalf("options %+v do not single out the keeper %d", d.Options, keeper)
	}
	return out
}
