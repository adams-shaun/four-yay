package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Meld", effMeld) }

// effMeld resolves the compiled Meld operands against the source permanent and
// its named battlefield partner. The low-level lifecycle is kept in Meld so
// both this API and direct rules callers share the same event-backed path.
func effMeld(h Host, c *Ctx, sa *cards.SA) {
	p := MeldOf(sa)
	if p == nil || c == nil || p.Primary == "" || p.Secondary == "" || p.Name == "" {
		return
	}
	g := h.Game()
	source := g.Obj(c.Source)
	if source == nil || source.Zone != state.ZBattlefield || source.Face() == nil {
		return
	}
	name := source.Face().Name
	if name != p.Primary && name != p.Secondary {
		return
	}
	partnerName := p.Primary
	if name == p.Primary {
		partnerName = p.Secondary
	}
	var partner state.ObjID
	for _, player := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, player) {
			o := g.Obj(id)
			if o == nil || id == c.Source || o.Face() == nil || o.Face().Name != partnerName ||
				o.Controller != c.Controller {
				continue
			}
			if p.Attacking && (!source.IsAttacking || !o.IsAttacking || source.Attacking != o.Attacking) {
				continue
			}
			partner = id
			break
		}
		if partner != 0 {
			break
		}
	}
	if partner == 0 || source.Controller != c.Controller {
		return
	}
	var defender state.PlayerID
	if p.Attacking {
		defender = source.Attacking
	}
	Meld(h, c.Controller, c.Source, partner, state.MeldOptions{
		ResultName: p.Name, PrimaryName: p.Primary, SecondaryName: p.Secondary,
		Tapped: p.Tapped, Attacking: p.Attacking, Defender: defender,
		DefenderBattle: source.AttackingBattle,
	})
}
