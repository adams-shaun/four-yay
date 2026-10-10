package rules

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/state"
)

// Derived-transparent rebuilds.
//
// The Derived memo's cross-walk reuse (derivedmemo.go) used to key on
// activeBuildSeq, which every non-inert event moves. Measured on the search
// bench (az leg, 32 games at 100 sims), 78.6M of the 115.7M derivations the
// bot's board build asks for missed that key, and 77.7M of those misses
// recomputed exactly the value the stale entry held: a mana tap, a pool
// change or a step change rebuilt active() and so retired every entry,
// though no derivation can read what those events write.
//
// derivedSeq is the memo's own key. It moves with every active() rebuild
// EXCEPT one that is provably transparent to every derivation:
//
//  1. every event logged since the previous build (or layer-inert re-stamp)
//     is a layer-inert kind (layercache.go) or a derivedQuietEvent --
//     Tap and Untap (they write o.Tapped and nothing else), ManaAdd and
//     ManaClear (they write a player's mana pool), StepChange (it writes
//     g.Step and the combat-mana bookkeeping), and the markers, combat,
//     life, clock and non-counter damage kinds derivedQuietEvent lists;
//  2. the continuous registry version and the object arena are unchanged;
//     An off-battlefield zone move (offBattlefieldMove) is admitted too: it
//     changes only the moved object's own derivation, so that object's memo
//     entries are retired instead (and the rebuild is not transparent if it
//     is a live effect's source);
//  3. the rebuilt list is equal to the previous one in every field a
//     derivation reads (derivedEffectEqual), in the same order -- so a
//     duration expiring at a step boundary, a static gate the event flipped,
//     or any static scan output change is a content change and moves the key;
//  4. every effect the derivation reads is local (derivedEffectLocal) --
//     required even across a run of layer-inert events alone, because a
//     forced rebuild (cascade.go's scratch window) can change a non-event
//     input a Count$ amount reads: its Affected$ spec
//     uses only predicates that read the candidate's own characteristics,
//     counters and controller and the effect's source and remembered set
//     (none of which a quiet event writes -- in particular none reads
//     tapped/untapped), and its P/T amounts are integer literals (a Count$
//     amount could read a pool, the step or a tapped count).
//
// The derived object's own fields are the remaining input: Tap writes its
// Tapped flag, which no derivation step reads outside an Affected$ predicate
// (and no local predicate reads it), and an object whose face carries a
// characteristic-defining P/T static is never stamped reusable at all
// (derivedMemoizedAt), because cdaSetPT evaluates an arbitrary count.
//
// Everything else that moves activeBuildSeq -- a re-entrant build, an explicit
// retirement (retireCrossWalkMemo) -- moves derivedSeq too. In the rules test
// binary derivedMemoVerify recomputes every cross-walk hit and panics on a
// difference, which checks this argument over the whole suite.

// derivedQuietKind is the set of non-layer-inert event kinds whose Apply
// writes only state no local derivation reads (condition 1 above). The SBA
// quiet skip shares it (sbaquiet.go): its pass loop reads none of it either.
func derivedQuietKind(k events.Kind) bool {
	switch k {
	case events.Tap, events.Untap, events.ManaAdd, events.ManaClear, events.StepChange:
		return true
	}
	return false
}

// derivedQuietEvent widens derivedQuietKind for the Derived memo alone. Each
// kind's Apply writes only fields the layer walk never reads and no local
// predicate tests:
//
//   - Note, ModeChosen, ManaActivate: pure markers, Apply writes nothing;
//   - Resolve: the per-turn resolved-ability tally;
//   - DeclareAttackers, DeclareBlockers, EndCombatReset: the combat fields
//     (IsAttacking, Attacking, AttackingBattle, AttacksThisTurn, BlockedBy)
//     and the blocker census -- no local predicate reads combat;
//   - TargetsChosen: a stack object's chosen targets;
//   - LandPlayed, LifeChange: a player's land count and life total;
//   - ClockTick: the game clock (timestamps already stamped are unchanged);
//   - DamageProvenance: the damage-provenance records;
//   - Damage to a player, or to an object whose printed face is neither a
//     planeswalker nor a battle: marked damage and the per-turn damage
//     tallies (foldDamage converts a walker's or battle's damage into
//     LOYALTY/DEFENSE counters, which a local counters_ predicate reads);
//   - LibraryOrder, Shuffle: a player's library list, nothing else -- no
//     object field (a derivation reads its object's own fields, never its
//     position in a zone list), and only a non-local spec (TopLibrary and
//     the like are not local predicates) could read the order;
//   - the keyword-action markers (pureMarkerKind): Apply writes nothing.
//
// It is NOT safe for the SBA skip: lethal damage reads marked damage.
func (e *Engine) derivedQuietEvent(ev *events.Event) bool {
	if pureMarkerKind(ev.Kind) {
		return true
	}
	switch ev.Kind {
	case events.Note, events.ModeChosen, events.ManaActivate, events.Resolve,
		events.DeclareAttackers, events.DeclareBlockers, events.EndCombatReset,
		events.TargetsChosen, events.LandPlayed, events.LifeChange, events.ClockTick,
		events.DamageProvenance, events.LibraryOrder, events.Shuffle:
		return true
	case events.Damage:
		if ev.Obj == 0 {
			return true
		}
		o := e.G.Obj(ev.Obj)
		if o == nil {
			return false
		}
		f := o.Face()
		return f != nil && !f.IsPlaneswalker() && !f.IsBattle()
	}
	return derivedQuietKind(ev.Kind)
}

// pureMarkerKind reports whether k is a keyword-action record whose Apply
// writes nothing at all (events/apply.go: each case is a documented no-op
// marker matched only by its trigger mode).
func pureMarkerKind(k events.Kind) bool {
	switch k {
	case events.SearchedLibrary, events.Explore, events.Investigate, events.Discover, events.Seek,
		events.Surveil, events.Scry, events.Proliferate, events.GiveGift, events.Evolved, events.Clash,
		events.AbilityTriggered:
		return true
	}
	return false
}

// derivedObjectLocalKind reports whether k's Apply writes only fields of the
// event's own object (Obj): CounterChange its counters, Choose its Chosen*
// answers / Remembered / mode picks, Imprint its imprint and exile-link
// lists, StoreSVar its runtime SVars. Another object's derivation reads
// those fields only through an effect whose source is the object (a local
// IsRemembered / ChosenCard / ChosenColor predicate, a source-relative
// amount) -- refused by the caller's source check -- or through a non-local
// spec or amount, which no transparent list holds. So, like an
// off-battlefield move, such an event changes only its own object's
// derivation, and that object is retired instead.
func derivedObjectLocalKind(k events.Kind) bool {
	switch k {
	case events.CounterChange, events.Choose, events.Imprint, events.StoreSVar:
		return true
	}
	return false
}

// derivedRebuildTransparent reports whether the list just built (fresh) can
// keep derivedSeq, given prev the list the previous build returned.
//
// Beyond the conditions above, the run may also hold:
//
//   - an object-local event (derivedObjectLocalKind) on a real object, which
//     is retired like a moved one;
//   - TriggerPush and AbilityPush, each minting one face-less ability object
//     onto the stack (events.foldTriggerPush/foldAbilityPush): the arena
//     may grow by exactly their count, every appended object face-less. A
//     face-less object derives to nothing and no local predicate or literal
//     amount of another object's derivation reads it or the stack list;
//     AbilityPush's ActivatedThisTurn tally on the source is read only by
//     the Count$ThisTurnActivated head, never a local amount;
//   - any other zone move (onto or off the battlefield, out of exile) of an
//     object that is no live effect's source, retired like an
//     off-battlefield mover (see derivedAnyMoveOK for what such a move
//     writes to OTHER objects, none of which a local derivation reads). Such
//     a run moves derivedBFSeq, the battlefield-membership half of the key
//     the rename table (setname.go) also needs.
//
// same says fresh is prev itself (active_same.go's unchanged list), so the
// per-effect equality is known; allLocal is derivedEffectLocal over every
// effect of fresh, computed once per list.
func (e *Engine) derivedRebuildTransparent(prev, fresh []ContinuousEffect, same, allLocal bool) bool {
	if e.derivedSeq == 0 || e.derivedPrevEpoch <= 0 || e.derivedPrevEpoch > len(e.L.Events) ||
		e.derivedPrevVersion != e.continuousVersion || e.derivedPrevObjs > len(e.G.Objs) ||
		len(prev) != len(fresh) {
		return false
	}
	touched := e.derivedTouched[:0]
	pushes := 0
	other := false
	evs := e.L.Events[e.derivedPrevEpoch:]
	for i := range evs {
		ev := &evs[i]
		switch k := ev.Kind; k {
		case events.DecisionAsk, events.DecisionMade, events.Priority:
		case events.TriggerPush, events.AbilityPush:
			pushes++
		case events.MoveZone, events.Draw, events.PutOnStack:
			if ev.Obj == 0 {
				e.derivedTouched = touched[:0]
				return false
			}
			touched = append(touched, ev.Obj)
			if !offBattlefieldMove(ev) {
				other = true
			}
		default:
			if derivedObjectLocalKind(k) && ev.Obj != 0 {
				touched = append(touched, ev.Obj)
				continue
			}
			if !e.derivedQuietEvent(ev) {
				e.derivedTouched = touched[:0]
				return false
			}
		}
	}
	e.derivedTouched = touched
	if !e.facelessAppended(e.derivedPrevObjs, pushes) {
		return false
	}
	if other && !e.derivedAnyMoveOK(evs, pushes) {
		return false
	}
	if !allLocal {
		return false
	}
	if !same {
		for i := range fresh {
			if !derivedEffectEqual(&prev[i], &fresh[i]) {
				return false
			}
		}
	}
	if len(touched) > 0 {
		for i := range fresh {
			if slices.Contains(touched, fresh[i].Source) {
				return false
			}
		}
	}
	// A moved object's own derivation changed (its zone, and the per-zone
	// resets its move folded): retire its memo entries from cross-walk reuse.
	for _, id := range touched {
		if m := e.derivedMemo.at(id); m != nil {
			m.seq = 0
		}
		if m := e.derivedMemoStack.at(id); m != nil {
			m.seq = 0
		}
	}
	if other {
		e.derivedBFSeq++
	}
	return true
}

// derivedAnyMoveOK checks the one input a zone move outside
// offBattlefieldMove can change silently for an object it does not name.
// events.Apply's move fold writes, besides the moved object itself, its zone
// lists and the zone-entry ledger:
//
//   - leaving exile: every object's ExiledCards / ExileReturn / EncodedCards
//     link to it (read only by the ExiledWith / encoded predicates and the
//     static scan's GainsAbilitiesOf spec -- non-local, or a static list
//     change the content check sees);
//   - leaving the battlefield: other objects' BlockedBy tombstones, a
//     soulbond partner's Paired, other objects' CrewedVehicles, goads
//     sourced by it, ring-bearer designations (all read only by combat, the
//     non-local filter predicates and triggers -- no layer step);
//   - leaving the battlefield as a mutated pile: each card merged beneath it
//     MOVES too, with no event of its own -- a derivation that did change.
//
// The last is the one to rule out, and every such nested move appends its
// own zone entry (Move's g.Entered append): so the ledger must have grown
// by exactly one entry per logged move (none for a ceased ability object
// leaving the stack, Move's ceasedAbility) and one per push (its mint is a
// library->stack Move). A shorter ledger (a CR 733.1 reversal truncates it)
// fails the count too.
func (e *Engine) derivedAnyMoveOK(evs []events.Event, pushes int) bool {
	want := e.derivedPrevEntered + pushes
	for i := range evs {
		ev := &evs[i]
		switch ev.Kind {
		case events.MoveZone, events.Draw, events.PutOnStack:
		default:
			continue
		}
		if o := e.G.Obj(ev.Obj); o != nil && !(ev.To != state.ZStack && o.Card == nil && o.Ability != nil) {
			want++
		}
	}
	return want == len(e.G.Entered)
}

// facelessAppended reports whether the object arena grew from oldObjs by
// exactly n objects, every one of them face-less (no card, no copied face):
// the ability objects TriggerPush/AbilityPush mint.
func (e *Engine) facelessAppended(oldObjs, n int) bool {
	if len(e.G.Objs)-oldObjs != n {
		return false
	}
	for i := oldObjs; i < len(e.G.Objs); i++ {
		if o := &e.G.Objs[i]; o.Card != nil || o.CopyFace != nil {
			return false
		}
	}
	return true
}

// offBattlefieldMove reports whether ev is a zone move (MoveZone, Draw,
// PutOnStack) between the library, hand, graveyard and stack, or from one of
// them into exile. events.Apply's move fold writes only the moved object's
// own fields there, its zone lists and the entry ledger: every write to
// ANOTHER object (the exile-link lists, a departed blocker, a merged pile, a
// soulbond partner, crewed vehicles) sits on a battlefield or from-exile arm.
// So the moved object is the only derivation such a move can change -- it is
// never on the battlefield before or after, so no battlefield-only reader
// (the rename table) sees it either -- provided it is no live effect's source
// (checked by the caller).
func offBattlefieldMove(ev *events.Event) bool {
	switch ev.Kind {
	case events.MoveZone, events.Draw, events.PutOnStack:
	default:
		return false
	}
	from := ev.From == state.ZLibrary || ev.From == state.ZHand || ev.From == state.ZGraveyard || ev.From == state.ZStack
	to := ev.To == state.ZLibrary || ev.To == state.ZHand || ev.To == state.ZGraveyard || ev.To == state.ZStack || ev.To == state.ZExile
	return ev.Obj != 0 && from && to
}

// derivedNoteBuild records the key the build or re-stamp that produced the
// current list was taken at.
func (e *Engine) derivedNoteBuild() {
	e.derivedPrevEntered = len(e.G.Entered)
	e.derivedPrevEpoch = len(e.L.Events)
	e.derivedPrevVersion = e.continuousVersion
	e.derivedPrevObjs = len(e.G.Objs)
}

// derivedEffectEqual compares every ContinuousEffect field the layer walk
// reads: derivedCompute, typeCharacteristicsActive, chars.abilityDependencyOrder,
// chars.PT and the matchesWithCharsPT bind. SVars is left out: only
// a non-literal amount resolves through it, and such an effect is never
// local (derivedEffectLocal), so a transparent rebuild never reads it.
func derivedEffectEqual(a, b *ContinuousEffect) bool {
	return a.Source == b.Source && a.Layer == b.Layer && a.Sub == b.Sub && a.Timestamp == b.Timestamp &&
		a.Affects == b.Affects && a.Controller == b.Controller && a.AffectedZone == b.AffectedZone &&
		a.MayPlay == b.MayPlay &&
		a.AddPower == b.AddPower && a.AddToughness == b.AddToughness &&
		a.DoublePower == b.DoublePower && a.DoubleToughness == b.DoubleToughness &&
		a.SetPower == b.SetPower && a.SetToughness == b.SetToughness && a.HasSet == b.HasSet &&
		a.StaticSet == b.StaticSet && a.SetPowerPresent == b.SetPowerPresent && a.SetToughnessPresent == b.SetToughnessPresent &&
		a.AddPowerExpr == b.AddPowerExpr && a.AddToughnessExpr == b.AddToughnessExpr &&
		a.AddPowerAffected == b.AddPowerAffected && a.AddToughnessAffected == b.AddToughnessAffected &&
		a.SetPowerExpr == b.SetPowerExpr && a.SetToughnessExpr == b.SetToughnessExpr &&
		a.SetPowerAffected == b.SetPowerAffected && a.SetToughnessAffected == b.SetToughnessAffected &&
		a.SetName == b.SetName && a.TextFrom == b.TextFrom && a.TextTo == b.TextTo &&
		a.TextSet == b.TextSet && a.TextSetSet == b.TextSetSet &&
		a.OverwriteColors == b.OverwriteColors && a.RemoveCreatureTypes == b.RemoveCreatureTypes &&
		a.RemoveSubTypes == b.RemoveSubTypes && a.SetCreatureTypes == b.SetCreatureTypes &&
		a.AddAllCreatureTypes == b.AddAllCreatureTypes && a.CDAAllCreatureTypes == b.CDAAllCreatureTypes && a.RemoveCardTypes == b.RemoveCardTypes &&
		a.RemoveLandTypes == b.RemoveLandTypes &&
		a.RemoveLegendary == b.RemoveLegendary && a.RemoveAbilities == b.RemoveAbilities &&
		slices.Equal(a.AddKeywords, b.AddKeywords) && slices.Equal(a.AddTypes, b.AddTypes) &&
		slices.Equal(a.AddColors, b.AddColors) && slices.Equal(a.RemoveTypes, b.RemoveTypes) &&
		slices.Equal(a.RemoveKeywords, b.RemoveKeywords) && slices.Equal(a.CantHaveKeywords, b.CantHaveKeywords) &&
		slices.Equal(a.Remembered, b.Remembered)
}

// derivedEffectLocal reports whether a derivation's read of ce depends only
// on the candidate's own characteristics, counters and controller and on
// ce's own fields and source (condition 4 above). An effect outside the
// layers a derivation reads is trivially local.
func derivedEffectLocal(ce *ContinuousEffect) bool {
	switch ce.Layer {
	case LText, LType, LColor, LAbilities, LPT:
	default:
		if len(ce.CantHaveKeywords) == 0 {
			return true
		}
	}
	if !literalAmount(ce.AddPowerExpr) || !literalAmount(ce.AddToughnessExpr) ||
		!literalAmount(ce.SetPowerExpr) || !literalAmount(ce.SetToughnessExpr) {
		return false
	}
	return specLocal(ce.Affects)
}

// literalAmount reports whether a P/T expression is empty or an optionally
// signed decimal integer -- an amount no game state can move.
func literalAmount(s string) bool {
	if s == "" {
		return true
	}
	if s[0] == '+' || s[0] == '-' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// specLocalMemo caches specLocal per spec text (a pure function of it):
// a direct-mapped table of immutable entries, safe to share across engines.
var specLocalMemo [256]atomic.Pointer[specLocalEntry]

type specLocalEntry struct {
	spec string
	ok   bool
}

func specLocal(spec string) bool {
	h := uint32(2166136261)
	for i := 0; i < len(spec); i++ {
		h = (h ^ uint32(spec[i])) * 16777619
	}
	slot := &specLocalMemo[h%uint32(len(specLocalMemo))]
	if en := slot.Load(); en != nil && en.spec == spec {
		return en.ok
	}
	ok := specLocalParse(spec)
	slot.Store(&specLocalEntry{spec: spec, ok: ok})
	return ok
}

// specLocalParse is the whitelist: every comma alternative is a base word
// (a type word, optionally non-prefixed, or Card/Permanent) followed by
// '+'-joined predicates from localSpecPredicates, a counters_<CMP><n>_<KIND>
// comparison with a literal count, or a with<Keyword>/without<Keyword> test.
// Anything else -- a numeric predicate with an SVar right-hand side, a
// tapped/untapped/attacking test, a cast-provenance token, a name test, a
// player qualifier -- is not local.
func specLocalParse(spec string) bool {
	if spec == "" {
		return true
	}
	for alt := range strings.SplitSeq(spec, ",") {
		base, preds, _ := strings.Cut(alt, ".")
		if !letterWord(base) {
			return false
		}
		if preds == "" {
			continue
		}
		for p := range strings.SplitSeq(preds, "+") {
			if !localPredicate(p) {
				return false
			}
		}
	}
	return true
}

func letterWord(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

func localPredicate(p string) bool {
	if localPredicateSet.Has(p) {
		return true
	}
	// A type word (Creature.Elf, Card.nonLand) tests the candidate's own
	// derived type list.
	if localTypeWord(p) {
		return true
	}
	if t, ok := strings.CutPrefix(p, "non"); ok && localTypeWord(t) {
		return true
	}
	if rest, ok := strings.CutPrefix(p, "counters_"); ok {
		if len(rest) < 4 {
			return false
		}
		if !localPredicateKeys.Has(rest[:2]) {
			return false
		}
		num, kind, ok := strings.Cut(rest[2:], "_")
		return ok && literalAmount(num) && num != "" && num[0] != '+' && num[0] != '-' && letterWordDigits(kind)
	}
	if kw, ok := strings.CutPrefix(p, "without"); ok {
		return letterWord(kw)
	}
	if kw, ok := strings.CutPrefix(p, "with"); ok {
		return letterWord(kw)
	}
	return false
}

// localTypeWord reports whether w is a card type, supertype, creature
// subtype or one of the common non-creature subtypes: a predicate the filter
// answers from the candidate's own type list.
func localTypeWord(w string) bool {
	if chars.IsCardType(w) || chars.IsSupertype(w) || effects.CreatureTypeWords(w) {
		return true
	}
	return localTypeWordSet.Has(w)
}

func letterWordDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// faceHasCDAStatic reports whether o's face carries a characteristic-
// defining Continuous static -- cdaSetPT's own test -- whose amount may read
// any game state, so the object's derivation is never reused across events.
func faceHasCDAStatic(o *state.Object) bool {
	if o == nil {
		return false
	}
	f := o.Face()
	if f == nil {
		return false
	}
	for i := range f.Statics {
		st := &f.Statics[i]
		if st.Mode == "Continuous" && strings.TrimSpace(st.ParamStr(cards.PKCharacteristicDefining)) != "" {
			return true
		}
	}
	return false
}

var localPredicateSet = state.NewNameSet(
	"Self",
	"Other",
	"YouCtrl",
	"OppCtrl",
	"YouOwn",
	"OppOwn",
	"EnchantedBy",
	"EquippedBy",
	"ChosenColor",
	"IsRemembered",
	"ChosenCard",
	"token",
	"nonToken",
	"White",
	"Blue",
	"Black",
	"Red",
	"Green",
	"Colorless",
	"MultiColor",
	"MonoColor",
	"nonWhite",
	"nonBlue",
	"nonBlack",
	"nonRed",
	"nonGreen",
	"nonColorless",
	"nonMultiColor",
)

var localPredicateKeys = state.NewNameSet("LT", "LE", "GT", "GE", "EQ", "NE")

var localTypeWordSet = state.NewNameSet(
	"Equipment",
	"Aura",
	"Vehicle",
	"Food",
	"Treasure",
	"Clue",
	"Saga",
	"Plains",
	"Island",
	"Swamp",
	"Mountain",
	"Forest",
)
