package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestRiotCopyKeepsTokenEffectRiders is the as-enters-election half of the
// one-publication-point rule (rules/token_rest.go publishTokenEntry): a
// resolving DB$ Token (TokenTapped$ True, RememberTokens$ True) whose mint a
// CreateToken replacement rewrites into a copy of a creature carrying an
// as-enters election keyword (Riot). The copy's battlefield MoveZone parks on
// the riot election, which applyETBChoiceReplacement poses from INSIDE the
// emit -- so the ask suspends the resolution before the copy has entered, and
// the election's answer re-drives the entry. Until that answer the copy has
// NOT entered, so neither TokenTapped$ nor RememberTokens$ may touch it; after
// the atomic entry each lands exactly once, the election's own grant applies
// (the +1/+1 counter or the haste), and the SubAbility$ runs once, after the
// mint.
//
// Two copiers, mirroring TestChosenCopyRidersWaitForItsStagedEntry: the real
// Esix, Fractal Bloom (an Optional$ election whose answer mints the copy) and
// an original test-only mandatory copier whose single candidate needs no
// election, so the copy mints synchronously inside the resolving effect's own
// EmitTokenCreate.
func TestRiotCopyKeepsTokenEffectRiders(t *testing.T) {
	for _, cp := range []struct {
		name string
		esix bool
	}{
		{"esix-election", true},
		{"mandatory-copier", false},
	} {
		for _, tc := range []struct {
			name string
			pick int
		}{{"counter", 0}, {"haste", 1}} {
			t.Run(cp.name+"/"+tc.name, func(t *testing.T) {
				riotCopyStagedRiders(t, cp.esix, tc.pick)
			})
		}
	}
}

func riotCopyStagedRiders(t *testing.T, useEsix bool, riotPick int) {
	t.Helper()
	rioter := card(t, "Name:Riot Copy Veteran\nTypes:Creature Human Warrior\nPT:2/2\nK:Riot\nOracle:x\n")
	if !rioter.Faces[0].HasKeyword("Riot") {
		t.Fatal("precondition: the riot fixture carries no Riot keyword")
	}
	var copier *cards.Card
	if useEsix {
		copier = tokenReplCorpusCard(t, "Esix, Fractal Bloom")
	} else {
		copier = card(t, "Name:Riot Mandatory Copier\nTypes:Enchantment\n"+
			"R:Event$ CreateToken | ActiveZones$ Battlefield | ValidPlayer$ You | Layer$ Copy | ReplaceWith$ DBCopy | Description$ Create copies of the other creature instead.\n"+
			"SVar:DBCopy:DB$ ReplaceToken | Type$ ReplaceToken | ValidChoices$ Creature.Other | TokenScript$ Chosen\nOracle:x\n")
	}
	spell := card(t, "Name:Tapped Riot Tokens\nManaCost:0\nTypes:Sorcery\n"+
		"A:SP$ Token | TokenAmount$ 1 | TokenScript$ c_a_powerstone | TokenTapped$ True | RememberTokens$ True | SubAbility$ Rider | SpellDescription$ x\n"+
		"SVar:Rider:DB$ GainLife | LifeAmount$ 1\nOracle:x\n")
	e, cfg := tokenReplGame(t, 1031, copier, rioter, spell)
	copierID := moveSeededCard(t, e, 0, copier, state.ZBattlefield)
	// The rioter enters through a real MoveZone: its OWN riot election parks
	// the entry (moveSeededCard straight to the battlefield would orphan the
	// ask on e.pending = nil), so setup answers it -- counter -- before the
	// resolving spell can park a copy on ITS riot election.
	rioterID := moveSeededCard(t, e, 0, rioter, state.ZHand)
	e.emit(events.Event{Kind: events.MoveZone, Obj: rioterID, From: state.ZHand, To: state.ZBattlefield})
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose {
		t.Fatalf("precondition: the rioter's own entry posed no riot election: %+v", d)
	} else if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	spellID := moveSeededCard(t, e, 0, spell, state.ZHand)
	for _, id := range []state.ObjID{copierID, rioterID} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %d not on the battlefield: %+v", id, o)
		}
	}
	if o := e.G.Obj(spellID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: spell not in hand: %+v", o)
	}
	addMana(t, e, 0, "")
	castSpellOption(t, e, "Tapped Riot Tokens")
	life := e.G.Players[0].Life
	first := e.G.NextID
	elections := 0
	var copyID state.ObjID
	for i := 0; i < 30; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while resolving")
		}
		var pick int
		switch d.Kind {
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				i = 30
				continue
			}
			passPriorityOnce(t, e)
			continue
		case decision.KChoose:
			if d.ResumeKind == "etb" {
				// The riot election on the copy's parked battlefield entry.
				elections++
				copyID = e.G.NextID - 1
				o := e.G.Obj(copyID)
				if o == nil || o.IsCopy == false || o.Face().Name != "Riot Copy Veteran" {
					t.Fatalf("precondition: at the riot ask the copy %d = %+v, want a copy of Riot Copy Veteran", copyID, o)
				}
				if o.Zone == state.ZBattlefield {
					t.Fatalf("precondition: copy %d entered before its riot answer", copyID)
				}
				if n := countKind(e.L.Events, events.Tap, copyID); n != 0 {
					t.Fatalf("TokenTapped$ tapped copy %d %d times before its riot answer (the copy has not entered)", copyID, n)
				}
				if n := rememberedChooses(e.L.Events, copyID); n != 0 {
					t.Fatalf("RememberTokens$ remembered copy %d %d times before its riot answer", copyID, n)
				}
				if got := e.G.Players[0].Life; got != life {
					t.Fatalf("SubAbility$ ran before the riot answer: life %d, want %d", got, life)
				}
				if len(d.Options) != 2 || d.Options[0].Kind != "riot" || d.Options[1].Kind != "riot" {
					t.Fatalf("precondition: not a riot election: %+v", d.Options)
				}
				pick = riotPick
			} else {
				// The chosen-copy CreateToken election (Esix only): pick the
				// rioter as the copy template.
				if !useEsix {
					t.Fatalf("unexpected non-riot KChoose without Esix: %+v", d)
				}
				pick = optionForObj(d, rioterID)
			}
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
	if copyID == 0 {
		t.Fatal("the riot election was never posed")
	}
	o := e.G.Obj(copyID)
	if o == nil || o.Zone != state.ZBattlefield || !o.IsCopy || o.Face().Name != "Riot Copy Veteran" {
		t.Fatalf("copy %d = %+v, want the Riot Copy Veteran copy on the battlefield after the answer", copyID, o)
	}
	switch riotPick {
	case 0:
		if got := o.Counter("P1P1"); got != 1 {
			t.Fatalf("copy %d P1P1 counters = %d, want 1 (the answered riot election's own grant)", copyID, got)
		}
	case 1:
		if !slices.Contains(o.IntrinsicKeywords, "Haste") {
			t.Fatalf("copy %d keywords %v, want haste from the answered riot election", copyID, o.IntrinsicKeywords)
		}
	}
	if !o.Tapped || countKind(e.L.Events, events.Tap, copyID) != 1 {
		t.Fatalf("copy %d tapped=%v with %d Tap events, want exactly one after its entry (TokenTapped$ rider)", copyID, o.Tapped, countKind(e.L.Events, events.Tap, copyID))
	}
	if n := rememberedChooses(e.L.Events, copyID); n != 1 {
		t.Fatalf("copy %d remembered %d times, want exactly once after its entry (RememberTokens$ rider)", copyID, n)
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
	if n := e.G.NextID - first; n != 1 {
		t.Fatalf("resolution minted %d objects, want exactly the one rewritten copy", n)
	}
	if got := e.G.Players[0].Life; got != life+1 {
		t.Fatalf("SubAbility$ life = %d, want %d exactly once, after the mints", got, life+1)
	}
	if s := e.G.Obj(spellID); s == nil || s.Zone != state.ZGraveyard {
		t.Fatalf("resolved spell = %+v, want it in the graveyard", s)
	}
	replayCheck(t, e, cfg)
}
