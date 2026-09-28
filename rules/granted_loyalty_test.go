package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// grantedRowanOption is the pending priority decision's "ability" option for
// Rowan's Talent's granted [+1] on the planeswalker pw, if offered.
func grantedRowanOption(e *Engine, pw state.ObjID) (decision.Option, bool) {
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		return decision.Option{}, false
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == pw && strings.Contains(o.Label, "+2/+0 and gains first strike") {
			return o, true
		}
	}
	return decision.Option{}, false
}

// TestGrantedLoyaltyAbilityIsOncePerTurn: Rowan's Talent grants the enchanted
// planeswalker "[+1]: Up to one target creature gets +2/+0 ..." through an
// AddAbility$ SVar grant whose body carries Planeswalker$ True -- a loyalty
// ability of the recipient, so CR 606.3's once-per-permanent-per-turn gate
// applies to it. The SVar-grant offer loop used to exempt every AddAbility$
// body from the loyalty gate, and a bot activated the [+1] 20000 times in one
// main phase (cardfuzz batch1 line 18). After one activation, neither the
// granted [+1] nor any printed loyalty ability of the same permanent is
// offered again this turn.
func TestGrantedLoyaltyAbilityIsOncePerTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := edrBoard(t, reg, 79, map[string]state.Zone{
		"Jaya Ballard":   state.ZBattlefield,
		"Rowan's Talent": state.ZHand,
		"Grizzly Bears":  state.ZBattlefield,
	})
	jaya, talent := ids["Jaya Ballard"], ids["Rowan's Talent"]
	// The Aura enters already attached (an unattached Aura would be put
	// into the graveyard by the CR 704.5m SBA before the attach lands).
	e.emit(events.Event{Kind: events.MoveZone, Obj: talent, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.Attach, Obj: talent, IDs: []state.ObjID{jaya}})
	addMana(t, e, 0, "")
	edrSeatZeroPriority(t, e)
	opt, ok := grantedRowanOption(e, jaya)
	if !ok {
		t.Fatalf("Rowan's Talent's granted [+1] is not offered on the enchanted Jaya Ballard (zone %v)", e.G.Obj(talent).Zone)
	}
	before := e.G.Obj(jaya).Counter("LOYALTY")
	submitChoices(t, e, opt.Index)
	// "Up to one target creature": target the Bears.
	for i := 0; i < 6; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		picked := []int{}
		for _, o := range d.Options {
			if o.Obj == ids["Grizzly Bears"] {
				picked = append(picked, o.Index)
			}
		}
		submitChoices(t, e, picked...)
	}
	passUntilStackEmpty(t, e, 20)
	edrSeatZeroPriority(t, e)
	if got := e.G.Obj(jaya).Counter("LOYALTY"); got != before+1 {
		t.Fatalf("the granted [+1] left LOYALTY=%d, want %d", got, before+1)
	}
	if _, again := grantedRowanOption(e, jaya); again {
		t.Fatal("the granted [+1] is offered a second time this turn (CR 606.3)")
	}
	if d := e.Pending(); d != nil {
		for _, o := range d.Options {
			if o.Kind == "ability" && o.Obj == jaya {
				t.Fatalf("a loyalty ability of Jaya Ballard is still offered after the granted [+1]: %q", o.Label)
			}
		}
	}
	replayCheck(t, e, cfg)
}
