package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// ControlDuration is a GainControl's parsed LoseControl$ list. Forge names
// each way the control effect can end; the effect lasts until the FIRST of
// them happens. The zero value is a permanent control change.
//
// Measured over the corpus (`LoseControl$` values, 169 files): EOT 127,
// LeavesPlay,LoseControl (either order) 20, LeavesPlay 11,
// UntilTheEndOfYourNextTurn 5, Untap,LeavesPlay,LoseControl 5,
// Untap,LeavesPlay 3, and one each of UntilSourceUnattached,
// StaticCommandCheck, Untap,LeavesPlay,LoseControl,StaticCommandCheck and
// EndOfCombat. Every token in that census is modelled here.
type ControlDuration struct {
	EOT         bool // CR 514.2: until end of turn
	EndOfCombat bool // CR 511.3: until end of combat
	NextTurn    bool // until the end of the effect controller's next turn
	LeavesPlay  bool // for as long as the source remains on the battlefield
	Untap       bool // for as long as the source remains tapped
	LoseControl bool // for as long as the effect's controller controls the source
	Unattached  bool // for as long as the triggering Aura remains attached to it
	StaticCheck bool // for as long as StaticCommandCheckSVar$ fails its compare
}

// Permanent reports a control change with no ending condition.
func (d ControlDuration) Permanent() bool { return d == ControlDuration{} }

// ParseControlDuration parses LoseControl$. unknown names the first token
// this build does not model; a caller must then not change control at all,
// because a silently permanent steal is the wrong answer for any lifetime.
func ParseControlDuration(raw string) (d ControlDuration, unknown string) {
	for tok := range strings.SplitSeq(raw, ",") {
		switch parseControlDurationCodes.Code(string(strings.TrimSpace(tok))) {
		case parseControlDurationEmpty:
		case parseControlDurationEOT:
			d.EOT = true
		case parseControlDurationEndOfCombat:
			d.EndOfCombat = true
		case parseControlDurationUntilTheEndOfYourNextTurn:
			d.NextTurn = true
		case parseControlDurationLeavesPlay:
			d.LeavesPlay = true
		case parseControlDurationUntap:
			d.Untap = true
		case parseControlDurationLoseControl:
			d.LoseControl = true
		case parseControlDurationUntilSourceUnattached:
			d.Unattached = true
		case parseControlDurationStaticCommandCheck:
			d.StaticCheck = true
		default:
			return ControlDuration{}, strings.TrimSpace(tok)
		}
	}
	return d, ""
}

// ControlGrant is one control-changing effect as the engine tracks it.
// Stamps are the battlefield timestamps of the controlled object and of the
// source when the effect began: a different timestamp is a different object
// (CR 400.7), so a source that left and came back does not keep the effect
// alive, and a stolen permanent that re-entered is no longer affected.
type ControlGrant struct {
	Obj         state.ObjID
	ObjStamp    uint32
	Previous    state.PlayerID // controller immediately before this effect
	Controller  state.PlayerID // controller this effect gives the object
	You         state.PlayerID // the controller of the GainControl effect
	Source      state.ObjID
	SourceStamp uint32
	Aura        state.ObjID // UntilSourceUnattached: the attachment that must remain
	Duration    ControlDuration
	// CheckSVar is StaticCommandCheckSVar$'s count, evaluated with the
	// controlled object as its host; Compare is StaticCommandSVarCompare$
	// (operator plus a literal or an SVar evaluated with the source as host).
	CheckSVar string
	Compare   string
	SVars     map[string]string
	// AddKeywords is GainControl's AddKWs$ ("gains haste" on a threaten
	// effect): keywords the object has for as long as this control effect
	// lasts.
	AddKeywords []string
}

// battlefieldStamped returns id only while it is the same battlefield object
// it was when stamp was taken.
func battlefieldStamped(g *state.Game, id state.ObjID, stamp uint32) *state.Object {
	o := g.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Timestamp != stamp {
		return nil
	}
	return o
}

// ControlGrantEnded evaluates the state-based "for as long as" terms of a
// grant (CR 611.2b). The turn-structure terms (EOT, EndOfCombat, NextTurn)
// end at fixed points the engine owns and are not read here. It is also the
// CR 611.2b check at the moment the effect would begin: a duration that has
// already ended means the effect does nothing.
func ControlGrantEnded(h Host, gr ControlGrant) bool {
	g := h.Game()
	d := gr.Duration
	if d.LeavesPlay || d.Untap || d.LoseControl {
		// "Remains tapped" and "you control CARDNAME" are both false for a
		// source that is no longer the same battlefield object.
		src := battlefieldStamped(g, gr.Source, gr.SourceStamp)
		if src == nil {
			return true
		}
		if d.Untap && !src.Tapped {
			return true
		}
		if d.LoseControl && src.Controller != gr.You {
			return true
		}
	}
	if d.Unattached {
		aura := g.Obj(gr.Aura)
		if aura == nil || aura.Zone != state.ZBattlefield || aura.AttachedTo != gr.Obj {
			return true
		}
	}
	if d.StaticCheck {
		obj := g.Obj(gr.Obj)
		if obj == nil || len(gr.Compare) < 3 {
			return true
		}
		left := EvalCount(h, NewCtxPtr(gr.Obj, obj.Controller, CtxInit{SVars: gr.SVars}), gr.CheckSVar)
		op, rhs := strings.ToUpper(gr.Compare[:2]), strings.TrimSpace(gr.Compare[2:])
		right, err := strconv.Atoi(rhs)
		if err != nil {
			body, ok := gr.SVars[rhs]
			if !ok {
				return true
			}
			right = int(EvalCount(h, NewCtxPtr(gr.Source, gr.You, CtxInit{SVars: gr.SVars}), body))
		}
		if compareCount(op, int(left), right) {
			return true
		}
	}
	return false
}

func compareCount(op string, left, right int) bool {
	switch CmpOpOf(op) {
	case CmpEQ:
		return left == right
	case CmpNE:
		return left != right
	case CmpLT:
		return left < right
	case CmpLE:
		return left <= right
	case CmpGT:
		return left > right
	case CmpGE:
		return left >= right
	}
	return false
}

type parseControlDurationCode uint16

const (
	parseControlDurationEmpty parseControlDurationCode = iota + 1
	parseControlDurationEOT
	parseControlDurationEndOfCombat
	parseControlDurationUntilTheEndOfYourNextTurn
	parseControlDurationLeavesPlay
	parseControlDurationUntap
	parseControlDurationLoseControl
	parseControlDurationUntilSourceUnattached
	parseControlDurationStaticCommandCheck
)

var parseControlDurationCodes = state.NewStrCodes(
	state.StrEntry[parseControlDurationCode]{Key: "", Val: parseControlDurationEmpty},
	state.StrEntry[parseControlDurationCode]{Key: "EOT", Val: parseControlDurationEOT},
	state.StrEntry[parseControlDurationCode]{Key: "EndOfCombat", Val: parseControlDurationEndOfCombat},
	state.StrEntry[parseControlDurationCode]{Key: "UntilTheEndOfYourNextTurn", Val: parseControlDurationUntilTheEndOfYourNextTurn},
	state.StrEntry[parseControlDurationCode]{Key: "LeavesPlay", Val: parseControlDurationLeavesPlay},
	state.StrEntry[parseControlDurationCode]{Key: "Untap", Val: parseControlDurationUntap},
	state.StrEntry[parseControlDurationCode]{Key: "LoseControl", Val: parseControlDurationLoseControl},
	state.StrEntry[parseControlDurationCode]{Key: "UntilSourceUnattached", Val: parseControlDurationUntilSourceUnattached},
	state.StrEntry[parseControlDurationCode]{Key: "StaticCommandCheck", Val: parseControlDurationStaticCommandCheck},
)
