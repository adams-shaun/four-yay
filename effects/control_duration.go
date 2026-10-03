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
		switch parseControlDurationa941Codes.Code(string(strings.TrimSpace(tok))) {
		case parseControlDurationa941Empty:
		case parseControlDurationa941EOT:
			d.EOT = true
		case parseControlDurationa941EndOfCombat:
			d.EndOfCombat = true
		case parseControlDurationa941UntilTheEndOfYourNextTurn:
			d.NextTurn = true
		case parseControlDurationa941LeavesPlay:
			d.LeavesPlay = true
		case parseControlDurationa941Untap:
			d.Untap = true
		case parseControlDurationa941LoseControl:
			d.LoseControl = true
		case parseControlDurationa941UntilSourceUnattached:
			d.Unattached = true
		case parseControlDurationa941StaticCommandCheck:
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
	switch compareCounta942Codes.Code(string(op)) {
	case compareCounta942EQ:
		return left == right
	case compareCounta942NE:
		return left != right
	case compareCounta942LT:
		return left < right
	case compareCounta942LE:
		return left <= right
	case compareCounta942GT:
		return left > right
	case compareCounta942GE:
		return left >= right
	}
	return false
}

const (
	parseControlDurationa941Empty                     uint16 = 1 // ""
	parseControlDurationa941EOT                       uint16 = 2 // "EOT"
	parseControlDurationa941EndOfCombat               uint16 = 3 // "EndOfCombat"
	parseControlDurationa941UntilTheEndOfYourNextTurn uint16 = 4 // "UntilTheEndOfYourNextTurn"
	parseControlDurationa941LeavesPlay                uint16 = 5 // "LeavesPlay"
	parseControlDurationa941Untap                     uint16 = 6 // "Untap"
	parseControlDurationa941LoseControl               uint16 = 7 // "LoseControl"
	parseControlDurationa941UntilSourceUnattached     uint16 = 8 // "UntilSourceUnattached"
	parseControlDurationa941StaticCommandCheck        uint16 = 9 // "StaticCommandCheck"
)

var parseControlDurationa941Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "", Val: parseControlDurationa941Empty},
	state.StrEntry[uint16]{Key: "EOT", Val: parseControlDurationa941EOT},
	state.StrEntry[uint16]{Key: "EndOfCombat", Val: parseControlDurationa941EndOfCombat},
	state.StrEntry[uint16]{Key: "UntilTheEndOfYourNextTurn", Val: parseControlDurationa941UntilTheEndOfYourNextTurn},
	state.StrEntry[uint16]{Key: "LeavesPlay", Val: parseControlDurationa941LeavesPlay},
	state.StrEntry[uint16]{Key: "Untap", Val: parseControlDurationa941Untap},
	state.StrEntry[uint16]{Key: "LoseControl", Val: parseControlDurationa941LoseControl},
	state.StrEntry[uint16]{Key: "UntilSourceUnattached", Val: parseControlDurationa941UntilSourceUnattached},
	state.StrEntry[uint16]{Key: "StaticCommandCheck", Val: parseControlDurationa941StaticCommandCheck},
)

const (
	compareCounta942EQ uint16 = 1 // "EQ"
	compareCounta942NE uint16 = 2 // "NE"
	compareCounta942LT uint16 = 3 // "LT"
	compareCounta942LE uint16 = 4 // "LE"
	compareCounta942GT uint16 = 5 // "GT"
	compareCounta942GE uint16 = 6 // "GE"
)

var compareCounta942Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "EQ", Val: compareCounta942EQ},
	state.StrEntry[uint16]{Key: "NE", Val: compareCounta942NE},
	state.StrEntry[uint16]{Key: "LT", Val: compareCounta942LT},
	state.StrEntry[uint16]{Key: "LE", Val: compareCounta942LE},
	state.StrEntry[uint16]{Key: "GT", Val: compareCounta942GT},
	state.StrEntry[uint16]{Key: "GE", Val: compareCounta942GE},
)
