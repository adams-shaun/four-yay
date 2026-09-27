package rules

import (
	"maps"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
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

// TestTokenElectionKeepsTokenEffectRiders follows a resolving DB$ Token's
// parked mint through a NESTED ask: Esix, Fractal Bloom's chosen-copy
// replacement poses a KChoose election -- alone as the first park, or inside
// the answer to its CR 616.1 order against Doubling Season. The copies the
// election's answer mints must still take Fallaji Excavation's
// TokenTapped$, and the later Powerstone mints and the GainLife sub must run
// once, after them.
func TestTokenElectionKeepsTokenEffectRiders(t *testing.T) {
	for _, tc := range []struct {
		name       string
		doubling   bool
		esixFirst  bool
		wantCopies int
		wantStones int
	}{
		{"esix-alone", false, false, 1, 2},
		{"esix-first", true, true, 2, 4},
		{"doubling-first", true, false, 2, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := tokenReplCorpusCard(t, "Doubling Season")
			esix := tokenReplCorpusCard(t, "Esix, Fractal Bloom")
			excavation := tokenReplCorpusCard(t, "Fallaji Excavation")
			bear := card(t, "Name:Election Test Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
			seat := []*cards.Card{esix, excavation, bear}
			if tc.doubling {
				seat = append(seat, ds)
			}
			e, cfg := tokenReplGame(t, 1001, seat...)
			var dsID state.ObjID
			if tc.doubling {
				dsID = moveSeededCard(t, e, 0, ds, state.ZBattlefield)
			}
			esixID := moveSeededCard(t, e, 0, esix, state.ZBattlefield)
			bearID := moveSeededCard(t, e, 0, bear, state.ZBattlefield)
			for _, id := range []state.ObjID{esixID, bearID} {
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("precondition: %d is not on the battlefield: %+v", id, o)
				}
			}
			spellID := moveSeededCard(t, e, 0, excavation, state.ZHand)
			addMana(t, e, 0, "GGGGG")
			castSpellOption(t, e, "Fallaji Excavation")
			life := e.G.Players[0].Life
			first := e.G.NextID
			orders, elections := 0, 0
			for i := 0; i < 40; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatal("no decision while resolving")
				}
				var pick int
				switch d.Kind {
				case decision.KPriority:
					if len(e.G.Stack) == 0 {
						i = 40
						continue
					}
					passPriorityOnce(t, e)
					continue
				case decision.KReplacement:
					orders++
					pick = optionForObj(d, dsID)
					if tc.esixFirst {
						pick = optionForObj(d, esixID)
					}
				case decision.KChoose:
					elections++
					if o := e.G.Obj(spellID); o == nil || o.Zone != state.ZStack {
						t.Fatalf("the resolving spell left the stack before the election: %+v", o)
					}
					if got := e.G.Players[0].Life; got != life {
						t.Fatalf("GainLife ran before the election's mint: life %d, want %d", got, life)
					}
					pick = optionForObj(d, bearID)
				default:
					t.Fatalf("unexpected decision %+v", d)
				}
				if pick < 0 {
					t.Fatalf("decision does not offer the expected option: %+v", d.Options)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
					t.Fatal(err)
				}
			}
			if elections != 1 || (tc.doubling && orders != 1) || (!tc.doubling && orders != 0) {
				t.Fatalf("asked %d order and %d election decisions, want %v order and 1 election", orders, elections, tc.doubling)
			}
			copies, stones := 0, 0
			for id := first; id < e.G.NextID; id++ {
				o := e.G.Obj(id)
				if o == nil || !o.IsToken || o.Zone != state.ZBattlefield {
					continue
				}
				switch {
				case o.IsCopy && o.Face().Name == "Election Test Bear":
					copies++
				case o.Face().Name == "Powerstone Token":
					stones++
				default:
					t.Fatalf("unexpected minted token %d %s", id, o.Face().Name)
				}
				if !o.Tapped || countKind(e.L.Events, events.Tap, id) != 1 {
					t.Fatalf("token %d (%s) tapped=%v with %d Tap events: TokenTapped$ must land exactly once across the election",
						id, o.Face().Name, o.Tapped, countKind(e.L.Events, events.Tap, id))
				}
			}
			if copies != tc.wantCopies || stones != tc.wantStones {
				t.Fatalf("minted %d Bear copies and %d Powerstones, want %d and %d", copies, stones, tc.wantCopies, tc.wantStones)
			}
			if got := e.G.Players[0].Life; got != life+3 {
				t.Fatalf("GainLife life = %d, want %d exactly once", got, life+3)
			}
			if o := e.G.Obj(spellID); o == nil || o.Zone != state.ZGraveyard {
				t.Fatalf("resolved spell = %+v, want it in the graveyard", o)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestParkedTokenImprintsItsMints pins the DB$ Token's ImprintCards$ tail
// across a staged order ask: `RememberTokens$ True | ImprintCards$
// Remembered` must imprint the source with the token the answer minted, so
// the tail runs after the mints, not on the pre-mint first pass.
func TestParkedTokenImprintsItsMints(t *testing.T) {
	scales := tokenReplCorpusCard(t, "Hardened Scales")
	evolution := tokenReplCorpusCard(t, "Branching Evolution")
	forge := card(t, "Name:Imprinting Forge\nTypes:Artifact\n"+
		"A:AB$ Token | Cost$ T | TokenScript$ imprint_staged | RememberTokens$ True | ImprintCards$ Remembered | SpellDescription$ Create a token and imprint it.\nOracle:x\n")
	token := card(t, "Name:Imprinted Staged Token\nTypes:Creature Construct\nPT:1/1\n"+
		"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ AddEntry | ReplacementResult$ Updated | Description$ entry counter\n"+
		"SVar:AddEntry:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\nOracle:x\n")
	e, cfg := tokenReplGame(t, 1003, scales, evolution, forge)
	cfg.Tokens = maps.Clone(cfg.Tokens)
	cfg.Tokens["imprint_staged"] = token
	e = New(cfg)
	e.Advance()
	for _, c := range []*cards.Card{scales, evolution} {
		if o := e.G.Obj(moveSeededCard(t, e, 0, c, state.ZBattlefield)); o == nil || o.Zone != state.ZBattlefield {
			t.Fatal("precondition: counter modifier absent")
		}
	}
	e.SetCounterAdder(0)
	forgeID := moveSeededCard(t, e, 0, forge, state.ZBattlefield)
	addMana(t, e, 0, "")
	submitChoices(t, e, abilityOption(t, e, forgeID, 0).Index)
	mintID := e.G.NextID
	asked := false
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while resolving")
		}
		if d.Kind == decision.KReplacement {
			asked = true
			if o := e.G.Obj(forgeID); len(o.Imprinted) != 0 {
				t.Fatalf("source imprinted %v before the token was minted", o.Imprinted)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision %+v", d)
		}
		if len(e.G.Stack) == 0 {
			break
		}
		passPriorityOnce(t, e)
	}
	if !asked {
		t.Fatal("precondition: the mint did not stage behind an order ask")
	}
	if o := e.G.Obj(mintID); o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != 4 {
		t.Fatalf("minted %+v, want the staged token with 4 counters", o)
	}
	if o := e.G.Obj(forgeID); !slices.Contains(o.Imprinted, mintID) {
		t.Fatalf("source imprinted %v, want the minted token %d (ImprintCards$ Remembered must run after the mint)", o.Imprinted, mintID)
	}
	replayCheck(t, e, cfg)
}

// TestEncoreCopiesStageAndKeepTheirRiders is the loop-caller class: Encore
// emits one CardToken copy per opponent, and two copies of one source are
// field-for-field equal events. With the copied creature's entry grant
// competing under Hardened Scales and Branching Evolution, the first copy
// stages its order ask; the second copy must be recognised as a NEW mint
// (not the staged one's re-drive, which swallowed it), and each copy must
// still gain haste and join the one end-step sacrifice group -- post-mint
// work that needs the ids the answers mint, so it resumes with them.
func TestEncoreCopiesStageAndKeepTheirRiders(t *testing.T) {
	for _, tc := range []struct {
		name string
		pick int
		want int32
	}{{"scales-first", 0, 4}, {"evolution-first", 1, 3}} {
		t.Run(tc.name, func(t *testing.T) {
			scales := tokenReplCorpusCard(t, "Hardened Scales")
			evolution := tokenReplCorpusCard(t, "Branching Evolution")
			grace := card(t, "Name:Encore Grace\nManaCost:1\nTypes:Creature Elf\nPT:1/1\nK:Encore:1\n"+
				"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ AddEntry | ReplacementResult$ Updated | Description$ entry counter\n"+
				"SVar:AddEntry:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\nOracle:x\n")
			reg := testutil.CorpusRegistry(t)
			cfg := seatZeroStart(Config{Seed: 1007, Names: []string{"a", "b", "c"},
				Decks: [][]*cards.Card{
					append([]*cards.Card{scales, evolution, grace}, mountainDeck(t, 37)...),
					mountainDeck(t, 40), mountainDeck(t, 40),
				},
				Tokens: reg.Tokens})
			e := New(cfg)
			e.Advance()
			for _, c := range []*cards.Card{scales, evolution} {
				if o := e.G.Obj(moveSeededCard(t, e, 0, c, state.ZBattlefield)); o == nil || o.Zone != state.ZBattlefield {
					t.Fatal("precondition: counter modifier absent")
				}
			}
			e.SetCounterAdder(0)
			graceID := moveSeededCard(t, e, 0, grace, state.ZGraveyard)
			addMana(t, e, 0, "C")
			submitChoices(t, e, abilityOption(t, e, graceID, 0).Index)
			first := e.G.NextID
			asks := 0
			for i := 0; i < 40; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatal("no decision while resolving")
				}
				if d.Kind == decision.KReplacement {
					asks++
					if len(e.G.Delayed) != 0 {
						t.Fatalf("ask %d: the sacrifice group registered before every copy was minted: %+v", asks, e.G.Delayed)
					}
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{tc.pick}}); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if d.Kind != decision.KPriority {
					t.Fatalf("unexpected decision %+v", d)
				}
				if len(e.G.Stack) == 0 {
					break
				}
				passPriorityOnce(t, e)
			}
			if asks != 2 {
				t.Fatalf("answered %d order asks, want 2 (one per opponent's copy)", asks)
			}
			var copies []state.ObjID
			for id := first; id < e.G.NextID; id++ {
				if o := e.G.Obj(id); o != nil && o.IsToken && o.Zone == state.ZBattlefield && o.Face() != nil && o.Face().Name == "Encore Grace" {
					copies = append(copies, id)
				}
			}
			if len(copies) != 2 {
				t.Fatalf("Encore minted %d copies, want 2 (one per opponent)", len(copies))
			}
			for _, id := range copies {
				if got := e.G.Obj(id).Counter("P1P1"); got != tc.want {
					t.Fatalf("copy %d counters = %d, want %d", id, got, tc.want)
				}
				if !e.HasKeyword(id, "Haste") {
					t.Fatalf("copy %d lacks haste: Encore's grant was lost across the order ask", id)
				}
			}
			if len(e.G.Delayed) != 1 || !slices.Equal(targetObjIDs(e.G.Delayed[0].Remembered), copies) {
				t.Fatalf("sacrifice group = %+v, want one registration remembering %v", e.G.Delayed, copies)
			}
			replayCheck(t, e, cfg)
		})
	}
}

func targetObjIDs(ts []state.Target) []state.ObjID {
	var ids []state.ObjID
	for _, t := range ts {
		if !t.IsPlayer {
			ids = append(ids, t.Obj)
		}
	}
	return ids
}

// TestEqualMintsWhileStagedAreNewMints pins the mint identity itself, for any
// caller that keeps emitting while a mint's order ask is outstanding (a loop
// with no id-dependent work, such as Investigate's Clues): a second
// field-for-field equal CardToken (and TokenCreate) is a NEW mint that stages
// behind its own ask, never the parked mint's re-drive.
func TestEqualMintsWhileStagedAreNewMints(t *testing.T) {
	for _, kind := range []events.Kind{events.CardToken, events.TokenCreate} {
		t.Run(kind.String(), func(t *testing.T) {
			scales := tokenReplCorpusCard(t, "Hardened Scales")
			evolution := tokenReplCorpusCard(t, "Branching Evolution")
			entrant := card(t, "Name:Equal Mint\nTypes:Creature Elf\nPT:1/1\n"+
				"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ AddEntry | ReplacementResult$ Updated | Description$ entry counter\n"+
				"SVar:AddEntry:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\nOracle:x\n")
			e, cfg := tokenReplGame(t, 1009, scales, evolution, entrant)
			cfg.Tokens = maps.Clone(cfg.Tokens)
			cfg.Tokens["equal_mint"] = entrant
			e = New(cfg)
			e.Advance()
			for _, c := range []*cards.Card{scales, evolution} {
				moveSeededCard(t, e, 0, c, state.ZBattlefield)
			}
			e.SetCounterAdder(0)
			mint := events.Event{Kind: kind, Player: 0, Text: "equal_mint"}
			if kind == events.CardToken {
				mint.Obj = moveSeededCard(t, e, 0, entrant, state.ZGraveyard)
			}
			first := e.G.NextID
			e.emit(mint)
			e.emit(mint)
			for n := 0; n < 2; n++ {
				d := e.Pending()
				if d == nil || d.Kind != decision.KReplacement {
					t.Fatalf("mint %d: expected its own staged order ask, got %+v", n, d)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
					t.Fatal(err)
				}
			}
			if got := e.G.NextID - first; got != 2 {
				t.Fatalf("two equal mints produced %d objects, want 2 (the second was swallowed as a re-drive)", got)
			}
			for id := first; id < first+2; id++ {
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != 4 {
					t.Fatalf("mint %d = %+v, want a battlefield token with 4 counters", id, o)
				}
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestIncubateAndAmassResumeOntoParkedMints covers the other two emitters
// whose post-mint work reads the minted id: Incubate's counters (real Eyes of
// Gitaxias, whose Incubator parks on Doubling Season against Worldwalker
// Helm) and Amass's counters on the Army it had to create (real Invade the
// City, whose Army parks on Doubling Season against Divine Visitation). The
// counters must land on the token the answer minted, and the SubAbility$ (or
// the resolution) must finish after it.
func TestIncubateAndAmassResumeOntoParkedMints(t *testing.T) {
	for _, tc := range []struct {
		name, spell, other, mana string
	}{
		{"incubate", "Eyes of Gitaxias", "Worldwalker Helm", "UUU"},
		{"amass", "Invade the City", "Divine Visitation", "UUR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := tokenReplCorpusCard(t, "Doubling Season")
			other := tokenReplCorpusCard(t, tc.other)
			spell := tokenReplCorpusCard(t, tc.spell)
			fodder := card(t, "Name:Graveyard Sorcery\nManaCost:0\nTypes:Sorcery\nA:SP$ GainLife | LifeAmount$ 1 | SpellDescription$ x\nOracle:x\n")
			e, cfg := tokenReplGame(t, 1011, ds, other, spell, fodder)
			moveSeededCard(t, e, 0, ds, state.ZBattlefield)
			moveSeededCard(t, e, 0, other, state.ZBattlefield)
			moveSeededCard(t, e, 0, fodder, state.ZGraveyard)
			spellID := moveSeededCard(t, e, 0, spell, state.ZHand)
			hand := len(e.G.Zone(state.ZHand, 0))
			addMana(t, e, 0, tc.mana)
			castSpellOption(t, e, tc.spell)
			first := e.G.NextID
			asks := 0
			for i := 0; i < 40; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatal("no decision while resolving")
				}
				if d.Kind == decision.KReplacement {
					asks++
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if d.Kind != decision.KPriority {
					t.Fatalf("unexpected decision %+v", d)
				}
				if len(e.G.Stack) == 0 {
					break
				}
				passPriorityOnce(t, e)
			}
			if asks != 1 {
				t.Fatalf("answered %d token order asks, want 1", asks)
			}
			o := e.G.Obj(first)
			if o == nil || !o.IsToken || o.Zone != state.ZBattlefield {
				t.Fatalf("first minted %d = %+v, want a battlefield token", first, o)
			}
			if o.Counter("P1P1") == 0 {
				t.Fatalf("%s's counters did not land on the token the answer minted (%s)", tc.spell, o.Face().Name)
			}
			if tc.name == "incubate" {
				// Cast from hand (-1), then the DBDraw sub (+1), exactly once.
				if got := len(e.G.Zone(state.ZHand, 0)); got != hand {
					t.Fatalf("hand = %d, want %d (the Draw sub runs once, after the mint)", got, hand)
				}
			}
			if s := e.G.Obj(spellID); s == nil || s.Zone != state.ZGraveyard {
				t.Fatalf("resolved spell = %+v, want it in the graveyard", s)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestInvestigateMarkerFollowsItsParkedClue: each Investigate marker (what
// trig:Investigated matches) is logged only after ITS Clue has entered. The
// Clue definition here carries an entry grant that competes under Hardened
// Scales and Branching Evolution, so every Clue stages behind an order ask;
// the marker, the later Clues and the rest of the resolution resume with
// the answer. Real Follow the Bodies (one investigate) and an original
// Num$ 2 sorcery, both answer orders.
func TestInvestigateMarkerFollowsItsParkedClue(t *testing.T) {
	double := card(t, "Name:Twice Investigate\nManaCost:0\nTypes:Sorcery\n"+
		"A:SP$ Investigate | Num$ 2 | SubAbility$ Rider | SpellDescription$ Investigate twice.\n"+
		"SVar:Rider:DB$ GainLife | LifeAmount$ 1\nOracle:x\n")
	for _, sp := range []struct {
		name, spell, mana string
		n                 int
		c                 *cards.Card
	}{
		{"single", "Follow the Bodies", "UUU", 1, nil},
		{"twice", "Twice Investigate", "", 2, double},
	} {
		for _, tc := range []struct {
			name string
			pick int
			want int32
		}{{"scales-first", 0, 4}, {"evolution-first", 1, 3}} {
			t.Run(sp.name+"/"+tc.name, func(t *testing.T) {
				scales := tokenReplCorpusCard(t, "Hardened Scales")
				evolution := tokenReplCorpusCard(t, "Branching Evolution")
				spell := sp.c
				if spell == nil {
					spell = tokenReplCorpusCard(t, sp.spell)
				}
				clue := card(t, "Name:Graced Clue\nTypes:Artifact Creature Clue\nPT:0/1\n"+
					"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ AddEntry | ReplacementResult$ Updated | Description$ entry counter\n"+
					"SVar:AddEntry:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\nOracle:x\n")
				e, cfg := tokenReplGame(t, 1013, scales, evolution, spell)
				cfg.Tokens = maps.Clone(cfg.Tokens)
				cfg.Tokens["c_a_clue_draw"] = clue
				e = New(cfg)
				e.Advance()
				for _, c := range []*cards.Card{scales, evolution} {
					if o := e.G.Obj(moveSeededCard(t, e, 0, c, state.ZBattlefield)); o == nil || o.Zone != state.ZBattlefield {
						t.Fatal("precondition: counter modifier absent")
					}
				}
				e.SetCounterAdder(0)
				spellID := moveSeededCard(t, e, 0, spell, state.ZHand)
				addMana(t, e, 0, sp.mana)
				castSpellOption(t, e, sp.spell)
				mark := len(e.L.Events)
				first := e.G.NextID
				asks := 0
				for i := 0; i < 40; i++ {
					d := e.Pending()
					if d == nil {
						t.Fatal("no decision while resolving")
					}
					if d.Kind == decision.KReplacement {
						asks++
						markers := 0
						for _, ev := range e.L.Events[mark:] {
							if ev.Kind == events.Investigate {
								markers++
							}
						}
						if markers != asks-1 {
							t.Fatalf("ask %d: %d Investigate markers logged, want %d (a marker precedes its Clue)", asks, markers, asks-1)
						}
						if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{tc.pick}}); err != nil {
							t.Fatal(err)
						}
						continue
					}
					if d.Kind != decision.KPriority {
						t.Fatalf("unexpected decision %+v", d)
					}
					if len(e.G.Stack) == 0 {
						break
					}
					passPriorityOnce(t, e)
				}
				if asks != sp.n {
					t.Fatalf("answered %d order asks, want %d", asks, sp.n)
				}
				// Log order: Clue k's entry, then marker k, alternating.
				var seq []events.Kind
				for _, ev := range e.L.Events[mark:] {
					if (ev.Kind == events.TokenCreate && ev.Text == "c_a_clue_draw") || ev.Kind == events.Investigate {
						seq = append(seq, ev.Kind)
					}
				}
				if len(seq) != 2*sp.n {
					t.Fatalf("logged %v, want %d Clue entries each followed by its marker", seq, sp.n)
				}
				for k := 0; k < sp.n; k++ {
					if seq[2*k] != events.TokenCreate || seq[2*k+1] != events.Investigate {
						t.Fatalf("logged %v, want Clue %d's entry before marker %d", seq, k, k)
					}
				}
				for id := first; id < first+state.ObjID(sp.n); id++ {
					if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != tc.want {
						t.Fatalf("Clue %d = %+v, want a battlefield token with %d counters", id, o, tc.want)
					}
				}
				if s := e.G.Obj(spellID); s == nil || s.Zone != state.ZGraveyard {
					t.Fatalf("resolved spell = %+v, want it in the graveyard", s)
				}
				replayCheck(t, e, cfg)
			})
		}
	}
}
