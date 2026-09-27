package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestOwnedByPlayerEnchantedBy(t *testing.T) {
	g, ids := board(t)
	aura := ids["myEnchantment"]
	if g.Obj(aura).Zone != state.ZBattlefield || !g.Obj(aura).Face().IsEnchantment() {
		t.Fatal("precondition: Aura source is not a battlefield enchantment")
	}
	events.Apply(g, events.Event{Kind: events.Attach, Obj: aura, Player: 0, Text: "attach to player"})
	if !g.Obj(aura).HasAttachedPlayer || g.Obj(aura).AttachedPlayer != 0 {
		t.Fatal("precondition: Aura did not attach to seat zero")
	}
	ownedBy := &state.Object{Owner: 0, Controller: 1}
	otherOwner := &state.Object{Owner: 1, Controller: 0}
	if ownedBy.Owner == ownedBy.Controller || ownedBy.Owner == otherOwner.Owner {
		t.Fatal("precondition: ownership and control values must differ")
	}
	for _, spec := range []string{"Card.OwnedBy Player.EnchantedBy", "Card.!OwnedBy Player.EnchantedBy"} {
		if unknown := UnknownPredicates(spec); len(unknown) != 0 {
			t.Fatalf("recognized referent %s reported unknown: %v", spec, unknown)
		}
	}
	if !MatchesObjectCtx(g, "Card.OwnedBy Player.EnchantedBy", ownedBy, SpecContext{}) {
		t.Fatal("the enchanted player's owned card did not match despite different controller")
	}
	if MatchesObjectCtx(g, "Card.ControlledBy Player.EnchantedBy", ownedBy, SpecContext{}) {
		t.Fatal("OwnedBy referent incorrectly compared the candidate controller")
	}
	if MatchesObjectCtx(g, "Card.OwnedBy Player.EnchantedBy", otherOwner, SpecContext{}) {
		t.Fatal("card owned by another player matched")
	}
	if !MatchesObjectCtx(g, "Card.!OwnedBy Player.EnchantedBy", otherOwner, SpecContext{}) {
		t.Fatal("negated ownership did not match a bound different owner")
	}
	events.Apply(g, events.Event{Kind: events.Attach, Obj: aura})
	positive := MatchesObjectCtx(g, "Card.OwnedBy Player.EnchantedBy", ownedBy, SpecContext{})
	negative := MatchesObjectCtx(g, "Card.!OwnedBy Player.EnchantedBy", ownedBy, SpecContext{})
	if positive || negative {
		t.Fatalf("detached Aura should leave the referent unbound, including beneath negation: positive=%t negative=%t attached=%t zone=%s", positive, negative, g.Obj(aura).HasAttachedPlayer, g.Obj(aura).Zone)
	}
	events.Apply(g, events.Event{Kind: events.Attach, Obj: aura, Player: 0, Text: "attach to player"})
	events.Apply(g, events.Event{Kind: events.MoveZone, Obj: aura, From: state.ZBattlefield, To: state.ZGraveyard})
	if MatchesObjectCtx(g, "Card.OwnedBy Player.EnchantedBy", ownedBy, SpecContext{}) ||
		MatchesObjectCtx(g, "Card.!OwnedBy Player.EnchantedBy", ownedBy, SpecContext{}) {
		t.Fatal("departed Aura should leave the referent unbound, including beneath negation")
	}
}
