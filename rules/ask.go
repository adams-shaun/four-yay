package rules

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
)

// chooseFor names the flow a pending KChoose decision belongs to. Task 9
// declares chooseCast (rules/cast.go); Tasks 12 and 18 add the "as this
// enters" and miracle cases in their own files.
type chooseFor uint8

const chooseNone chooseFor = iota

// commanderCardLegal reports whether ONE card may be a commander under

func (e *Engine) ask(d *decision.Decision) {
	// The resolution kernel (rules/resolve): a legacy ask inside a tape run
	// ends it (legacy in place, or abort and legacy replay), and one during
	// an exempted resolution is a predicate miss.
	if e.tape.Watching() {
		if tapeCastAsk(e, d) {
			return
		}
		if e.hostAsking == 0 && e.cast == nil && !e.Suspended() && e.offStackMana == nil {
			// An engine flow's own decision, posed with no resolution
			// continuation behind it (the next priority, a combat damage
			// division, a CR 616.1 order from the turn structure): the
			// handler that answers it carries the flow on, so the tape run
			// ends here and the decision is posed as an ordinary one.
			e.tape.Boundary()
		}
		if e.tape.Unserved() {
			tapeLegacyAsked(e, d, true)
		}
		if e.tape.OnAsk() {
			tapeMissed(e, d)
		}
	}
	// CR 903.9 ordering: a commander's zone change parked mid-chain asks its
	// owner at once (parkCommanderZoneMove), but the chain that parked it
	// keeps running -- Path to Exile's exile parks Rakdos, then the same
	// resolution's "its controller may search" poses its own choice. Posing
	// that choice here would OVERWRITE the unanswered commander-zone ask: the
	// parked move is never emitted, the commander stays on the battlefield,
	// and every later park of it is dropped by the queue's dedup (the
	// botbench Phyrexian Altar livelock, seed 9702). The later ask instead
	// waits behind the owner's answer and is posed by Submit once that
	// answer has been applied (drainDeferredAsks). Only a DIFFERENT decision
	// is deferred: a second commander-zone park never reaches ask while one
	// is pending (parkCommanderZoneMove queues it on cmdZone).
	if e.pending != nil && e.pending.Kind == decision.KCommanderZone && d.Kind != decision.KCommanderZone {
		e.deferredAsks = append(e.deferredAsks, d)
		return
	}
	e.searchControlRedirect(d)
	e.controlPlayerRedirect(d)
	// Empty-answer-only tripwire (the class the Squadron Hawk fail-to-find
	// search wedged): a decision whose ONLY legal answer is the empty one
	// (Min 0 with Max 0, or no options at all) can never be answered
	// differently by any seat, so posing it strands the game on an ask a
	// client has no control to send. Every asking primitive in effects goes
	// through effects.Ask, which refuses to post the shape and resolves it
	// silently instead; this boundary guard is what fails a test loudly if
	// any construction site -- here or a future one -- ever posts one
	// anyway. Panic rather than quietly fixing: by the time a decision
	// reaches ask the asking caller has already chosen its resolution path,
	// and silently swallowing it here would leave the caller's suspended
	// half-resolution dangling.
	// One mid-resolution decision at a time (the structural guard for the
	// overwrite class findings-sol4 proved on the life-replacement draw
	// loop): a caller reaching ask while a SUSPENDED RESOLUTION is awaiting
	// its answer would overwrite e.pending and orphan that resolution's
	// ask -- no seat can ever answer it, and the surviving ask's answer is
	// applied to the wrong resolution. Every guarded caller checks
	// e.pending/e.Suspended() before asking (effDraw's cursor loop,
	// lifeReplacementDraw's park, advanceStep's draw-step return, the
	// replacement-choice queue's askNextReplacementChoice, DrawFor's
	// suspended degrade); a caller that does not is a bug of exactly the
	// class those guards exist for. Panic, the same stance as the two
	// checks below: the host crashes the match loudly rather than shipping
	// a log with a decision nobody can answer.
	//
	// A pending decision with e.resume == nil is deliberately NOT a panic,
	// and neither is a pose with e.resume != nil but e.pending == nil: the
	// replacement-order flow (handleReplacement) parks its resume point
	// while it finishes the parked event's remaining work -- a combat pass's
	// completeCombatPass then poses the round's priority with e.pending nil,
	// nothing is displaced, and the parked frame resumes when the engine
	// returns to it. What the guard exists for is the OVERWRITE: an ask
	// displacing a decision a seat has not answered yet. In engine flow
	// nothing emits while such a decision is outstanding (Advance is parked
	// on it and handle runs only after Submit cleared it), but the test
	// probes drive e.emit directly while a setup priority ask is pending,
	// and an emit that poses an ask is then the probe's intent -- the
	// priority ask it displaces is re-granted by the same Submit tail. That
	// displacement is engine-unreachable and probe-owned.
	if effects.OnlyEmptyAnswer(d) {
		panic(fmt.Sprintf("rules: decision %s for seat %d posed with only the empty answer legal (Min %d Max %d, %d options) -- asking primitives must resolve this shape silently (effects.Ask), never post it",
			d.Kind, d.Player, d.Min, d.Max, len(d.Options)))
	}
	// Option.Index/position identity (finding bi). Every decision that can
	// reach a seat flows through ask -- ask is what sets d.Seq and e.pending,
	// so a decision that skipped it is not pending and no seat can answer it
	// -- which makes this the ONE place the invariant is enforced for every
	// construction site at once, including the sites that never call
	// decision.New (all but mulligan's two build the struct literal directly;
	// effects' two mid-resolution asks reach e.pending through Engine.Ask,
	// which calls back into ask below). A mis-indexed list is a programming
	// error, not a bad client answer: Chosen resolves an intent by position,
	// so an option whose Index has drifted off its slot makes the engine
	// resolve a different option than the client named, silently. Panic here
	// rather than return an error because an error hands the caller the
	// choice to swallow it and ship the broken Decision to a seat -- exactly
	// the silent wrong-option failure the invariant exists to make
	// unrepresentable.
	for i := range d.Options {
		if d.Options[i].Index != i {
			panic(fmt.Sprintf("rules: decision option %d has Index %d, want position %d (%s)",
				i, d.Options[i].Index, i, d.Kind))
		}
	}
	d.Seq = uint64(len(e.L.Events))
	// Cache the ordinary offer-walk tail before any opt-in consumer requests
	// the separate payment extension. EnsurePaymentActions may run later, after
	// this ask has returned.
	if d.Kind == decision.KPriority {
		e.recordDerivedMemoTail(d)
	}
	// pendingMintSink records the named mint collector (tokenMintSinkID) this
	// pose runs under, so an as-enters election parked here from inside a
	// resolving DB$ Token's mint emit carries that collector to its answer
	// (rules/turn.go's election arms re-emit the parked entry under
	// withMintSink with it). A pose outside any collector scope records 0 and
	// the arms run unchanged.
	e.pendingMintSink = e.tokenMintSinkID
	e.emit(events.Event{Kind: events.DecisionAsk, Player: d.Player, Text: string(d.Kind)})
	e.pending = d
	e.potentialAskSerial++
}

// decisionMadeText is the DecisionMade event text, byte-identical to
// fmt.Sprintf("%s:%v", kind, choices) ("priority:[0 3]") -- the text is
// hash-chained, so its bytes are fixed -- built without fmt's reflection and
// boxing, since every Submit pays it.
func decisionMadeText(kind decision.Kind, choices []int) string {
	// The common shapes -- no choice or one small choice -- are memoized
	// process-wide (decisionMadeTextTable): the text is a pure function of
	// (kind, choice), and every DecisionMade event built it afresh.
	if k := decisionKindIndex(kind); k >= 0 {
		c := decisionMadeTextNone
		if len(choices) == 1 && choices[0] >= 0 && choices[0] < decisionMadeTextNone {
			c = choices[0]
		} else if len(choices) != 0 {
			return decisionMadeTextBuild(kind, choices)
		}
		slot := &decisionMadeTextTable[k][c]
		if p := slot.Load(); p != nil {
			return *p
		}
		t := decisionMadeTextBuild(kind, choices)
		slot.Store(&t)
		return t
	}
	return decisionMadeTextBuild(kind, choices)
}

// decisionMadeTextNone is the table column of an empty choice list; columns
// below it are the single choice of that value.
const decisionMadeTextNone = 128

var decisionMadeTextTable [13][decisionMadeTextNone + 1]atomic.Pointer[string]

// decisionKindIndex is kind's row in decisionMadeTextTable (-1: none).
func decisionKindIndex(kind decision.Kind) int {
	switch kind {
	case decision.KPriority:
		return 0
	case decision.KTarget:
		return 1
	case decision.KAttackers:
		return 2
	case decision.KBlockers:
		return 3
	case decision.KMulligan:
		return 4
	case decision.KModes:
		return 5
	case decision.KTriggerOrder:
		return 6
	case decision.KTriggerOptional:
		return 7
	case decision.KCommanderZone:
		return 8
	case decision.KChoose:
		return 9
	case decision.KReplacement:
		return 10
	case decision.KArrange:
		return 11
	case decision.KStartingPlayer:
		return 12
	}
	return -1
}

func decisionMadeTextBuild(kind decision.Kind, choices []int) string {
	var sb strings.Builder
	sb.Grow(len(kind) + 3 + 4*len(choices))
	sb.WriteString(string(kind))
	sb.WriteString(":[")
	var num [20]byte
	for i, c := range choices {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.Write(strconv.AppendInt(num[:0], int64(c), 10))
	}
	sb.WriteByte(']')
	return sb.String()
}

// decisionMadePaymentText is the planned-cast extension of decisionMadeText.
// The legacy spelling is deliberately left entirely alone: these bytes are
// chain-bound.  Action and plan are full canonical V1 digests, so this suffix
// binds the submitted witness without making labels or planner order part of
// replay history.
func decisionMadePaymentText(kind decision.Kind, choices []int, payment *decision.PaymentSelection) string {
	text := decisionMadeText(kind, choices)
	if payment == nil {
		return text
	}
	return text + ";payment:" + payment.ActionID + ":" + payment.Plan.ID
}

// drainDeferredAsks poses the front decision ask deferred behind a
// commander-zone choice, once nothing is pending. A deferred decision is
// posed through ask exactly as it would have been, so its DecisionAsk event,
// Seq and any search-control redirect reflect the moment it is actually put
// to a seat.
func drainDeferredAsks(e *Engine) {
	for e.pending == nil && len(e.deferredAsks) > 0 {
		d := e.deferredAsks[0]
		e.deferredAsks = e.deferredAsks[1:]
		if len(e.deferredAsks) == 0 {
			e.deferredAsks = nil
		}
		e.ask(d)
	}
}
