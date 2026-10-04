// Counter trigger modes.
//
// Mode$ CounterAdded, CounterAddedOnce, CounterRemoved and CounterRemovedOnce.
//
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package trigmatch

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// counterAddedMatches implements the "when a counter is put on" trigger
// family for BOTH Mode$ CounterAdded (Shang-Chi and the Ten Rings' "When the
// tenth +1/+1 counter is put on NICKNAME") and Mode$ CounterAddedOnce (Simic
// Ascendancy's "Whenever one or more +1/+1 counters are put on a creature you
// control"). The two modes share this matcher because the engine emits ONE
// CounterChange event per placement batch, with the whole batch in Amount:
// "one trigger per counter-placing event, not per counter" is exactly the
// CounterAddedOnce contract, and the modes differ only in that
// CounterAddedOnce never carries CounterAmount$ (measured: 0 corpus lines)
// while its bodies read TriggerCount$Amount instead. The gate is the
// CounterChange
// event that put counters (Amount > 0: a removal event never adds one).
// CounterType$ names the kind. CounterAmount$ <op><n> is the crossing gate
// the card text means: the trigger fires when the put takes the event's
// counter kind's total on the object from below n to at least n -- the tenth
// counter is put whether one event put 10 or a 5-then-5 pair crossed, and a
// batch that overshoots (9+2) also crossed it. A put that does not cross
// (7+1, 11+1) fires nothing. The threshold ops (EQ/GT/GE) all read as that
// one crossing -- the corpus's CounterAdded CounterAmount$ values are EQ only
// (EQ3..EQ12, every one an oracle "when the <Nth> counter is put" gate), so
// EQ is the measured shape and GT/GE collapse onto it unmeasured. An absent
// CounterAmount$ is Forge's plain "whenever a counter is put" -- every put
// admits it.
func counterAddedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.CounterChange || ev.Amount <= 0 {
		return false
	}
	o := e.Game().Obj(ev.Obj)
	if o == nil {
		return false
	}
	if kind := t.ParamStr(cards.PKCounterType); kind != "" && !strings.EqualFold(kind, ev.Counter) {
		return false
	}
	if !eventCardAndPlayerMatch(e, t, source, ev.Obj, o.Controller) {
		return false
	}
	// ValidSource$ names the player PUTTING the counters ("whenever YOU put
	// one or more +1/+1 counters on this creature": Exemplar of Light,
	// Hapatra, Nest of Scarabs -- every corpus CounterAdded/CounterAddedOnce
	// carrier spells it `You`). The CounterChange event records only the
	// recipient, so the putter is read from the same in-flight adder
	// counterPlayerAddedAllMatches and the AddCounter replacement class use;
	// an unattributed placement fails the line closed rather than matching
	// an opponent's Battlegrowth (CR 109.5: "you" is the controller).
	if vs := strings.TrimSpace(t.ParamStr(cards.PKValidSource)); vs != "" {
		adder, ok := e.InFlightCounterAdder()
		if !ok || !effects.MatchesPlayerSpec(e.Game(), vs, adder, e.ControllerOf(source)) {
			return false
		}
	}
	if cmp := t.ParamStr(cards.PKCounterAmount); cmp != "" {
		op, n, ok := SplitCompare(strings.TrimSpace(cmp))
		if !ok {
			return false
		}
		after := o.Counter(ev.Counter)
		before := after - ev.Amount
		if before < 0 {
			before = 0
		}
		// The crossing semantics for the threshold ops: below n before, at
		// least n after. applyCompare(after, op, n) would miss an overshooting
		// batch (9+2 on EQ10: after=11 is not == 10) and re-fire on every
		// later put that lands exactly on n -- the oracle text ("when the
		// tenth counter is put") fires once, on the crossing, so EQ/GT/GE
		// collapse onto before < n && after >= n. LT/LE/NE do not occur in
		// the corpus on this mode; they keep the plain post-event comparison.
		switch effects.CmpOpOf(op) {
		case effects.CmpEQ, effects.CmpGT, effects.CmpGE:
			if !(before < int32(n) && after >= int32(n)) {
				return false
			}
		default:
			if !ApplyCompare(int(after), op, n) {
				return false
			}
		}
	}
	return true
}

// counterRemovedMatches is CounterAdded's mirror for Mode$ CounterRemoved AND
// Mode$ CounterRemovedOnce ("whenever a counter is removed from ~", "when the
// last <kind> counter is removed from ~", "whenever one or more counters are
// removed from ~"): the event is a CounterChange with a NEGATIVE Amount
// (effects/counters.go's removal primitives, rules/turn.go:64's suspend TIME
// upkeep decrement), filtered by CounterType$ (case-insensitive, same as the
// Added arm), ValidCard$/ValidPlayer$ and TriggerZones$ (the shared zoneGate
// already ran). CounterChange carries no player field, so ValidPlayer$ is
// matched against the object's controller -- the convention counterAddedMatches
// uses. NewCounterAmount$ is Forge's "the LAST counter" gate: the post-event
// total for the counter kind must equal the named value -- events.Apply folds
// the removal before checkTriggers runs, so o.Counter(ev.Counter) is already
// the total after. A malformed value fails closed (the Added arm's
// strconv/splitCompare style). Fire-once semantics are the event granularity:
// one Amount: -N batch removal is ONE trigger, exactly as CounterAdded fires
// once per CounterChange, and that IS the CounterRemovedOnce contract --
// "one or more counters removed" is one CounterChange, never one trigger per
// counter. The two modes differ only in that a CounterRemovedOnce body reads
// the removed magnitude through TriggerCount$Amount (Chandra, Fire Artisan's
// "deals that much damage"; B.O.B. Bevy of Beebles; Regenerations Restored),
// which the referent capture in trigger_referents.go supplies. No corpus
// CounterRemovedOnce line carries CounterAmount$/NewCounterAmount$.
func counterRemovedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.CounterChange || ev.Amount >= 0 {
		return false
	}
	o := e.Game().Obj(ev.Obj)
	if o == nil {
		return false
	}
	if kind := t.ParamStr(cards.PKCounterType); kind != "" && !strings.EqualFold(kind, ev.Counter) {
		return false
	}
	if !eventCardAndPlayerMatch(e, t, source, ev.Obj, o.Controller) {
		return false
	}
	// Vanishing's last-counter trigger requires a positive-to-zero
	// transition, not merely a CounterChange whose clamped result is zero.
	// The TIME-removal LKI is captured before Apply in Engine.emit.
	if t.ParamStr(cards.PKKeyword) == "Vanishing" && (lki == nil || lki.Counter("TIME") <= 0) {
		return false
	}
	if want := t.ParamStr(cards.PKNewCounterAmount); want != "" {
		n, err := strconv.Atoi(strings.TrimSpace(want))
		if err != nil {
			return false
		}
		if o.Counter(ev.Counter) != int32(n) {
			return false
		}
	}
	return true
}

// counterPlayerAddedAllMatches implements Mode$ CounterPlayerAddedAll --
// "Whenever you put one or more counters on a(n) <spec>" (Generous Patron,
// Rikku Resourceful Guardian, Kros Defense Contractor): the batch trigger
// that follows the PLAYER putting the counters, not (only) the recipient.
// It fires on BOTH placement events: CounterChange (an object gained
// counters) and PlayerCounterChange (a player gained counters -- The Great
// Goblin's "a permanent or player", All Will Be One's same shape), with
// Amount > 0 (a removal never adds one). The trigger-level params (PlayerTurn$,
// ActivationLimit$, the intervening-if) run through the shared gates like
// every other event mode.
//
// Batch granularity is the EVENT, never the counter: the engine emits one
// placement event per recipient object with the whole batch in Amount, and
// one matching event queues exactly one trigger however many counters that
// one batch carried -- the "one or more" reading, the same
// one-batch-per-action granularity the ChangesZoneAll registration shares
// (rules/trigmatch/zone.go; the api:Dig/Phases/Mill/RepeatEach brackets open it). One activation hitting several recipients
// emits one event per recipient and queues one trigger per MATCHING
// recipient event (each named by its own text); a batch that lands N
// counters on ONE object queues one trigger, not N.
//
// ValidObject$ names the counter RECIPIENT as comma alternatives, object
// and player forms mixed (All Will Be One's
// "Permanent.inRealZoneBattlefield,Player"; the player form is
// matched with MatchesPlayerSpec, the object form with MatchesSpecFrom,
// each alternative by its own form -- a creature spec never matches a
// player recipient and vice versa). A line with no ValidObject$ admits both
// forms. ValidCard$/ValidPlayer$ keep the generic trigger read through
// eventCardAndPlayerMatch for scripts that spell the recipient that way
// (no corpus line of this mode does; measured at the ticket pin).
//
// ValidSource$ names the player CAUSING the placement (Generous Patron's
// ValidSource$ You): a role the CounterChange event does not carry (it
// records the recipient only), read from the engine's in-flight adder
// scratch -- inFlightCounterAdder, the same read the AddCounter
// replacement's ValidSource$ line uses (a published cost/turn-site
// override wins, else the resolving ability's controller, else the line
// fails closed rather than matching every placement).
//
// ValidObjectToSource$ (Bold Plagiarist) is NOT implemented: the
// recipient spec whose You anchors to the counter-putter, with no second
// carrier to pin the reading against -- a trigger carrying it fails closed
// (never fires) rather than matching with a guessed anchor. Measured, the
// only corpus carrier is bold_plagiarist (Aragorn, Company Leader, whose
// earlier census note named it here, in fact uses ValidObject$
// Card.Self+inRealZoneBattlefield).
func counterPlayerAddedAllMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Amount <= 0 {
		return false
	}
	switch ev.Kind {
	case events.CounterChange:
	case events.PlayerCounterChange:
	default:
		return false
	}
	you := e.ControllerOf(source)
	if spec := strings.TrimSpace(t.ParamStr(cards.PKValidObject)); spec != "" {
		matched := false
		for alt := range strings.SplitSeq(spec, ",") {
			alt = strings.TrimSpace(alt)
			if alt == "" {
				continue
			}
			if ev.Kind == events.CounterChange {
				if e.MatchesSpec(alt, ev.Obj, source, you, SpecOpts{}) {
					matched = true
					break
				}
				continue
			}
			if effects.MatchesPlayerSpec(e.Game(), alt, ev.Player, you) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if vs := strings.TrimSpace(t.ParamStr(cards.PKValidSource)); vs != "" {
		adder, ok := e.InFlightCounterAdder()
		if !ok || !effects.MatchesPlayerSpec(e.Game(), vs, adder, you) {
			return false
		}
	}
	if t.ParamStr(cards.PKValidObjectToSource) != "" {
		// Unimplemented recipient anchor (see the doc above): fail closed.
		return false
	}
	if !eventCardAndPlayerMatch(e, t, source, ev.Obj, gainingPlayerOf(e, ev)) {
		return false
	}
	if kind := t.ParamStr(cards.PKCounterType); kind != "" && !strings.EqualFold(kind, ev.Counter) {
		return false
	}
	return true
}

// gainingPlayerOf is the recipient a generic ValidPlayer$ spec matches: the
// event's own player for a PlayerCounterChange put, else the recipient
// object's controller -- the convention counterAddedMatches and
// counterRemovedMatches both use for the same read.
func gainingPlayerOf(e Board, ev events.Event) state.PlayerID {
	if ev.Kind == events.PlayerCounterChange {
		return ev.Player
	}
	if o := e.Game().Obj(ev.Obj); o != nil {
		return o.Controller
	}
	return 0
}

func init() {
	registerTrigMatcher(counterAddedMatches, "CounterAdded", "CounterAddedOnce")
	registerTrigMatcher(counterRemovedMatches, "CounterRemoved", "CounterRemovedOnce")
	registerTrigMatcher(counterPlayerAddedAllMatches, "CounterPlayerAddedAll")
}
