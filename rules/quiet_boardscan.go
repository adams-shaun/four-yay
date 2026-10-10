package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// quietBoardHit is a bitmask of the board-wide static shapes the proof's
// board reader blocks on. They are the existence tests quietBoardBlocker used
// to ask of materialised views (collectAddAbilityCarriers, three activeStatics
// reads, collectCostStatics), answered by one early-exit pass that builds no
// view and allocates nothing.
type quietBoardHit uint8

const (
	qbhAddAbility quietBoardHit = 1 << iota // Continuous static with a non-blank AddAbility$
	qbhFlash                                // battlefield CastWithFlash static
	qbhPlotZone                             // battlefield PlotZone static
	qbhMayPlay                              // battlefield Continuous static with MayPlay$ True
	qbhCostStatic                           // printed ReduceCost/SetCost static not self-only
)

// quietBoardStaticScan walks the static-hot objects of every alive seat and
// zone the old view collectors walked (seats x staticSourceZones, the shared
// stack once) and reports which board shapes exist. With all false it stops
// at the first hit (the proof only needs "any"); verify mode passes all true
// so each shape can be checked against the view-based answer.
//
// Soundness: this is an OVER-approximation of the view reads. Every static a
// view collector admits is visited here (same hot-id source, the hot subset
// being a superset of every object carrying a static), and a hit applies only
// gates the collector also applied, evaluated lazily on a candidate, so a
// candidate this pass rejects is one the collector rejected too. Nothing the
// collectors kept can be missed; extra hits only cost coverage.
//
// The effect-delivered cost statics (appendEffectCostStatics) are not read
// here: continuousMintsCostStatic, run before this in quietBoardBlocker, is a
// superset of that arm's mint conditions.
func (e *Engine) quietBoardStaticScan(all bool) quietBoardHit {
	var hit quietBoardHit
	nPlayers := len(e.G.Players)
	stackDone := false
	for pi := 0; pi < nPlayers; pi++ {
		p := state.PlayerID(pi)
		if e.G.Players[pi].Lost {
			continue
		}
		for _, z := range staticSourceZones {
			if z == state.ZStack {
				if stackDone {
					continue
				}
				stackDone = true
			}
			for _, id := range e.staticSourceIDs(p, z) {
				o := e.G.Obj(id)
				if o == nil || o.Card == nil {
					continue
				}
				f := o.Face()
				if f == nil {
					continue
				}
				if len(o.MergedCards) != 0 {
					for si, sn := 0, o.PileStaticCount(); si < sn; si++ {
						pst, ok := o.PileStaticAt(si)
						if !ok {
							continue
						}
						hit |= e.quietStaticHit(o, z, &pst.Static, pst.Face == f)
						if hit != 0 && !all {
							return hit
						}
					}
					continue
				}
				for si := range f.Statics {
					hit |= e.quietStaticHit(o, z, &f.Statics[si], true)
					if hit != 0 && !all {
						return hit
					}
				}
			}
		}
	}
	return hit
}

// quietStaticHit classifies one static of o (in zone z) and returns the board
// shape it opens, or 0. topFace is true when st belongs to o's current face
// (the Room door-lock gate applies to those only, as scanActiveStatics').
func (e *Engine) quietStaticHit(o *state.Object, z state.Zone, st *cards.Static, topFace bool) quietBoardHit {
	var h quietBoardHit
	switch st.ModeKind() {
	case cards.StaticContinuous:
		if v, _ := cards.ParamSetParam(st.ParamSetOf(), st.Params, cards.PKAddAbility); strings.TrimSpace(v) != "" {
			h |= qbhAddAbility
		}
		if z == state.ZBattlefield {
			if v, _ := cards.ParamSetParam(st.ParamSetOf(), st.Params, cards.PKMayPlay); strings.EqualFold(strings.TrimSpace(v), "True") {
				h |= qbhMayPlay
			}
		}
	case cards.StaticCastWithFlash:
		if z == state.ZBattlefield {
			h |= qbhFlash
		}
	case cards.StaticReduceCost, cards.StaticSetCost:
		if v, has := cards.ParamSetParam(st.ParamSetOf(), st.Params, cards.PKValidCard); !(has && validCardSpecIsSelf(v)) {
			h |= qbhCostStatic
		}
	case 0:
		// PlotZone is outside the dense StaticMode vocabulary.
		if z == state.ZBattlefield && st.Mode == "PlotZone" {
			h |= qbhPlotZone
		}
	}
	if h == 0 {
		return 0
	}
	// A candidate: apply the collectors' own object gates (all pure reads),
	// each of which only ever removes a hit.
	if z == state.ZBattlefield && o.PhasedOut {
		return 0
	}
	if z != state.ZBattlefield && offBattlefieldStaticsInert(z, o) {
		return 0
	}
	if e.printedAbilitiesGone(o) {
		return 0
	}
	if h&(qbhFlash|qbhPlotZone|qbhMayPlay) != 0 && topFace && isRoom(o) && !o.DoorUnlocked(int(o.FaceIdx)) {
		h &^= qbhFlash | qbhPlotZone | qbhMayPlay
	}
	if h&qbhCostStatic != 0 && !cardsEffectZoneOK(st, o.Zone) {
		h &^= qbhCostStatic
	}
	if h&^qbhCostStatic != 0 && !staticEffectZoneOK(*st, o.Zone) {
		h &^= qbhAddAbility | qbhFlash | qbhPlotZone | qbhMayPlay
	}
	return h
}

// cardsEffectZoneOK is the cost scan's EffectZone$ admission for st.
func cardsEffectZoneOK(st *cards.Static, z state.Zone) bool {
	v, _ := cards.ParamSetParam(st.ParamSetOf(), st.Params, cards.PKEffectZone)
	return effectZoneOK(v, z)
}

// quietBoardStaticGuard asserts, in verify mode, that the existence scan is at
// least the old view-based answer for every shape.
func (e *Engine) quietBoardStaticGuard(p state.PlayerID) {
	got := e.quietBoardStaticScan(true)
	check := func(old bool, bit quietBoardHit, name string) {
		if old && got&bit == 0 {
			panic(fmt.Sprintf("rules: quiet board static scan missed %s that the view collector found (turn %d)", name, e.G.Turn))
		}
	}
	check(len(e.collectAddAbilityCarriers()) > 0, qbhAddAbility, "an AddAbility$ carrier")
	check(len(e.activeStatics("CastWithFlash")) > 0, qbhFlash, "a CastWithFlash static")
	check(len(e.activeStatics("PlotZone")) > 0, qbhPlotZone, "a PlotZone static")
	mayPlay := false
	for _, sv := range e.activeStatics("Continuous") {
		if strings.EqualFold(strings.TrimSpace(sv.ParamStr(cards.PKMayPlay)), "True") {
			mayPlay = true
			break
		}
	}
	check(mayPlay, qbhMayPlay, "a MayPlay$ True Continuous static")
	// The proof passes board=false to the may-play enumerators on the strength
	// of the scan: the scan found no MayPlay$ True static, so the board must
	// read closed (the caller already returned on any active MayPlay effect).
	if got&qbhMayPlay == 0 && e.mayPlayBoardGrantsOpen(p) {
		panic(fmt.Sprintf("rules: quiet proof treats the may-play board as closed for seat %d but mayPlayBoardGrantsOpen is true (turn %d)", p, e.G.Turn))
	}
	cs := e.collectCostStatics()
	// The effect-delivered arm is covered by continuousMintsCostStatic, which
	// the caller ran first; only a printed (non-effect) view is compared.
	check(quietCostStaticBoardBlocker(&cs) && !e.quietCostGrantLive(), qbhCostStatic, "a reduce/set cost static")
}

// quietCostGrantLive reports whether some active effect mints a cost static
// (continuousMintsCostStatic), the case the caller already blocked on.
func (e *Engine) quietCostGrantLive() bool {
	ces := e.active()
	for i := range ces {
		if continuousMintsCostStatic(&ces[i]) {
			return true
		}
	}
	return false
}
