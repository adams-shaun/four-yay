package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// walkBoardFacts are the board-wide answers the battlefield section's
// per-object readers each re-derived: every one is a pure read of the
// active() list or the walk's static snapshot, and the walk is a pure read,
// so one read per walk answers every object. They are taken at the top of
// the battlefield section -- never inside a face probe (faceprobe.go), whose
// flipped face must not leak into a walk-scoped answer.
type walkBoardFacts struct {
	ready bool
	// hasLType / hasGrants are activeSummaryOf(active())'s two facts.
	hasLType  bool
	hasGrants bool
	// addAbility: the walk's Continuous snapshot holds an AddAbility$
	// carrier (actionStaticSource.addAbilityContinuous non-empty).
	addAbility bool
	// kwOK is set when the read happened outside an active() build
	// (activeDepth 0), the only case in which mayHaveDerivedKeywordAnyH reads
	// activeKWHeads; kwMaybe is then whether any active AddKeywords head is
	// one of grantedKWHeads.
	kwOK    bool
	kwMaybe bool
}

// grantedKWHeads are the four heads cards.GrantedKeywordAbility expands
// (mayDeriveKeywordLine's set).
var grantedKWHeads = [...]kwHead{kwhCycling, kwhTypeCycling, kwhSaddle, kwhCrew}

// boardFacts reads the walk's board facts once and publishes them on the
// walk's action-static source, where the mana walk picks them up.
func (w *legalWalk) boardFacts() walkBoardFacts {
	s := &w.actionStatics
	if s.board.ready {
		return s.board
	}
	e := w.e
	outer := e.activeDepth == 0
	sum := e.activeSummaryOf(e.active())
	b := walkBoardFacts{ready: true, hasLType: sum.hasLType, hasGrants: sum.hasGrants,
		addAbility: len(s.addAbilityContinuous()) > 0}
	if outer {
		b.kwOK = true
		for _, h := range e.activeKWHeads {
			for _, hd := range grantedKWHeads {
				if strings.EqualFold(h, hd.s) {
					b.kwMaybe = true
				}
			}
		}
	}
	s.board = b
	return b
}

// manaWalkEmpty reports, without running it, that the mana walk
// (appendAvailableManaAbilitiesGate) returns nothing for o: a single-face,
// face-up object whose face prints no mana ability (walkFaceFacts.mana),
// on a board with no AddAbility$ carrier and no ability grant, and outside
// the CR 305.6 granted-intrinsic block's reach. Those are exactly the walk's
// sources: printed abilities, granted land-type intrinsics, printed
// ManaReflected, AddAbility$ statics and grantedAbilities.
func (w *legalWalk) manaWalkEmpty(b walkBoardFacts, o *state.Object, id state.ObjID, f *cards.Face) bool {
	e := w.e
	if !b.ready || b.addAbility || b.hasGrants || len(o.MergedCards) != 0 {
		return false
	}
	ff := e.walkFaceFactsOf(f)
	if ff == nil || ff.mana || e.faceDownPrintedHides(o) {
		return false
	}
	if b.hasLType && len(e.landTypeWords) > 0 && o.Zone == state.ZBattlefield && e.controllerOf(id) == w.p {
		return false
	}
	return true
}

// pileAbilitiesEmpty reports that the activated-ability offer loop can offer
// nothing from o's printed abilities in zone z: o is a single-face object
// and no ability of its face survives the loop's kind/zone/mana-ness skips in
// z -- counting, when p does not control o, only abilities with an
// Activator$ (a blank one admits the controller alone).
func (w *legalWalk) pileAbilitiesEmpty(o *state.Object, id state.ObjID, f *cards.Face, z state.Zone) bool {
	if len(o.MergedCards) != 0 {
		return false
	}
	ff := w.e.walkFaceFactsOf(f)
	if ff == nil {
		return false
	}
	mask := ff.abZones
	if mask != 0 && w.e.controllerOf(id) != w.p {
		mask = ff.abZonesActivator
	}
	return !zoneBit(mask, z)
}

// grantedKeywordLines is Engine.grantedKeywordLines with its precheck's
// board half (activeKWHeads) answered once per walk: when neither the board
// nor the object can carry one of the four heads the answer is nil.
func (w *legalWalk) grantedKeywordLines(b walkBoardFacts, o *state.Object, id state.ObjID, f *cards.Face) []string {
	e := w.e
	if b.kwOK && !b.kwMaybe && !objectGrantedKWMaybe(o, f) {
		if walkSkipVerify {
			if got := e.grantedKeywordLines(id); len(got) != 0 {
				panic(fmt.Sprintf("rules: granted-keyword skip dropped %q on obj %d", got, id))
			}
		}
		return nil
	}
	return e.grantedKeywordLines(id)
}

// objectGrantedKWMaybe is mayHaveDerivedKeywordAnyH's per-object half over
// grantedKWHeads: the status flags, the printed keyword lines, the intrinsic
// keywords and the keyword counters.
func objectGrantedKWMaybe(o *state.Object, f *cards.Face) bool {
	if o.Cloaked || o.Suspected || o.SuspendGranted {
		return true
	}
	if len(f.Keywords) > 0 {
		for _, h := range grantedKWHeads {
			if f.KeywordLinesHaveHead(h.s, h.id) {
				return true
			}
		}
	}
	for _, k := range o.IntrinsicKeywords {
		if grantedKWHeadMatch(k) {
			return true
		}
	}
	for _, c := range o.Counters {
		if kwName, ok := cards.CounterKeyword(c.Kind); ok && c.N > 0 && grantedKWHeadMatch(kwName) {
			return true
		}
	}
	return false
}

func grantedKWHeadMatch(k string) bool {
	head := cards.KeywordHead(k)
	for _, h := range grantedKWHeads {
		if strings.EqualFold(head, h.s) {
			return true
		}
	}
	return false
}
