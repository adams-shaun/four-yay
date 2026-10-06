package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// host_cast_ledger.go is the engine's implementation of
// effects.HostCastLedger, the cast-history half of effects.Host's ledger role
// (rules-engine refactor spec W1d): what was cast this turn, by whom, how and
// from where, read from the event log.

// CastThisTurn satisfies effects.Host's CastThisTurn for Count$ThisTurnCast
// (Task 17/Storm): the spells cast this turn by ANY player, counted from
// the same event log spellsCastThisTurn reads, so a replay that rebuilds the
// game derives the identical number -- never a live-only engine counter.
// Storm subtracts one (Count$ThisTurnCast/Minus1) because the resolving
// spell's own PutOnStack is already in the log by the time its trigger
// effect runs, which would otherwise overcount by exactly one.
func (e *Engine) CastThisTurn() int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PutOnStack {
			n++
		}
	}
	return n
}

// SpellsCastThisTurnMatching satisfies effects.Host's
// SpellsCastThisTurnMatching for Count$ThisTurnCast_<spec> (the
// "first/second spell you cast" cost modifiers and triggers): spells put on
// the stack this turn whose object matches the Forge spec. When the spec
// carries a You* qualifier the count scopes to YOU's casts; otherwise it
// counts everyone's. Derived from the event log like CastThisTurn.
func (e *Engine) SpellsCastThisTurnMatching(you state.PlayerID, spec string) int {
	return len(e.spellsCastThisTurnMatching(you, spec, 0))
}

// SpellsCastThisTurnMatchingExcluding is SpellsCastThisTurnMatching with one
// object's own cast excluded -- the bare !CastSaSource qualifier's engine
// reading (the count's "other than the spell being cast" device; effects
// stripBareCastSaSource strips the token and routes here with the ctx
// source). Derived from the event log like CastThisTurn.
func (e *Engine) SpellsCastThisTurnMatchingExcluding(you state.PlayerID, spec string, exclude state.ObjID) int {
	return len(e.spellsCastThisTurnMatching(you, spec, exclude))
}

// EachSpellCastThisTurnMatching satisfies effects.Host's method of the same
// name: the matching casts' OBJECT IDS (the ARGUMENTED !CastSaSource$<Prop>
// aggregate forms' engine side; effects' aggregateCastProperty sums the
// property over them). Derived from the event log like the count forms.
func (e *Engine) EachSpellCastThisTurnMatching(you state.PlayerID, spec string, exclude state.ObjID) []state.ObjID {
	return e.spellsCastThisTurnMatching(you, spec, exclude)
}

func (e *Engine) spellsCastThisTurnMatching(you state.PlayerID, spec string, exclude state.ObjID) []state.ObjID {
	youScoped := strings.Contains(spec, "You")
	// The CastSa count specs (Rain of Riches' gate) are evaluated per cast
	// event against THAT cast's spend window, not the object's latest one —
	// a re-cast object's older cast must not inherit the newer cast's spend
	// — so the backward walk carries a per-caster spend bucket: a negative
	// ManaAdd (a spend event carries no Obj) belongs to the NEXT PutOnStack
	// the walk reaches for its player — the cast it sits above in the log —
	// exactly the window trigmatch.ManaSpentForCast reads for the SA-level ValidSA$
	// family. Specs without a CastSa token take the unchanged per-event
	// chain call (their castProvenanceAdmits strip is event-local and
	// stateless).
	saTokens := castSaTokensIn(spec)
	// Flag tokens (CastSa Spell.Mayhem, Spell.MayPlaySource, Spell.Warp)
	// read the cast's pay-time CastInfo
	// flags rather than a spend bucket: the backward walk records each
	// object's most recent CastInfo flags (latest-first, first write wins)
	// and the push consumes its own cast's entry, so a re-cast object's
	// older cast never inherits the newer cast's flags — the per-event
	// mirror of castSaAdmits' latest-cast read. A plain cast emits no
	// pay-time CastInfo at all, so a missing entry reads as no flags.
	wantFlags := false
	for _, tok := range saTokens {
		if tok.flag != 0 {
			wantFlags = true
		}
	}
	var castFlags map[state.ObjID]uint64
	if wantFlags {
		castFlags = make(map[state.ObjID]uint64)
	}
	// The in-flight cast's own grant walk (queueCascadeTriggers' scratch,
	// rules/cascade.go) counts PRIOR casts only: the Affected$ half of the
	// same static evaluates the current cast's own qualification, and the
	// gate's EQ0 is Forge's "the first" idiom — the twelve AffectedZone$
	// Stack SVarCompare$ gates in the corpus are all EQ0. Outside the walk
	// the count is inclusive (Vengevine's "the second creature spell" EQ2
	// gate is evaluated with the triggering cast in the log and must count
	// it).
	var buckets [8]castSpendFacts
	useAcc := len(saTokens) > 0
	var out []state.ObjID
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		switch ev.Kind {
		case events.CastInfo:
			if wantFlags {
				if _, seen := castFlags[ev.Obj]; !seen {
					castFlags[ev.Obj] = events.FlagsFrom(ev.Counter)
				}
			}
			continue
		case events.ManaAdd:
			if useAcc && ev.Amount < 0 && int(ev.Player) < len(buckets) {
				buckets[ev.Player].spent += -ev.Amount
				if tag, _, ok := state.TypedManaCounter(ev.Counter); ok {
					base, artifact := state.ManaUnitTypes(tag)
					buckets[ev.Player].tagged[base] += -ev.Amount
					if artifact && base != state.TypedArtifact {
						buckets[ev.Player].tagged[state.TypedArtifact] += -ev.Amount
					}
				}
			}
			continue
		case events.PutOnStack:
		default:
			continue
		}
		// This push closes the spend window of the cast it announces: the
		// caster's bucket holds exactly the spends since the walk start,
		// which are this cast's own (plus the caster's own post-payment
		// floating — the trigmatch.ManaSpentForCast convention). Take the facts and
		// reset, so an older cast of the same object (a hand cast before a
		// flashback) does not inherit them and the in-flight cast's window
		// belongs to no counted cast.
		var facts castSpendFacts
		if useAcc && int(ev.Player) < len(buckets) {
			facts = buckets[ev.Player]
			buckets[ev.Player] = castSpendFacts{}
		}
		// The push itself proves a cast exists: the window's ok read.
		facts.ok = true
		// This cast's own pay-time CastInfo flags (see wantFlags above): the
		// entry recorded at the CastInfo the backward walk already passed —
		// the payment runs after the push, so its CastInfo sits BELOW the
		// push in log order — is exactly this cast's.
		var evFlags uint64
		if wantFlags {
			evFlags = castFlags[ev.Obj]
			delete(castFlags, ev.Obj)
		}
		if (e.stackGrantCast != 0 && ev.Obj == e.stackGrantCast) ||
			(e.costCompositionEvent != 0 && i+1 == e.costCompositionEvent) {
			continue
		}
		if exclude != 0 && ev.Obj == exclude {
			continue
		}
		if youScoped && ev.Player != you {
			continue
		}
		matchSpec := spec
		alive := true
		for _, tok := range saTokens {
			var held bool
			if matchSpec, held = admitProvenanceAlternatives(matchSpec, tok.token, castSaTokenHolds(tok, facts, evFlags)); !held {
				alive = false
				break
			}
		}
		if !alive {
			continue
		}
		// The bare wasCastFromYourHandByYou qualifier (the 5 end-step "if you
		// haven't cast a spell from your hand this turn" carriers'
		// Count$ThisTurnCast_Card.wasCastFromYourHandByYou bodies) is
		// evaluated per cast event against the log (task castprov1); the
		// wasCastByYou sibling (task castprov2) rides the same combined read.
		matchSpec, ok := e.castProvenanceAdmits(matchSpec, ev.Obj, you)
		if !ok {
			continue
		}
		if e.matchesSpecFrom(matchSpec, ev.Obj, you, ev.Obj) {
			out = append(out, ev.Obj)
		}
	}
	return out
}

// WasCastFromHandByYou satisfies effects.Host's WasCastFromHandByYou for the
// Count$wasCastFromYourHandByYou branch head (the Myojin cycle's etbCounter
// CheckSVar$ gate) and the Card.wasCastFromYourHandByYou filter predicate:
// obj's latest PutOnStack event names the cast that put it on the stack —
// From is the zone the cast came from, Player the caster. Provenance is
// game-long, so the scan is not bounded by the turn; if the card was later
// cast again from another zone, the latest cast wins. Derived from the event
// log like SpellsCastThisTurnMatching, so a replay derives the same answer.
func (e *Engine) WasCastFromHandByYou(obj state.ObjID, p state.PlayerID) bool {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.PutOnStack && ev.Obj == obj {
			return ev.From == state.ZHand && ev.Player == p
		}
	}
	return false
}

// WasCastFromHand satisfies effects.Host's WasCastFromHand for the BARE
// wasCastFromYourHand filter family (task castprov3 — the "from anywhere
// other than your hand" carriers whose scripts spell the predicate without
// the ByYou suffix: Vega the Watcher, Bilbo Thief in the Night, Mm'menon's
// RestrictValid$): obj's latest PutOnStack event names the cast that put it
// on the stack, and From is the zone that cast came from — ANY caster. Every
// carrier that needs player scoping supplies it elsewhere (measured over the
// 46 raw carrier files: ValidActivatingPlayer$ You on the trigger lines,
// YouCtrl or wasCastByYou in the same Affected$/Count spec). A copy was
// never cast (the rules-side split, castProvenanceAdmits, applies the same
// IsCopy guard; this read answers the log question alone); a card never put
// on the stack (cheated into play) reads false. Derived from the event log
// like WasCastFromHandByYou, so a replay derives the same answer;
// latest-cast-wins.
func (e *Engine) WasCastFromHand(obj state.ObjID) bool {
	from, _, ok := e.latestCastOrigin(obj)
	return ok && from == state.ZHand
}

// WasCastFromExile satisfies effects.Host's WasCastFromExile for the
// Count$wasCastFromExile branch head (task wascastfrom): obj's LATEST
// PutOnStack cast came from EXILE (foretell, warp, may-play — no CastFlags
// bit carries an exile origin). Derived from the event log the way
// WasCastFromHand is, so a replay derives the same answer; a copy was never
// cast (the rules-side split applies that guard, this read answers the log
// question alone); a card never put on the stack reads false.
func (e *Engine) WasCastFromExile(obj state.ObjID) bool {
	from, _, ok := e.latestCastOrigin(obj)
	return ok && from == state.ZExile
}

// CostMovesInWindow satisfies effects.Host's CostMovesInWindow: the cost
// parts obj's own activation paid, selected by kind. It is the ONE channel
// behind the ConditionDefined$ Discarded (CostMoveDiscard, Moria Scavenger's
// "If the discarded card was a creature card"), Returned (CostMoveReturn,
// Wonderscape Sage) and Collected (CostMoveEvidence, Analyze the Pollen's
// "if evidence was collected, instead...") groups. The walk itself (and the
// activation-window boundary rule) is costMovesInWindow's; a new cost kind
// adds a row here rather than a new Host method.
func (e *Engine) CostMovesInWindow(obj state.ObjID, k events.CostMoveKind) []state.ObjID {
	return e.costMovesInWindow(obj, events.CostMoveMatcher(k))
}

// costMovesInWindow walks obj's activation window backward over the event log
// and returns (in log order) every MoveZone event match admits — the cost
// acts obj's OWN activation paid, which is what the ConditionDefined$
// Discarded/Returned groups enumerate. The scan starts at obj's resolving
// wrapper and stops at the first unrelated stack push, step/turn change, pool
// clear or player loss — while crossing obj's OWN push events, because the
// two cost orderings share the one rule: an ability's cost parts are paid
// BEFORE its AbilityPush mints the wrapper (rules/cast.go's activation
// branch), a spell's AFTER its PutOnStack (the spell branch), and no other
// wrapper's push can sit between a cost payment and the resolution that
// follows it. Priority passes are deliberately NOT a boundary: an activated
// ability can sit on the stack across any number of passes before it
// resolves, and the cost it paid belongs to exactly that resolution. Derived
// from the log the way WasCastFromHandByYou is, so a replay derives the same
// answer.
func (e *Engine) costMovesInWindow(obj state.ObjID, match func(events.Event) bool) []state.ObjID {
	if obj == 0 {
		return nil
	}
	// An ability wrapper's AbilityPush carries the SOURCE permanent's id
	// (events.Apply mints the wrapper; Event.Obj names its source), so the
	// scan crosses its own push by wrapper id or source id alike.
	var src state.ObjID
	if o := e.G.Obj(obj); o != nil {
		src = o.Source
	}
	var out []state.ObjID
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		switch ev.Kind {
		case events.MoveZone:
			if match(ev) {
				out = append(out, ev.Obj)
				continue
			}
			// An ordinary move inside the window is not a boundary — an
			// ability's payment can move several cards (exile parts, tapped
			// entries) between its cost payment and its push.
			continue
		case events.PutOnStack, events.AbilityPush, events.TriggerPush,
			events.DelayedPush, events.GrantTriggerPush:
			if ev.Obj == obj || (src != 0 && ev.Obj == src) {
				continue // the resolving object's own push: cross it
			}
			return reverseIDs(out)
		case events.StepChange, events.TurnChange, events.ManaClear, events.PlayerLost:
			return reverseIDs(out)
		}
	}
	return reverseIDs(out)
}

// reverseIDs restores log order to a backward scan's collection.
func reverseIDs(in []state.ObjID) []state.ObjID {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
	return in
}

// WasCast satisfies effects.Host's WasCast (Forge Card.wasCast():
// castFrom != null), the Count$IfCastInOwnMainPhase third conjunct (task
// ifcastmain1). The pending CR 601.2c announcement ask is a cast in progress:
// pushCast runs AFTER targetAsk, so the log scan alone would misread Return
// to Dust's own TargetMax$ X bound as uncast; the live pending cast closes
// that window (Forge sets castFrom before setupTargets). !e.cast.isAbility()
// excludes an ACTIVATED-ABILITY activation (printed or granted, task
// grantcost1), which Forge never treats as a cast. A copy was never cast
// (IsCopy), and a card never put on the stack (cheated into play) reads
// false. Derived from the event log plus the live pending cast, so a replay
// derives the same answer.
func (e *Engine) WasCast(obj state.ObjID) bool {
	if e.cast != nil && e.cast.card == obj && !e.cast.isAbility() {
		return true
	}
	if o := e.G.Obj(obj); o == nil || o.IsCopy {
		return false
	}
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.PutOnStack && ev.Obj == obj {
			return true
		}
	}
	return false
}

// WasCastByYou reports whether card obj was CAST AT ALL by player p — the
// bare wasCastByYou qualifier's engine read (task castprov2: the "When
// CARDNAME enters, if you cast it" ETB family — Zacama, Marina Vendrell's
// Grimoire — and Nine-Lives Familiar's etbCounter gate field): the LATEST
// PutOnStack event for this object names you as caster, whatever zone the
// cast came from (a normal hand cast, a flashback, any origin — the oracle's
// "if you cast it" does not care where from). LATEST-cast, not exists-anywhere:
// the battlefield entry this gate answers for followed the latest cast, so
// that cast is the provenance the oracle means; the corner this leaves is
// you cast it, it left the battlefield again, and an OPPONENT later cast the
// same object — the gate then reads false even though you did cast it
// (measured: no corpus carrier exercises the corner; an exists-scan would
// instead answer true for a card whose latest cast was an opponent's, the
// wider wrong). Copies were never cast; the rules-side split
// (castProvenanceAdmits) applies that guard, this read answers the log
// question alone. Derived from the event log like WasCastFromHandByYou, so
// a replay derives the same answer; a card never put on the stack (cheated
// into play) reads false, and so does a card that left the battlefield after
// that cast and came back without one (reanimated, blinked: CR 400.7).
func (e *Engine) WasCastByYou(obj state.ObjID, p state.PlayerID) bool {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Obj != obj {
			continue
		}
		if ev.Kind == events.MoveZone && ev.From == state.ZBattlefield {
			// It left the battlefield after its latest cast: whatever is asked
			// about now is a new object that was never cast (CR 400.7) --
			// Nine-Lives Familiar's own return must not re-enter with eight.
			return false
		}
		if ev.Kind == events.PutOnStack {
			return ev.Player == p
		}
	}
	return false
}

// DepartureCounters answers the counters obj carried when it last left the
// battlefield (CR 608.2h's last-known information for a reader that has no
// trigger snapshot -- a delayed trigger's remembered card). Derived from the
// event log: the counter changes between its latest battlefield entry and its
// latest departure, folded in order. ok is false
// when the log holds no departure.
func (e *Engine) DepartureCounters(obj state.ObjID) ([]state.Counter, bool) {
	left := -1
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := &e.L.Events[i]
		if ev.Kind == events.MoveZone && ev.Obj == obj && ev.From == state.ZBattlefield {
			left = i
			break
		}
	}
	if left < 0 {
		return nil, false
	}
	entered := 0
	for i := left - 1; i >= 0; i-- {
		ev := &e.L.Events[i]
		if ev.Kind == events.MoveZone && ev.Obj == obj && ev.To == state.ZBattlefield {
			entered = i + 1
			break
		}
	}
	var scratch state.Object
	for i := entered; i < left; i++ {
		ev := &e.L.Events[i]
		// An EntryCounterNotice counts too: its placement was folded inside the
		// entry MoveZone, and the notice is the log's only record of it.
		if ev.Kind == events.CounterChange && ev.Obj == obj {
			scratch.AddCounter(ev.Counter, ev.Amount)
		}
	}
	return scratch.Counters, true
}

// SpellsCastThisTurnBy satisfies effects.Host's SpellsCastThisTurnBy for the
// PlayerCount<group>$Condition<N> SpellsCastThisTurn property (Ertai's
// Scorn / Mindbreak Trap / Whiplash Trap: "for each opponent who cast two
// or more spells this turn"): every PutOnStack naming p since the last
// TurnChange. Derived from the event log like CastThisTurn, so a replay
// derives the same number.
func (e *Engine) SpellsCastThisTurnBy(p state.PlayerID) int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PutOnStack && ev.Player == p {
			n++
		}
	}
	return n
}

// castsThisTurnMatchingBy counts the spells p put on the stack since the last
// TurnChange whose object satisfies match, skipping exclude (the in-flight
// candidate, whose own PutOnStack is already logged at the CR 601.2e
// recheck). A free function over the log so the per-caster limit rule
// (castLimitBinds) needs no Engine receiver.
func castsThisTurnMatchingBy(log []events.Event, p state.PlayerID, exclude state.ObjID, match func(state.ObjID) bool) int {
	n := 0
	for i := len(log) - 1; i >= 0; i-- {
		ev := log[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind != events.PutOnStack || ev.Player != p || (exclude != 0 && ev.Obj == exclude) {
			continue
		}
		if match(ev.Obj) {
			n++
		}
	}
	return n
}

// TurnsTaken satisfies effects.Host's TurnsTaken for Count$YourTurns (Serra
// Avenger's "your first, second, or third turns of the game"): the number of
// turns that have BEGUN with p as the active player, current turn included.
// Derived from the event log like LifeLostThisTurn — every turn p begins
// emits exactly one TurnChange naming p (events.Apply's TurnChange case),
// extra turns included, so a replay that rebuilds the log arrives at the
// same count. The whole-log walk (not a TurnChange-bounded scan) is the
// point: the count spans the game, not one turn.
// CommanderCastsFromCommandZone satisfies effects.Host's method of the
// same name for Count$TotalCommanderCastFromCommandZone: how many times
// player p has cast one of THEIR OWN commanders from the command zone this
// game. It walks the whole log for PutOnStack events whose caster is p,
// origin is the command zone and object is one of p's commanders — the
// exact criteria recordCmdCast (rules/cast.go) applies when it maintains
// the parallel CmdCasts slice, and commitCast's PutOnStack emit is the ONE
// site that can produce such an event, so this head and the CR 903.8 tax
// can never disagree. Whole-game scope like TurnsTaken (the whole-log walk
// is the point); derived from the event log, so a replay derives the same
// number. A non-Commander seat carries an empty Commanders list, so the
// count is 0 there by construction.
func (e *Engine) CommanderCastsFromCommandZone(p state.PlayerID) int32 {
	if p < 0 || int(p) >= len(e.G.Players) {
		return 0
	}
	var n int32
	for _, ev := range e.L.Events {
		if ev.Kind != events.PutOnStack || ev.Player != p || ev.From != state.ZCommand {
			continue
		}
		for _, cid := range e.G.Players[p].Commanders {
			if cid == ev.Obj {
				n++
				break
			}
		}
	}
	return n
}
