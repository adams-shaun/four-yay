package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The restriction and the spent-mana rider must survive the same ManaAdd
// encoding. The SVar is Sunken Palace's real SpellAbilityCast rider; the
// RestrictValid parameter creates the combined carrier absent in the corpus.
func TestRestrictedManaKeepsSpentActivationRider(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	src := card(t, "Name:Restricted Palace\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ T | Produced$ U | RestrictValid$ Activated.Creature | TriggersWhenSpent$ TrigCopy\n"+
		"SVar:TrigCopy:Mode$ SpellAbilityCast | ValidSA$ Spell,Activated | ValidActivatingPlayer$ You | Execute$ TrigCopyMain\n"+
		"SVar:TrigCopyMain:DB$ CopySpellAbility | Defined$ TriggeredSpellAbility | MayChooseTarget$ True\n"+
		"Oracle:Add mana.\n")
	e, _ := colourIdentityGame(t, 922, FormatCommander, corpusCommander(t, reg, "Wort, Boggart Auntie"), nil,
		src, corpusCommander(t, reg, "Wall of Water"))
	producer := moveToBattlefieldByName(t, e, 0, "Restricted Palace")
	wall := moveToBattlefieldByName(t, e, 0, "Wall of Water")
	if e.G.Obj(producer).Zone != state.ZBattlefield || e.G.Obj(wall).Zone != state.ZBattlefield {
		t.Fatal("precondition: producer and ability source must both be on battlefield")
	}
	e.priorityRound()
	activateMana(t, e, producer)
	if len(e.G.Players[0].RestrictedMana) != 1 || e.G.Players[0].RestrictedMana[0].Source != producer || e.G.Players[0].RestrictedMana[0].Valid != "Activated.Creature" {
		t.Fatalf("restriction and rider source must coexist on one batch: %+v", e.G.Players[0].RestrictedMana)
	}
	before := len(e.L.Events)
	e.priorityRound()
	submitChoices(t, e, abilityOption(t, e, wall, 0).Index)
	if len(e.G.Players[0].RestrictedMana) != 0 {
		t.Fatalf("activation did not consume restricted mana: %+v", e.G.Players[0].RestrictedMana)
	}
	count := 0
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.GrantTriggerPush && ev.Obj == producer {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("combined restriction + rider produced %d trigger pushes, want one", count)
	}
}
