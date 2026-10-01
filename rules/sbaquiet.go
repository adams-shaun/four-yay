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
// around it still run on every call. Clone() carries the key only in the
// layer-inert-only form sbaQuietCarry derives (else zero: ep 0 never
// matches), and a direct e.G write in a test with no event is the one
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
	// pushes counts the TriggerPush/AbilityPush events before scanned: the
	// arena may have grown by exactly that many face-less ability objects.
	pushes int
}

// sbaQuietVerifyFlag turns verify mode on in a non-test binary:
// go build -ldflags "-X github.com/adams-shaun/gorge/rules.sbaQuietVerifyFlag=1".
var sbaQuietVerifyFlag string

var sbaQuietVerify = sbaQuietVerifyFlag != ""

// sbaQuietNow reports whether the pass loop would provably apply nothing.
func (e *Engine) sbaQuietNow() bool {
	q := &e.sbaQuiet
	if q.ep <= 0 || e.legendBatch != nil || q.ver != e.continuousVersion || q.objs > len(e.G.Objs) ||
		!e.sbaInputsQuietSince(q) || !e.facelessAppended(q.objs, q.pushes) {
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
// length ep0 and reached its fixed point. A run that deferred on a runtime
// input is never quiet. A run that emitted is quiet at its END when its final
// pass -- the one that found nothing -- skipped no candidate on the call's
// attempt memory (fresh): that pass then read exactly what a fresh call's
// first pass reads from the current state, and found nothing.
func (e *Engine) sbaRecordQuiet(ep0 int, fresh bool) {
	if (len(e.L.Events) != ep0 && !fresh) || e.sbaUnquiet || e.legendBatch != nil {
		e.sbaQuiet = sbaQuietKey{}
		return
	}
	e.active()
	n := len(e.L.Events)
	e.sbaQuiet = sbaQuietKey{ep: n, ver: e.continuousVersion, objs: len(e.G.Objs), dseq: e.derivedSeq}
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
			if ev.Kind == events.TriggerPush || ev.Kind == events.AbilityPush {
				q.pushes++
			}
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
//     never records a quiet key. Positive damage to an object is NOT
//     quiet: lethal damage reads marked damage (its removal is: see below).
//   - the keyword-action markers (pureMarkerKind): Apply writes nothing;
//   - TriggerPush and AbilityPush by a player still in the game: each mints
//     one face-less ability object on the stack (sbaQuietNow holds the arena
//     growth to exactly their count, every appended object face-less). The
//     pass loop reads a non-battlefield object only to cease a token, to
//     sweep a DEPARTED player's objects (this controller has not departed:
//     PlayerLost is not quiet) and in the Saga/dungeon "busy" tests, whose
//     deferral never records a quiet key -- a push can make a recorded
//     board busier, never complete a Saga or dungeon. AbilityPush's
//     ActivatedThisTurn tally on its source is read by no SBA;
//   - a battlefield entry the pass loop provably finds nothing to do with
//     (sbaQuietEntry).
func (e *Engine) sbaQuietEvent(ev *events.Event) bool {
	if pureMarkerKind(ev.Kind) {
		return true
	}
	if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield {
		return e.sbaQuietEntry(ev.Obj)
	}
	switch ev.Kind {
	case events.TriggerPush, events.AbilityPush:
		return int(ev.Player) < len(e.G.Players) && !e.G.Players[ev.Player].Lost
	case events.Note, events.ModeChosen, events.ManaActivate, events.Resolve,
		events.DeclareAttackers, events.DeclareBlockers, events.EndCombatReset,
		events.TargetsChosen, events.LandPlayed, events.LifeChange, events.ClockTick,
		events.DamageProvenance, events.LibraryOrder, events.Shuffle:
		return true
	case events.Damage:
		// Damage to a player, or a non-positive amount to an object
		// (cleanup's marked-damage removal): foldDamage then only lowers
		// o.Damage (floored at zero) -- or, on a non-creature planeswalker
		// or battle, writes nothing -- and the pass loop reads marked damage
		// only to find lethal damage, which less of it cannot newly meet.
		return ev.Obj == 0 || ev.Amount <= 0
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
//     arbitrary condition, read by legendGroups) -- needed only while some
//     controller has a legend pair: legendGroups reads the statics only past
//     its mayHaveLegendPair gate, and no quiet kind can form a pair (a quiet
//     entry is never printed Legendary, and phasing and control moves are
//     not quiet);
//   - no Aura is attached to a player, and every Aura attached to an object
//     has a local derived Enchant spec (specLocal: attachmentSBAs matches it
//     against the bearer, and a non-local spec -- tapped, a count -- could
//     flip on a quiet event).
func (e *Engine) sbaQuietKindsSafe() bool {
	pair := false
	for _, p := range e.G.AliveFrom(0) {
		legends := 0
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			// mayHaveLegendPair's own count, fused into this scan.
			if !pair && !o.PhasedOut && o.Face() != nil && o.Face().IsLegendary() {
				if legends++; legends >= 2 {
					pair = true
				}
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
	if sbaQuietVerify && pair != e.mayHaveLegendPair() {
		panic("rules: SBA kinds-safe legend count disagrees with mayHaveLegendPair")
	}
	return !pair || len(e.activeStatics("IgnoreLegendRule")) == 0
}

// sbaQuietCarry returns the quiet key a clone of e may start with. A clone
// shares e's board and log but not its layer caches, so its derivedSeq means
// nothing against e's: the carried key is re-recorded at the current log head
// with quiet = 2, which admits only layer-inert events after it -- exactly
// the original skip argument, needing no derivedSeq. It is carried only when
// e's own key would skip now on that same argument alone: every event since
// the record is layer-inert (none of the quiet kinds), the registry version
// and the arena are unchanged, and no legend batch is parked. No layer read
// is made, so Clone stays a pure copy.
func (e *Engine) sbaQuietCarry() (sbaQuietKey, bool) {
	q := &e.sbaQuiet
	n := len(e.L.Events)
	if q.ep <= 0 || q.ep > n || q.scanned > n || q.sawQuiet || e.legendBatch != nil ||
		q.ver != e.continuousVersion || q.objs != len(e.G.Objs) {
		return sbaQuietKey{}, false
	}
	for i := max(q.ep, q.scanned); i < n; i++ {
		switch e.L.Events[i].Kind {
		case events.DecisionAsk, events.DecisionMade, events.Priority:
		default:
			return sbaQuietKey{}, false
		}
	}
	return sbaQuietKey{ep: n, objs: len(e.G.Objs), quiet: 2, scanned: n}, true
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

// sbaQuietEntry reports whether a permanent's battlefield entry leaves the
// pass loop with nothing to do. An entry writes the entering object and its
// zone lists (and, through the move fold, other objects' combat, soulbond,
// crew and exile-link fields no SBA reads); every other object's derived
// characteristics are covered by the unmoved derivedSeq sbaInputsQuietSince
// already requires (derived_transparent.go retires only the mover, and only
// when it is no live effect's source). So only the entering object itself
// can be new work, and it is not when it is, at the classification:
//
//   - still on the battlefield, phased in, face up, controlled by a player
//     still in the game, with no static on any face (objectStaticHotOn: so
//     no CDA, no IgnoreLegendRule and no other static the loop consults --
//     sbaQuietKindsSafe's board facts hold with it too) and no merged pile;
//   - carrying no counters (no planeswalker loyalty, Saga lore, battle
//     defense, +1/+1 and -1/-1 pair, deathtouch marker) and no damage;
//   - attached to nothing (attachmentSBAs) and not an Aura (the derived
//     read: an unattached Aura is binned);
//   - not printed Legendary, World, planeswalker, battle or Saga (the legend
//     and world rules and the loyalty/defense/chapter walks read the printed
//     face, or the derived World with a layer-4 effect live -- hasType);
//   - not a creature, or a creature whose toughness is above zero.
//
// No later quiet event can change any of these (each would need a counter,
// damage, attach, control, phase or zone event, none of them quiet), so the
// verdict holds for the rest of the key's life.
func (e *Engine) sbaQuietEntry(id state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Card == nil || o.PhasedOut || o.FaceDown ||
		len(o.MergedCards) > 0 || objectStaticHotOn(o) || len(o.Counters) > 0 || o.Damage != 0 ||
		o.AttachedTo != 0 || o.HasAttachedPlayer ||
		int(o.Controller) >= len(e.G.Players) || e.G.Players[o.Controller].Lost {
		return false
	}
	f := o.Face()
	if f == nil || f.IsLegendary() || f.IsWorld() || f.IsPlaneswalker() || f.IsBattle() {
		return false
	}
	if n, _ := chapterSpec(f); n != 0 {
		return false
	}
	if e.isAura(o) || e.hasType(o, "World") {
		return false
	}
	return !e.IsCreature(id) || e.Toughness(id) > 0
}
