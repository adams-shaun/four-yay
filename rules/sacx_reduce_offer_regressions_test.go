package rules

// fb-20260923T005805Z-1301f55a / agent-20260923T132455Z-3f6a9ab9: regression
// coverage for the OTHER corpus carriers of the announced Sac<X/Spec> count
// whose X drives a ReduceCost static. Dargo, the Shipwrecker already has its
// own regression in rules/dargo_cast_offer_test.go; these are the same shape
// with different reduction/filter faces:
//
//   - Torgaar, Famine Incarnate: Sac<X/Creature> with an X->Y Times.2
//     reduction (SVar:Y:SVar$X/Times.2).
//   - Rottenmouth Viper: Sac<X/Permanent.nonLand>, a one-per-sacrifice
//     reduction, and a compound nonland filter that must admit nonlands and
//     exclude a land.
//   - Awaken the Blood Avatar: the cost is on the card's A:SP$ Sacrifice
//     spell ability (Cost$ 6 B R Sac<X/Creature>), folded by
//     withSpellAbilityExtras at the ordinary hand-cast offer, plus the same
//     X->Y Times.2 reduction.
//
// Corpus prevalence re-measured in this worktree:
//
//   $ /usr/bin/grep -rlE '^A:SP\$.*Sac<X' .cards/cardsfolder | xargs /usr/bin/grep -l 'Mode\$ ReduceCost' | wc -l
//   4   (Dargo, Torgaar, Rottenmouth, Awaken)
//   $ /usr/bin/grep -rlE '^A:AB\$.*Sac<X' .cards/cardsfolder | xargs /usr/bin/grep -l 'Mode\$ ReduceCost' | wc -l
//   0
//
// The shared offer-time choke point is (*Engine).offerSacXMods in
// rules/mana.go; the candidate walk is sacrificeCostCandidates /
// sacrificeCostAssignable in rules/cast.go. Every test asserts its own setup
// preconditions so a vacuous board cannot pass the offer assertion.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// sacXFixture puts a real corpus card in seat 0's hand, moves the given
// permanents onto seat 0's battlefield, and funds the pool. It is the
// dargoEngine recipe generalised to any Sac<X>-reduction carrier.
func sacXFixture(t *testing.T, cardName string, permanents []string, mana string) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	e := handEngineTokens(t, corpusAlternativeCard(t, cardName))
	spell := e.G.Zone(state.ZHand, 0)[0]
	var ids []state.ObjID
	for _, src := range permanents {
		o := e.G.AddObject(card(t, src), 0)
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
		ids = append(ids, o.ID)
	}
	addMana(t, e, 0, mana)
	return e, spell, ids
}

// announceSacX submits the cast option, asserts the Sac<X> count ask appeared,
// picks the given announced count, and returns the resulting sacrifice ask.
func announceSacX(t *testing.T, e *Engine, spell state.ObjID, x int) *decision.Decision {
	t.Helper()
	return announceSacXAt(t, e, castOption(t, e, spell), x)
}

func announceSacXAt(t *testing.T, e *Engine, option, x int) *decision.Decision {
	t.Helper()
	submitChoices(t, e, option)
	dx := e.Pending()
	if dx == nil || dx.Kind != decision.KChoose || len(dx.Options) == 0 || dx.Options[0].Kind != "x" {
		t.Fatalf("Sac<X> did not announce a count ask: %+v", dx)
	}
	seen := -1
	for _, o := range dx.Options {
		if o.Kind == "x" && o.Amount == x {
			seen = o.Index
		}
	}
	if seen < 0 {
		t.Fatalf("X=%d (the reducing announcement) not offered: %+v", x, dx.Options)
	}
	if x == 3 {
		for _, o := range dx.Options {
			if o.Kind == "x" && o.Amount == 4 {
				t.Fatalf("X=4 offered despite only three eligible nonlands: %+v", dx.Options)
			}
		}
	}
	submitChoices(t, e, seen)
	ds := e.Pending()
	if ds == nil || ds.Kind != decision.KChoose || ds.Min != x || ds.Max != x {
		t.Fatalf("sacrifice ask %+v, want Min/Max %d", ds, x)
	}
	return ds
}

// drainResolution drives a resolved spell and any trigger it queues to
// completion, taking the deterministic first option of each real ask and
// declining optional targets. It is bounded so a real non-termination bug
// fails an assertion instead of hanging the binary.
func drainResolution(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for n := 0; n < limit; n++ {
		d := e.Pending()
		if d == nil {
			return
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return
			}
			submitPass(t, e)
			continue
		}
		switch d.Kind {
		case decision.KTarget:
			// An "up to one target" ask (Torgaar's life-set trigger): decline
			// it, which Min 0 makes legal.
			choices := []int{}
			for i := 0; i < d.Min && i < len(d.Options); i++ {
				choices = append(choices, d.Options[i].Index)
			}
			submitChoices(t, e, choices...)
		case decision.KChoose, decision.KModes:
			if len(d.Options) == 0 {
				t.Fatalf("decision %s with no option to take", d.Kind)
			}
			submitChoices(t, e, d.Options[0].Index)
		default:
			t.Fatalf("unhandled decision kind %s while draining a trigger", d.Kind)
		}
	}
	t.Fatalf("resolution did not settle within %d steps", limit)
}

// TestTorgaarCastOfferedWhenSacrificeReducesManaCost is the offer/announcement
// regression for the Sac<X/Creature> + X->Y Times.2 carrier. Four creatures
// and a pool of {B}{B} cannot pay the unreduced {6}{B}{B} (8), but X=4
// sacrifices four creatures, {2} less each, leaving {B}{B}.
func TestTorgaarCastOfferedWhenSacrificeReducesManaCost(t *testing.T) {
	t.Parallel()
	e, spell, ids := sacXFixture(t, "Torgaar, Famine Incarnate", []string{
		"Name:C1\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:C2\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:C3\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:C4\nTypes:Creature\nPT:1/1\nOracle:x\n",
	}, "BB")
	// Precondition: the four creatures are really on the battlefield and the
	// pool really is below the unreduced {6}{B}{B}=8. A vacuous board or an
	// already-full pool must fail here, not pass the offer assertion silently.
	onField := 0
	for _, id := range ids {
		if e.G.Obj(id).Zone == state.ZBattlefield {
			onField++
		}
	}
	if onField != 4 {
		t.Fatalf("precondition: %d of 4 creatures on the battlefield, want 4", onField)
	}
	if pool := e.G.Players[0].Pool; pool.Total() >= 8 {
		t.Fatalf("precondition: pool %+v totals %d, want below the unreduced {6}{B}{B}=8", pool, pool.Total())
	}
	ds := announceSacX(t, e, spell, 4)
	if len(ds.Options) != 4 {
		t.Fatalf("sacrifice options %+v, want exactly the four creatures", ds.Options)
	}
	chosen := make([]int, 0, 4)
	for _, o := range ds.Options {
		chosen = append(chosen, o.Index)
	}
	submitChoices(t, e, chosen...)
	drainResolution(t, e, 60)
	for i, id := range ids {
		if e.G.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("creature %d zone=%s, want graveyard (all four sacrificed)", i, e.G.Obj(id).Zone)
		}
	}
	if e.G.Obj(spell).Zone != state.ZBattlefield {
		t.Fatalf("Torgaar zone=%s, want battlefield (cast resolved)", e.G.Obj(spell).Zone)
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool after payment=%+v, want empty ({6}{B}{B} minus {8} paid {B}{B})", pool)
	}
}

// TestRottenmouthCastOfferedWhenSacrificeReducesManaCost is the
// Sac<X/Permanent.nonLand> carrier with a one-per-sacrifice reduction. The
// battlefield holds two creatures, an artifact and a LAND: the land must not
// be a candidate, so the announced maximum is 3 and the sacrifice ask offers
// exactly the three nonlands.
func TestRottenmouthCastOfferedWhenSacrificeReducesManaCost(t *testing.T) {
	t.Parallel()
	e, spell, ids := sacXFixture(t, "Rottenmouth Viper", []string{
		"Name:C1\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:C2\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:Anvil\nTypes:Artifact\nOracle:x\n",
		"Name:Isle\nTypes:Basic Land Island\nOracle:x\n",
	}, "BBB")
	land := ids[3]
	// Precondition: the land is on the battlefield alongside three nonlands,
	// and the pool is below the unreduced {5}{B}=6.
	if e.G.Obj(land).Zone != state.ZBattlefield {
		t.Fatal("precondition: the land is not on the battlefield")
	}
	if e.G.Obj(land).Face() == nil || !e.G.Obj(land).Face().IsLand() {
		t.Fatalf("precondition: %s is not a land", e.G.Obj(land).Face().Name)
	}
	if pool := e.G.Players[0].Pool; pool.Total() >= 6 {
		t.Fatalf("precondition: pool %+v totals %d, want below the unreduced {5}{B}=6", pool, pool.Total())
	}
	ds := announceSacX(t, e, spell, 3)
	// The X ask's max is 3, not 4: the land was never a candidate. announceSacX
	// already proved X=3 is offered; the land exclusion is the point below.
	chosen := make([]int, 0, 3)
	wantIDs := map[state.ObjID]bool{ids[0]: true, ids[1]: true, ids[2]: true}
	gotIDs := map[state.ObjID]bool{}
	for _, o := range ds.Options {
		if o.Obj == land {
			t.Fatalf("the land was offered as a Permanent.nonLand sacrifice candidate: %+v", o)
		}
		chosen = append(chosen, o.Index)
		gotIDs[o.Obj] = true
	}
	if len(chosen) != 3 || len(gotIDs) != len(wantIDs) {
		t.Fatalf("sacrifice options %+v, want exactly the three nonlands", ds.Options)
	}
	for id := range wantIDs {
		if !gotIDs[id] {
			t.Fatalf("sacrifice options %+v omit eligible nonland Obj %d", ds.Options, id)
		}
	}
	submitChoices(t, e, chosen...)
	drainResolution(t, e, 60)
	for i := 0; i < 3; i++ {
		if e.G.Obj(ids[i]).Zone != state.ZGraveyard {
			t.Fatalf("nonland %d zone=%s, want graveyard", i, e.G.Obj(ids[i]).Zone)
		}
	}
	if e.G.Obj(land).Zone != state.ZBattlefield {
		t.Fatalf("land zone=%s, want battlefield (never a candidate)", e.G.Obj(land).Zone)
	}
	if e.G.Obj(spell).Zone != state.ZBattlefield {
		t.Fatalf("Rottenmouth zone=%s, want battlefield (cast resolved)", e.G.Obj(spell).Zone)
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool after payment=%+v, want empty ({5}{B} minus {3} paid {2}{B})", pool)
	}
}

// TestAwakenTheBloodAvatarAbilityOfferSacrificeReducesManaCost exercises the
// A:SP$ Sacrifice spell-ability cost fold with the same X->Y Times.2
// reduction. The cost lives on the card's spell ability
// (Cost$ 6 B R Sac<X/Creature>), so withSpellAbilityExtras is what reaches
// offerSacXMods at the hand-cast offer; four creatures and a pool of {B}{R}
// cannot pay the unreduced {6}{B}{R} (8), but X=4 leaves {B}{R}.
//
// The card is the back face of the modal Extus, Oriq Overlord. Its reachable
// CR 712.8 modal-spell offer must select and cast that face from the hand.
func TestAwakenTheBloodAvatarAbilityOfferSacrificeReducesManaCost(t *testing.T) {
	t.Parallel()
	e, spell, ids := sacXFixture(t, "Awaken the Blood Avatar", []string{
		"Name:C1\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:C2\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:C3\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:C4\nTypes:Creature\nPT:1/1\nOracle:x\n",
	}, "BR")
	o := e.G.Obj(spell)
	if o.FaceIdx != 0 || o.Face().Name != "Extus, Oriq Overlord" || o.Zone != state.ZHand {
		t.Fatalf("precondition: card is zone=%s face=%d %q, want Extus front in hand", o.Zone, o.FaceIdx, o.Face().Name)
	}
	var modalOption int = -1
	for _, option := range e.Pending().Options {
		if option.Kind == "cast" && option.Obj == spell && option.Mode == "modal_spell" && option.Label == "Cast Awaken the Blood Avatar" {
			modalOption = option.Index
		}
	}
	if modalOption < 0 {
		t.Fatalf("precondition: reachable Awaken modal-spell offer missing: %+v", e.Pending().Options)
	}
	if onField := len(e.G.Zone(state.ZBattlefield, 0)); onField != 4 {
		t.Fatalf("precondition: %d permanents on seat 0's battlefield, want the four creatures", onField)
	}
	if pool := e.G.Players[0].Pool; pool.Total() >= 8 {
		t.Fatalf("precondition: pool %+v totals %d, want below the unreduced {6}{B}{R}=8", pool, pool.Total())
	}
	ds := announceSacXAt(t, e, modalOption, 4)
	if e.G.Obj(spell).FaceIdx != 1 || e.G.Obj(spell).Face().Name != "Awaken the Blood Avatar" {
		t.Fatalf("modal spell offer did not select Awaken face: %+v", e.G.Obj(spell))
	}
	if len(ds.Options) != 4 {
		t.Fatalf("sacrifice options %+v, want exactly the four creatures", ds.Options)
	}
	chosen := make([]int, 0, 4)
	for _, o := range ds.Options {
		chosen = append(chosen, o.Index)
	}
	submitChoices(t, e, chosen...)
	drainResolution(t, e, 80)
	for i, id := range ids {
		if e.G.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("creature %d zone=%s, want graveyard (all four sacrificed)", i, e.G.Obj(id).Zone)
		}
	}
	// The sorcery resolved and left the stack for its owner's graveyard.
	if e.G.Obj(spell).Zone != state.ZGraveyard {
		t.Fatalf("Awaken zone=%s, want graveyard (sorcery resolved)", e.G.Obj(spell).Zone)
	}
	// The SubAbility token (3/6 Avatar) was created: at least one permanent
	// on seat 0's battlefield has 3/6 and is not one of the sacrificed cards.
	token := false
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.Power(id) == 3 && e.Toughness(id) == 6 {
			token = true
		}
	}
	if !token {
		t.Fatalf("no 3/6 Avatar token on seat 0's battlefield after resolution: %v", e.G.Zone(state.ZBattlefield, 0))
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool after payment=%+v, want empty ({6}{B}{R} minus {8} paid {B}{R})", pool)
	}
}
