package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Skipping a state-based-action pass loop that provably applies nothing.
//
// checkStateBased runs after nearly every Submit and step, and its pass loop
// rescans the battlefield (derived toughness, attachments, sagas, tokens,
// departed players) every time -- although most of those calls follow
// nothing but priority bookkeeping since the previous call, which found
// nothing to do.
//
// The pass loop is a deterministic function of the game state (e.G), the
// continuous-effect registry and a few engine runtime fields. When one run of
// the loop emits no event at all it changed nothing, so a later run from the
// same inputs emits nothing either. sbaQuiet records the key of such a run:
// the log length after it, continuousVersion and len(e.G.Objs). The next
// call skips the loop when every event appended since is layer-inert
// (DecisionAsk, DecisionMade, Priority -- layercache.go's argument: Apply
// writes nothing for the two markers and only g.Priority/g.Passes for
// Priority, which no SBA reads, directly or through the layer path) and the
// other two inputs are unchanged. That is exactly the layer caches' reuse
// key, so the derived characteristics the loop reads are unchanged too.
//
// The runtime fields the loop reads are excluded rather than keyed:
//
//   - e.pending / e.choosing / e.legendBatch decide only whether a found CR
//     704.5j duplicate set is parked (an emitting ask) or deferred (no emit).
//     A deferral sets sbaUnquiet, so a run that deferred never records.
//   - e.pendingTriggers decides whether a concluded Saga is "busy" (its
//     chapter ability still pending). A busy-deferred Saga sets sbaUnquiet.
//   - the per-call attempt memory (sbaAttempts) is local to one call, and a
//     quiet run attempted nothing that emitted; the departed-player sweep
//     re-runs every call but emits nothing once a player is swept, which is
//     precisely a quiet run.
//
// Only the pass loop is skipped: the legend re-pose guard, expireControl,
// reconcileControlStatics, checkGameOver and the departed-decision release
// around it still run on every call. Clone() leaves sbaQuiet zero (ep 0
// never matches), and a direct e.G write in a test with no event is the one
// input the key cannot see -- sbaQuietVerify, on in the rules test binary,
// runs the loop anyway on every would-be skip and panics if it emits.

type sbaQuietKey struct {
	ep   int
	ver  int
	objs int
	// dseq is derivedSeq at the record. quiet caches whether the recorded
	// board admits the quiet-kind extension (sbaQuietKindsSafe): 0 not yet
	// asked, 1 yes, 2 no. It is computed on first need -- the quiet kinds
	// change none of its inputs, so the answer at the record and at the
	// first quiet-kind skip are the same.
	dseq  uint64
	quiet uint8
	// scanned is the log index the incremental classification
	// (sbaInputsQuietSince) has reached, and sawQuiet whether an event
	// before it was a quiet kind rather than layer-inert.
	scanned  int
	sawQuiet bool
}

// sbaQuietVerifyFlag turns verify mode on in a non-test binary:
// go build -ldflags "-X github.com/adams-shaun/gorge/rules.sbaQuietVerifyFlag=1".
var sbaQuietVerifyFlag string

var sbaQuietVerify = sbaQuietVerifyFlag != ""

// sbaQuietNow reports whether the pass loop would provably apply nothing.
func (e *Engine) sbaQuietNow() bool {
	q := e.sbaQuiet
	if q.ep <= 0 || e.legendBatch != nil || q.ver != e.continuousVersion || q.objs != len(e.G.Objs) ||
		!e.sbaInputsQuietSince(&e.sbaQuiet) {
		return false
	}
	// The player-level losses (CR 704.5a/b, 903.10) are re-read on every
	// call: a per-seat scan, cheap next to the battlefield passes, and it
	// keeps a direct life/poison write (a test's board setup, the one input
	// the log key cannot see) from being skipped past.
	for i := range e.G.Players {
		p := &e.G.Players[i]
		if p.Lost {
			continue
		}
		if p.Life <= 0 || p.Counter("POISON") >= 10 {
			return false
		}
		for _, dmg := range p.CmdDamage {
			if dmg >= 21 {
				return false
			}
		}
	}
	return true
}

// sbaRecordQuiet records the key after a pass loop that started at log
// length ep0 and reached its fixed point; only a run that emitted nothing
// and deferred nothing on a runtime input is quiet.
func (e *Engine) sbaRecordQuiet(ep0 int) {
	if len(e.L.Events) != ep0 || e.sbaUnquiet || e.legendBatch != nil {
		e.sbaQuiet = sbaQuietKey{}
		return
	}
	e.active()
	e.sbaQuiet = sbaQuietKey{ep: ep0, ver: e.continuousVersion, objs: len(e.G.Objs), dseq: e.derivedSeq}
}

// sbaInputsQuietSince reports whether nothing the pass loop reads has moved
// since the quiet record q: every event since is layer-inert (the original
// argument above), or -- the quiet-kind extension -- every event since is
// layer-inert or an sbaQuietEvent (the derivedQuietKind set -- Tap, Untap,
// ManaAdd, ManaClear, StepChange -- plus the markers, combat and life kinds
// and the off-battlefield moves it lists), the recorded board admits the
// extension (q.quiet), and
// derivedSeq has not moved (derived_transparent.go: every layer-derived
// characteristic the loop reads -- toughness, types, keywords, names,
// protection -- is unchanged). The derivedQuietKind set writes a permanent's
// Tapped flag, a pool, the step and the combat-mana tallies; the pass loop's direct reads
// (damage, counters, zones, attachments, timestamps, life and poison, the
// Saga and dungeon bookkeeping) are none of them. Its remaining indirect
// reads are what sbaQuietKindsSafe excludes.
func (e *Engine) sbaInputsQuietSince(q *sbaQuietKey) bool {
	n := len(e.L.Events)
	if q.ep > n || q.scanned > n {
		return false
	}
	// The scan is incremental: every event in [q.ep, q.scanned) was already
	// classified by an earlier call on this same key (the log only grows
	// under one key -- a shorter log fails the test above), and q.sawQuiet
	// remembers whether one of them was a quiet kind. A per-event verdict
	// depends on nothing a later accepted event can change (sbaQuietEvent).
	from := max(q.ep, q.scanned)
	for i := from; i < n; i++ {
		ev := &e.L.Events[i]
		switch ev.Kind {
		case events.DecisionAsk, events.DecisionMade, events.Priority:
		default:
			if q.quiet == 2 || !e.sbaQuietEvent(ev) {
				return false
			}
			q.sawQuiet = true
		}
		q.scanned = i + 1
	}
	if !q.sawQuiet {
		return true
	}
	if q.quiet == 0 {
		q.quiet = 2
		if e.sbaQuietKindsSafe() {
			q.quiet = 1
		}
	}
	if q.quiet != 1 {
		return false
	}
	e.active()
	return e.derivedSeq == q.dseq
}

// sbaQuietEvent reports whether ev is a quiet kind for the SBA skip: an event
// whose Apply writes nothing the pass loop reads directly, so -- together
// with an unmoved derivedSeq, which every derivedQuietEvent and every
// off-battlefield move of a non-source keeps -- the pass loop's inputs are
// unchanged. Beyond derivedQuietKind:
//
//   - the markers and tallies derivedQuietEvent admits (Note, ModeChosen,
//     ManaActivate, Resolve, TargetsChosen, LandPlayed, ClockTick,
//     DamageProvenance): Apply writes no object damage, counter, zone,
//     attachment, timestamp, Saga or dungeon state;
//   - the combat kinds (DeclareAttackers, DeclareBlockers, EndCombatReset):
//     the pass loop reads combat state only in ceaseDepartedObjects, for an
//     object attacking a departed player -- and a declaration can only name a
//     player still in the game;
//   - LifeChange and damage to a player: sbaQuietNow re-reads every
//     player's life, poison and commander damage on every call;
//   - LibraryOrder and Shuffle: Apply writes only a library list, whose
//     order no state-based action reads;
//   - an off-battlefield move (offBattlefieldMove: library, hand, graveyard
//     and stack, or into exile) of a non-token no battlefield permanent is
//     attached to. The pass loop reads hidden and stack objects only to cease
//     a token (CR 704.5d), to sweep a departed player and to judge an Aura
//     whose bearer left the battlefield (auraEnchantZoneAdmits, the Animate
//     Dead family) -- and a Saga's or dungeon's "busy" test, whose deferral
//     never records a quiet key. Damage to an object is NOT quiet: lethal
//     damage reads marked damage.
func (e *Engine) sbaQuietEvent(ev *events.Event) bool {
	switch ev.Kind {
	case events.Note, events.ModeChosen, events.ManaActivate, events.Resolve,
		events.DeclareAttackers, events.DeclareBlockers, events.EndCombatReset,
		events.TargetsChosen, events.LandPlayed, events.LifeChange, events.ClockTick,
		events.DamageProvenance, events.LibraryOrder, events.Shuffle:
		return true
	case events.Damage:
		return ev.Obj == 0
	}
	if derivedQuietKind(ev.Kind) {
		return true
	}
	if !offBattlefieldMove(ev) {
		return false
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.IsToken {
		return false
	}
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			if a := e.G.Obj(id); a != nil && a.AttachedTo == ev.Obj {
				return false
			}
		}
	}
	return true
}

// sbaQuietKindsSafe reports whether the pass loop's reads that do not go
// through the layer walk are provably blind to the quiet kinds on this board:
//
//   - no battlefield object prints a characteristic-defining static (its
//     P/T is an arbitrary count, which derivedSeq does not cover -- a
//     "number of untapped lands" toughness can reach zero on a Tap);
//   - no IgnoreLegendRule static is live (its "as long as" gate is an
//     arbitrary condition, read by legendGroups);
//   - no Aura is attached to a player, and every Aura attached to an object
//     has a local derived Enchant spec (specLocal: attachmentSBAs matches it
//     against the bearer, and a non-local spec -- tapped, a count -- could
//     flip on a quiet event).
func (e *Engine) sbaQuietKindsSafe() bool {
	if len(e.activeStatics("IgnoreLegendRule")) > 0 {
		return false
	}
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			if faceHasCDAStatic(o) {
				return false
			}
			if o.HasAttachedPlayer {
				return false
			}
			if o.AttachedTo == 0 || !e.isAura(o) {
				continue
			}
			if param, ok := e.derivedKeywordParamH(o.ID, kwhEnchant); ok {
				spec, _, _ := strings.Cut(param, ":")
				if !specLocal(strings.TrimSpace(spec)) {
					return false
				}
			}
		}
	}
	return true
}

func (e *Engine) verifySBAQuiet(ep0 int) {
	if n := len(e.L.Events); n != ep0 {
		panic(fmt.Sprintf("rules: SBA quiet skip at log %d disagrees with a full pass (%d events emitted, first %v)",
			ep0, n-ep0, e.L.Events[ep0].Kind))
	}
	if e.legendBatch != nil {
		panic(fmt.Sprintf("rules: SBA quiet skip at log %d disagrees with a full pass (legend batch parked)", ep0))
	}
}
