package rules

import (
	"maps"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestEntryCounterStageTokenEffectRiders is the resolving-effect half of the
// token staging: a real `SP$ Token` (Fallaji Excavation: "Create three
// tapped Powerstone tokens. You gain 3 life.") whose mint stages behind a
// CR 616.1 entry-counter order ask must not lose the effect's per-mint work.
// The ask suspends the resolution INSIDE the mint, so the minted ids arrive
// only with the answer; the token effect has to resume there -- apply
// TokenTapped$ to the token the answer minted, mint the remaining tokens
// (each staging its own ask), and only then walk its SubAbility$.
//
// The powerstone script is replaced by an original test-only creature token
// whose Updated PutCounter|ETB$ True body is the entry grant, so Hardened
// Scales and Branching Evolution (both real) compete non-commutatively:
// 1 -> 2 -> 4 against 1 -> 2 -> 3.
func TestEntryCounterStageTokenEffectRiders(t *testing.T) {
	for _, tc := range []struct {
		name string
		pick int
		want int32
	}{{"scales-first", 0, 4}, {"evolution-first", 1, 3}} {
		t.Run(tc.name, func(t *testing.T) {
			scales := tokenReplCorpusCard(t, "Hardened Scales")
			evolution := tokenReplCorpusCard(t, "Branching Evolution")
			excavation := tokenReplCorpusCard(t, "Fallaji Excavation")
			sa := excavation.Faces[0].SpellAbility()
			if sa == nil || sa.Params["TokenTapped"] != "True" || sa.Params["TokenAmount"] != "3" ||
				sa.Params["TokenScript"] != "c_a_powerstone" || sa.Params["SubAbility"] == "" {
				t.Fatalf("precondition: Fallaji Excavation's shape changed: %+v", sa)
			}
			token := card(t, "Name:Staged Rider Token\nTypes:Creature Construct\nPT:1/1\n"+
				"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ AddEntry | ReplacementResult$ Updated | Description$ entry counter\n"+
				"SVar:AddEntry:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\nOracle:x\n")
			e, cfg := tokenReplGame(t, 997, scales, evolution, excavation)
			cfg.Tokens = maps.Clone(cfg.Tokens)
			cfg.Tokens["c_a_powerstone"] = token
			e = New(cfg)
			e.Advance()
			for _, c := range []*cards.Card{scales, evolution} {
				id := moveSeededCard(t, e, 0, c, state.ZBattlefield)
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
					t.Fatal("precondition: counter modifier absent")
				}
			}
			e.SetCounterAdder(0)
			spellID := moveSeededCard(t, e, 0, excavation, state.ZHand)
			if o := e.G.Obj(spellID); o == nil || o.Zone != state.ZHand {
				t.Fatalf("precondition: spell not in hand: %+v", o)
			}
			addMana(t, e, 0, "GGGGG")
			castSpellOption(t, e, "Fallaji Excavation")
			life := e.G.Players[0].Life

			// Resolve: the first mint parks its order ask mid-resolution.
			for i := 0; i < 8; i++ {
				d := e.Pending()
				if d == nil || d.Kind != decision.KPriority {
					break
				}
				passPriorityOnce(t, e)
			}
			first := e.G.NextID
			var minted []state.ObjID
			for n := 0; n < 3; n++ {
				d := e.Pending()
				if d == nil || d.Kind != decision.KReplacement {
					t.Fatalf("mint %d: expected the staged entry-counter order ask, got %+v", n, d)
				}
				if o := e.G.Obj(spellID); o == nil || o.Zone != state.ZStack {
					t.Fatalf("mint %d: the resolving spell left the stack before its answer: %+v", n, o)
				}
				if got := e.G.Players[0].Life; got != life {
					t.Fatalf("mint %d: SubAbility$ GainLife ran before every mint landed: life %d, want %d", n, got, life)
				}
				if e.G.NextID != first+state.ObjID(n) {
					t.Fatalf("mint %d: an object was minted before its answer (NextID %d, want %d)", n, e.G.NextID, first+state.ObjID(n))
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{tc.pick}}); err != nil {
					t.Fatal(err)
				}
				id := first + state.ObjID(n)
				o := e.G.Obj(id)
				if o == nil || o.Zone != state.ZBattlefield || !o.IsToken {
					t.Fatalf("mint %d: answered token %d = %+v, want a battlefield token", n, id, o)
				}
				minted = append(minted, id)
			}
			if d := e.Pending(); d != nil && d.Kind == decision.KReplacement {
				t.Fatalf("a fourth order ask after three mints: %+v", d)
			}
			for _, id := range minted {
				o := e.G.Obj(id)
				if got := o.Counter("P1P1"); got != tc.want {
					t.Fatalf("token %d counters = %d, want %d (the answer decides 1->2->4 vs 1->2->3)", id, got, tc.want)
				}
				if !o.Tapped {
					t.Fatalf("token %d entered untapped: DB$ Token's TokenTapped$ rider was lost across the order ask", id)
				}
				if n := countKind(e.L.Events, events.Tap, id); n != 1 {
					t.Fatalf("token %d tapped %d times, want exactly once", id, n)
				}
			}
			if n := e.G.NextID - first; n != 3 {
				t.Fatalf("resolution minted %d objects, want exactly three tokens", n)
			}
			if got := e.G.Players[0].Life; got != life+3 {
				t.Fatalf("SubAbility$ GainLife life = %d, want %d (exactly once, after the mints)", got, life+3)
			}
			if o := e.G.Obj(spellID); o == nil || o.Zone != state.ZGraveyard {
				t.Fatalf("resolved spell = %+v, want it in the graveyard", o)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestTokenOrderAskKeepsTokenEffectRiders is the same continuation under the
// OTHER mint-parking competition, CR 616.1's order over CreateToken
// replacements (replChoiceToken): a real Blood Money resolves with two
// nontoken creatures destroyed, so its `DB$ Token | TokenAmount$ X |
// TokenTapped$ True` owes two Treasure mints, and each mint parks on the
// Doubling Season (Amount) against Worldwalker Helm (AddToken) order (a
// multiplier and an adder are not a commuting class, so the creator is
// asked). Every token of every answered plan must enter tapped, the second
// mint must not be emitted into the first mint's outstanding ask, and the
// SubAbility$ Cleanup (ClearRemembered) must run once, after both.
func TestTokenOrderAskKeepsTokenEffectRiders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dsFirst bool
	}{{"doubling-first", true}, {"helm-first", false}} {
		t.Run(tc.name, func(t *testing.T) {
			ds := tokenReplCorpusCard(t, "Doubling Season")
			helm := tokenReplCorpusCard(t, "Worldwalker Helm")
			money := tokenReplCorpusCard(t, "Blood Money")
			sub := money.Faces[0].SpellAbility()
			if sub == nil || sub.Sub == nil || sub.Sub.API != "Token" || sub.Sub.Params["TokenTapped"] != "True" ||
				sub.Sub.Params["TokenAmount"] != "X" || sub.Sub.Sub == nil || sub.Sub.Sub.API != "Cleanup" {
				t.Fatalf("precondition: Blood Money's DestroyAll -> Token -> Cleanup shape changed: %+v", sub)
			}
			bear := card(t, "Name:Rider Test Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
			e, cfg := tokenReplGame(t, 998, ds, helm, money, bear, bear)
			dsID := moveSeededCard(t, e, 0, ds, state.ZBattlefield)
			helmID := moveSeededCard(t, e, 0, helm, state.ZBattlefield)
			for range 2 {
				if o := e.G.Obj(moveSeededCard(t, e, 0, bear, state.ZBattlefield)); o == nil || o.Zone != state.ZBattlefield {
					t.Fatal("precondition: a nontoken creature for Blood Money to destroy is absent")
				}
			}
			spellID := moveSeededCard(t, e, 0, money, state.ZHand)
			addMana(t, e, 0, "BBBBBBB")
			castSpellOption(t, e, "Blood Money")
			for i := 0; i < 8; i++ {
				if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
					break
				}
				passPriorityOnce(t, e)
			}
			first := e.G.NextID
			for n := 0; n < 2; n++ {
				d := e.Pending()
				if d == nil || d.Kind != decision.KReplacement {
					t.Fatalf("mint %d: expected the token replacement-order ask, got %+v", n, d)
				}
				pick := optionForObj(d, helmID)
				if tc.dsFirst {
					pick = optionForObj(d, dsID)
				}
				if pick < 0 {
					t.Fatalf("mint %d: order ask does not offer both replacements: %+v", n, d.Options)
				}
				if o := e.G.Obj(spellID); o == nil || o.Zone != state.ZStack {
					t.Fatalf("mint %d: the resolving spell left the stack before its answer: %+v", n, o)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
					t.Fatal(err)
				}
			}
			if d := e.Pending(); d != nil && d.Kind == decision.KReplacement {
				t.Fatalf("a third order ask after two mints: %+v", d)
			}
			names := map[string]int{}
			for id := first; id < e.G.NextID; id++ {
				o := e.G.Obj(id)
				if o == nil || !o.IsToken || o.Zone != state.ZBattlefield {
					t.Fatalf("minted %d = %+v, want a battlefield token", id, o)
				}
				names[o.Face().Name]++
				if !o.Tapped {
					t.Fatalf("token %d (%s) entered untapped: TokenTapped$ was lost across the order ask", id, o.Face().Name)
				}
				if n := countKind(e.L.Events, events.Tap, id); n != 1 {
					t.Fatalf("token %d tapped %d times, want exactly once", id, n)
				}
			}
			// Both creations landed, each with Doubling Season's extra
			// Treasure and Helm's Map: neither mint was swallowed.
			if names["Treasure Token"] < 4 || names["Map Token"] < 2 {
				t.Fatalf("minted tokens %v, want both creations' Treasures (doubled) and Maps", names)
			}
			if o := e.G.Obj(spellID); o == nil || o.Zone != state.ZGraveyard {
				t.Fatalf("resolved spell = %+v, want it in the graveyard", o)
			}
			if o := e.G.Obj(spellID); len(o.Remembered) != 0 {
				t.Fatalf("SubAbility$ Cleanup did not run after the mints: remembered %v", o.Remembered)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestTokenRewriteSettlesBeforeEntryStaging pins the ordering the staging
// pass relies on: CreateToken replacements settle FIRST, so a mint rewritten
// to a script without an entry grant stages nothing. Divine Visitation (real)
// turns the staging token -- whose entry grant would compete under Hardened
// Scales and Branching Evolution -- into a 4/4 Angel, which enters at once
// with no order ask and no counters.
func TestTokenRewriteSettlesBeforeEntryStaging(t *testing.T) {
	scales := tokenReplCorpusCard(t, "Hardened Scales")
	evolution := tokenReplCorpusCard(t, "Branching Evolution")
	dv := tokenReplCorpusCard(t, "Divine Visitation")
	token := card(t, "Name:Rewritten Entry Token\nTypes:Creature Construct\nPT:1/1\n"+
		"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ AddEntry | ReplacementResult$ Updated | Description$ entry counter\n"+
		"SVar:AddEntry:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\nOracle:x\n")
	e, cfg := tokenReplGame(t, 999, scales, evolution, dv)
	cfg.Tokens = maps.Clone(cfg.Tokens)
	cfg.Tokens["rewritten_entry"] = token
	e = New(cfg)
	e.Advance()
	for _, c := range []*cards.Card{scales, evolution, dv} {
		if o := e.G.Obj(moveSeededCard(t, e, 0, c, state.ZBattlefield)); o == nil || o.Zone != state.ZBattlefield {
			t.Fatal("precondition: a replacement source is absent")
		}
	}
	e.SetCounterAdder(0)
	// Precondition: without the rewrite this mint stages (the same token and
	// modifiers TestEntryCounterStageTokenEffectRiders asks over).
	if grants := e.entryBodyCandidates(events.Event{Kind: events.TokenCreate, Player: 0, Text: "rewritten_entry"}); !grants {
		t.Fatal("precondition: the unrewritten token carries no entry grant")
	}
	ids := e.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: 0, Text: "rewritten_entry"})
	if d := e.Pending(); d != nil && d.Kind == decision.KReplacement {
		t.Fatalf("a rewritten mint posed the original script's entry-counter ask: %+v", d)
	}
	if len(ids) != 1 {
		t.Fatalf("rewritten creation minted %v, want exactly the one Angel", ids)
	}
	o := e.G.Obj(ids[0])
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || o.Face().Name != "Angel Token" {
		t.Fatalf("minted %+v, want Divine Visitation's Angel on the battlefield", o)
	}
	if got := o.Counter("P1P1"); got != 0 {
		t.Fatalf("Angel entered with %d +1/+1 counters, want 0 (the replaced script's grant must not stage)", got)
	}
	replayCheck(t, e, cfg)
}
