package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The api:Token PumpKeywords$/PumpDuration$ rider (task
// agent-20260919T185907Z-1c13f0f9): a token's script-driver line may grant
// the created token a keyword for a bounded lifetime ("That token gains haste
// until end of turn"). effToken previously created the token and ignored
// the rider, so every such token silently lost its keyword. These tests pin
// the rider end to end on real corpus cards:
//
//   - Loyal Apprentice's Lieutenant trigger (PumpDuration$ EOT), the filing
//     card, and
//   - Legion Warboss's begin-combat trigger (PumpDuration$ EndOfTurn), and
//   - Baral and Kari Zev's no-free-spell branch (PumpDuration$ EOT).

// tokenOnSeatID returns seat p's battlefield object whose face name is
// exactly name, or 0 when no such token exists. Prefer this over
// countTokensNamedOnSeat when the assertions need the object (keyword reads).
func tokenOnSeatID(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	return 0
}

// driveThroughBeginCombat drives from the current point to seat 0's
// DeclareAttackers step on turn 1, passing priority through the begin-combat
// step so a `Phase$ BeginCombat` trigger is queued and resolves on the way.
func driveThroughBeginCombat(t *testing.T, e *Engine) {
	t.Helper()
	driveToStep(t, e, 1, 0, state.StepDeclareAttackers)
}

// driveToNextTurnDecliningAttackers drives to seat 0's Main1 on the given
// turn, answering the intervening `declare attackers` asks with no attackers
// (the token tests do not attack) and any cleanup discard with the naive
// first-Max answer -- the same shape driveToStep uses. It exists because
// driveToStep refuses a non-priority decision, and the expiry assertion has
// to cross a combat step.
func driveToNextTurnDecliningAttackers(t *testing.T, e *Engine, turn int32) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn == turn && e.G.Active == 0 && e.G.Step == state.StepMain1 {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before seat 0's turn %d main phase", turn)
		}
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while driving to turn %d main phase", turn)
		}
		switch d.Kind {
		case decision.KAttackers:
			// A token the rider made "attacks this combat if able" (Legion
			// Warboss's MustAttack static) is Required; the validator rejects an
			// empty answer then. Declare exactly the required attackers, no
			// others, so both the ordinary and the must-attack shape cross the
			// combat step.
			var choices []int
			for _, o := range d.Options {
				if o.Required {
					choices = append(choices, o.Index)
				}
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("declare required attackers: %v", err)
			}
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
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		default:
			t.Fatalf("unexpected decision %+v while driving to turn %d main phase", d, turn)
		}
	}
	t.Fatalf("did not reach seat 0's turn %d main phase", turn)
}

// TestLoyalApprenticeLieutenantThopterGainsHasteUntilEOT is the filing card:
// resolving Loyal Apprentice's Lieutenant token trigger creates a 1/1 Thopter
// that gains haste until end of turn. The test asserts the haste is present
// on the turn it was granted AND gone at the next turn's main phase (the
// lifetime, not just the grant). It also asserts the PRECONDITION the
// Lieutenant gate depends on (seat 0 controls its commander on the
// battlefield), because without it the trigger would never fire and the
// "no haste" assertion could pass vacuously.
func TestLoyalApprenticeLieutenantThopterGainsHasteUntilEOT(t *testing.T) {
	t.Parallel()
	apprentice := tokenReplCorpusCard(t, "Loyal Apprentice")
	// The Lieutenant condition is `IsPresent$ Card.IsCommander+YouOwn+YouCtrl`,
	// which reads Players[].Commanders; the fixture is an authored creature
	// (never a corpus .txt) marked as seat 0's commander.
	cmdrSrc := "Name:Test Commander\nTypes:Legendary Creature Human\nPT:3/3\nOracle:x\n"
	cmdr := cardByName(t, cmdrSrc)

	e, _ := tokenReplGame(t, 77, apprentice, cmdr)
	apprenticeID := moveSeededCard(t, e, 0, apprentice, state.ZBattlefield)
	cmdrID := moveSeededCard(t, e, 0, cmdr, state.ZBattlefield)
	e.G.Players[0].Commanders = []state.ObjID{cmdrID}
	e.pending = nil
	e.Advance()

	// Precondition: the Lieutenant source and the commander it gates on are
	// both on seat 0's battlefield under seat 0's control.
	if o := e.G.Obj(apprenticeID); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Loyal Apprentice not on seat 0's battlefield: %+v", o)
	}
	if o := e.G.Obj(cmdrID); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: commander not on seat 0's battlefield: %+v", o)
	}

	driveThroughBeginCombat(t, e)

	thopterID := tokenOnSeatID(t, e, 0, "Thopter Token")
	if thopterID == 0 {
		t.Fatal("Loyal Apprentice's Lieutenant trigger created no Thopter Token")
	}
	// Precondition for the negative half below: a 1/1 flying Thopter with no
	// printed Haste, so the only possible source of Haste is the rider.
	if e.HasKeyword(thopterID, "Haste") == false {
		t.Fatal("Loyal Apprentice's Thopter Token did not gain haste until end of turn")
	}
	if e.HasKeyword(thopterID, "Flying") == false {
		t.Fatal("precondition: the Thopter Token must carry its script's printed Flying")
	}

	// Roll to seat 0's next turn: the UntilEOT grant is dropped at the
	// preceding cleanup, so the same token no longer has haste.
	driveToNextTurnDecliningAttackers(t, e, 3)
	still := e.G.Obj(thopterID)
	if still == nil || still.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the Thopter must survive to the next turn, got %+v", still)
	}
	if e.HasKeyword(thopterID, "Haste") {
		t.Fatal("the Thopter kept haste past end of turn (PumpDuration$ EOT did not expire)")
	}
}

// TestLegionWarbossTokenGainsHasteEndOfTurnSpelling pins the corpus's other
// measured PumpDuration$ spelling, `EndOfTurn` (Legion Warboss), on the same
// real trigger shape and asserts the same expiry at the following turn.
func TestBaralAndKariZevNoFreeSpellCreatesHastyRagavan(t *testing.T) {
	t.Parallel()
	baral := tokenReplCorpusCard(t, "Baral and Kari Zev")
	spell := cardByName(t, "Name:Zero Mana Instant\nManaCost:0\nTypes:Instant\nOracle:x\n")
	e, cfg := tokenReplGame(t, 79, baral, spell)
	baralID := moveSeededCard(t, e, 0, baral, state.ZBattlefield)
	spellID := moveSeededCard(t, e, 0, spell, state.ZHand)
	e.pending = nil
	e.Advance()
	if o := e.G.Obj(baralID); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Baral and Kari Zev not controlled on battlefield: %+v", o)
	}
	toMain1(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v", d)
	}
	castIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spellID {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("no cast option for spell %d: %+v", spellID, d.Options)
	}
	submitChoices(t, e, castIdx)
	// The triggered optional free-play decision is declined; with no other
	// instant in hand the no-free-spell branch must mint Ragavan.
	for i := 0; i < 100; i++ {
		if id := tokenOnSeatID(t, e, 0, "First Mate Ragavan"); id != 0 {
			if !e.HasKeyword(id, "Haste") {
				t.Fatal("Ragavan lacks EOT haste")
			}
			replayCheck(t, e, cfg)
			return
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while resolving Baral trigger")
		}
		switch d.Kind {
		case decision.KChoose:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "decline" || o.Kind == "no" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("no decline option: %+v", d)
			}
			submitChoices(t, e, idx)
		case decision.KPriority:
			passPriorityOnce(t, e)
		default:
			t.Fatalf("unexpected decision while resolving trigger: %+v", d)
		}
	}
	t.Fatal("Baral trigger did not create Ragavan")
}

func TestLegionWarbossTokenGainsHasteEndOfTurnSpelling(t *testing.T) {
	t.Parallel()
	warboss := tokenReplCorpusCard(t, "Legion Warboss")
	e, _ := tokenReplGame(t, 78, warboss)
	_ = moveSeededCard(t, e, 0, warboss, state.ZBattlefield)
	e.pending = nil
	e.Advance()

	driveThroughBeginCombat(t, e)

	goblinID := tokenOnSeatID(t, e, 0, "Goblin Token")
	if goblinID == 0 {
		t.Fatal("Legion Warboss's begin-combat trigger created no Goblin Token")
	}
	if !e.HasKeyword(goblinID, "Haste") {
		t.Fatal("Legion Warboss's Goblin Token did not gain haste (PumpDuration$ EndOfTurn)")
	}

	driveToNextTurnDecliningAttackers(t, e, 3)
	if o := e.G.Obj(goblinID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the Goblin must survive to the next turn, got %+v", o)
	}
	if e.HasKeyword(goblinID, "Haste") {
		t.Fatal("the Goblin kept haste past end of turn (PumpDuration$ EndOfTurn did not expire)")
	}
}
