package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Class keyword's level-up action changes a designation, not a counter.
func init() { Register("ClassLevelUp", effClassLevelUp) }

// effClassLevelUp applies CR 716.2d: activating a Class's level ability sets
// the Class's level to the level that ability indicates. The activator's
// Level$ N carries that target; the emitted Amount is the delta the event
// fold adds, so the designation lands exactly on N. The kw:Class gate keeps
// the activator legal only at exactly N-1 (CR 716.2b/716.2d), so the delta is
// 1 for a legal activation, but computing it keeps the fold correct if the
// designation was moved by anything else.
func effClassLevelUp(h Host, c *Ctx, sa *cards.SA) {
	o := h.Game().Obj(c.Source)
	if o == nil || o.Zone != state.ZBattlefield {
		return
	}
	target := 0
	if v := strings.TrimSpace(sa.ParamStr(cards.PKLevel)); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 2 {
			return
		}
		target = n
	}
	if target == 0 {
		return
	}
	delta := int32(target) - o.ClassLevel()
	if delta <= 0 {
		return
	}
	h.Emit(events.Event{Kind: events.ClassLevelChange, Obj: o.ID, Amount: delta})
}
