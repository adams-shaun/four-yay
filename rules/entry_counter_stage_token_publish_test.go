package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// stagedGrantCreature is an original test-only creature whose Updated
// PutCounter|ETB$ True body is an entry grant, so Hardened Scales and
// Branching Evolution compete non-commutatively on its entry: 1 -> 2 -> 4
// (Scales first) against 1 -> 2 -> 3 (Evolution first).
func stagedGrantCreature(t *testing.T, name, types string) *cards.Card {
	t.Helper()
	return card(t, "Name:"+name+"\nTypes:"+types+"\nPT:1/1\n"+
		"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ AddEntry | ReplacementResult$ Updated | Description$ entry counter\n"+
		"SVar:AddEntry:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\nOracle:x\n")
}

// rememberedChooses counts the Choose "remembered" events that name id (the
// RememberTokens$ rider's event-backed half).
func rememberedChooses(log []events.Event, id state.ObjID) int {
	n := 0
	for _, ev := range log {
		if ev.Kind == events.Choose && ev.Counter == "remembered" && slices.Contains(ev.IDs, id) {
			n++
		}
	}
	return n
}

// TestChosenCopyRidersWaitForItsStagedEntry is the chosen-copy half of the
// one-publication-point rule (rules/token_rest.go publishTokenEntry): a
// resolving DB$ Token (TokenTapped$ True, RememberTokens$ True) whose mint a
// chosen-copy CreateToken replacement rewrites into a copy of a creature
// whose entry grant competes under Hardened Scales and Branching Evolution.
// The copy's CopyToken mints it in the library; its battlefield MoveZone
// stages behind the CR 616.1 order ask. Until that answer the copy has NOT
// entered, so neither TokenTapped$ nor RememberTokens$ may touch it
// (events.Apply accepts a Tap on a library object, so a premature rider is
// silently "successful"); after the atomic entry each lands exactly once,
// and the SubAbility$ runs once, after every mint.
//
// Two copiers: the real Esix, Fractal Bloom (an Optional$ election, so the
// copy mints inside the election's answer), and an original test-only
// mandatory copier whose single candidate needs no election, so the copy
// mints synchronously inside the resolving effect's own EmitTokenCreate --
// the path where a pre-published id reached effToken's riders at once. (Its
// second creation then sees two candidates, the Bear and the first copy, and
// elects.)
func TestChosenCopyRidersWaitForItsStagedEntry(t *testing.T) {
	mandatory := card(t, "Name:Mandatory Copier\nTypes:Enchantment\n"+
		"R:Event$ CreateToken | ActiveZones$ Battlefield | ValidPlayer$ You | Layer$ Copy | ReplaceWith$ DBCopy | Description$ Create copies of the other creature instead.\n"+
		"SVar:DBCopy:DB$ ReplaceToken | Type$ ReplaceToken | ValidChoices$ Creature.Other | TokenScript$ Chosen\nOracle:x\n")
	for _, cp := range []struct {
		name              string
		esix              bool
		elections, copies int
	}{
		{"esix-election", true, 1, 1},
		{"mandatory-copier", false, 1, 2},
	} {
		for _, tc := range []struct {
			name string
			pick int
			want int32
		}{{"scales-first", 0, 4}, {"evolution-first", 1, 3}} {
			t.Run(cp.name+"/"+tc.name, func(t *testing.T) {
				chosenCopyStagedRiders(t, cp.esix, mandatory, cp.elections, cp.copies, tc.pick, tc.want)
			})
		}
	}
}

func chosenCopyStagedRiders(t *testing.T, useEsix bool, mandatory *cards.Card, wantElections, wantCopies, pick0 int, want int32) {
	scales := tokenReplCorpusCard(t, "Hardened Scales")
	evolution := tokenReplCorpusCard(t, "Branching Evolution")
	copier := mandatory
	if useEsix {
		copier = tokenReplCorpusCard(t, "Esix, Fractal Bloom")
	}
	bear := stagedGrantCreature(t, "Staged Copy Bear", "Creature Bear")
	spell := card(t, "Name:Tapped Remembered Tokens\nManaCost:0\nTypes:Sorcery\n"+
		"A:SP$ Token | TokenAmount$ 2 | TokenScript$ c_a_powerstone | TokenTapped$ True | RememberTokens$ True | SubAbility$ Rider | SpellDescription$ x\n"+
		"SVar:Rider:DB$ GainLife | LifeAmount$ 1\nOracle:x\n")
	e, cfg := tokenReplGame(t, 1021, scales, evolution, copier, bear, spell)
	// The Bear enters before the counter modifiers, so its own entry is
	// uncontested; only the copies' entries stage.
	copierID := moveSeededCard(t, e, 0, copier, state.ZBattlefield)
	bearID := moveSeededCard(t, e, 0, bear, state.ZBattlefield)
	for _, c := range []*cards.Card{scales, evolution} {
		if o := e.G.Obj(moveSeededCard(t, e, 0, c, state.ZBattlefield)); o == nil || o.Zone != state.ZBattlefield {
			t.Fatal("precondition: counter modifier absent")
		}
	}
	e.SetCounterAdder(0)
	for _, id := range []state.ObjID{copierID, bearID} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %d not on the battlefield: %+v", id, o)
		}
	}
	spellID := moveSeededCard(t, e, 0, spell, state.ZHand)
	addMana(t, e, 0, "")
	castSpellOption(t, e, "Tapped Remembered Tokens")
	life := e.G.Players[0].Life
	first := e.G.NextID
	elections, orders := 0, 0
	var copies []state.ObjID
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
		case decision.KChoose:
			elections++
			pick = optionForObj(d, bearID)
		case decision.KReplacement:
			orders++
			// The staged copy: the newest object, minted by CopyToken and
			// still in the library.
			copyID := e.G.NextID - 1
			o := e.G.Obj(copyID)
			if o == nil || o.Zone != state.ZLibrary || !o.IsCopy {
				t.Fatalf("precondition: at order ask %d the chosen copy %d = %+v, want a copy still in the library", orders, copyID, o)
			}
			if n := countKind(e.L.Events, events.Tap, copyID); n != 0 {
				t.Fatalf("TokenTapped$ tapped copy %d %d times before its entry-order answer (still in the library)", copyID, n)
			}
			if n := rememberedChooses(e.L.Events, copyID); n != 0 {
				t.Fatalf("RememberTokens$ remembered copy %d %d times before its entry-order answer", copyID, n)
			}
			if got := e.G.Players[0].Life; got != life {
				t.Fatalf("SubAbility$ ran before copy %d entered: life %d, want %d", copyID, got, life)
			}
			copies = append(copies, copyID)
			pick = pick0
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
	if elections != wantElections || orders != wantCopies || len(copies) != wantCopies {
		t.Fatalf("asked %d elections and %d order asks, want %d and %d", elections, orders, wantElections, wantCopies)
	}
	for _, copyID := range copies {
		o := e.G.Obj(copyID)
		if o == nil || o.Zone != state.ZBattlefield || !o.IsCopy || o.Face().Name != "Staged Copy Bear" {
			t.Fatalf("copy %d = %+v, want the staged Bear copy on the battlefield", copyID, o)
		}
		if got := o.Counter("P1P1"); got != want {
			t.Fatalf("copy %d counters = %d, want %d (the answer decides 1->2->4 vs 1->2->3)", copyID, got, want)
		}
		if !o.Tapped || countKind(e.L.Events, events.Tap, copyID) != 1 {
			t.Fatalf("copy %d tapped=%v with %d Tap events, want exactly one after its entry", copyID, o.Tapped, countKind(e.L.Events, events.Tap, copyID))
		}
		if n := rememberedChooses(e.L.Events, copyID); n != 1 {
			t.Fatalf("copy %d remembered %d times, want exactly once after its entry", copyID, n)
		}
		// The riders follow the atomic entry in the log.
		entryAt, tapAt, memAt := -1, -1, -1
		for i, ev := range e.L.Events {
			switch {
			case ev.Kind == events.MoveZone && ev.Obj == copyID && ev.To == state.ZBattlefield && entryAt < 0:
				entryAt = i
			case ev.Kind == events.Tap && ev.Obj == copyID:
				tapAt = i
			case ev.Kind == events.Choose && ev.Counter == "remembered" && slices.Contains(ev.IDs, copyID):
				memAt = i
			}
		}
		if entryAt < 0 || tapAt < entryAt || memAt < entryAt {
			t.Fatalf("copy %d entry at %d, Tap at %d, remember at %d: every rider must follow the entry", copyID, entryAt, tapAt, memAt)
		}
	}
	stones := 0
	for id := first; id < e.G.NextID; id++ {
		if s := e.G.Obj(id); s != nil && s.Zone == state.ZBattlefield && s.Face().Name == "Powerstone Token" {
			stones++
			if countKind(e.L.Events, events.Tap, id) != 1 || rememberedChooses(e.L.Events, id) != 1 {
				t.Fatalf("powerstone %d took its riders %d/%d times, want once each", id,
					countKind(e.L.Events, events.Tap, id), rememberedChooses(e.L.Events, id))
			}
		}
	}
	if stones != 2-wantCopies {
		t.Fatalf("minted %d Powerstones, want %d", stones, 2-wantCopies)
	}
	if got := e.G.Players[0].Life; got != life+1 {
		t.Fatalf("SubAbility$ life = %d, want %d exactly once", got, life+1)
	}
	if s := e.G.Obj(spellID); s == nil || s.Zone != state.ZGraveyard {
		t.Fatalf("resolved spell = %+v, want it in the graveyard", s)
	}
	replayCheck(t, e, cfg)
}
