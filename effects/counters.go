package effects

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("PutCounter", effPutCounter)
	Register("Poison", effPoison)
	Register("Radiation", effRadiation)
	Register("RadiationDrain", effRadiationDrain)
	Register("PutCounterAll", effPutCounterAll)
	Register("RemoveCounterAll", effRemoveCounterAll)
	Register("RemoveCounter", effRemoveCounter)
	Register("AddOrRemoveCounter", effAddOrRemoveCounter)
	Register("MoveCounter", effMoveCounter)
	Register("MultiplyCounter", effMultiplyCounter)
	Register("Proliferate", effProliferate)
	Register("Regenerate", effRegenerate)
}

// effMultiplyCounter is Forge's MultiplyCounterEffect: for each object or
// player the Defined$/ValidTgts$ spec names, ADD (Multiplier-1) x the current
// count of the affected counter kind(s) -- so the default Multiplier$ 2
// exactly DOUBLES them. CounterType$ names the ONE kind to multiply; absent
// ("double the number of EACH KIND of counter on target permanent",
// Aetheric Amplifier, Deepglow Skate, The Thing, Miles Morales), every kind
// the object already carries is multiplied. Multiplier$ resolves through Num,
// so a literal (the whole corpus: Multiplier$ 2), an SVar or an inline
// Count$ expression all price the same path; an absent Multiplier$ is 2.
//
// A target with no counters of the relevant kind(s) emits nothing -- adding
// zero is a no-op and one CounterChange of Amount 0 would be log noise (the
// effRemoveCounterAll discipline). One event per kind per target, so the
// event stream records the real CounterChange/PlayerCounterChange the engine
// folds, never a snapshot write.
func effMultiplyCounter(h Host, c *Ctx, sa *cards.SA) {
	mult := Num(h, c, sa, "Multiplier", 2)
	if mult < 1 {
		mult = 1
	}
	kind := strings.TrimSpace(sa.ParamStr(cards.PKCounterType))
	g := h.Game()
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			p := PlayerOf(h, c, t)
			if int(p) < 0 || int(p) >= len(g.Players) {
				continue
			}
			pl := &g.Players[p]
			// Deterministic: the player's own counter slice order, which is
			// insertion order and rebuilt identically on replay.
			kinds := counterKinds(kind, len(pl.Counters), func(i int) string { return pl.Counters[i].Kind }, func(i int) int32 { return pl.Counters[i].N })
			for _, k := range kinds {
				if add := (mult - 1) * pl.Counter(k); add > 0 {
					h.Emit(events.Event{Kind: events.PlayerCounterChange, Player: p,
						Counter: k, Amount: add})
				}
			}
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		kinds := counterKinds(kind, len(o.Counters), func(i int) string { return o.Counters[i].Kind }, func(i int) int32 { return o.Counters[i].N })
		for _, k := range kinds {
			if add := (mult - 1) * o.Counter(k); add > 0 {
				h.Emit(events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: k, Amount: add})
			}
		}
	}
}

// counterKinds is the kind list a counter primitive walks: the single
// CounterType$ when named, otherwise every kind the carrier already holds, in
// its own deterministic slice order (never a map walk).
//
// It reports only kinds the carrier actually has a POSITIVE count of. A slot
// can survive its counters being removed down to zero -- state's AddCounter
// clamps at zero and never prunes the slice entry (state/object.go,
// state/game.go) -- so a drained slot must not count as "has this kind":
// proliferating onto it would add a counter of a kind that is no longer there
// (CR 701.27a) and the eligibility gate below would offer a recipient with no
// counters at all. Callers pass the per-index count so the one helper is the
// single place that filters.
func counterKinds(kind string, n int, at func(int) string, count func(int) int32) []string {
	if kind != "" {
		return []string{kind}
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if count(i) > 0 {
			out = append(out, at(i))
		}
	}
	return out
}

// hasCounters reports whether a counter carrier holds at least one counter of
// ANY kind, testing the COUNT and not the slice length (a drained slot stays in
// the slice at N == 0). This is the CR 701.27a eligibility gate, shared by the
// object and player walks.
func hasCounters(kinds []state.Counter) bool {
	for i := range kinds {
		if kinds[i].N > 0 {
			return true
		}
	}
	return false
}

func splitCounterKinds(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// effPoison applies Forge's Poison effect to player targets. Poison is a
// player counter, but unlike PutCounter its signed Num$ is intentional:
// Leeches uses a negative amount to remove the target's existing poison.
// PlayerCounterChange is the shared event choke point, so replacement effects
// and the poison-loss SBA observe both placement and removal.
func effRadiation(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "Num", 1)
	g := h.Game()
	for _, t := range Defined(h, c, sa) {
		if !t.IsPlayer {
			continue
		}
		p := PlayerOf(h, c, t)
		if int(p) < 0 || int(p) >= len(g.Players) {
			continue
		}
		h.Emit(events.Event{Kind: events.PlayerCounterChange, Player: p, Counter: "RAD", Amount: n})
	}
}

func effRadiationDrain(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	p := c.Controller
	if int(p) < 0 || int(p) >= len(g.Players) {
		return
	}
	n := g.Players[p].Counter("RAD")
	for i := int32(0); i < n; i++ {
		lib := g.Zone(state.ZLibrary, p)
		if len(lib) == 0 {
			break
		}
		id := lib[0]
		o := g.Obj(id)
		land := false
		if o != nil && o.Face() != nil {
			for _, typ := range o.Face().Types {
				if typ == "Land" {
					land = true
					break
				}
			}
		}
		h.Emit(events.Mill(id, p))
		if !land {
			h.Emit(events.Event{Kind: events.LifeChange, Player: p, Amount: -1})
			h.Emit(events.Event{Kind: events.PlayerCounterChange, Player: p, Counter: "RAD", Amount: -1})
		}
	}
}

func effPoison(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "Num", 1)
	g := h.Game()
	for _, t := range Defined(h, c, sa) {
		if !t.IsPlayer {
			continue
		}
		p := PlayerOf(h, c, t)
		if int(p) < 0 || int(p) >= len(g.Players) {
			continue
		}
		h.Emit(events.Event{Kind: events.PlayerCounterChange, Player: p,
			Counter: "POISON", Amount: n})
	}
}

// caseVariantCounterKinds maps the corpus's minority spellings of a counter
// kind to the one every reader uses. Forge resolves CounterType$ through
// CounterEnumType case-insensitively, so `CounterType$ Stun` (72 PutCounter
// lines, e.g. Fear of Sleep Paralysis) IS the STUN counter that the untap
// replacement (effects/untap.go) and `ValidCounterType$ STUN` read; likewise
// Overseer of Vault 76's `Quest` (paid back as RemoveAnyCounter<3/QUEST>) and
// Lost Isle Calling's `Verse` (read back as CardCounters.VERSE). These are the
// only kinds the corpus spells in two casings (measured over every
// CounterType$ line), and every minority spelling is a PutCounter line. A
// blanket upper-casing would be wrong: keyword counters (Flying, Deathtouch,
// ...) and the engine's own Shield marker are case-significant here.
var caseVariantCounterKinds = map[string]string{
	"Stun":  "STUN",
	"Quest": "QUEST",
	"Verse": "VERSE",
}

// canonicalCounterKind folds a CounterType$ value (or each entry of a comma
// list) onto its canonical spelling; anything else is returned unchanged.
func canonicalCounterKind(kind string) string {
	if !strings.Contains(kind, ",") {
		if k, ok := caseVariantCounterKinds[strings.TrimSpace(kind)]; ok {
			return k
		}
		return kind
	}
	parts := strings.Split(kind, ",")
	for i, p := range parts {
		if k, ok := caseVariantCounterKinds[strings.TrimSpace(p)]; ok {
			parts[i] = k
		}
	}
	return strings.Join(parts, ",")
}

// effPutCounterAll sweeps ValidCards$ (default "Permanent") over the
// battlefield in deterministic order (g.AliveFrom(0), then each seat's zone
// order -- never a map range) and places CounterType$ (default "P1P1")
// counters on each match: CounterNum$ resolved through Num (default 1,
// negative clamped to 0 like both siblings), one events.CounterChange per
// recipient with a signed positive Amount. It is the mass-placement mirror
// of effRemoveCounterAll.
//
// Player-targeted sweep (ValidTgts$ Player, 7 raw corpus lines over 6
// carriers, e.g.
// Meadowboon's "put a +1/+1 counter on each creature target player
// controls"): the sweep is scoped to each CHOSEN player target's battlefield
// instead of the whole table; ValidCards$ stays the filter inside that
// scope, so an unqualified "Creature" means the target player's creatures.
// A ValidTgts$ Player line that reaches resolution with no chosen player
// target is loud, never silent.
//
// A second batch (ValidCards2$/CounterType2$/CounterNum2$, e.g. Brokers
// Ascendancy's "...and a loyalty counter on each planeswalker you control")
// runs as a second sweep after the first, with its own filter, kind (default
// P1P1) and count (default 1).
//
// Exotic parameters the core sweep cannot express stay LOUD (the
// effManifest out-of-scope pattern): Placer$ (who places, 7 corpus lines),
// TargetUnique$ (2) and AmountByChosenMap$ (1), and a ValidZone$ naming any
// zone other than the battlefield (2, both Exile -- suspended TIME
// counters). Registering the API removed the generic "unimplemented API"
// fallback, so without these notes the shapes would silently place nothing.
func effPutCounterAll(h Host, c *Ctx, sa *cards.SA) {
	var exotic []string
	if strings.TrimSpace(sa.ParamStr(cards.PKPlacer)) != "" {
		exotic = append(exotic, "Placer$")
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKTargetUnique)) != "" {
		exotic = append(exotic, "TargetUnique$")
	}
	if strings.TrimSpace(sa.Params["AmountByChosenMap"]) != "" {
		exotic = append(exotic, "AmountByChosenMap$")
	}
	if zone := strings.TrimSpace(sa.ParamStr(cards.PKValidZone)); zone != "" && !strings.EqualFold(zone, "Battlefield") {
		exotic = append(exotic, "ValidZone$ "+zone)
	}
	// A ValidTgts$ naming anything but the plain chosen-player sweep is an
	// exotic shape: anything else (Corrosion's "Opponent", a named
	// selector, a compound) would fall through to the whole-table branch
	// below and sweep the WRONG-WIDE set silently. Loud instead.
	if tgts := strings.TrimSpace(sa.ParamStr(cards.PKValidTgts)); tgts != "" && !strings.EqualFold(tgts, "Player") {
		exotic = append(exotic, "ValidTgts$ "+tgts)
	}
	if len(exotic) > 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented PutCounterAll shape: " + strings.Join(exotic, ", ")})
		return
	}
	putCounterAllSweep(h, c, sa, sa.ParamStr(cards.PKValidCards), sa.ParamStr(cards.PKCounterType), Num(h, c, sa, "CounterNum", 1))
	if strings.TrimSpace(sa.Params["ValidCards2"]) != "" {
		putCounterAllSweep(h, c, sa, sa.Params["ValidCards2"], sa.Params["CounterType2"], Num(h, c, sa, "CounterNum2", 1))
	}
}

// putCounterAllSweep is one PutCounterAll batch: filter spec, counter kind
// and count already resolved from their literal Params keys.
func putCounterAllSweep(h Host, c *Ctx, sa *cards.SA, spec, kind string, n int32) {
	if kind == "" {
		kind = "P1P1"
	}
	if spec == "" {
		spec = "Permanent"
	}
	if n < 0 {
		n = 0
	}
	if n <= 0 {
		// Same discipline as effRemoveCounterAll: a zero-amount batch is a
		// no-op, and emitting one CounterChange whose Amount overstates what
		// changed per object is log noise (an unresolvable CounterNum$
		// degrades to 0 through Num).
		return
	}
	players := h.Game().AliveFrom(0)
	if strings.TrimSpace(sa.ParamStr(cards.PKValidTgts)) == "Player" {
		players = nil
		for _, t := range c.Targets {
			if t.IsPlayer {
				players = append(players, t.Player)
			}
		}
		if len(players) == 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented PutCounterAll shape: player-targeted sweep with no chosen player target"})
			return
		}
	}
	g := h.Game()
	for _, p := range players {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if !MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				continue
			}
			h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: kind, Amount: n})
		}
	}
}

// effRemoveCounterAll sweeps ValidCards$ (default "Permanent") on the
// battlefield and removes CounterType$ counters from each match: CounterNum$
// (default 1) of them, or every counter of that kind the object actually has
// when AllCounters$ is "True". state.Object.AddCounter already clamps at
// zero, so removing more than an object has is harmless either way; the
// AllCounters$ case reads the object's own count first purely to avoid an
// event whose Amount overstates what changed.
func effRemoveCounterAll(h Host, c *Ctx, sa *cards.SA) {
	kind := sa.ParamStr(cards.PKCounterType)
	if kind == "" {
		return
	}
	spec := sa.ParamStr(cards.PKValidCards)
	if spec == "" {
		spec = "Permanent"
	}
	all := sa.Params["AllCounters"] == "True"
	n := Num(h, c, sa, "CounterNum", 1)
	if n < 0 {
		n = 0
	}
	g := h.Game()
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if !MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				continue
			}
			amt := n
			if all {
				amt = g.Obj(id).Counter(kind)
			}
			if amt <= 0 {
				continue
			}
			h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: kind, Amount: -amt})
		}
	}
}

// effRemoveCounter is Forge's RemoveCounterEffect: for each object or player
// the Defined$ spec names (an ability with no Defined$ acts on its chosen
// targets through Ctx.Targets; one that names neither acts on its source --
// the ordinary Defined contract), remove CounterNum$ counters of CounterType$
// from it. The corpus shape is overwhelmingly the chained DB$ sub-ability
// (178 of 202 raw RemoveCounter lines), Self-dominated: Prize Pig's
// "remove those counters and untap it" payoff is the pin.
//
// CounterNum$ resolves through the shared Num evaluator (a literal, an SVar
// name such as the corpus's X/Y/SekkiX/Result/NumDmg, or an inline Count$);
// "All" means every counter of that kind the object actually has -- the
// AllCounters$ discipline effRemoveCounterAll uses, reading the count first
// so the event's Amount never overstates. A literal n over an object holding
// fewer also clamps to what is there (state.Object.AddCounter clamps at zero
// either way; emitting -n would overstate). One events.CounterChange with a
// signed negative Amount per (object, kind); an object with none of the kind
// emits nothing (the zero-batch no-op discipline both siblings follow).
//
// CounterType$ All means EVERY kind the object holds, in its own
// deterministic slice order (the counterKinds helper this file already
// shares with MultiplyCounter), one CounterChange per kind present.
//
// RememberRemoved$ True records one remembered entry per removed counter on
// the source's persistent (event-backed) remembered list -- one Choose
// "remembered" event per (object, kind) batch, the object's id repeated once
// per removed counter, so Count$RememberedSize (the HOST CARD's list,
// effects/count.go) reads the truthful size. Prize Pig's untap gate is
// ConditionCheckSVar$ X with SVar:X:Count$RememberedSize, so this rider is
// load-bearing for it. Duplicates are deliberate: the only consumer measured
// for this rider is a size count (and Cleanup's ClearRemembered$ clears the
// list again), so one entry per counter is the honest encoding.
//
// Exotic shapes stay LOUD (the effPutCounterAll exotic pattern -- one Note
// naming the shape, nothing moves): CounterType$ Any (a choose-which-kind
// ask), ChoiceOptional$ without Choices$ (a malformed mid-resolution pick),
// UpTo$ (a bounded election), CounterNum$ Any, CounterNumShared$, and a
// TgtZone$ naming anything but the battlefield (the suspended-TIME-counter
// family), RememberAmount$
// (a removed NUMBER the remembered list has no honest channel for) and
// Optional$ (a may-remove election). Registering the API removed the generic
// "unimplemented API" fallback, so without these notes the shapes would
// silently remove nothing.
// One Note per Defined$-named OBJECT that is not on the battlefield (or no
// longer exists) instead of the silent skip r1 shipped: the corpus's
// RememberedLKI/Imprinted/ChosenCard defined sets can name a graveyard or
// exile card, the TgtZone$ gate above is the only sanctioned off-battlefield
// family, and swallowing a named target silently is an unledgered narrowing.
// The note records the skip loudly; nothing moves (removal off-battlefield
// stays a TgtZone$ task).
func effRemoveCounter(h Host, c *Ctx, sa *cards.SA) {
	// fx42 scoping: the answered bare-Choices$ pick rides the SHARED
	// "counter_pick" arm's fields (Ctx.CounterPick — the same transport
	// PutCounter's bare pick uses, since the option lists decode identically),
	// so capture and clear them at the very top, before any exotic check or
	// nested RemoveCounter in the same chain can see them.
	pickAns := c.CounterPick
	pickDone := c.CounterPickDone
	c.CounterPick, c.CounterPickDone = nil, false
	var exotic []string
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKCounterType)), "Any") {
		exotic = append(exotic, "CounterType$ Any")
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKChoices)) != "" {
		// The card-election arm (counterchoice1): the 10 raw corpus
		// RemoveCounter lines carrying Choices$. Shapes this arm cannot
		// express stay loud inside removeCounterChoose.
		removeCounterChoose(h, c, sa, pickAns, pickDone)
		return
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKChoiceOptional)) != "" {
		exotic = append(exotic, "ChoiceOptional$")
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKUpTo)) != "" {
		exotic = append(exotic, "UpTo$")
	}
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKCounterNum)), "Any") {
		exotic = append(exotic, "CounterNum$ Any")
	}
	if strings.TrimSpace(sa.Params["CounterNumShared"]) != "" {
		exotic = append(exotic, "CounterNumShared$")
	}
	if zone := strings.TrimSpace(sa.ParamStr(cards.PKTgtZone)); zone != "" && !strings.EqualFold(zone, "Battlefield") {
		exotic = append(exotic, "TgtZone$ "+zone)
	}
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberAmount)), "True") {
		exotic = append(exotic, "RememberAmount$")
	}
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKOptional)), "True") {
		exotic = append(exotic, "Optional$")
	}
	if len(exotic) > 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented RemoveCounter shape: " + strings.Join(exotic, ", ")})
		return
	}
	kind := strings.TrimSpace(sa.ParamStr(cards.PKCounterType))
	if kind == "" {
		kind = "P1P1"
	}
	allKinds := strings.EqualFold(kind, "All")
	numAll := false
	n := int32(1)
	if raw := strings.TrimSpace(sa.ParamStr(cards.PKCounterNum)); raw != "" {
		if strings.EqualFold(raw, "All") {
			numAll = true
		} else {
			n = Num(h, c, sa, "CounterNum", 1)
			if n < 0 {
				n = 0
			}
		}
	}
	kindArg := kind
	if allKinds {
		kindArg = "" // counterKinds: every kind the carrier holds
	}
	g := h.Game()
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			p := PlayerOf(h, c, t)
			if int(p) < 0 || int(p) >= len(g.Players) {
				continue
			}
			pl := &g.Players[p]
			for _, k := range dedupeKinds(counterKinds(kindArg, len(pl.Counters), func(i int) string { return pl.Counters[i].Kind }, func(i int) int32 { return pl.Counters[i].N })) {
				count := pl.Counter(k)
				amt := n
				if numAll {
					amt = count
				}
				removed := amt
				if removed > count {
					removed = count
				}
				if removed <= 0 {
					continue
				}
				h.Emit(events.Event{Kind: events.PlayerCounterChange, Player: p, Counter: k, Amount: -removed})
				rememberRemoved(h, c, sa, state.PlayerRef(p), removed)
			}
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: fmt.Sprintf("RemoveCounter target %d no longer exists; skipped", t.Obj)})
			continue
		}
		// An exiled card keeps its counters (CR 122.2 -- a suspended card's
		// time counters, CR 702.62a), so removing one from it is real:
		// Greater Gargadon's own ActivationZone$ Exile ability, Jhoira's
		// Timebug-style "suspended card you own". Every other off-battlefield
		// zone holds no counters worth removing and stays a loud skip.
		if o.Zone != state.ZBattlefield && o.Zone != state.ZExile {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: fmt.Sprintf("RemoveCounter target %d is not on the battlefield (zone %s); skipped", o.ID, o.Zone)})
			continue
		}
		for _, k := range dedupeKinds(counterKinds(kindArg, len(o.Counters), func(i int) string { return o.Counters[i].Kind }, func(i int) int32 { return o.Counters[i].N })) {
			count := o.Counter(k)
			amt := n
			if numAll {
				amt = count
			}
			removed := amt
			if removed > count {
				removed = count
			}
			if removed <= 0 {
				continue
			}
			h.Emit(events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: k, Amount: -removed})
			rememberRemoved(h, c, sa, o.ID, removed)
		}
	}
}

// dedupeKinds preserves first-occurrence order and drops repeats, so a
// carrier whose Counters slice ever holds the same kind twice emits one
// CounterChange per kind, never two.
func dedupeKinds(kinds []string) []string {
	if len(kinds) < 2 {
		return kinds
	}
	seen := make(map[string]bool, len(kinds))
	out := kinds[:0]
	for _, k := range kinds {
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

// removeCounterChoose runs the bare-Choices$ RemoveCounter card-election arm
// (counterchoice1; 10 raw corpus RemoveCounter lines carry Choices$). The
// CHOOSER (the resolving controller; every carrier) picks objects out of the
// Choices$ pool in ChoiceZone$ (default the battlefield; Amy Pond's and Mari
// the Killing Quill's triggers name Exile), and EACH chosen object loses
// CounterNum$ counters of CounterType$ (the shared Num grammar — Amy Pond's
// CounterNum$ X is the triggering damage amount). The answer re-enters
// through the shared "counter_pick" resume arm (Ctx.CounterPick), consumed
// and cleared by effRemoveCounter at its top (fx42).
//
// How many objects: ChoiceNum$-exact (default 1; Amy Pond, Mari the Killing
// Quill) unless ChoiceOptional$ True is present, which is Forge's "each of
// ANY number" — both reachable carriers (Garnet, Princess of Alexandria's
// "remove a lore counter from each of any number of Sagas you control" and
// Chandra, Legacy of Fire's [0] over "any number of permanents you control")
// pair it with NO ChoiceNum$, so the bound is 0..len(eligible) and a two-Saga
// board really can take BOTH. ChoiceOptional$ True WITH an explicit
// ChoiceNum$ reads as up-to-N (0..ChoiceNum$); no corpus line carries that
// pair today, so that branch is corpus-unreachable.
//
// RememberAmount$ True is the removed-COUNT transport (Garnet's
// SVar:X:Count$RememberedNumber payoff, Chandra's Z, Dyadrine, Synthesis
// Amalgam's DBDraw/DBToken ConditionCheckSVar$ Z): the chosen object's id is
// appended to Ctx.Remembered ONCE PER COUNTER REMOVED, exactly the encoding
// effMoveCounter's own RememberAmount$ rider uses, so Count$RememberedNumber
// reads the truthful total across every chosen object. It is orthogonal to
// RememberRemoved$, which writes the event-backed host list instead; a line
// carrying both gets both.
//
// The ask gate is the strict-supersets rule (a decision nobody could answer
// differently is never emitted): zero eligible objects acts on nothing, an
// exact count at or above the eligible count takes the deterministic first
// Max, and everything else — an Optional$ 0..1 over even ONE eligible object
// — is a real election. The no-host fallback (R-9) and botpolicy's
// "counter_pick" arm both take the first Max eligible in zone order, so a
// bot-answered ask emits the same events a silent build would have.
//
// Loud-and-unmodelled (one Note naming the shapes, nothing moves — the same
// exotic discipline the defined path keeps): CounterType$ Any/All (a
// which-kind pick stacked on the card election), CounterNum$ Any (a
// how-many-per-card ask), UpTo$/CounterNumShared$ (a divided total), and a
// ChoiceZone$ naming neither the battlefield nor exile.
func removeCounterChoose(h Host, c *Ctx, sa *cards.SA, ans []state.ObjID, done bool) {
	var loud []string
	kind := strings.TrimSpace(sa.ParamStr(cards.PKCounterType))
	if kind == "" {
		kind = "P1P1" // the defined path's default kind
	}
	if strings.EqualFold(kind, "Any") || strings.EqualFold(kind, "All") {
		loud = append(loud, "CounterType$ "+kind)
	}
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKCounterNum)), "Any") {
		loud = append(loud, "CounterNum$ Any")
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKUpTo)) != "" {
		loud = append(loud, "UpTo$")
	}
	if strings.TrimSpace(sa.Params["CounterNumShared"]) != "" {
		loud = append(loud, "CounterNumShared$")
	}
	zone := state.ZBattlefield
	switch strings.TrimSpace(sa.ParamStr(cards.PKChoiceZone)) {
	case "", "Battlefield":
	case "Exile":
		zone = state.ZExile
	default:
		loud = append(loud, "ChoiceZone$ "+strings.TrimSpace(sa.ParamStr(cards.PKChoiceZone)))
	}
	if len(loud) > 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented RemoveCounter choice shape: " + strings.Join(loud, ", ")})
		return
	}
	if done {
		removeCounterPickApply(h, c, sa, zone, kind, Num(h, c, sa, "CounterNum", 1), ans)
		return
	}
	g := h.Game()
	spec := strings.TrimSpace(sa.ParamStr(cards.PKChoices))
	var eligible []state.ObjID
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(zone, p) {
			if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				eligible = append(eligible, id)
			}
		}
	}
	num := Num(h, c, sa, "CounterNum", 1)
	if num < 0 {
		num = 0
	}
	exact := Num(h, c, sa, "ChoiceNum", 1)
	if exact < 0 {
		exact = 0
	}
	minv, maxv := exact, exact
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKChoiceOptional)), "True") {
		// Forge's "each of any number": Min 0, and — with no explicit
		// ChoiceNum$ bounding it (both reachable carriers) — Max the whole
		// eligible pool, so Garnet's two Sagas can BOTH be chosen. An
		// explicit ChoiceNum$ alongside it keeps its own Max (up-to-N).
		minv = 0
		if strings.TrimSpace(sa.Params["ChoiceNum"]) == "" {
			maxv = int32(len(eligible))
		}
	}
	if maxv > int32(len(eligible)) {
		maxv = int32(len(eligible))
	}
	if minv > maxv {
		minv = maxv
	}
	fallback := func() {
		picks := eligible
		if int32(len(picks)) > maxv {
			picks = picks[:maxv]
		}
		removeCounterPickApply(h, c, sa, zone, kind, num, picks)
	}
	if len(eligible) == 0 || (minv == maxv && maxv >= int32(len(eligible))) {
		// Nothing eligible, or the forced exact set (the strict-supersets
		// rule): the deterministic act with no ask.
		fallback()
		return
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
		Min:        int(minv),
		Max:        int(maxv),
		Source:     c.Source,
		ResumeKind: "counter_pick",
		ResumeSA:   sa,
		Prompt:     "Choose card(s) to remove " + kind + " counters from"}
	for _, id := range eligible {
		name := "a card"
		if o := g.Obj(id); o != nil && o.Face() != nil {
			name = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "counter_pick", Label: name, Obj: id, Player: c.Controller})
	}
	if ans, ok := AskTape(h, d); ok {
		// The "counter_pick" answer in hand: the re-entry's removal, with
		// the count read exactly as the re-entry reads it.
		removeCounterPickApply(h, c, sa, zone, kind, Num(h, c, sa, "CounterNum", 1), counterAnswerObjs(ans))
		return
	}
	if Ask(h, d) == AskAsked {
		return // resolution suspended; the answer re-enters with Ctx.CounterPick set.
	}
	fallback()
}

// removeCounterPickApply removes num counters of kind from each live chosen
// object (a chosen object that left the choice zone while the decision was
// outstanding takes nothing — the same staleness stance putCounterPickApply
// takes), honouring the RememberRemoved$ rider through the shared helper and
// the RememberAmount$ rider through Ctx.Remembered (the effMoveCounter
// encoding: the object's id once per counter removed, so the chained
// Count$RememberedNumber payoff — Garnet's X, Chandra's Z, Dyadrine's
// ConditionCheckSVar$ Z — reads the real total).
func removeCounterPickApply(h Host, c *Ctx, sa *cards.SA, zone state.Zone, kind string, num int32, picks []state.ObjID) {
	g := h.Game()
	rememberAmount := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberAmount)), "True")
	var amountIDs []state.ObjID
	for _, id := range picks {
		o := g.Obj(id)
		if o == nil || o.Zone != zone {
			continue
		}
		count := o.Counter(kind)
		removed := num
		if removed > count {
			removed = count
		}
		if removed <= 0 {
			continue
		}
		h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: kind, Amount: -removed})
		rememberRemoved(h, c, sa, id, removed)
		for i := int32(0); i < removed; i++ {
			amountIDs = append(amountIDs, id)
		}
	}
	if rememberAmount && len(amountIDs) > 0 {
		c.Remembered = append(c.Remembered, objTargets(amountIDs)...)
	}
}

// effAddOrRemoveCounter is Forge's AddOrRemoveCounterEffect (counterchoice1;
// 11 corpus files): "Choose a counter on target permanent or suspended card.
// Remove that counter from it or put another of those counters on it" — the
// add-or-remove election over one target that may live on the battlefield OR
// in exile (a suspended card; TgtZone$ Battlefield,Exile).
//
// Three shapes, split on the corpus's own parameters:
//
//  1. RemoveConditionSVar$ (Etched Host Doombringer, Portent Tracker, Shape
//     of the Wiitigo): the condition picks the arm — count > 0 removes,
//     otherwise puts. No ask: the condition IS the choice ("if an opponent
//     protects it, remove ...; otherwise, put ..."). An unresolvable
//     condition body is one loud Note and nothing moves (fail closed, the
//     NumResolved verdict — never a degrade-to-zero that silently picked a
//     branch).
//  2. A named CounterType$ without a condition (Plague Boiler, Sigurd, Guile,
//     Jhoira's Timebug, Lavabrink Floodgates): the decider (DefinedPlayer$,
//     default the resolving controller) answers a real election — remove or
//     put, with a skip option when Optional$ True (Lavabrink's "that player
//     may ...").
//  3. No CounterType$ (Clockspinning) or EachExistingCounter$ True
//     (Dramatist's Puppet, Quarry Hauler): the kinds the target ACTUALLY
//     carries are enumerated first — Clockspinning asks ONE combined pick
//     over (remove|put) × the kinds present (the answered option carries both
//     halves, "aor_remove:TIME"), EachExistingCounter$ elects for EVERY kind
//     in sequence — then the same add/remove election runs per kind.
//
// Each answered election re-enters through ResumeKind "aor_elect"; the
// election's kind is parsed out of the answer's own option encoding
// ("aor_remove:<kind>"), so no ask-to-ask state rides the fresh Ctx. The
// EachExistingCounter$ per-kind cursor rides rules' aorAsk pending map (the
// moveCounterAsk discipline), seeded into Ctx.AorAnswered.
func effAddOrRemoveCounter(h Host, c *Ctx, sa *cards.SA) {
	elect, akind := c.AorElect, c.AorKind
	answered := c.AorAnswered
	c.AorElect, c.AorKind, c.AorAnswered, c.AorDone = "", "", nil, false

	g := h.Game()
	named := strings.TrimSpace(sa.ParamStr(cards.PKCounterType))
	eachExisting := strings.EqualFold(strings.TrimSpace(sa.Params["EachExistingCounter"]), "True")
	if eachExisting && named != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented AddOrRemoveCounter shape: EachExistingCounter$ with CounterType$ " + named})
		return
	}
	amount := Num(h, c, sa, "CounterNum", 1)
	if amount < 0 {
		amount = 0
	}
	// The target: Defined$ when present, else the announced ValidTgts$ targets
	// Ctx.Targets carries (Defined's own fallback — the etched-host Charm mode
	// named its battle at cast time). The corpus is single-target throughout;
	// a walk naming several acts on the first under one loud Note (the
	// deliberate narrowing, corpus-unreachable).
	var target *state.Object
	objTargets := 0
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		if o := g.Obj(t.Obj); o != nil && (o.Zone == state.ZBattlefield || o.Zone == state.ZExile) {
			objTargets++
			if target == nil {
				target = o
			}
		}
	}
	if target == nil {
		return // nothing to act on (the defined path's silent skip)
	}
	if objTargets > 1 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented AddOrRemoveCounter shape: multiple targets; acted on the first"})
	}
	// The condition shape (named kind + RemoveConditionSVar$): no ask.
	if cond := strings.TrimSpace(sa.Params["RemoveConditionSVar"]); cond != "" && named != "" {
		n, ok := NumResolved(h, c, sa, "RemoveConditionSVar", 0)
		if !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented AddOrRemoveCounter shape: RemoveConditionSVar$ unresolvable (" + cond + ")"})
			return
		}
		if n > 0 {
			aorApplyAct(h, c, sa, target, named, "remove", amount)
		} else {
			aorApplyAct(h, c, sa, target, named, "put", amount)
		}
		return
	}
	// The kinds the election walks: the named kind, or the kinds the target
	// actually carries (positive counts, slice order — never a map walk).
	kinds := []string{named}
	if named == "" {
		kinds = counterKinds("", len(target.Counters),
			func(i int) string { return target.Counters[i].Kind },
			func(i int) int32 { return target.Counters[i].N })
		if len(kinds) == 0 {
			return // no counter to choose (Clockspinning on a bare card)
		}
	}
	// The decider: DefinedPlayer$ when it resolves (Lavabrink Floodgates'
	// DefinedPlayer$ TriggeredPlayer — the upkeep player chooses), else the
	// resolving controller. A chooser is never guessed: an unresolvable
	// DefinedPlayer$ is one loud Note and nothing moves.
	decider := c.Controller
	if spec := strings.TrimSpace(sa.ParamStr(cards.PKDefinedPlayer)); spec != "" {
		ts := Defined(h, c, &cards.SA{Params: map[string]string{"Defined": spec}})
		if len(ts) == 0 || !ts[0].IsPlayer {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "AddOrRemoveCounter DefinedPlayer$ unresolvable (" + spec + ")"})
			return
		}
		decider = ts[0].Player
	}
	// Apply the answered election first (fx42); a skip applies nothing but
	// still retires the kind (the pending map recorded it).
	if akind != "" && elect != "" && elect != "skip" {
		aorApplyAct(h, c, sa, target, akind, elect, amount)
	}
	optional := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKOptional)), "True")
	if !eachExisting && named == "" {
		if elect != "" {
			// The answered combined pick was applied above; this re-entry is
			// done — no second ask, no loop.
			return
		}
		// The absent-kind shape (Clockspinning): ONE combined pick —
		// (remove|put) × the kinds present — so the answered option carries
		// both halves of the choice ("aor_remove:TIME") and no ask-to-ask
		// state needs to survive the round trip. A single-kind target asks
		// the same 2-option election the named-kind shape does; Optional$
		// adds the skip option.
		d := &decision.Decision{Player: decider, Kind: decision.KChoose,
			Min: 1, Max: 1, Source: c.Source,
			ResumeKind: "aor_elect", ResumeSA: sa,
			Prompt: "Choose a counter: remove it, or put another one on it?"}
		for _, k := range kinds {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "aor_remove:" + k, Label: "Remove a " + k + " counter", Obj: target.ID, Player: decider})
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "aor_put:" + k, Label: "Put another " + k + " counter on it", Obj: target.ID, Player: decider})
		}
		if optional {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "aor_skip", Label: "Do nothing", Obj: target.ID, Player: decider})
		}
		if ans, ok := AskTape(h, d); ok {
			// The "aor_elect" answer in hand: the re-entry's application
			// (its multi-target Note first, as the re-entered walk emits it).
			aorTapeApply(h, c, sa, target, ans, amount, objTargets > 1)
			return
		}
		if Ask(h, d) == AskAsked {
			return // resolution suspended; the answer re-enters with Ctx.AorElect/AorKind set.
		}
		// No-host fallback (R-9): the first option — remove the first kind —
		// the exact mirror of botpolicy's first-option KChoose answer.
		aorApplyAct(h, c, sa, target, kinds[0], "remove", amount)
		return
	}
	for _, k := range kinds {
		if containsKind(answered, k) || (elect != "" && k == akind) {
			continue // already answered (earlier round, or this re-entry above)
		}
		d := &decision.Decision{Player: decider, Kind: decision.KChoose,
			Min: 1, Max: 1, Source: c.Source,
			ResumeKind: "aor_elect", ResumeSA: sa,
			Prompt: "Remove a " + k + " counter from it, or put another one on it?"}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "aor_remove:" + k, Label: "Remove a " + k + " counter", Obj: target.ID, Player: decider})
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "aor_put:" + k, Label: "Put another " + k + " counter on it", Obj: target.ID, Player: decider})
		if optional {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "aor_skip:" + k, Label: "Do nothing", Obj: target.ID, Player: decider})
		}
		if ans, ok := AskTape(h, d); ok {
			// The "aor_elect" answer in hand: apply it and walk on to the
			// next kind, as the re-entered walk does.
			aorTapeApply(h, c, sa, target, ans, amount, objTargets > 1)
			continue
		}
		if Ask(h, d) == AskAsked {
			return // resolution suspended; the answer re-enters with Ctx.AorElect/AorKind set.
		}
		// No-host fallback (R-9): the first option — remove — the exact mirror
		// of botpolicy's first-option KChoose answer.
		aorApplyAct(h, c, sa, target, k, "remove", amount)
	}
}

// aorAnswer decodes an "aor_elect" answer exactly as rules' resume arm does:
// the election ("remove", "put" or "skip") and its kind, parsed out of the
// answered option's own encoding ("aor_remove:<kind>"). A malformed answer
// elects remove with no kind.
func aorAnswer(ans []decision.Option) (elect, kind string) {
	elect = "remove"
	if len(ans) == 0 {
		return elect, ""
	}
	if ans[0].Kind == "aor_skip" {
		return "skip", ""
	}
	if strings.HasPrefix(ans[0].Kind, "aor_put:") {
		elect = "put"
	} else if strings.HasPrefix(ans[0].Kind, "aor_skip:") {
		elect = "skip"
	}
	if _, k, found := strings.Cut(ans[0].Kind, ":"); found {
		kind = k
	}
	return elect, kind
}

// aorTapeApply applies a tape-served "aor_elect" answer the way the legacy
// re-entry does: the re-entered walk re-emits its multi-target Note, then
// applies the answered election (a skip applies nothing).
func aorTapeApply(h Host, c *Ctx, sa *cards.SA, target *state.Object, ans []decision.Option, amount int32, multi bool) {
	if multi {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented AddOrRemoveCounter shape: multiple targets; acted on the first"})
	}
	elect, akind := aorAnswer(ans)
	if akind != "" && elect != "skip" {
		aorApplyAct(h, c, sa, target, akind, elect, amount)
	}
}

// aorApplyAct emits one election's CounterChange. Removal clamps to what the
// object actually carries (state's AddCounter clamps at zero either way; a
// negative overstatement would misreport) and puts at least one; a zero
// amount acts on nothing (the zero-batch no-op discipline both siblings
// follow). RememberRemovedCards$ True (Guile, Sonic Soldier) remembers the
// card the counter was removed FROM — one Choose "remembered" entry, the
// consumer (the chained ImmediateTrigger's ConditionDefined$ Remembered gate)
// reads presence, not count.
func aorApplyAct(h Host, c *Ctx, sa *cards.SA, o *state.Object, kind, act string, amount int32) {
	switch act {
	case "remove":
		count := o.Counter(kind)
		removed := amount
		if removed > count {
			removed = count
		}
		if removed <= 0 {
			return
		}
		h.Emit(events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: kind, Amount: -removed})
		if strings.EqualFold(strings.TrimSpace(sa.Params["RememberRemovedCards"]), "True") && c.Source != 0 {
			h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "remembered",
				IDs: []state.ObjID{o.ID}})
		}
	case "put":
		if amount <= 0 {
			return
		}
		h.Emit(events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: kind, Amount: amount})
	}
}

// containsKind reports whether the answered-kind cursor names k.
func containsKind(kinds []string, k string) bool {
	for _, have := range kinds {
		if have == k {
			return true
		}
	}
	return false
}

// effMoveCounter implements Forge's MoveCounterEffect: counters of
// CounterType$ move from an ORIGIN set to a DESTINATION set -- a counter
// leaves its origin once and lands ONCE (CR 122.5: a counter moves, it is
// not copied and not removed-and-put; a multi-destination sweep DISTRIBUTES
// the moved total across the destinations, round-robin in destination
// order). `effects.Register("MoveCounter")` is what makes the ability offered
// at all; before it every carrier fell to the generic
// "unimplemented API MoveCounter" Note and moved nothing (Diamond City's
// second ability, Weapon Rack, Aetherborn Marauder, Spike Cannibal, Bioshift,
// ... -- 33 raw SA lines over 32 corpus files).
//
// Forge names the two sets with four params, and the corpus's own oracle text
// disambiguates every combination:
//
//   - Origin = Source$ when present (a Defined$-style selector: Self is the
//     ability's source, ParentTarget/Targeted the parent ability's chosen
//     targets), else ValidSource$ (a battlefield filter sweep), else the
//     chosen targets.
//   - Destination = Defined$ when present, else ValidDefined$ (a sweep),
//     else the chosen targets -- but when the origin ALSO came from the
//     chosen targets (the 2-target "... onto another target ..." spells),
//     target 0 is the origin and the REMAINING targets are the destinations.
//
// CounterType$: a literal kind (P1P1/LOYALTY/SHIELD/...), All (every kind the
// carrier holds), EachNotOn (each kind the origin holds that the destination
// does not -- Goldberry), or Any (a real "choose a kind" ask among the
// distinct kinds the origin holds; a single-kind origin takes that kind with
// no ask, the strict-supersets convention). CounterNum$: a literal, X, All
// (everything of the kind on the origin), or Any (a real any-number ask up to
// what the origin holds -- Min 0 is a legitimate decline).
//
// Riders: RememberPut$ True appends each DESTINATION that received a counter
// to Ctx.Remembered (Goldberry's SVar:X:Remembered$Amount gate);
// RememberAmount$ True appends the origin id once per counter moved, so the
// engine's list-length channel (Count$RememberedNumber == len(Ctx.Remembered),
// the same encoding rememberRemoved uses) reads the AMOUNT (Black Panther's
// SVar:X:Count$RememberedNumber). An unresolvable CounterNum$ body, a
// CounterNum$ Any riding a multi-kind CounterType$ (All/EachNotOn), a
// TgtZone$ naming anything but the battlefield, and player origins or
// destinations are LOUD-degraded -- one Note naming the shape, nothing moves
// (the effPutCounterAll/effRemoveCounter exotic pattern). TargetUnique$ True
// (one carrier, vacuous on a single-target ask) is tolerated silently.
func effMoveCounter(h Host, c *Ctx, sa *cards.SA) {
	// fx42 scoping: capture and clear the answered asks BEFORE the walk, so a
	// nested MoveCounter below this one poses its own ask instead of
	// inheriting the outer answer (the Proliferate discipline).
	kindAns, kindDone := c.MoveCounterKind, c.MoveCounterKindDone
	nAns, nDone := c.MoveCounterN, c.MoveCounterNDone
	c.MoveCounterKind, c.MoveCounterKindDone = "", false
	c.MoveCounterN, c.MoveCounterNDone = 0, false

	kindParam := strings.TrimSpace(sa.ParamStr(cards.PKCounterType))
	numParam := strings.TrimSpace(sa.ParamStr(cards.PKCounterNum))

	// Loud-degrade the shapes the core cannot express, before anything moves.
	var exotic []string
	if zone := strings.TrimSpace(sa.ParamStr(cards.PKTgtZone)); zone != "" && !strings.EqualFold(zone, "Battlefield") {
		exotic = append(exotic, "TgtZone$ "+zone)
	}
	if raw, present := sa.Param(cards.PKCounterNum); present && numParam != "" && !strings.EqualFold(numParam, "All") && !strings.EqualFold(numParam, "Any") {
		if _, ok := NumResolved(h, c, sa, "CounterNum", 1); !ok {
			// A body Num cannot price (a bare SVar name the face lacks, an
			// exotic Count$ head) would silently move zero; name it instead.
			exotic = append(exotic, "CounterNum$ "+raw)
		}
	}
	if len(exotic) > 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented MoveCounter shape: " + strings.Join(exotic, ", ")})
		return
	}

	origin, originFromTargets, ok := moveCounterOrigin(h, c, sa)
	if !ok {
		return // unresolvable Source$: fail closed (the definedSpec discipline)
	}
	if len(origin) == 0 {
		return
	}
	dests := moveCounterDest(h, c, sa, originFromTargets)
	if len(dests) == 0 {
		return
	}

	// Player origins/destinations are out of scope (no corpus carrier needs
	// PlayerCounterChange here); name the shape rather than invent a move.
	for _, t := range origin {
		if t.IsPlayer {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented MoveCounter shape: player origin"})
			return
		}
	}
	for _, t := range dests {
		if t.IsPlayer {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented MoveCounter shape: player destination"})
			return
		}
	}

	g := h.Game()
	// Resolve the origin object(s) once (a battlefield object that left while
	// a nested ask was outstanding is skipped by the move walk below).
	origins := make([]*state.Object, 0, len(origin))
	for _, t := range origin {
		if o := g.Obj(t.Obj); o != nil && o.Zone == state.ZBattlefield {
			origins = append(origins, o)
		}
	}
	if len(origins) == 0 {
		return
	}

	// CounterType$ Any: a real kind pick over the distinct kinds the origins
	// hold, when more than one kind is present (a single-kind origin takes
	// that kind with no ask). A multi-origin Any shape has no single pick to
	// pose; loud-degrade it.
	chosenKind := kindParam
	if strings.EqualFold(kindParam, "Any") {
		if !kindDone {
			if len(origins) != 1 {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "unimplemented MoveCounter shape: CounterType$ Any over multiple origins"})
				return
			}
			kinds := moveCounterKindsOf(origins[0])
			if len(kinds) >= 2 {
				chooser := c.Controller
				d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
					Min: 1, Max: 1, Source: c.Source, ResumeKind: "move_counter_kind", ResumeSA: sa,
					Prompt: "MoveCounter: choose a counter kind to move"}
				for _, k := range kinds {
					d.Options = append(d.Options, decision.Option{Index: len(d.Options),
						Kind: "move_counter_kind", Label: k})
				}
				if ans, ok := AskTape(h, d); ok {
					// The "move_counter_kind" answer in hand; an empty
					// one moves nothing, as the re-entry reads it.
					chosenKind = counterAnswerLabel(ans)
					if chosenKind == "" {
						return
					}
				} else if Ask(h, d) == AskAsked {
					return
				} else {
					// No host (R-9): the deterministic first-kind stand-in, the
					// same pick botpolicy's arm takes.
					chosenKind = kinds[0]
				}
			} else if len(kinds) == 1 {
				chosenKind = kinds[0]
			} else {
				return // origin holds no counters: nothing moves
			}
		} else {
			chosenKind = kindAns
			if chosenKind == "" {
				return
			}
		}
	}

	// CounterNum$ All means "everything of the kind on the origin" -- resolved
	// per origin below. A literal/X resolves once. CounterNum$ Any asks the
	// amount: only a single-kind, single-origin shape can pose one ask.
	numAll := strings.EqualFold(numParam, "All")
	numAny := strings.EqualFold(numParam, "Any")
	var num int32
	if !numAll && !numAny {
		num = Num(h, c, sa, "CounterNum", 1)
		if num < 0 {
			num = 0
		}
	}
	if numAny {
		if strings.EqualFold(kindParam, "All") || strings.EqualFold(kindParam, "EachNotOn") {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented MoveCounter shape: CounterNum$ Any over multiple kinds"})
			return
		}
		if len(origins) != 1 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented MoveCounter shape: CounterNum$ Any over multiple origins"})
			return
		}
		if !nDone {
			maxN := origins[0].Counter(chosenKind)
			// No host (R-9): the deterministic take-all stand-in.
			num = maxN
			if maxN > 0 {
				chooser := c.Controller
				d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
					Min: 0, Max: int(maxN), Source: c.Source, ResumeKind: "move_counter", ResumeSA: sa,
					Prompt: "MoveCounter: choose how many counters to move"}
				for i := 0; i <= int(maxN); i++ {
					d.Options = append(d.Options, decision.Option{Index: len(d.Options),
						Kind: "move_counter", Label: fmt.Sprintf("%d", i), Amount: i})
				}
				if ans, ok := AskTape(h, d); ok {
					// The "move_counter" amount in hand (0 is a decline;
					// a malformed answer moves nothing).
					num = 0
					if len(ans) > 0 {
						num = int32(ans[0].Amount)
					}
				} else if Ask(h, d) == AskAsked {
					return
				}
			}
		} else {
			num = nAns
		}
	}

	// The move walk. Each origin loses `moved` of each kind; the moved total
	// is DISTRIBUTED across the destinations (CR 122.5 -- see the walk).
	// RememberPut$ appends the destinations that actually received a counter;
	// RememberAmount$ appends the origin once per counter moved.
	rememberPut := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberPut)), "True")
	rememberAmount := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberAmount)), "True")
	var rememberedDests []state.Target
	var amountIDs []state.ObjID
	for _, o := range origins {
		for _, k := range moveCounterKindsFor(chosenKind, o, dests, g) {
			count := o.Counter(k)
			if count <= 0 {
				continue
			}
			moved := num
			if numAll {
				moved = count
			}
			if moved > count {
				moved = count
			}
			if moved <= 0 {
				continue
			}
			// The live destinations for this origin: on the battlefield and
			// not the origin itself (moving a counter from a permanent to
			// itself is a no-op, never a -/+ pair on the same id). Built BEFORE
			// the origin's loss is emitted: an origin whose every destination
			// left the battlefield (or was never live) keeps its counters.
			live := make([]*state.Object, 0, len(dests))
			for _, t := range dests {
				if d := g.Obj(t.Obj); d != nil && d.Zone == state.ZBattlefield && d.ID != o.ID {
					live = append(live, d)
				}
			}
			if len(live) == 0 {
				continue
			}
			h.Emit(events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: k, Amount: -moved})
			// CR 122.5: a moved counter leaves the origin once and lands ONCE
			// -- a multi-destination sweep (Forgotten Ancient's "move any number
			// of +1/+1 counters ... onto other creatures") DISTRIBUTES moved
			// across the destinations, it never gives each destination the
			// whole moved set (that would mint (M-1)*moved counters out of
			// nothing). Distribution is the deterministic round-robin in
			// destination order (a stand-in for the per-destination election
			// the card text implies -- M4): destination j receives
			// floor((moved+M-1-j)/M), so 2 over 2 creatures is 1 and 1, and a
			// single-destination shape -- every other measured carrier -- is
			// byte-identical to the whole-set move it had before.
			m := int32(len(live))
			for j, d := range live {
				share := (moved + m - 1 - int32(j)) / m
				if share <= 0 {
					continue
				}
				h.Emit(events.Event{Kind: events.CounterChange, Obj: d.ID, Counter: k, Amount: share})
				rememberedDests = append(rememberedDests, state.Target{Obj: d.ID})
			}
			for i := int32(0); i < moved; i++ {
				amountIDs = append(amountIDs, o.ID)
			}
		}
	}
	if rememberPut && len(rememberedDests) > 0 {
		c.Remembered = append(c.Remembered, rememberedDests...)
	}
	if rememberAmount && len(amountIDs) > 0 {
		c.Remembered = append(c.Remembered, objTargets(amountIDs)...)
	}
}

// moveCounterChosen is the chosen-target set a MoveCounter's origin/
// destination defaults read: the generic ValidTgts$ pre-ask's ANSWER
// (c.PickedTargets, mvts1) when one is outstanding, else the resolution's own
// target list. Preferring PickedTargets is the established convention
// (effects/context.go Defined, effects/damage.go, effects/zone.go): a sub the
// pre-ask asked (Nesting Grounds' `Source$ ParentTarget | ValidTgts$
// Permanent`, Rikku's, Black Panther's) must receive the sub's OWN chosen
// targets, never the parent's (c.Targets), while a depth-0 shape the
// placement/announcement ask covered (Bioshift's TargetMin$ 2, a Weapon Rack
// activation) has PickedTargets nil and keeps c.Targets.
func moveCounterChosen(c *Ctx) []state.Target {
	if c.PickedTargets != nil {
		return c.PickedTargets
	}
	return c.Targets
}

// moveCounterOrigin resolves the FROM set of a MoveCounter: Source$ (a
// fail-closed Defined$-style selector), else ValidSource$ (a battlefield
// filter sweep), else the chosen targets -- ALL of them when the SA names its
// destination explicitly (Defined$/ValidDefined$, so the chosen targets are
// only the origin half of a "target X, onto CARDNAME" shape), else only
// target 0 (the 2-target "... onto another target ..." spells, where the
// remaining targets are the destinations). originFromTargets reports the
// 2-target branch so the destination default can drop target 0. ok is false
// only for an explicit Source$ this build cannot resolve -- the fail-closed
// direction, never a fallback to the source or the targets.
func moveCounterOrigin(h Host, c *Ctx, sa *cards.SA) (ts []state.Target, fromTargets, ok bool) {
	if src := strings.TrimSpace(sa.Params["Source"]); src != "" {
		t, resolved := definedSpec(h, c, src)
		return t, false, resolved
	}
	if filt := strings.TrimSpace(sa.ParamStr(cards.PKValidSource)); filt != "" {
		return battlefieldValidTargets(h, c, filt), false, true
	}
	if moveCounterNamesDestination(sa) {
		return copyTargets(moveCounterChosen(c)), false, true
	}
	chosen := moveCounterChosen(c)
	if len(chosen) == 0 {
		return nil, true, true
	}
	return copyTargets(chosen[:1]), true, true
}

// moveCounterNamesDestination reports whether the SA names its TO set
// explicitly, which decides whether the chosen targets are the whole origin
// set or just target 0.
func moveCounterNamesDestination(sa *cards.SA) bool {
	return strings.TrimSpace(sa.ParamStr(cards.PKDefined)) != "" ||
		strings.TrimSpace(sa.Params["ValidDefined"]) != ""
}

// moveCounterDest resolves the TO set: Defined$, else ValidDefined$, else the
// chosen targets (moveCounterChosen: the pre-ask's answer when one is
// outstanding) -- the remaining targets after target 0 when the origin also
// came from the chosen targets (the 2-target shapes), else all of them.
func moveCounterDest(h Host, c *Ctx, sa *cards.SA, originFromTargets bool) []state.Target {
	if strings.TrimSpace(sa.ParamStr(cards.PKDefined)) != "" {
		return Defined(h, c, sa)
	}
	if filt := strings.TrimSpace(sa.Params["ValidDefined"]); filt != "" {
		return battlefieldValidTargets(h, c, filt)
	}
	ts := moveCounterChosen(c)
	if originFromTargets {
		if len(ts) <= 1 {
			return nil
		}
		return copyTargets(ts[1:])
	}
	return copyTargets(ts)
}

// moveCounterKindsOf lists the distinct counter kinds an object holds with a
// POSITIVE count, in deterministic slice order (the counterKinds discipline).
func moveCounterKindsOf(o *state.Object) []string {
	return dedupeKinds(counterKinds("", len(o.Counters),
		func(i int) string { return o.Counters[i].Kind },
		func(i int) int32 { return o.Counters[i].N }))
}

// moveCounterKindsFor expands the CounterType$ parameter for one origin: a
// literal or an Any pick is that one kind; All is every kind the origin
// holds; EachNotOn is each kind the origin holds that NO destination already
// has (Goldberry's "each kind not on CARDNAME").
func moveCounterKindsFor(kind string, o *state.Object, dests []state.Target, g *state.Game) []string {
	switch {
	case strings.EqualFold(kind, "All"):
		return moveCounterKindsOf(o)
	case strings.EqualFold(kind, "EachNotOn"):
		var out []string
		for _, k := range moveCounterKindsOf(o) {
			present := false
			for _, t := range dests {
				if d := g.Obj(t.Obj); d != nil && d.Counter(k) > 0 {
					present = true
					break
				}
			}
			if !present {
				out = append(out, k)
			}
		}
		return out
	default:
		return []string{kind}
	}
}

// rememberRemoved records RememberRemoved$'s persistent half: one Choose
// "remembered" event on the source object with the removed-from object's id
// repeated once per removed counter (events.Apply appends each id, so the
// source's event-backed Remembered -- Count$RememberedSize's read -- grows by
// exactly the removed count). A player entry rides the PlayerRef encoding
// rememberedFrom decodes. The ctx-level list is deliberately NOT touched:
// the only consumer measured for this rider reads the persistent list, and a
// ctx append would widen a chained Defined$ Remembered reader for free.
func rememberRemoved(h Host, c *Ctx, sa *cards.SA, id state.ObjID, removed int32) {
	if !strings.EqualFold(strings.TrimSpace(sa.Params["RememberRemoved"]), "True") || c.Source == 0 {
		return
	}
	ids := make([]state.ObjID, 0, removed)
	for i := int32(0); i < removed; i++ {
		ids = append(ids, id)
	}
	h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "remembered", IDs: ids})
}

// effProliferate implements Forge's Proliferate (CR 701.27): the resolving
// player chooses any number of permanents and/or players that already have at
// least one counter (of any kind), and gives each chosen recipient another
// counter of EACH kind already there. A recipient carrying no counter of any
// kind is not choosable (CR 701.27a's "that have a counter on them"), and
// chooses nothing when no such recipient exists -- silently, with no ask and
// no Note (the OnlyEmptyAnswer discipline: a Max-0 KChoose is never posted).
//
// The eligible population is the battlefield in seat order (g.AliveFrom, then
// g.Zone per seat -- both deterministic slices, never a map walk) followed by
// the alive players with a counter. The chooser is the resolving controller
// (every corpus carrier proliferates for its own controller; Forge's
// ProliferateEffect takes its chooser from the ability's controller).
//
// The ask is the any-number convention effDiscard's Optional$ shape uses, NOT
// the strict-supersets gate putCounterChoose follows: Min 0 and Max the
// eligible count is a REAL choice the moment one eligible recipient exists
// ("{} vs {that one}" is a genuine election), so `len(eligible) < 2` does not
// suppress it. Only zero eligible is a no-choice shape.
//
// Amount$ is the number of times to proliferate, resolved through Num (a
// literal, an SVar or an inline Count$ expression). A value Num cannot resolve
// (the corpus's X-on-a-non-cast and `Number$2/Minus.Y` bodies) loud-degrades
// with one Note and proliferates nothing -- the documented fail-closed
// direction, never a silent 1. Because proliferation adds +1 of each EXISTING
// kind, N proliferations over one fixed recipient set equal one batch of +N;
// this build poses ONE ask and applies +Amount per recipient (deterministic
// and documented) rather than N sequential asks.
//
// The answer re-enters through ResumeKind "proliferate" with Ctx.Proliferate,
// which carries BOTH shapes -- an object recipient (Obj) and a player
// recipient (Player + IsPlayer) -- so the shared "counter_pick" arm, which
// reads Obj only, is deliberately not reused. RememberPut$ True (Ripples of
// Potential) remembers exactly the recipients that took a counter.
func effProliferate(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	// fx42 scoping: capture and clear the answered pick BEFORE the walk, so a
	// nested Proliferate below this one poses its own ask instead of
	// inheriting the outer answer (the BlightPicks discipline).
	picks := c.Proliferate
	done := c.ProliferateDone
	c.Proliferate, c.ProliferateDone = nil, false

	// Loud-fail-closed on any parameter outside the whitelist (the effBlight
	// case-whitelist shape): Amount$/RememberPut$ read here, Defined$/
	// ValidTgts$ are inert on every corpus carrier, Cost$/SorcerySpeed$/
	// Planeswalker$ are activation metadata the offer machinery already reads,
	// the Condition* family is consumed by the shared SVar-condition gate in
	// Resolve, SubAbility$ is chained by the ordinary walk, and the
	// description keys are display-only. One Note names the first unknown key
	// in sorted order (a map range must never reach an event unsorted) and the
	// whole body no-ops -- so an unmodelled shape degrades loudly rather than
	// silently guessing.
	var unknown []string
	for k := range sa.Params {
		switch k {
		case "Amount", "RememberPut", "Defined", "ValidTgts",
			"Cost", "SorcerySpeed", "Planeswalker",
			"ConditionCheckSVar", "ConditionSVarCompare",
			"ConditionDefined", "ConditionPresent", "ConditionCompare",
			"SubAbility", "SpellDescription", "StackDescription",
			"TriggerDescription", "Description", "PrecostDesc", "CostDesc",
			"ActivationZone", "AILogic", "AIPreference", "DeckHas", "DeckHints":
		default:
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "Proliferate unmodelled parameter " + unknown[0]})
		return
	}

	n, ok := NumResolved(h, c, sa, "Amount", 1)
	if !ok {
		// Num resolved an absent Amount$ to its default 1; a value that is
		// PRESENT but unresolvable is the loud-degrade shape. NumResolved
		// reports ok=false for both, so distinguish by presence.
		if _, present := sa.Param(cards.PKAmount); present {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "Proliferate Amount$ unresolvable (" + sa.ParamStr(cards.PKAmount) + ")"})
			return
		}
		n = 1
	}
	// A bare `Amount$ X` resolves through the cast's own X, but an activated
	// ability (Karn's Bastion-style `Cost$ 1 T | Amount$ X`) has no X to
	// resolve -- Num returns the zero c.X, which is not a legitimate "zero
	// times" but an unannounced count. Loud-degrade it (the same fail-closed
	// direction as the unresolvable bodies) rather than silently proliferating
	// nothing: the caller can tell an announced X (nonzero) from none.
	if strings.TrimSpace(sa.ParamStr(cards.PKAmount)) == "X" && n <= 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "Proliferate Amount$ X unresolvable (no X announced)"})
		return
	}
	if n <= 0 {
		return
	}

	if done {
		applyProliferate(h, c, sa, picks, n)
		h.Emit(events.Event{Kind: events.Proliferate, Obj: c.Source, Player: c.Controller})
		return
	}

	// The eligible set: battlefield recipients in seat/zone order, then alive
	// players with a counter. Both walks are deterministic slices.
	var eligible []state.Target
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if o := g.Obj(id); o != nil && hasCounters(o.Counters) {
				eligible = append(eligible, state.Target{Obj: id})
			}
		}
	}
	for _, p := range g.AliveFrom(0) {
		if hasCounters(g.Players[p].Counters) {
			eligible = append(eligible, state.Target{Player: p, IsPlayer: true})
		}
	}
	if len(eligible) == 0 {
		// Nothing carries a counter: no choice exists, so no ask (and no
		// Note -- this is the correct resolution, not a degradation). The
		// proliferate action still happened, so the completed-action marker
		// still records it (trig-proliferate: a "whenever you proliferate"
		// trigger fires on the action, not on a counter landing).
		h.Emit(events.Event{Kind: events.Proliferate, Obj: c.Source, Player: c.Controller})
		return
	}

	chooser := c.Controller
	d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
		Min:        0,
		Max:        len(eligible),
		Source:     c.Source,
		ResumeKind: "proliferate",
		ResumeSA:   sa,
		Prompt:     "Proliferate: choose any number of permanents and/or players"}
	for _, t := range eligible {
		label := "a player"
		if t.IsPlayer {
			if t.Player >= 0 && int(t.Player) < len(g.Players) {
				label = g.Players[t.Player].Name
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "proliferate", Label: label, Obj: 0, Player: t.Player})
			continue
		}
		o := g.Obj(t.Obj)
		if o != nil && o.Face() != nil {
			label = o.Face().Name
		}
		owner := chooser
		if o != nil {
			owner = o.Controller
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options),
			Kind: "proliferate", Label: label, Obj: t.Obj, Player: owner})
	}
	if ans, ok := AskTape(h, d); ok {
		// The "proliferate" answer in hand: the re-entry's application.
		applyProliferate(h, c, sa, proliferateAnswer(ans), n)
		h.Emit(events.Event{Kind: events.Proliferate, Obj: c.Source, Player: c.Controller})
		return
	}
	if Ask(h, d) == AskAsked {
		return // resolution suspended; the answer re-enters with Ctx.Proliferate set.
	}
	// The no-host (R-9) and empty-answer stand-in takes ALL eligible, the
	// exact mirror of botpolicy's "proliferate" arm, so a bot-answered ask
	// emits the same events the silent build would.
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: chooser,
		Text: "proliferate resolved without a choice (no engine host to ask)"})
	applyProliferate(h, c, sa, eligible, n)
	h.Emit(events.Event{Kind: events.Proliferate, Obj: c.Source, Player: c.Controller})
}

// applyProliferate gives each live chosen recipient +n of each kind of
// counter it already carries, one event per kind (objects -> CounterChange,
// players -> PlayerCounterChange), then remembers the recipients when the SA
// carries RememberPut$ True. A recipient that left the battlefield (or died)
// while the decision was outstanding takes nothing -- the putCounterPickApply
// zone-guard stance.
func applyProliferate(h Host, c *Ctx, sa *cards.SA, picks []state.Target, n int32) {
	g := h.Game()
	remember := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberPut)), "True")
	var placed []state.Target
	for _, t := range picks {
		if t.IsPlayer {
			p := t.Player
			if int(p) < 0 || int(p) >= len(g.Players) || g.Players[p].Lost {
				continue
			}
			pl := &g.Players[p]
			kinds := counterKinds("", len(pl.Counters), func(i int) string { return pl.Counters[i].Kind }, func(i int) int32 { return pl.Counters[i].N })
			for _, k := range kinds {
				h.Emit(events.Event{Kind: events.PlayerCounterChange, Player: p,
					Counter: k, Amount: n})
			}
			placed = append(placed, state.Target{Player: p, IsPlayer: true})
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		kinds := counterKinds("", len(o.Counters), func(i int) string { return o.Counters[i].Kind }, func(i int) int32 { return o.Counters[i].N })
		for _, k := range kinds {
			h.Emit(events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: k, Amount: n})
		}
		placed = append(placed, state.Target{Obj: o.ID})
	}
	if remember && len(placed) > 0 {
		c.Remembered = append(c.Remembered, placed...)
	}
}

// effRegenerate grants a this-turn shield consumed by ReplaceDestruction.
func effRegenerate(h Host, c *Ctx, sa *cards.SA) {
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		h.Emit(events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: "Shield", Amount: 1})
	}
}

// proliferateAnswer decodes a "proliferate" answer exactly as rules' resume
// arm does: an object recipient carries Obj, a player recipient carries
// Player with Obj 0.
func proliferateAnswer(ans []decision.Option) []state.Target {
	out := make([]state.Target, 0, len(ans))
	for _, o := range ans {
		if o.Obj != 0 {
			out = append(out, state.Target{Obj: o.Obj})
			continue
		}
		out = append(out, state.Target{Player: o.Player, IsPlayer: true})
	}
	return out
}

// counterAnswerObjs is the object list an answered pick names, in answer
// order (the "counter_pick"/"counter_dist" arms' decode: options with no
// object are dropped).
func counterAnswerObjs(ans []decision.Option) []state.ObjID {
	out := make([]state.ObjID, 0, len(ans))
	for _, o := range ans {
		if o.Obj != 0 {
			out = append(out, o.Obj)
		}
	}
	return out
}

// counterAnswerLabel is a one-pick answer's Label ("" for a malformed empty
// answer), the "counter_kind"/"move_counter_kind" arms' decode.
func counterAnswerLabel(ans []decision.Option) string {
	if len(ans) == 0 {
		return ""
	}
	return ans[0].Label
}

// counterAnswerLabels is every answered option's Label, the "counter_kinds"
// arm's decode.
func counterAnswerLabels(ans []decision.Option) []string {
	var out []string
	for _, o := range ans {
		out = append(out, o.Label)
	}
	return out
}
