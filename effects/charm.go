package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func CharmRepeatModes(sa *cards.SA) bool {
	return sa != nil && strings.EqualFold(strings.TrimSpace(sa.Params["CanRepeatModes"]), "True")
}

// CharmModeBounds resolves a Charm's selectable range. Forge defaults
// MinCharmNum$ to CharmNum$, but an explicit MinCharmNum$ permits choosing
// fewer modes. Both values use Num so literal, SVar, and inline Count$ forms
// share the same evaluation in spell, trigger, and resolution paths.
// Optional$ True (Shadrix Silverquill's "you may choose two") lowers the
// minimum to 0: the election is real, and choosing nothing is a legal answer
// at every site that asks (the placement ask, the cast announcement and
// effCharm's own mid-resolution ask all share this one helper).
//
// The third result is CanRepeatModes$: when it is set, max is NOT clamped to
// the number of distinct modes (a repeatable CharmNum$ 5 over 3 modes is
// legal -- the Commands cycle), and the caller must mark its decision
// Repeatable so Decision.Validate permits the repeated index. The clamp is
// what makes a non-repeatable CharmNum$ greater than its mode count degrade
// to "pick every distinct mode" rather than demand an impossible answer.
func CharmModeBounds(h Host, c *Ctx, sa *cards.SA, choices int) (min, max int, repeat bool) {
	repeat = CharmRepeatModes(sa)
	max = int(Num(h, c, sa, "CharmNum", 1))
	if max < 1 {
		max = 1
	}
	min = max
	if _, ok := sa.Params["MinCharmNum"]; ok {
		min = int(Num(h, c, sa, "MinCharmNum", int32(min)))
	}
	if strings.EqualFold(sa.ParamStr(cards.PKOptional), "True") {
		min = 0
	}
	if !repeat && max > choices {
		max = choices
	}
	if min < 0 {
		min = 0
	}
	return min, max, repeat
}

// CharmChoiceRestriction returns a Charm SA's ChoiceRestriction$ value
// ("ThisTurn", "ThisGame", "YourLastCombat"), or "" when the SA carries
// none. It is the ONE reader the ask sites filter through and the answer
// handler records through, so they cannot disagree about the scope.
func CharmChoiceRestriction(sa *cards.SA) string {
	if sa == nil {
		return ""
	}
	return strings.TrimSpace(sa.Params["ChoiceRestriction"])
}

// CharmEligibleModes filters a Charm's Choices$ SVar names against the mode
// picks already recorded on source (state.Object.ModeChoices) under the SA's
// ChoiceRestriction$ scope -- the "choose one that hasn't been chosen this
// turn" rule. The returned slice keeps the input's order, so the caller's
// option indices stay dense and map back to the same SVar names.
//
// ThisTurn, ThisGame and YourLastCombat are read from the source object's
// event-folded pick log. Unknown non-empty scopes fail open (with one Note per
// ask), rather than silently withholding a potentially legal mode.
func CharmEligibleModes(h Host, source state.ObjID, sa *cards.SA, choices []string) []string {
	scope := CharmChoiceRestriction(sa)
	if scope == "" || source == 0 {
		return choices
	}
	if scope != state.ModeScopeThisTurn && scope != state.ModeScopeThisGame && scope != state.ModeScopeYourLastCombat {
		h.Emit(events.Event{Kind: events.Note, Obj: source, Text: "unmodelled ChoiceRestriction$ scope: " + scope})
		return choices
	}
	o := h.Game().Obj(source)
	if o == nil || len(o.ModeChoices) == 0 {
		return choices
	}
	excluded := make(map[string]bool, len(choices))
	for _, mc := range o.ModeChoices {
		if mc.Scope != scope {
			continue
		}
		if scope == state.ModeScopeYourLastCombat &&
			mc.Turn == o.CurCombatTurn && mc.Combat == o.CurCombatCombat {
			// A pick made in the current combat does not constrain another
			// trigger in that same combat; YourLastCombat means the preceding
			// combat, whose picks were retained at BeginCombat rotation.
			continue
		}
		excluded[mc.Mode] = true
	}
	if len(excluded) == 0 {
		return choices
	}
	out := make([]string, 0, len(choices))
	for _, name := range choices {
		if !excluded[name] {
			out = append(out, name)
		}
	}
	return out
}

// RecordCharmChoices emits one events.Choose marker per answered mode name so
// a LATER offer of the same Charm on the same source sees the pick through
// CharmEligibleModes. It records all three supported corpus scopes; unknown
// scopes are noted at offer time and remain unrecorded. The scope is encoded
// in the event's Counter (state.ModeChoiceCounterPrefix + scope) and the mode
// name in Text; events.Apply stamps combat identity from its own clock, so a
// replay derives the same log.
func RecordCharmChoices(h Host, source state.ObjID, sa *cards.SA, names []string) {
	scope := CharmChoiceRestriction(sa)
	if (scope != state.ModeScopeThisTurn && scope != state.ModeScopeThisGame && scope != state.ModeScopeYourLastCombat) || source == 0 || len(names) == 0 {
		return
	}
	counter := state.ModeChoiceCounterPrefix + scope
	for _, name := range names {
		if name == "" {
			continue
		}
		h.Emit(events.Event{Kind: events.Choose, Obj: source, Counter: counter, Text: name})
	}
}

// CharmUniqueNone/Supported/Unsupported classify a Charm's chosen-mode set
// against the cross-mode "each mode must target a different player" family
// (Shadrix Silverquill, the Tarkir/Ninja duo cycle, Balor, Vindictive Lich,
// Chaos Balor -- 8 corpus files, all trigger-side DB$ Charm).
type CharmUniqueStatus int

const (
	// CharmUniqueNone: fewer than two target-bearing chosen modes, or none
	// of them carries TargetUnique$ — the ordinary shared-target narrowing
	// applies, byte-identically to the pre-family engine.
	CharmUniqueNone CharmUniqueStatus = iota
	// CharmUniqueSupported: at least two target-bearing chosen modes, every
	// one of them single-target (no TargetMin$/TargetMax$ beyond 1), every
	// one targeting the SAME player-kind spec ("Player" or "Opponent"),
	// and at least one carrying TargetUnique$ True. The combined
	// different-player target ask is posed and the per-mode split applies.
	CharmUniqueSupported
	// CharmUniqueUnsupported: TargetUnique$ is present on the chosen modes
	// but some member the combined ask cannot serve — differing ValidTgts$
	// specs, a non-player spec, or multi-target bounds. The ordinary
	// narrowing keeps and a loud Note names the shape (never silent).
	CharmUniqueUnsupported
)

// charmUniquePlayerSpec reports whether a ValidTgts$ spec names players in
// the exact form every corpus carrier of the family uses. Wider player
// grammars are not served: a "You" spec could never satisfy two different
// players anyway, and a compound spec's candidates are not all players.
func charmUniquePlayerSpec(spec string) bool {
	return spec == "Player" || spec == "Opponent"
}

// charmUniqueBounds mirrors rules' targetBounds for the single-target check:
// absent TargetMin$/TargetMax$ mean 1..1 (the M1 single-target contract).
// Anything a caller set explicitly beyond 1..1 keeps the mode out of the
// combined ask.
func charmUniqueBounds(sa *cards.SA) (min, max int) {
	min, max = 1, 1
	if v, ok := sa.Param(cards.PKTargetMin); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			min = n
		}
	}
	if v, ok := sa.Param(cards.PKTargetMax); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			max = n
		}
	}
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	return min, max
}

// CharmCrossModeShape classifies a Charm's mode list for the cross-mode
// TargetUnique family, with a reason fragment for the Unsupported loud Note.
// It is deliberately a property of the CHARM's full
// Choices$ list, not of whichever subset a particular answer selected: the
// classification must be stable across the charm's whole lifetime, because
// the per-mode target split (effCharm) and the suspension re-entry
// (rules' resumeResolution) re-derive it after a mid-mode ask — and a
// continuation can carry only a suffix of the original chosen order.
// Measured at the corpus pin: every charm outside the 8-file family carries
// TargetUnique$ on NONE of its target-bearing modes (the anyUnique trigger
// below never fires for them), so they classify None and keep today's
// narrowing byte-identically.
func CharmCrossModeShape(svars map[string]string, modes []string) (CharmUniqueStatus, string) {
	var spec string
	tbms, anyUnique, oneSpec, boundsOK := 0, false, true, true
	for _, name := range modes {
		sub := cards.ResolveSVar(svars, strings.TrimSpace(name))
		if sub == nil {
			continue
		}
		s := strings.TrimSpace(sub.ParamStr(cards.PKValidTgts))
		if s == "" {
			continue
		}
		tbms++
		if strings.EqualFold(sub.ParamStr(cards.PKTargetUnique), "True") {
			anyUnique = true
		}
		if spec == "" {
			spec = s
		} else if s != spec {
			oneSpec = false
		}
		if min, max := charmUniqueBounds(sub); min != 1 || max != 1 {
			boundsOK = false
		}
	}
	if tbms < 2 || !anyUnique {
		return CharmUniqueNone, ""
	}
	if !oneSpec {
		return CharmUniqueUnsupported, "differing ValidTgts$ specs across the target-bearing modes"
	}
	if !charmUniquePlayerSpec(spec) {
		return CharmUniqueUnsupported, "ValidTgts$ " + spec + " is not a player-kind spec"
	}
	if !boundsOK {
		return CharmUniqueUnsupported, "a target-bearing mode declares multi-target bounds"
	}
	return CharmUniqueSupported, ""
}

// charmCrossModeRun is effCharm's cross-mode TargetUnique family runner. It
// runs the chosen modes in order, giving each target-bearing mode its OWN
// target — the positional slice of Ctx.Targets the combined placement ask
// recorded — instead of the one-undivided target list every mode shared
// before. A mode that suspends (donnie's and mikey's hidden graveyard pick)
// stops the run and reports the remaining modes as a continuation
// (Host.SuspendCharmRest), so the rest re-enter through the answered ask's
// chain rather than running while the suspension is still outstanding.
// Non-target-bearing modes keep the shared context exactly as before.
// Returns false when the shape does not apply and the caller must keep the
// historical shared-target loop.
func charmCrossModeRun(h Host, c *Ctx, sa *cards.SA, names []string) bool {
	choices := strings.Split(sa.ParamStr(cards.PKChoices), ",")
	for i := range choices {
		choices[i] = strings.TrimSpace(choices[i])
	}
	if status, _ := CharmCrossModeShape(c.SVars, choices); status != CharmUniqueSupported {
		return false
	}
	var tbmIdx []int
	for i, name := range names {
		if sub := cards.ResolveSVar(c.SVars, name); sub != nil && strings.TrimSpace(sub.ParamStr(cards.PKValidTgts)) != "" {
			tbmIdx = append(tbmIdx, i)
		}
	}
	k := len(tbmIdx)
	if k == 0 || len(c.Targets) < k {
		// The running mode set carries target-bearing modes the placement ask
		// could not serve (an insufficient-candidate fallback, or a spell-side
		// single-target ask): keep the shared list, exactly the historical
		// narrowing, rather than inventing an assignment the ask never made.
		return false
	}
	// Assignment: the j-th target-bearing mode of the RUNNING list takes
	// targets[len(targets)-k+j]. On a full run that is targets[j] — the
	// combined ask's answer order, which is the chosen-mode order. On a
	// suffix continuation the remaining target-bearing modes are the last
	// ones of the original order, so the last k targets are theirs.
	base := len(c.Targets) - k
	ti := 0
	for i, name := range names {
		sub := cards.ResolveSVar(c.SVars, name)
		if sub == nil {
			continue
		}
		if ti < k && tbmIdx[ti] == i {
			saved := c.Targets
			savedOffered := c.OfferedSA
			c.Targets = []state.Target{c.Targets[base+ti]}
			// The combined placement ask covered THIS mode's targeting (its
			// assignment is positional); mark it so the generic ValidTgts$
			// pre-ask does not re-pose the cross-mode question per mode --
			// both on the initial pass (where the resolution-level marker's
			// bool would also suppress it) and on a charm_rest resume, where
			// the resume ctx carries only the FIRST chosen mode as OfferedSA
			// (task mvts1).
			c.OfferedSA = sub
			Resolve(h, c, sub)
			c.OfferedSA = savedOffered
			c.Targets = saved
			ti++
		} else {
			Resolve(h, c, sub)
		}
		if h.Suspended() {
			// The mode's own chain posed a mid-resolution ask: stop here. The
			// remaining modes resume through SuspendCharmRest's continuation
			// once the answer lands — never while the suspension is live (the
			// historical loop ran them immediately, before the answered mode
			// had even completed). An empty rest (this was the last mode) is
			// still reported, so the Charm re-enters and walks its own Sub
			// instead of the enclosing loop recording a plain continuation
			// that resumes at a nil Sub and emits a false degradation Note.
			h.SuspendCharmRest(sa, names[i+1:])
			return true
		}
	}
	return true
}

// effVillainousChoice makes the player named by Defined$ choose one of the
// supplied ability bodies. Unlike a modal trigger's placement choice, the
// victim's choice happens during resolution: the victim is remembered before
// the chosen body runs, so Defined$ Remembered and Player.IsRemembered in the
// body refer to the victim.
func effVillainousChoice(h Host, c *Ctx, sa *cards.SA) {
	choices := strings.Split(sa.ParamStr(cards.PKChoices), ",")
	if len(choices) == 0 || c.SVars == nil {
		return
	}
	for i := range choices {
		choices[i] = strings.TrimSpace(choices[i])
	}
	// A resumed answer is scoped to the current victim. Once its body has
	// completed, advance to the next Defined$ player and pose a fresh ask.
	if c.Modes != nil {
		names := c.Modes
		c.Modes = nil
		for _, name := range names {
			if sub := cards.ResolveSVar(c.SVars, name); sub != nil {
				Resolve(h, c, sub)
			}
			if h.Suspended() {
				// The chosen body posed a nested mid-resolution ask (DBSac's
				// sacrifice picker is the live carrier). Record this
				// primitive's own continuation so the remaining victims are
				// still asked once that ask's chain completes, instead of
				// being stranded: the enclosing Resolve loop would otherwise
				// resume only sa.Sub (nil for a VillainousChoice) and the
				// outer levels would degrade to no-sub-ability Notes.
				h.SuspendVillainousRest(sa, VillainousRest{
					Victims: append([]state.Target(nil), c.VillainousVictims...),
					Next:    c.VillainousIndex + 1})
				return
			}
		}
		c.VillainousIndex++
	}
	if c.VillainousVictims == nil {
		for _, target := range Defined(h, c, sa) {
			if target.IsPlayer {
				c.VillainousVictims = append(c.VillainousVictims, target)
			}
		}
		// The trigger's original Remembered can already end in its victim
		// (Attacks supplies the defender). That is not proof of a resumed
		// body: the explicit VillainousRest cursor handles nested asks.
	}
	for c.VillainousIndex < len(c.VillainousVictims) {
		victim := c.VillainousVictims[c.VillainousIndex]
		// The body is evaluated against this victim, not an earlier victim.
		c.Remembered = []state.Target{victim}
		d := &decision.Decision{Player: victim.Player, Kind: decision.KModes,
			Min: 1, Max: 1, Source: c.Source, ResumeKind: "villainous",
			ResumeSA: sa, ResumeModes: append([]string(nil), choices...),
			ResumeRemembered:        append([]state.Target(nil), c.Remembered...),
			ResumeVillainousVictims: append([]state.Target(nil), c.VillainousVictims...),
			ResumeVillainousIndex:   c.VillainousIndex,
			Prompt:                  "Choose a villainous option"}
		for i, name := range choices {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: "mode",
				Label: CharmModeLabel(cards.ResolveSVar(c.SVars, name), name),
				Obj:   c.Source, Player: victim.Player})
		}
		if Ask(h, d) == AskAsked {
			return
		}
		// R-9: an effects-only host has no chooser, so deterministically take
		// the first option and continue to the next victim.
		if sub := cards.ResolveSVar(c.SVars, choices[0]); sub != nil {
			Resolve(h, c, sub)
		}
		if h.Suspended() {
			return
		}
		c.VillainousIndex++
	}
	// Every victim has chosen: the cursor is spent. Reset it (the per-player
	// GenericChoice loop's discipline), or a later VillainousChoice on the
	// same Ctx -- the next iteration of an enclosing Repeat -- finds the
	// exhausted cursor and asks nobody.
	c.VillainousVictims = nil
	c.VillainousIndex = 0
}

// charmDistinctTargetRun runs a distinct modal Charm with one target group
// per selected target-bearing mode. ModeTargets is aligned to those modes;
// non-targeting modes still run with the ordinary shared context.
func charmDistinctTargetRun(h Host, c *Ctx, sa *cards.SA, names []string) bool {
	if len(c.ModeTargets) < 2 {
		return false
	}
	offset := 0
	for _, name := range c.ModesSeen {
		if sub := cards.ResolveSVar(c.SVars, name); sub != nil && strings.TrimSpace(sub.ParamStr(cards.PKValidTgts)) != "" {
			offset++
		}
	}
	for i, name := range names {
		sub := cards.ResolveSVar(c.SVars, name)
		if sub == nil {
			continue
		}
		savedTargets, savedOffered, savedMarker := c.Targets, c.OfferedSA, c.TargetsOffered
		savedScope, savedScopeSA := c.CharmModeScope, c.CharmModeSA
		if strings.TrimSpace(sub.ParamStr(cards.PKValidTgts)) != "" {
			if offset >= len(c.ModeTargets) {
				return false
			}
			c.Targets = c.ModeTargets[offset]
			c.OfferedSA = sub
			c.TargetsOffered = true
			// Publish the narrowed group so a mid-resolution ask posed
			// anywhere under this mode re-enters scoped to it instead of to
			// the stack object's whole flat list (Ctx.CharmModeScope).
			c.CharmModeScope = c.Targets
			c.CharmModeSA = sub
			offset++
		}
		Resolve(h, c, sub)
		c.Targets, c.OfferedSA, c.TargetsOffered = savedTargets, savedOffered, savedMarker
		c.CharmModeScope, c.CharmModeSA = savedScope, savedScopeSA
		if h.Suspended() {
			h.SuspendCharmRest(sa, names[i+1:])
			return true
		}
	}
	return true
}

// charmGenericPlayers is the per-Defined$-player api:GenericChoice path: each
// player the SA's Defined$ names chooses one of the same Choices$ in turn.
// With TempRemember$ Chooser the chooser is bound as Ctx.Remembered for ITS
// chosen body (the binding Seize the Spotlight's Fame/Fortune bodies read as
// Defined$ Remembered) and unbound once that body finishes; without the param
// Remembered is left untouched, as Forge leaves it. A choice whose
// UnlessCost$ the chooser cannot presently pay is not offered, and when no
// choice is payable FallbackAbility$ is resolved for that chooser instead of
// asking (Forge ChooseGenericEffect). The outer SubAbility$ runs once, after
// every chooser has answered.
//
// It returns true when it owns the resolution. It deliberately declines the
// shapes the existing controller ask already serves correctly:
//
//   - a Charm (never defined$-per-player);
//   - a GenericChoice with no Choices$;
//   - a GenericChoice whose Defined$ is absent, resolves to exactly the
//     resolving controller (Defined$ You), or resolves to anything that is
//     not all players (Defined$ Targeted on a spell's creature, Defined$
//     Valid <filter>): those keep the existing ask, so this ticket cannot
//     change an object-defined carrier.
//
// A GenericChoice whose Defined$ is one of the PLAYER-role selectors
// (Opponent / Player / Player.Opponent / Player.Other / You) but resolves to NO
// players (an unbound TriggeredPlayer, an empty Remembered) does NOT fall back
// to the controller: the ask would invent a chooser the card never named, so
// the resolution is a loud no-op instead. Any other selector resolving empty is
// left to the existing path.
func charmGenericPlayers(h Host, c *Ctx, sa *cards.SA) bool {
	if sa.API != "GenericChoice" {
		return false
	}
	choices := strings.Split(sa.ParamStr(cards.PKChoices), ",")
	if len(choices) == 0 {
		return false
	}
	for i := range choices {
		choices[i] = strings.TrimSpace(choices[i])
	}
	// A re-entry for an answered/continued chooser carries the cursor; the SA
	// may be reached mid-resolution with c.Modes already naming the answer.
	if c.GenericChoosers != nil {
		return charmGenericPlayersRun(h, c, sa, choices)
	}
	defined := strings.TrimSpace(sa.ParamStr(cards.PKDefined))
	if defined == "" {
		return false
	}
	resolved := Defined(h, c, sa)
	var players []state.Target
	for _, t := range resolved {
		if !t.IsPlayer {
			// A mixed or object-defined set (Defined$ Targeted, Defined$
			// Valid <filter>): not the per-player shape, so keep the existing
			// controller ask untouched.
			return false
		}
		players = append(players, t)
	}
	if len(players) == 1 && players[0].Player == c.Controller {
		// Defined$ You: the existing controller ask is already exact. Leave it
		// (and its ResumeKind "modes") alone.
		return false
	}
	if len(players) == 0 {
		if !playerRoleDefined(defined) {
			// A non-player selector that happened to resolve to nothing: leave
			// it to the existing path rather than changing its behaviour.
			return false
		}
		// A player-role selector that named no player: asking the controller
		// would choose for a player the card never named. Record why and do
		// nothing.
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "GenericChoice Defined$ " + defined + " resolved no players"})
		return true
	}
	c.GenericChoosers = players
	c.GenericChooserIndex = 0
	return charmGenericPlayersRun(h, c, sa, choices)
}

// playerRoleDefined reports whether a GenericChoice Defined$ selector names a
// PLAYER ROLE (so an empty resolution is a real "no choosers", not an
// object-definition that must keep the existing path). The qualified
// Player.Opponent / Player.Other spellings are included, as are the trigger
// roles that always resolve to a player. Every ambiguous selector (Targeted,
// TriggeredTarget, ParentTarget, Valid <filter>, Remembered, ...) is NOT, so an
// empty resolution there keeps the existing path unchanged.
func playerRoleDefined(defined string) bool {
	switch defined {
	case "Opponent", "Player", "Player.Opponent", "Player.Other", "You",
		"TriggeredPlayer", "TriggeredDefendingPlayer":
		return true
	}
	return false
}

// charmGenericPlayersRun drives the chooser loop. Ctx.GenericChooserIndex is
// the chooser still to ask; a non-nil Ctx.Modes is the answer for the chooser
// at index-1, whose chosen body has not yet run (resumeResolution advanced the
// index PAST the answered chooser, so running it here and then continuing the
// loop asks the next chooser exactly once). A nested ask inside a chosen body
// reports SuspendGenericChoiceRest so the remaining choosers survive it; a
// nested GenericChoice in that body sees a nil cursor for the body's walk
// (cleared around Resolve, the fx41 discipline) and resolves its own Defined$
// rather than inheriting this one.
func charmGenericPlayersRun(h Host, c *Ctx, sa *cards.SA, choices []string) bool {
	// TempRemember$ (Forge ChooseGenericEffect): when present, the chooser is
	// the Remembered object for ITS chosen body (Defined$/UnlessPayer$
	// Remembered name that player), and the binding is dropped after the
	// chooser's body finishes so it does not leak to the next chooser or the
	// outer SubAbility$. Without the param Forge leaves Remembered untouched,
	// so a body that did not ask for the binding must not get one.
	tempRemember := strings.TrimSpace(sa.Params["TempRemember"]) != ""
	baselineRemembered := append([]state.Target(nil), c.Remembered...)
	// FallbackAbility$ (Forge ChooseGenericEffect): the ability resolved for a
	// chooser when NONE of the Choices$ is payable RIGHT NOW -- Forge drops a
	// choice whose UnlessCost$ the chooser cannot pay, and when that empties
	// the list it runs the fallback instead of asking.
	fallback := strings.TrimSpace(sa.Params["FallbackAbility"])
	// runBody runs one chosen SVar with the chooser cursor cleared (a nested
	// GenericChoice resolves its own Defined$, never inheriting this cursor).
	// It reports whether the body suspended on a nested ask.
	runBody := func(name string) bool {
		sub := cards.ResolveSVar(c.SVars, name)
		if sub == nil {
			return false
		}
		savedChoosers, savedIndex := c.GenericChoosers, c.GenericChooserIndex
		c.GenericChoosers, c.GenericChooserIndex = nil, 0
		Resolve(h, c, sub)
		c.GenericChoosers, c.GenericChooserIndex = savedChoosers, savedIndex
		return h.Suspended()
	}
	if c.Modes != nil {
		names := c.Modes
		c.Modes = nil
		chooser := c.GenericChoosers[c.GenericChooserIndex-1]
		for _, name := range names {
			if tempRemember {
				c.Remembered = []state.Target{chooser}
			}
			suspended := runBody(name)
			c.Remembered = append([]state.Target(nil), baselineRemembered...)
			if !suspended {
				continue
			}
			// Preserve the enclosing remembered set as well as the chooser
			// cursor while the chosen body's nested ask is suspended.
			h.SuspendGenericChoiceRest(sa, GenericChoiceRest{
				Choosers:   append([]state.Target(nil), c.GenericChoosers...),
				Next:       c.GenericChooserIndex,
				Remembered: append([]state.Target(nil), baselineRemembered...)})
			return true
		}
	}
	for c.GenericChooserIndex < len(c.GenericChoosers) {
		chooser := c.GenericChoosers[c.GenericChooserIndex]
		// The chosen body reads Defined$ Remembered as THIS chooser, and only
		// when TempRemember$ asked for that binding.
		if tempRemember {
			c.Remembered = []state.Target{chooser}
		}
		available := genericChoiceAvailable(h, c, chooser.Player, choices)
		c.Remembered = append([]state.Target(nil), baselineRemembered...)
		if len(available) == 0 {
			// No choice is payable. Forge runs FallbackAbility$ instead of
			// asking; with the chooser still bound (TempRemember$) its body
			// reads Defined$ Remembered as that player.
			if fallback != "" {
				if tempRemember {
					c.Remembered = []state.Target{chooser}
				}
				suspended := runBody(fallback)
				c.Remembered = append([]state.Target(nil), baselineRemembered...)
				if suspended {
					h.SuspendGenericChoiceRest(sa, GenericChoiceRest{
						Choosers:   append([]state.Target(nil), c.GenericChoosers...),
						Next:       c.GenericChooserIndex + 1,
						Remembered: append([]state.Target(nil), baselineRemembered...)})
					return true
				}
			} else {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "GenericChoice no payable choice and no FallbackAbility"})
			}
			c.GenericChooserIndex++
			continue
		}
		var pick string
		if GenericChoiceAtRandom(sa) {
			// param:api:GenericChoice.AtRandom: the engine picks for this
			// chooser from its seeded rng; the chooser is never asked.
			if pick = genericChoiceRandomPick(h, c, sa, chooser.Player, available); pick == "" {
				c.GenericChooserIndex++
				continue
			}
		} else {
			d := &decision.Decision{Player: chooser.Player, Kind: decision.KModes,
				Min: 1, Max: 1, Source: c.Source, ResumeKind: "generic_players", ResumeSA: sa,
				ResumeModes:               append([]string(nil), available...),
				ResumeRemembered:          append([]state.Target(nil), c.Remembered...),
				ResumeGenericChoosers:     append([]state.Target(nil), c.GenericChoosers...),
				ResumeGenericChooserIndex: c.GenericChooserIndex,
				Prompt:                    "Choose 1 to 1 mode(s)"}
			for i, name := range available {
				d.Options = append(d.Options, decision.Option{Index: i, Kind: "mode",
					Label: CharmModeLabel(cards.ResolveSVar(c.SVars, name), name),
					Obj:   c.Source, Player: chooser.Player})
			}
			if Ask(h, d) == AskAsked {
				return true
			}
			// R-9: an effects-only host has no chooser, so deterministically
			// take the first option for this chooser and continue to the next.
			pick = available[0]
		}
		if tempRemember {
			c.Remembered = []state.Target{chooser}
		}
		suspended := runBody(pick)
		c.Remembered = append([]state.Target(nil), baselineRemembered...)
		if suspended {
			// Preserve both cursor and outer remembered set across the nested ask.
			h.SuspendGenericChoiceRest(sa, GenericChoiceRest{
				Choosers:   append([]state.Target(nil), c.GenericChoosers...),
				Next:       c.GenericChooserIndex + 1,
				Remembered: append([]state.Target(nil), baselineRemembered...)})
			return true
		}
		c.GenericChooserIndex++
	}
	if tempRemember {
		// Forge restores the complete remembered set that preceded the
		// temporary chooser binding, including any enclosing player remembers.
		c.Remembered = append([]state.Target(nil), baselineRemembered...)
	}
	c.GenericChoosers = nil
	c.GenericChooserIndex = 0
	return true
}

// genericChoiceAvailable filters a GenericChoice's Choices$ to the names the
// chooser can actually select, per Forge ChooseGenericEffect: a choice whose
// UnlessCost$ the chooser cannot presently pay is not offered (the same
// reachability gate the unless ask itself uses, so the option list and the pay
// decision cannot disagree). A choice with no UnlessCost$ is always available;
// a host that cannot price costs (an effects-only embedder) keeps the historic
// full list, so R-9 still degrades deterministically rather than silently
// dropping choices. FORGE ALSO drops a choice failing getRestrictions(); the
// engine's cards.SA carries no such field, and no corpus GenericChoice choice
// uses Restrictions$ today.
func genericChoiceAvailable(h Host, c *Ctx, payer state.PlayerID, choices []string) []string {
	checker, priced := h.(interface {
		UnlessCostPayableFromCtx(state.PlayerID, string, *Ctx) bool
	})
	out := make([]string, 0, len(choices))
	for _, name := range choices {
		sub := cards.ResolveSVar(c.SVars, name)
		if sub == nil {
			continue
		}
		if !priced {
			out = append(out, name)
			continue
		}
		raw := strings.TrimSpace(sub.ParamStr(cards.PKUnlessCost))
		if raw == "" || checker.UnlessCostPayableFromCtx(payer, raw, c) {
			out = append(out, name)
		}
	}
	return out
}

// effCharm runs the selected Choices$ sub-abilities in chosen order.
// Cast spells (CR 601.2b) and triggered abilities (CR 603.3c) arrive with
// Ctx.Modes pre-seeded from their earlier announcement. A Charm reached only
// during resolution still poses KModes and suspends until resumeResolution
// re-enters it with Ctx.Modes. A host that cannot ask retains the deterministic
// first-mode stand-in and records why with a Note.
func effCharm(h Host, c *Ctx, sa *cards.SA) {
	if c.SVars == nil {
		return
	}
	if charmGenericPlayers(h, c, sa) {
		return
	}
	choices := strings.Split(sa.ParamStr(cards.PKChoices), ",")
	if len(choices) == 0 {
		return
	}
	for i := range choices {
		choices[i] = strings.TrimSpace(choices[i])
	}
	// Re-entry after the modal choice was answered: Ctx.Modes already names
	// the chosen SVars in execution order, so run exactly those and do not
	// ask again.
	if c.Modes != nil {
		// fx41: take the names into a local and clear c.Modes BEFORE running
		// them. The same Ctx is handed to Resolve for every mode AND to the
		// Charm's own SubAbility$, and nothing else in the walk reads Modes,
		// so an uncleared field would leak the OUTER Charm's answered modes
		// into a NESTED Charm reached anywhere below it -- that inner
		// effCharm sees Modes != nil, takes this re-entry branch, and "runs"
		// the outer's mode names against its own SVars instead of posing its
		// own ask (or, when a name resolves back to a chain containing it,
		// re-resolves itself endlessly). Clearing here confines the answer
		// to the Charm that asked for it.
		names := c.Modes
		c.Modes = nil
		if charmDistinctTargetRun(h, c, sa, names) {
			return
		}
		if charmCrossModeRun(h, c, sa, names) {
			return
		}
		// Task mvts1, two guards the generic ValidTgts$ pre-ask needs here.
		//
		// Coverage: a placement-announced modal resolution asked every CHOSEN
		// target-bearing mode's targeting in its placement ask (the combined
		// per-mode ask), but a resume ctx carries only the FIRST of them as
		// Ctx.OfferedSA (rules' offeredTargetSA returns the first
		// target-bearing chosen mode). modalOffered detects that derivation
		// -- OfferedSA set and NOT the Charm root itself -- and marks each
		// target-bearing mode as covered while it dispatches, so the pre-ask
		// cannot re-pose the placement question per mode. A mid-resolution
		// Charm (its own KModes answered) has no modal derivation -- its
		// OfferedSA is nil or the root's own covered SA -- and its
		// target-bearing modes keep their real asks.
		modalOffered := c.OfferedSA != nil && c.OfferedSA.Line != sa.Line
		// CanRepeatModes$ (CR 601.2b): the covering ask -- the cast
		// announcement's or the placement ask's ONE target list -- covers the
		// FIRST occurrence of each target-bearing mode only. A later occurrence
		// of the same mode must keep its own targeting: OfferedSA is dropped
		// for the dispatch (chosenTargetsFor's Line match would otherwise skip
		// it) and the root TargetsOffered marker is shed for it (both pre-ask
		// gates read it at depth 0), so the mode's own mid-resolution ask --
		// chosenTargetsFor's for every API, changeZoneChosenTargets's for an
		// API$ ChangeZone body, which also needs the shared list out of sight
		// (its len(c.Targets) > 0 placement guard) -- poses for THIS instance.
		// "Return target creature to its owner's hand" chosen three times then
		// asks three targets and returns three creatures, instead of silently
		// re-running the mode against the one shared target. The seen-set is
		// seeded from Ctx.ModesSeen (rules' charm_rest arm): after a suspension
		// the re-entry walks only the REST of the multiset, so "first occurrence
		// in this walk" alone cannot see the instances the earlier passes
		// already ran.
		seen := make(map[string]bool, len(names))
		for _, n := range c.ModesSeen {
			seen[n] = true
		}
		for i, name := range names {
			if sub := cards.ResolveSVar(c.SVars, name); sub != nil {
				savedOffered, savedTargets, savedMark := c.OfferedSA, c.Targets, c.TargetsOffered
				first := !seen[name]
				seen[name] = true
				if modalOffered && strings.TrimSpace(sub.ParamStr(cards.PKValidTgts)) != "" {
					if first {
						c.OfferedSA = sub
					} else {
						c.OfferedSA = nil
						c.TargetsOffered = false
						if sub.CompiledAPI() == cards.APIChangeZone || sub.API == "ChangeZone" {
							c.Targets = nil
						}
					}
				}
				Resolve(h, c, sub)
				c.OfferedSA, c.Targets, c.TargetsOffered = savedOffered, savedTargets, savedMark
			}
			if h.Suspended() {
				// A mode's own chain posed a mid-resolution ask: never run the
				// remaining modes while a decision is pending (Engine.ask
				// panics on the overwrite). Report the rest as a charm-rest
				// continuation, the same report the cross-mode runner makes,
				// so they run once the answer lands. An empty rest (this was
				// the LAST mode) is reported too, so the Charm re-enters to
				// walk its own Sub instead of the enclosing loop recording a
				// plain continuation that degrades to a false no-sub-ability
				// Note.
				h.SuspendCharmRest(sa, names[i+1:])
				return
			}
		}
		return
	}
	// Subs is resolved once per choice, so the label (SpellDescription$ on
	// the choice's own SVar body) and the mode-run share one parse; the
	// ordering of options mirrors Choices$ order, which is also how the
	// engine maps a chosen index back to an SVar name.
	subs := make([]*cards.SA, len(choices))
	for i, name := range choices {
		subs[i] = cards.ResolveSVar(c.SVars, name)
	}
	// ChoiceRestriction$ ("choose one that hasn't been chosen this turn / this
	// game"): drop the modes the source already chose under the same scope
	// before posing the ask. The filtered list is what the bounds clamp and the
	// options are built from, so the answer's indices map straight back to
	// eligible SVar names (d.ResumeModes). When every mode is exhausted the
	// ordinary min-over-modes decline below makes the Charm do nothing, which
	// is exactly the oracle's "if you can't choose, nothing happens".
	if eligible := CharmEligibleModes(h, c.Source, sa, choices); len(eligible) != len(choices) {
		filteredSubs := make([]*cards.SA, len(eligible))
		for i, name := range eligible {
			filteredSubs[i] = cards.ResolveSVar(c.SVars, name)
		}
		choices, subs = eligible, filteredSubs
	}
	min, max, repeat := CharmModeBounds(h, c, sa, len(choices))
	if min > len(choices) && !repeat {
		// Forge declines a Charm whose required minimum exceeds its available
		// modes. A repeatable Charm can always fill its slots by repeating a
		// single mode, so it never declines on this ground. A no-engine host
		// must likewise make no arbitrary choice.
		return
	}
	// param:api:Charm.Random (Random$ True / Random$ Compare with
	// RandomCompareSVar$/RandomCompare$): a Charm whose mode is picked AT
	// RANDOM rather than asked. The direction is the card oracle's, not the
	// brief's gloss: Typhoid Mary, Fractured ("choose one at random. If you
	// discarded a card this turn, you choose one instead", RandomCompare$
	// LT1 over SVar Y = CardsDiscardedThisTurn) is random exactly while the
	// comparison HOLDS, and a failed comparison reverts to the ordinary
	// KModes ask below. An unresolvable comparison (a missing
	// RandomCompareSVar$, an unmodelled count head, an unparseable
	// comparator) fails to the ask too -- never to a fake random, the
	// permissive direction. Only the single-slot shape is picked: a Random$
	// Charm whose CharmNum$ fills several slots keeps the ordinary ask
	// (measured corpus-unreachable -- every Random$ carrier, 5 files, is
	// single-slot), because a multi-pick cannot share this suspension-free
	// path.
	if GenericChoiceAtRandom(sa) {
		genericChoiceRandomRun(h, c, sa, choices)
		return
	}
	if CharmRandomChosen(h, c, sa) && min == 1 && max == 1 && !repeat {
		idx := h.Rand(len(choices))
		label := charmModeLabel(choices, subs, idx)
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "chose a mode at random: " + label})
		if subs[idx] != nil {
			Resolve(h, c, subs[idx])
		}
		return
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KModes,
		Min: min, Max: max, Source: c.Source, Repeatable: repeat,
		ResumeKind: "modes", ResumeSA: sa,
		Prompt: "Choose " + strconv.Itoa(min) + " to " + strconv.Itoa(max) + " mode(s)"}
	for i, name := range choices {
		d.Options = append(d.Options, decision.Option{
			Index: i, Kind: "mode", Label: CharmModeLabel(subs[i], name),
			Obj: c.Source, Player: c.Controller})
	}
	if Ask(h, d) == AskAsked {
		return // resolution suspended; the answer re-enters this effect with Ctx.Modes set.
	}
	// Fuzz/no-engine host: the deterministic first-mode default (R-9), with
	// the Note that records why the richer path did not run. (AskEmpty is
	// unreachable by construction -- charmNum is clamped to >= 1 and
	// strings.Split never yields fewer than one choice -- but the shared
	// helper owns the guard either way.)
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
		Text: "chose its first mode (no engine host to ask)"})
	if subs[0] != nil {
		Resolve(h, c, subs[0])
	}
}

// CharmRandomChosen reports whether the Charm's mode is chosen AT RANDOM
// rather than asked (param:api:Charm.Random):
//
//   - `Random$ True` is always random (Outlaws' Merriment, Cult of Skaro,
//     Umaru the Raging Yeti, Summon the Magus Sisters);
//   - `Random$ Compare` is random exactly while the RandomCompareSVar$
//     comparison holds -- the card oracle's direction, NOT the brief's
//     gloss. Typhoid Mary, Fractured's own oracle quote ("choose one at
//     random. If you discarded a card this turn, you choose one instead")
//     with RandomCompare$ LT1 over Y = CardsDiscardedThisTurn reads: zero
//     discards (LT1 holds) -> random; a discard this turn (LT1 fails) ->
//     the player chooses.
//
// The comparison rides the shared CheckSVarHolds evaluator (the SVar table,
// then the source face's own, through EvalCountOK), so every head that
// evaluates for CheckSVar$/SVarCompare$ gates evaluates here too. A
// comparison that does not EVALUATE (missing RandomCompareSVar$, an
// unmodelled count head, an unparseable comparator) reports false -- the
// ordinary ask keeps the choice, never a fake random. Any other Random$
// value is unread: the ordinary ask applies.
//
// Both mode-ask SITES consult this beside effCharm itself: the trigger
// placement ask (rules' askTriggerModes) and the cast-time announcement
// (rules' castModeAsk) skip their ask for a random Charm, so the pick (or
// the failed comparison's ask) happens once, at resolution, in effCharm --
// the rng draw stays in the replay-exact resolution path instead of
// split-braining a placement-time pick with a resolution-time run.
func CharmRandomChosen(h Host, c *Ctx, sa *cards.SA) bool {
	switch strings.TrimSpace(sa.ParamStr(cards.PKRandom)) {
	case "True":
		return true
	case "Compare":
		holds, evaluated := CheckSVarHolds(h, c, sa.Params["RandomCompareSVar"], sa.Params["RandomCompare"])
		return evaluated && holds
	}
	return false
}

// CharmModeLabel is the printed display label of one Charm mode whose
// resolved body is sub: the mode body's own SpellDescription$ when it
// carries one, else the first SpellDescription$ found walking the body's
// SubAbility$ chain, else fallback -- the raw SVar name. In the corpus the
// chain-only shape is exactly the printed bullet text of the card's Oracle
// line: What Must Be Done's Release Juno mode carries its description one
// hop down (on DBChangeZone), and Varchild's War-Riders' two upkeep modes
// carry theirs on SurvivorDistribution and Sacrifice, so a mode whose
// SpellDescription$ rides a sub is still labelled by the card's printed
// words, not its SVar name. Body-first precedence keeps every mode that
// already labelled by its own SpellDescription$ byte-identical. The chain
// is linked by cards' resolver (ResolveSVar/link, depth-capped at
// maxSVarDepth), so the walk is a plain pointer walk: no re-resolution and
// no new cycle risk. A nil sub returns the fallback.
func CharmModeLabel(sub *cards.SA, fallback string) string {
	if sub == nil {
		return fallback
	}
	if d := strings.TrimSpace(sub.ParamStr(cards.PKSpellDescription)); d != "" {
		return d
	}
	for s := sub.Sub; s != nil; s = s.Sub {
		if d := strings.TrimSpace(s.ParamStr(cards.PKSpellDescription)); d != "" {
			return d
		}
	}
	return fallback
}

// charmModeLabel is the display label of choice slot idx: CharmModeLabel of
// the slot's resolved body, falling back to the SVar name -- the same label
// the KModes decision's options carry, so the random-pick Note names the
// mode exactly as an answered ask would.
func charmModeLabel(choices []string, subs []*cards.SA, idx int) string {
	if idx < 0 || idx >= len(choices) {
		return ""
	}
	return CharmModeLabel(subs[idx], choices[idx])
}

// effVote records one Note per voting player. Two shapes:
//
//   - the fixed-list shape ("Will of the Planeswalkers", Expropriate):
//     Choices$ names an SVar per ballot option, each player votes for the
//     first (the deterministic stand-in), Notes record it, and the WINNING
//     option's SVar runs. A tie runs VoteTiedAbility$ when the SA carries
//     one (the path cycle's DBChaos), else the first tied option's SVar.
//     Before this the fixed-list shape resolved nothing at all, so a Path
//     of the Ghosthunter vote recorded its Notes and then did nothing --
//     the "chosen outcome" the brief expected to hit Planeswalk/
//     ChaosEnsues never ran. The tie branch takes its tally from Ctx.Votes
//     when a caller has answered one (the seam a real per-player ask fills,
//     and what lets the tie be pinned against a real compiled SA); absent,
//     the deterministic stand-in applies.
//   - the card-ballot shape (Council's Judgment): VoteCard$ is a permanent
//     filter, so the ballot is the battlefield permanents matching it
//     (matched from the spell's controller: "a nonland permanent YOU don't
//     control"), each Defined$ player votes, and every permanent with the
//     most votes or tied for most lands in the resolution's Remembered set
//     for VoteSubAbility$ (DBExile's ChangeZone Defined$ Remembered).
//
// Fixed and card ballots use the real per-voter ask path below; a host that
// cannot answer retains the R-9 first-option fallback. Both VoteCard$ and
// VoteSubAbility$ are genuinely read on the ballot path.
func effVote(h Host, c *Ctx, sa *cards.SA) {
	if ballot := strings.TrimSpace(sa.Params["VoteCard"]); ballot != "" {
		effCardVote(h, c, sa, ballot)
		return
	}
	// The PLAYER ballot (task votepb1): VotePlayer$ with no Choices$ list
	// names the ballot entries as players (Mob Verdict's `VotePlayer$ Other`).
	// Choices$ keeps its precedence -- Forge's VoteEffect reads Choices first,
	// then VoteCard$, then VotePlayer$ -- so this fires only when the vote
	// carries no fixed option list.
	if vp := strings.TrimSpace(sa.Params["VotePlayer"]); vp != "" && len(voteChoiceNames(sa)) == 0 {
		effPlayerVote(h, c, sa)
		return
	}
	choices := voteChoiceNames(sa)
	voters := definedPlayers(h, c, sa)
	// A live fixed-list ballot uses the same private, per-voter KChoose path as
	// VotePlayer$. Keep Ctx.Votes as the small direct seam used by unit tests;
	// real answers travel only through the decision's ResumeChoices.
	if c.Votes == nil {
		picks, complete := askFixedVote(h, c, sa, choices, voters)
		if !complete {
			return
		}
		for i, t := range voters {
			label := ""
			if i < len(picks) && picks[i].Obj > 0 && int(picks[i].Obj-1) < len(choices) {
				label = choices[picks[i].Obj-1]
			}
			// Secret ballots are revealed here too: every vote is already in
			// (secret council: "then those votes are revealed").
			h.Emit(events.Event{Kind: events.Note, Player: t, Text: "votes for " + label})
		}
		counts := make([]int, len(choices))
		for _, p := range picks {
			if p.Obj > 0 && int(p.Obj-1) < len(choices) {
				counts[p.Obj-1]++
			}
		}
		if len(choices) > 0 && len(voters) > 0 {
			resolveVoteOutcomes(h, c, sa, choices, counts)
		}
		ballots := make([]VoteBallot, len(voters))
		for i, t := range voters {
			ballots[i] = VoteBallot{Player: t, Pick: int(picks[i].Obj) - 1}
		}
		emitVoteFinished(h, c, ballots, len(choices) > 0, strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKSecretly)), "True"))
		return
	}
	// Ctx.Votes is the answered per-voter choice list (a real per-player
	// ask's result, or a test seam): one option index per voter, in voter
	// order. It is consumed and cleared at the top of the walk so a nested
	// Vote cannot inherit it (fx42), the same scoping every other asking
	// primitive uses. Absent, the deterministic stand-in applies: every
	// voter takes the first option.
	answered := c.Votes
	c.Votes = nil
	counts := make([]int, len(choices))
	// picks records each voter's answered option index (-1: an out-of-range
	// answer, i.e. a vote for nothing) so the canonical vote-finished Note's
	// same/diff split below reads the votes that were actually cast -- the
	// same data the tally uses, never a second answer source.
	picks := make([]int, len(voters))
	for i := range picks {
		picks[i] = -1
	}
	for i, t := range voters {
		choice := 0
		if answered != nil && i < len(answered) {
			choice = answered[i]
		}
		label := ""
		if choice >= 0 && choice < len(choices) {
			label = choices[choice]
			counts[choice]++
			picks[i] = choice
		}
		// Secret ballots are revealed here too: every vote is already in
		// (secret council: "then those votes are revealed").
		h.Emit(events.Event{Kind: events.Note, Player: t, Text: "votes for " + label})
	}
	if len(choices) > 0 && len(voters) > 0 {
		resolveVoteOutcomes(h, c, sa, choices, counts)
	}
	// The canonical vote-finished carrier (trig:Vote, effects/vote.go):
	// emitted AFTER the winning outcome resolved -- the vote (outcome
	// included) finishes, then "whenever players finish voting" sees it. It
	// carries the RAW ballots, not a pre-split: the List$ referent sets are
	// relative to the TRIGGER SOURCE'S controller, which is only known on the
	// rules side (rules/trigger_referents' Vote case re-splits with
	// effects.VoteSplit against e.controllerOf(source)). It is emitted even
	// when there was no ballot and/or no voter, the same always-fire reading
	// the card-ballot shape takes; "whenever players finish voting" has no
	// intervening-if. ballotExisted is false for an empty Choices$ ballot,
	// which binds neither set.
	ballots := make([]VoteBallot, len(voters))
	for i, t := range voters {
		ballots[i] = VoteBallot{Player: t, Pick: picks[i]}
	}
	emitVoteFinished(h, c, ballots, len(choices) > 0, strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKSecretly)), "True"))
}

// resolveVoteOutcomes executes the winning option normally. StoreVoteNum$ is
// the multi-outcome form: publish each option's tally as VoteNum in a private
// copy of the source SVar table, then resolve every option body so its numeric
// effects consume that option's count (including zero).
func resolveVoteOutcomes(h Host, c *Ctx, sa *cards.SA, choices []string, counts []int) {
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKStoreVoteNum)), "True") {
		for i, name := range choices {
			count := 0
			if i < len(counts) {
				count = counts[i]
			}
			svars := make(map[string]string, len(c.SVars)+1)
			for key, body := range c.SVars {
				svars[key] = body
			}
			// Forge reuses the SVar name VoteNum for each outcome body, binding
			// that option's tally while resolving it.
			svars["VoteNum"] = "Number$" + strconv.Itoa(count)
			cc := *c
			cc.SVars = svars
			if sub := cards.ResolveSVar(cc.SVars, name); sub != nil {
				Resolve(h, &cc, sub)
			}
		}
		return
	}
	best, tied := voteWinner(counts)
	name := choices[best]
	if tied {
		if alt := strings.TrimSpace(sa.Params["VoteTiedAbility"]); alt != "" {
			name = alt
		}
	}
	if sub := cards.ResolveSVar(c.SVars, name); sub != nil {
		Resolve(h, c, sub)
	}
}

// askFixedVote poses one private KChoose per voter. The answer is encoded as
// ObjID(index+1), avoiding a second answer channel while keeping ResumeChoices
// decision-scoped. A host that cannot answer takes option zero (R-9).
func askFixedVote(h Host, c *Ctx, sa *cards.SA, choices []string, voters []state.PlayerID) ([]state.Target, bool) {
	picks := append([]state.Target(nil), c.VotePicks...)
	i := c.VoteTarget
	if c.VoteDone {
		if len(c.VoteAnswer) > 0 {
			picks = append(picks, c.VoteAnswer[0])
		} else {
			picks = append(picks, state.Target{})
		}
		c.VoteDone, c.VoteAnswer = false, nil
		i++
	}
	for ; i < len(voters); i++ {
		voter := voters[i]
		min := 1
		if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKUpTo)), "True") {
			min = 0
		}
		prompt := strings.TrimSpace(sa.Params["VoteMessage"])
		if prompt == "" {
			prompt = "Vote for an option"
		}
		d := &decision.Decision{Player: voter, Kind: decision.KChoose, Source: c.Source,
			Min: min, Max: 1, ResumeKind: "vote", ResumeSA: sa, ResumeTarget: i,
			ResumeChoices: append([]state.Target(nil), picks...), Prompt: prompt}
		for j, name := range choices {
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "vote", Label: name, Obj: state.ObjID(j + 1)})
		}
		if len(d.Options) == 0 {
			picks = append(picks, state.Target{})
			continue
		}
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand (the "vote" resume
			// arm's VoteAnswer): option j is encoded as ObjID(j+1).
			pick := state.Target{}
			if len(ans) > 0 {
				pick = state.Target{Obj: ans[0].Obj}
			}
			picks = append(picks, pick)
			continue
		}
		if Ask(h, d) == AskAsked {
			return nil, false
		}
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "vote resolved as the first ballot entry (no engine host to ask)"})
		picks = append(picks, state.Target{Obj: 1})
	}
	c.VotePicks, c.VoteTarget, c.VoteDone, c.VoteAnswer = nil, 0, false, nil
	return picks, true
}

// voteWinner returns the index of the highest count and whether that count is
// shared by more than one option. It is a separate function (rather than
// inline in effVote) so the tie branch is testable on its own: the current
// deterministic stand-in gives every vote to option 0, so a real tie cannot
// arise from a live resolution yet, and an untested branch would be dead code
// waiting to rot. The first highest index wins the tie, matching the
// oracle's "if X gets more votes" over "or the vote is tied" ordering.
func voteWinner(counts []int) (int, bool) {
	if len(counts) == 0 {
		return 0, false
	}
	best := 0
	for i, n := range counts {
		if n > counts[best] {
			best = i
		}
	}
	tied := 0
	for _, n := range counts {
		if n == counts[best] {
			tied++
		}
	}
	return best, tied > 1
}

// voteChoiceNames splits a Vote's Choices$ into its SVar names, trimmed and
// with empty entries dropped. Shared by both vote shapes so the option list
// the tally indexes is parsed one way.
func voteChoiceNames(sa *cards.SA) []string {
	raw := sa.ParamStr(cards.PKChoices)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// effCardVote is effVote's card-ballot half: the battlefield permanents
// VoteCard$ admits are the options, each voting player answers a private ask,
// and the most-voted -- every
// member of the tie -- is remembered for VoteSubAbility$, which runs once
// at the end (Council's Judgment's "exile each permanent with the most
// votes or tied for most votes").
func askCardVote(h Host, c *Ctx, sa *cards.SA, options []state.ObjID, voters []state.PlayerID) ([]state.ObjID, bool) {
	picks := append([]state.Target(nil), c.VotePicks...)
	i := c.VoteTarget
	if c.VoteDone {
		if len(c.VoteAnswer) > 0 {
			picks = append(picks, c.VoteAnswer[0])
		} else {
			picks = append(picks, state.Target{})
		}
		c.VoteDone, c.VoteAnswer = false, nil
		i++
	}
	for ; i < len(voters); i++ {
		voter := voters[i]
		min := 1
		if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKUpTo)), "True") {
			min = 0
		}
		prompt := strings.TrimSpace(sa.Params["VoteMessage"])
		if prompt == "" {
			prompt = "Vote for a permanent"
		}
		d := &decision.Decision{Player: voter, Kind: decision.KChoose, Source: c.Source, Min: min, Max: 1,
			ResumeKind: "vote", ResumeSA: sa, ResumeTarget: i, ResumeChoices: append([]state.Target(nil), picks...), Prompt: prompt}
		for j, id := range options {
			label := "permanent"
			var controller state.PlayerID
			if o := h.Game().Obj(id); o != nil && o.Face() != nil {
				label = o.Face().Name
				// The subject's controller is public information (CR 400.2) and
				// the one fact the voter's policy needs to prefer a foreign
				// permanent over its own: Council's Judgment's ballot excludes
				// only the CASTER's permanents, so a 3+ seat ballot offers a
				// voter both its own and an opponent's permanents. Option.Player
				// already carries exactly this subject-controller convention for
				// player targets, so no new wire field is needed.
				controller = o.Controller
			}
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "vote_card", Label: label, Obj: id, Player: controller})
		}
		if len(d.Options) == 0 {
			picks = append(picks, state.Target{})
			continue
		}
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand (the "vote" resume
			// arm's VoteAnswer).
			pick := state.Target{}
			if len(ans) > 0 && ans[0].Obj != 0 {
				pick = state.Target{Obj: ans[0].Obj}
			}
			picks = append(picks, pick)
			continue
		}
		if Ask(h, d) == AskAsked {
			return nil, false
		}
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "card vote resolved as the first ballot entry (no engine host to ask)"})
		picks = append(picks, state.Target{Obj: options[0]})
	}
	c.VotePicks, c.VoteTarget, c.VoteDone, c.VoteAnswer = nil, 0, false, nil
	out := make([]state.ObjID, len(picks))
	for j, p := range picks {
		out[j] = p.Obj
	}
	return out, true
}

func effCardVote(h Host, c *Ctx, sa *cards.SA, ballot string) {
	g := h.Game()
	var options []state.ObjID
	for i := range g.Players {
		for _, id := range g.Zone(state.ZBattlefield, state.PlayerID(i)) {
			if o := g.Obj(id); o != nil && c.MatchSpec(g, ballot, id, c.Controller) {
				options = append(options, id)
			}
		}
	}
	counts := map[state.ObjID]int{}
	max := 0
	voters := definedPlayers(h, c, sa)
	var picks []int
	if c.Votes != nil {
		// Direct seam retained for effects tests and replay-independent callers.
		picks = append([]int(nil), c.Votes...)
		c.Votes = nil
	} else {
		answered, complete := askCardVote(h, c, sa, options, voters)
		if !complete {
			return
		}
		picks = make([]int, len(answered))
		for i, id := range answered {
			picks[i] = -1
			if id != 0 {
				for j, option := range options {
					if option == id {
						picks[i] = j
						break
					}
				}
			}
		}
	}
	for i, t := range voters {
		label := "nothing"
		if i < len(picks) && picks[i] >= 0 && picks[i] < len(options) {
			id := options[picks[i]]
			if o := g.Obj(id); o != nil && o.Face() != nil {
				label = o.Face().Name
			}
			counts[id]++
			if counts[id] > max {
				max = counts[id]
			}
		}
		// Secret ballots are revealed here too: every vote is already in
		// (secret council: "then those votes are revealed").
		h.Emit(events.Event{Kind: events.Note, Player: t, Text: "votes for " + label})
	}
	// The card ballot's per-subject tally, for the chained AmountFromVotes$
	// reader (task votepb1): one entry per ballot permanent, published behind
	// StoreVoteNum$ True -- the parameter Forge requires before it stores its
	// VoteNum<card> SVars. Forge's StoreVoteNum branch (a card ballot has no
	// Choices$) is authoritative on what the resolution REMEMBERS as well:
	// when the vote stores its per-subject tallies, the most-votes remember
	// path does not run at all, and the only remember is
	// RememberVotedObjects$'s `host.addRemembered(votes.keySet())` -- exactly
	// the objects that RECEIVED a vote, each once. Without StoreVoteNum$ the
	// most-votes append stands (Council's Judgment's "exile each permanent
	// with the most votes or tied for most votes", feeding VoteSubAbility$);
	// there a bare RememberVotedObjects$ beside it dedupes against the
	// most-votes set instead of duplicating it (no corpus carrier combines
	// the two without StoreVoteNum$, so the dedupe is the structural guard,
	// not a behaviour change any carrier can see).
	storeVoteNum := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKStoreVoteNum)), "True")
	rememberVoted := strings.EqualFold(strings.TrimSpace(sa.Params["RememberVotedObjects"]), "True")
	if storeVoteNum {
		publishVoteCounts(c, voteCountsForObjects(options, counts))
	} else if max > 0 {
		for _, id := range options {
			if counts[id] == max {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
			}
		}
	}
	if rememberVoted {
		// Exactly the objects that received a vote, each once: on the
		// non-StoreVoteNum path the most-votes append above may already hold a
		// voted object, so the voted set never duplicates it.
		mostVoted := map[state.ObjID]bool{}
		if !storeVoteNum && max > 0 {
			for _, id := range options {
				if counts[id] == max {
					mostVoted[id] = true
				}
			}
		}
		for _, id := range options {
			if counts[id] > 0 && !mostVoted[id] {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
			}
		}
	}
	// VoteSubAbility$ resolves AFTER the tally publish and the remember, so a
	// chained body sees Votes bound and the remembered set complete (fx42's
	// consumers read before their own re-entries).
	if sub := strings.TrimSpace(sa.Params["VoteSubAbility"]); sub != "" {
		if resolved := cards.ResolveSVar(c.SVars, sub); resolved != nil {
			Resolve(h, c, resolved)
		}
	}
	// The canonical vote-finished carrier (trig:Vote, effects/vote.go),
	// emitted after VoteSubAbility$ ran -- the same after-the-vote point the
	// fixed-list shape emits at. Like the fixed-list shape it carries the RAW
	// ballots and the rules side re-splits against the carrier controller.
	// The deterministic stand-in gives every voter the ballot's FIRST option,
	// so a controller who voted sees every other voter in the same set. A
	// vote with no ballot option at all (an empty battlefield) had nobody
	// vote for anything, so ballotExisted=false binds neither set -- the
	// trigger still fires and its same/diff bodies act on nobody, the same
	// always-fire reading the fixed-list shape takes.
	ballots := make([]VoteBallot, len(voters))
	for i, t := range voters {
		ballots[i] = VoteBallot{Player: t, Pick: picks[i]}
	}
	emitVoteFinished(h, c, ballots, len(options) > 0, strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKSecretly)), "True"))
}

// effBecomeMonarch records the game-level designation as an event so a
// conditional trigger observes it identically in the live game and on replay.
//
// The event is a TRANSITION (CR 720.2: a player "becomes" the monarch only
// when the designation moves to them), so a resolution that names the
// reigning monarch as its target is a no-op: the designation does not move
// and no "whenever a player becomes the monarch" trigger may fire. This is
// load-bearing for events.MonarchChange's one reader, rules'
// trigmatch.BecomeMonarchMatches -- it sees only the post-fold designation, so an
// unconditional emit here would queue trig:BecomeMonarch for a repeat
// BecomeMonarch (Custodi Lich resolving twice, two Peacekeeper Colossi, etc.).
// Suppressing at the source rather than inventing a previous-monarch field
// keeps events.Event's encoding untouched and replay-exact.
