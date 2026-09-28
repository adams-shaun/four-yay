package rules

// The real-corpus end-to-end pin for param:api:ChooseCard.ChooseEach: Tragic
// Arrogance (the ticket's deck carrier) casts for real, its RepeatEach walk
// runs one ChooseCard per player with ChooseEach$ Artifact & Creature &
// Enchantment & Planeswalker, and the spell's controller answers one ask per
// GROUP per player through the ordinary "choice" resume arm. Each player
// keeps one permanent of each group (the creature group's pick is a real
// election, not the deterministic first candidate), a group with no
// candidate (Planeswalker) resolves silently, and the chained SacrificeAll
// sacrifices everything else nonland — the oracle's whole contract.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const (
	chooseEachArtifactSrc = "Name:Trag Art\nTypes:Artifact\nOracle:x\n"
	chooseEachCreOneSrc   = "Name:Trag Cre One\nTypes:Creature\nPT:1/1\nOracle:x\n"
	chooseEachCreTwoSrc   = "Name:Trag Cre Two\nTypes:Creature\nPT:1/1\nOracle:x\n"
	chooseEachEnchSrc     = "Name:Trag Ench\nTypes:Enchantment\nOracle:x\n"
)

// chooseEachBoard seeds a mirror board: each seat controls one artifact, two
// creatures, and one enchantment (no planeswalker), and returns the object
// ids in [artifact, creature one, creature two, enchantment] order per seat.
func chooseEachBoard(t *testing.T) (*Engine, Config, [2][4]state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Tragic Arrogance"),
			card(t, chooseEachArtifactSrc), card(t, chooseEachCreOneSrc),
			card(t, chooseEachCreTwoSrc), card(t, chooseEachEnchSrc)},
		[]*cards.Card{card(t, chooseEachArtifactSrc), card(t, chooseEachCreOneSrc),
			card(t, chooseEachCreTwoSrc), card(t, chooseEachEnchSrc)})
	var board [2][4]state.ObjID
	for p := range board {
		for i, name := range [4]string{"Trag Art", "Trag Cre One", "Trag Cre Two", "Trag Ench"} {
			board[p][i] = moveByName(t, e, state.PlayerID(p), name, state.ZBattlefield)
		}
	}
	return e, cfg, board
}

// submitCardChoice answers the pending KChoose with the option naming id.
func submitCardChoice(t *testing.T, e *Engine, d *decision.Decision, id state.ObjID) {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == id {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("ask offers no option for object %d: %+v", id, d.Options)
}

func TestTragicArroganceChooseEachKeepsOnePerTypePerPlayer(t *testing.T) {
	t.Parallel()
	e, cfg, board := chooseEachBoard(t)
	targ := moveByName(t, e, 0, "Tragic Arrogance", state.ZHand)
	addMana(t, e, 0, "WWWWW")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("before the cast, pending = %+v, want priority", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == targ {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Tragic Arrogance: %+v", d.Options)
	}
	submitChoices(t, e, idx)

	// The walk: chooser-major over the RepeatEach players, group-minor within
	// each ChooseCard (flat ResumeTarget 0,1,2 per iteration). Every ask goes
	// to seat 0 (Defined$ You — "you choose from among the permanents that
	// player controls"), and each per-group pool is exactly that player's
	// cards of the group's type. The artifact pool being the artifact ALONE —
	// not every permanent — is the precondition the per-group claim depends
	// on. Seat 0 elects the SECOND creature, seat 1 keeps the first: two
	// different answers, so a walk that collapses to a deterministic option
	// fails one side or the other.
	for pi, p := range []state.PlayerID{0, 1} {
		wantPools := [][]state.ObjID{
			{board[p][0]},              // Artifact group
			{board[p][1], board[p][2]}, // Creature group: a real two-way election
			{board[p][3]},              // Enchantment group
		}
		for gi, pool := range wantPools {
			d := passUntilAskKind(t, e, decision.KChoose, 200)
			if d.ResumeKind != "choice" || d.Player != 0 || d.Min != 1 || d.Max != 1 {
				t.Fatalf("group ask = %+v, want a Mandatory single choice for seat 0", d)
			}
			if got := d.ResumeTarget; got != gi {
				t.Fatalf("group ask ResumeTarget = %d, want %d", got, gi)
			}
			if len(d.Options) != len(pool) {
				t.Fatalf("group %d options = %+v, want exactly the %d-card pool %v", gi, d.Options, len(pool), pool)
			}
			for i, want := range pool {
				if d.Options[i].Obj != want {
					t.Fatalf("group %d option %d = %d, want %d (pool must be the player's cards of the group's type in registration order)", gi, i, d.Options[i].Obj, want)
				}
			}
			if pi == 0 && gi == 1 {
				submitCardChoice(t, e, d, pool[1]) // the election must bind
			} else {
				submitChoices(t, e, d.Options[0].Index)
			}
		}
	}
	passUntilStackEmpty(t, e, 200)

	// Each player kept the artifact, the chosen creature and the
	// enchantment; the UNCHOSEN creature of each seat was sacrificed to the
	// chained SacrificeAll (RememberChosen$ kept the picks, !IsRemembered
	// excluded them).
	kept := func(p state.PlayerID) map[string]bool {
		out := map[string]bool{}
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			if o := e.G.Obj(id); o != nil {
				out[o.Face().Name] = true
			}
		}
		return out
	}
	if got := kept(0); !got["Trag Art"] || !got["Trag Cre Two"] || !got["Trag Ench"] || got["Trag Cre One"] {
		t.Fatalf("seat 0 kept %v, want the artifact, the ELECTED second creature and the enchantment", got)
	}
	if got := kept(1); !got["Trag Art"] || !got["Trag Cre One"] || !got["Trag Ench"] || got["Trag Cre Two"] {
		t.Fatalf("seat 1 kept %v, want the artifact, the FIRST creature and the enchantment", got)
	}
	if o := e.G.Obj(targ); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Tragic Arrogance after resolution = %+v, want it in the graveyard", o)
	}
	// Seat 0 elected the second creature (Cre One sacrificed), seat 1 kept
	// the first (Cre Two sacrificed).
	for _, id := range []state.ObjID{board[0][1], board[1][2]} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("object %d = %+v, want it sacrificed to the graveyard", id, o)
		}
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && (strings.Contains(ev.Text, "unimplemented") || strings.Contains(ev.Text, "no engine host")) {
			t.Fatalf("unexpected degradation Note %q", ev.Text)
		}
	}
	replayCheck(t, e, cfg)
}
