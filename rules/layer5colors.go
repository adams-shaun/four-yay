package rules

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// The layer-5 derived-colour table: the bridge between rules' layer walk and
// the effects tier's colour predicates (Black, nonBlack, Colorless,
// MultiColor, ChosenColor, SharesColorWith).
//
// Those predicates read an object's PRINTED colours (effects.ColorMaskOf): a
// colour a continuous effect set in layer 5 (CR 613.1e -- a SetColor$/
// AddColor$ static, an Animate's Colors$) was applied by Derived and seen by
// every direct colour read (protection, Fear, convoke), but not by the filter
// grammar, so a black creature made green and white by Witness Protection was
// still "nonBlack"-illegal for Doom Blade. matchesSpec binds this table on
// SpecContext.DerivedColors for a spec that names a colour word, exactly as
// DerivedTypes carries layer 4 and EffectiveNames layer 3.
//
// Unlike those two tables it is built ON DEMAND, not after every event: only
// a colour-reading spec asks, and only a board with a live layer-5 effect
// (active()'s summary) builds anything, so a match with no colour effect pays
// one cached probe per colour spec. The key is active()'s own -- log head,
// continuous version, object count -- because the derived colours are a pure
// function of the board at that key.
//
// Scope: battlefield objects. An AffectedZone$ colour grant reaching another
// zone (Painter's Servant's cards in hand) keeps the printed read in the
// filter grammar, as before.

// derivedColorTable returns the entries for the battlefield objects whose
// derived colours differ from their printed ones, nil when there are none.
func (e *Engine) derivedColorTable() []effects.ObjectColors {
	if e.colorsBuilding || e.derivedDepth != 0 {
		// Mid-derivation (a layer-7 amount counting by colour, or this very
		// build's Derived reads): the table is not readable; the printed
		// read stands, as it did before the table existed.
		return nil
	}
	act := e.active()
	if !e.activeSummaryOf(act).hasLColor {
		return nil
	}
	if n := len(e.L.Events); e.colorsValid && e.colorsEpoch == n && e.colorsVersion == e.continuousVersion && e.colorsObjs == len(e.G.Objs) {
		return e.layer5Colors
	}
	e.colorsBuilding = true
	defer func() { e.colorsBuilding = false }()
	buf := e.layer5Colors[:0]
	add := func(o *state.Object) {
		if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
			return
		}
		printed := effects.ColorMaskOf(o)
		if derived := effects.ColorMaskFromLetters(e.Derived(o.ID).Colors); derived != printed {
			buf = append(buf, effects.ObjectColors{ID: o.ID, Mask: derived})
		}
	}
	// When every live colour effect names a bounded object set (an Aura's
	// Creature.EnchantedBy, a manland's Card.Self) only those objects can
	// carry an entry (setname.go's bounded reach); they are visited in id
	// order, the whole walk's order.
	var arr [layer4MaxSelfSources]state.ObjID
	reach, bounded := arr[:0], true
	for i := range act {
		ce := &act[i]
		if ce.Layer != LColor {
			continue
		}
		bits := affectsReach(ce.Affects)
		if bits == 0 {
			bounded = false
			break
		}
		if bits&reachSelf != 0 && ce.Source != 0 {
			reach, bounded = appendReach(reach, ce.Source)
		}
		if bounded && bits&reachAttached != 0 {
			if s := e.G.Obj(ce.Source); s != nil && s.Zone == state.ZBattlefield && s.AttachedTo != 0 {
				reach, bounded = appendReach(reach, s.AttachedTo)
			}
		}
		if !bounded {
			break
		}
	}
	if bounded {
		slices.Sort(reach)
		for _, id := range reach {
			add(e.G.Obj(id))
		}
	} else {
		// e.G.Objs is append-ordered, so this walk is deterministic.
		for i := range e.G.Objs {
			add(&e.G.Objs[i])
		}
	}
	// A face-down permanent is colourless (CR 708.5) whatever its printed
	// face says; the bounded walk above does not visit it, and the filter
	// tier's own face-down handling is unchanged by this table.
	e.layer5Colors = buf
	e.colorsValid, e.colorsEpoch, e.colorsVersion, e.colorsObjs = true, len(e.L.Events), e.continuousVersion, len(e.G.Objs)
	if len(buf) == 0 {
		return nil
	}
	return buf
}

// computeSpecReadsColors reports whether a filter spec can name a colour predicate,
// textually and as a superset: every colour word of the grammar -- the five
// colour names (and their non<X> negations), Colorless, MultiColor,
// MonoColor, ChosenColor, SharesColorWith, Worthy -- contains one of these
// capitalised fragments. A spec containing none never reads a colour, so
// binding the table for it would be dead work. matchesSpec asks it through
// the cached front (specderived.go's specBindFacts).
func computeSpecReadsColors(spec string) bool {
	return strings.Contains(spec, "Color") || strings.Contains(spec, "Black") || strings.Contains(spec, "White") ||
		strings.Contains(spec, "Blue") || strings.Contains(spec, "Red") || strings.Contains(spec, "Green") ||
		strings.Contains(spec, "Worthy")
}

// EffectiveColors publishes the table to the effects tier (effects'
// colorTableHost), which asks at each resolving body that names a colour
// word and binds it on the resolving Ctx.
func (e *Engine) EffectiveColors() []effects.ObjectColors { return e.derivedColorTable() }
