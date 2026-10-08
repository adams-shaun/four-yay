package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCopyPermanentDefinedNameCopiesTheNamedCard pins the std3 fix for
// CopyPermanent's DefinedName$ (ECL Mutable Explorer: "create a tapped
// Mutavault token"). The copy source is a CARD from the compiled corpus, not
// an object on the battlefield, so the effect must mint a token whose printed
// characteristics are the named card's, entered tapped, with no fail-closed
// Note.
func TestCopyPermanentDefinedNameCopiesTheNamedCard(t *testing.T) {
	h := newHost(t, 2)
	src := ufBattlefield(t, h, "Name:Mutable Explorer\nManaCost:2 G\nTypes:Creature Shapeshifter\nPT:1/1\nOracle:x\n")
	muta := mkCard(t, "Name:Mutavault\nManaCost:no cost\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ C\nOracle:x\n")
	h.g.NameUniverse = cards.UniverseOf([]*cards.Card{muta})
	// Precondition: the universe actually resolves the name the script uses.
	// Without this the assertion below would be vacuous (a missing universe
	// would make the effect return with a Note, not mint).
	if h.g.NamedCard("Mutavault") == nil {
		t.Fatal("precondition: NameUniverse must resolve Mutavault")
	}
	line := "SP$ CopyPermanent | DefinedName$ Mutavault | TokenTapped$ True"
	cp := CopyPermanentOf(sa(t, line))
	if cp.DefinedName != "Mutavault" {
		t.Fatalf("CopyPermanentOf.DefinedName = %q, want Mutavault", cp.DefinedName)
	}
	if cp.Blocked {
		t.Fatalf("DefinedName$ still blocks the call: %+v", cp)
	}
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, line))

	var tok *state.Object
	for i := range h.g.Objs {
		o := &h.g.Objs[i]
		if o.IsToken && o.Face() != nil && strings.EqualFold(o.Face().Name, "Mutavault") {
			tok = o
			break
		}
	}
	if tok == nil {
		t.Fatalf("no Mutavault token minted; notes %v", ufNotes(h))
	}
	// Precondition: the token really is the named card, not the resolving
	// source (the two have different types and P/T).
	if f := tok.Face(); f == nil || !containsFold(f.Types, "Land") {
		t.Fatalf("token face = %+v, want Mutavault's Land type", tok.Face())
	}
	if !tok.IsCopy {
		t.Fatalf("minted token IsCopy = false, want true (a copy, not a plain token)")
	}
	if tok.Zone != state.ZBattlefield {
		t.Fatalf("token zone = %v, want battlefield", tok.Zone)
	}
	if !tok.Tapped {
		t.Fatalf("TokenTapped$ True was not honoured: token not tapped")
	}
	for _, n := range ufNotes(h) {
		if strings.Contains(n, "DefinedName") {
			t.Fatalf("resolved DefinedName$ still noted %q", n)
		}
	}
}

// TestCopyPermanentDefinedNameUnknownIsLoud: a name the universe cannot
// resolve mints nothing and says so, never silently falling through to the
// resolving source.
func TestCopyPermanentDefinedNameUnknownIsLoud(t *testing.T) {
	h := newHost(t, 2)
	src := ufBattlefield(t, h, "Name:Spell\nTypes:Sorcery\nOracle:x\n")
	// Universe present but does not contain the named card.
	h.g.NameUniverse = cards.UniverseOf([]*cards.Card{mkCard(t, "Name:Something Else\nTypes:Land\nOracle:x\n")})
	line := "SP$ CopyPermanent | DefinedName$ Mutavault"
	Resolve(h, &Ctx{Source: src, Controller: 0}, sa(t, line))
	for i := range h.g.Objs {
		if o := &h.g.Objs[i]; o.IsToken {
			t.Fatalf("minted a token for an unresolvable DefinedName$: %+v", o.Face())
		}
	}
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "DefinedName$ Mutavault") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a loud Note about the unresolvable DefinedName$; notes %v", ufNotes(h))
	}
}

func containsFold(words []string, want string) bool {
	for _, w := range words {
		if strings.EqualFold(w, want) {
			return true
		}
	}
	return false
}
