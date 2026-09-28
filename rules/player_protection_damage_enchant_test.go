package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const playerEnchantAuraScript = "Name:Test Player Aura\nTypes:Enchantment Aura\nK:Enchant:Player\nOracle:x\n"

func TestPlayerProtectionDamage(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Absolute Virtue", "Grizzly Bears"}, []string{"Hill Giant"})
	av := findOnBoard(t, e, 0, "Absolute Virtue")
	if o := e.G.Obj(av); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Absolute Virtue must be on the battlefield")
	}
	wantPlayerKeywordPrefix(t, e, 0, "Protection:Player.Opponent")
	opponent := findOnBoard(t, e, 1, "Hill Giant")
	own := findOnBoard(t, e, 0, "Grizzly Bears")
	if opponent == own || e.controllerOf(opponent) == e.controllerOf(own) {
		t.Fatal("precondition: damage sources must have different controllers")
	}

	start := e.G.Players[0].Life
	e.damaging = opponent
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
	e.damaging = 0
	if got := e.G.Players[0].Life; got != start {
		t.Fatalf("opponent damage was not prevented: life=%d, want %d", got, start)
	}
	e.damaging = own
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 2})
	e.damaging = 0
	if got := e.G.Players[0].Life; got != start-2 {
		t.Fatalf("nonmatching damage was prevented or misapplied: life=%d, want %d", got, start-2)
	}
}

func TestPlayerProtectionEnchant(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Absolute Virtue", "Grizzly Bears"}, []string{"Hill Giant"})
	av := findOnBoard(t, e, 0, "Absolute Virtue")
	if e.G.Obj(av) == nil || e.G.Obj(av).Zone != state.ZBattlefield {
		t.Fatal("precondition: Absolute Virtue must be on the battlefield")
	}
	wantPlayerKeywordPrefix(t, e, 0, "Protection:Player.Opponent")

	opponentAura := onBoard(t, e, 1, playerEnchantAuraScript)
	ownAura := onBoard(t, e, 0, playerEnchantAuraScript)
	if e.controllerOf(opponentAura) == e.controllerOf(ownAura) {
		t.Fatal("precondition: Aura controllers must differ")
	}
	e.emit(events.Event{Kind: events.Attach, Obj: opponentAura, Player: 0, Text: "attach to player"})
	if o := e.G.Obj(opponentAura); o.HasAttachedPlayer && o.AttachedPlayer == 0 {
		t.Fatal("opponent-controlled Aura attached to the player protected from opponents")
	}
	e.emit(events.Event{Kind: events.Attach, Obj: ownAura, Player: 0, Text: "attach to player"})
	if o := e.G.Obj(ownAura); !o.HasAttachedPlayer || o.AttachedPlayer != 0 {
		t.Fatalf("own Aura failed to attach despite not matching protection: %+v", o)
	}
}

func TestPlayerProtectionExistingAura(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Absolute Virtue", "Grizzly Bears"}, []string{"Hill Giant"})
	av := findOnBoard(t, e, 0, "Absolute Virtue")
	if e.G.Obj(av) == nil || e.G.Obj(av).Zone != state.ZBattlefield {
		t.Fatal("precondition: Absolute Virtue must begin on the battlefield")
	}
	// Remove the grant first, attach the opponent's Aura, then restore the
	// protection source to exercise CR 704.5m's later-illegality path.
	e.emit(events.Event{Kind: events.MoveZone, Obj: av, From: state.ZBattlefield, To: state.ZHand})
	aura := onBoard(t, e, 1, playerEnchantAuraScript)
	e.emit(events.Event{Kind: events.Attach, Obj: aura, Player: 0, Text: "attach to player"})
	if o := e.G.Obj(aura); !o.HasAttachedPlayer || o.AttachedPlayer != 0 {
		t.Fatalf("precondition: Aura did not attach before protection was gained: %+v", o)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: av, From: state.ZHand, To: state.ZBattlefield})
	wantPlayerKeywordPrefix(t, e, 0, "Protection:Player.Opponent")
	if !e.playerProtectedFrom(0, aura) {
		t.Fatal("precondition: player did not gain protection from the Aura's controller")
	}
	if !e.attachmentSBAs() {
		t.Fatal("protection gain did not make the existing Aura attachment illegal")
	}
	if o := e.G.Obj(aura); o.Zone != state.ZGraveyard || o.HasAttachedPlayer {
		t.Fatalf("illegal player-attached Aura survived the SBA: %+v", o)
	}
}
