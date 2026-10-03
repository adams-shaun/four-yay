package effects

import (
	"math/bits"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// SetSVars binds a copy of the SVar table to a context. A nil input leaves
// c.SVars nil, preserving the defensive-copy convention established by
// copyTargets in context.go.
func SetSVars(c *Ctx, sv map[string]string) {
	if sv == nil {
		c.SVars = nil
		return
	}
	copied := make(map[string]string, len(sv))
	for k, v := range sv {
		copied[k] = v
	}
	c.SVars = copied
}

// SVarBinding is one resolution-scoped SVar publication (Ctx.PublishedSVars).
type SVarBinding struct {
	Name, Value string
}

// publishSVar binds name to value in the resolution's SVar table and records
// the publication, so a resume across a later ask re-binds it.
func publishSVar(c *Ctx, name, value string) {
	if c.SVars == nil {
		c.SVars = make(map[string]string, 1)
	}
	c.SVars[name] = value
	c.PublishedSVars = append(c.PublishedSVars, SVarBinding{Name: name, Value: value})
}

// PublishedSVarsOf is a copy of c's published SVar bindings (nil for a nil
// Ctx or none published): what a suspended resolution's resume point keeps.
func PublishedSVarsOf(c *Ctx) []SVarBinding {
	if c == nil || len(c.PublishedSVars) == 0 {
		return nil
	}
	return append([]SVarBinding(nil), c.PublishedSVars...)
}

// RebindPublishedSVars re-applies a resume point's published SVar bindings,
// in publication order, over a rebuilt Ctx's SVar table (SetSVars first).
func RebindPublishedSVars(c *Ctx, bs []SVarBinding) {
	for _, b := range bs {
		publishSVar(c, b.Name, b.Value)
	}
}

// stripCastSourceAggregate peels the COUNT-level trailing `$<Property>`
// suffix off a Count$ThisTurnCast_<spec> body (the third argument form,
// task: ThisTurnCast extreme suffix), returning the peeled spec and the
// property. It fires ONLY when the segment after the LAST `$` is in the
// admitted vocabulary -- the four extreme reductions (isExtremeProperty)
// plus CardTypes -- so every other suffix keeps the pre-existing whole-token
// fail-closed read byte-identically. The peel happens at the END of the
// whole body because Forge attaches the property to the count, not to the
// last comma alternative: Rootha's `Instant.YouCtrl,Sorcery.YouCtrl$X`
// means "the greatest X among instant AND sorcery spells", and peeling the
// suffix before the comma split leaves both alternatives real predicates.
func stripCastSourceAggregate(spec string) (rest, prop string, ok bool) {
	i := strings.LastIndexByte(spec, '$')
	if i < 0 {
		return "", "", false
	}
	p := spec[i+1:]
	if !isExtremeProperty(p) && p != "CardTypes" {
		return "", "", false
	}
	return spec[:i], p, true
}

// aggregateCastSourceProperty folds a property over the matching casts'
// objects for the COUNT-level trailing `$<Property>` suffix. CardTypes
// counts the DISTINCT card types among the matching casts' faces (April
// O'Neil's "draw a card for each card type among spells you've cast this
// turn"), the zone head's distinct-set read; the extreme reductions take the
// max (or min, for Least) via extremePropertyValue, with zero matches
// yielding 0 rather than a sentinel (the zone fix's seen-guard convention).
// Both folds are order-insensitive, so the match order never reaches an
// event or a view.
func aggregateCastSourceProperty(h Host, ids []state.ObjID, prop string) (int32, bool) {
	g := h.Game()
	if prop == "CardTypes" {
		seen := make(map[string]bool)
		for _, id := range ids {
			o := g.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			for _, typ := range o.Face().Types {
				if cardTypeWords[typ] {
					seen[typ] = true
				}
			}
		}
		return int32(len(seen)), true
	}
	least := isLeastProperty(prop)
	var best int32
	seenAny := false
	for _, id := range ids {
		o := g.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		v := extremePropertyValue(h, o, prop)
		if !seenAny || (least && v < best) || (!least && v > best) {
			best, seenAny = v, true
		}
	}
	return best, true
}

// aggregateCastProperty sums one numeric property over the matching casts'
// objects (the ARGUMENTED !CastSaSource$<Property> aggregate forms' shared
// read; task castprov2). The property vocabulary is the zone-count heads':
// CardManaCost sums the faces' converted costs, CardPower/CardToughness the
// engine's derived (layer-aware) characteristics; any other property is
// unresolvable (0, false) — the whole Count$ then degrades per its caller's
// documented direction. Measured population: CardManaCost x1
// (call_forth_the_tempest); the other two are supported for symmetry.
func aggregateCastProperty(h Host, ids []state.ObjID, prop string) (int32, bool) {
	g := h.Game()
	var n int32
	for _, id := range ids {
		switch prop {
		case "CardManaCost":
			if o := g.Obj(id); o != nil && o.Face() != nil {
				n += o.Face().Cmc()
			}
		case "CardPower":
			n += h.Power(id)
		case "CardToughness":
			n += h.Toughness(id)
		default:
			return 0, false
		}
	}
	return n, true
}

// sumCounters totals a counter slice's POSITIVE counts (a drained slot sits
// in the slice at N == 0 and adds nothing), in slice order -- the ALL
// wildcard's one shared read for both the Count$CardCounters.ALL head and
// the ref-property form. The engine's OWN status markers ("Shield"/"Deathtouched",
// state.InternalCounterMarker) are excluded, the same exclusion
// state.Object.Counter("ALL") applies -- the two ALL reads cannot disagree
// (the marker-exclusion fix, branch agent-20260920T071934Z-c2f52dab).
func sumCounters(cs []state.Counter) int32 {
	var n int32
	for i := range cs {
		if cs[i].N > 0 && !state.InternalCounterMarker(cs[i].Kind) {
			n += cs[i].N
		}
	}
	return n
}

// partyRoles are CR 700.8's four party roles.
var partyRoles = [4]string{"Cleric", "Rogue", "Warrior", "Wizard"}

// partySize is CR 700.8's party size for c's controller: the largest number
// of distinct roles among Cleric, Rogue, Warrior and Wizard that can be
// filled by DIFFERENT creatures they control (a Changeling or a multi-role
// creature fills only one). Each creature's role set is read through the
// ordinary spec matcher (derived types, Changeling), then a 16-state subset
// walk finds the best assignment -- deterministic, no map ranged.
func partySize(g *state.Game, c *Ctx) int32 {
	sc := c.SpecContext(c.Controller)
	var reach [16]bool
	reach[0] = true
	for _, id := range g.Zone(state.ZBattlefield, c.Controller) {
		if !MatchesSpecCtx(g, "Creature", id, sc) {
			continue
		}
		var roles uint8
		for i, r := range partyRoles {
			if MatchesSpecCtx(g, "Creature."+r, id, sc) {
				roles |= 1 << i
			}
		}
		if roles == 0 {
			continue
		}
		next := reach
		for set := 0; set < 16; set++ {
			if !reach[set] {
				continue
			}
			for i := 0; i < 4; i++ {
				if roles&(1<<i) != 0 && set&(1<<i) == 0 {
					next[set|1<<i] = true
				}
			}
		}
		reach = next
	}
	best := 0
	for set := 0; set < 16; set++ {
		if reach[set] {
			if n := bits.OnesCount8(uint8(set)); n > best {
				best = n
			}
		}
	}
	return int32(best)
}
