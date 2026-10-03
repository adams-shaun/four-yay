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
	return sa != nil && CharmOf(sa).CanRepeatModes
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
	p := CharmOf(sa)
	repeat = p.CanRepeatModes
	max = int(numText(h, c, p.CharmNum, 1))
	if max < 1 {
		max = 1
	}
	min = max
	if p.MinCharmNum.Present {
		min = int(numText(h, c, p.MinCharmNum, int32(min)))
	}
	if p.OptionalTrue {
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
	return CharmOf(sa).ChoiceRestriction
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
		s := ModeTargetSpec(sub)
		if s == "" {
			continue
		}
		tbms++
		if modeTargetUnique(sub) {
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
	choices := CharmOf(sa).Modes
	if status, _ := CharmCrossModeShape(c.SVars, choices); status != CharmUniqueSupported {
		return false
	}
	var tbmIdx []int
	for i, name := range names {
		if sub := cards.ResolveSVar(c.SVars, name); sub != nil && ModeTargetSpec(sub) != "" {
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
		asks := askCount(h)
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
		charmRestNote(h, c, sa, asks)
	}
	return true
}

// effVillainousChoice makes the player named by Defined$ choose one of the
// supplied ability bodies. Unlike a modal trigger's placement choice, the
// victim's choice happens during resolution: the victim is remembered before
// the chosen body runs, so Defined$ Remembered and Player.IsRemembered in the
// body refer to the victim.
func effVillainousChoice(h Host, c *Ctx, sa *cards.SA) {
	choices := CharmOf(sa).Modes
	if len(choices) == 0 || c.SVars == nil {
		return
	}
	// A resumed answer is scoped to the current victim. Once its body has
	// completed, advance to the next Defined$ player and pose a fresh ask.
	if c.Modes != nil {
		names := c.Modes
		c.Modes = nil
		if villainousRunChoice(h, c, sa, names) {
			return
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
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: run the victim's chosen
			// body (the "villainous" resume arm's choice, ResumeModes order)
			// and go on to the next victim.
			var names []string
			if len(ans) > 0 && ans[0].Index >= 0 && ans[0].Index < len(choices) {
				names = []string{choices[ans[0].Index]}
			}
			if villainousRunChoice(h, c, sa, names) {
				return
			}
			c.VillainousIndex++
			continue
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

// charmRestNote mirrors, on the resolution kernel's path, the one event the
// legacy charm-rest / generic-players-rest re-entry adds: a mode or chooser
// body that asked suspended the legacy resolution, and its rest frame
// re-entered effCharm from its first line, emitting the unread-parameter
// Note again before running the remaining modes or choosers. A body whose
// asks the kernel served (asks counted since asksBefore, no suspension) owes
// the same Note at the same point, so the two logs stay identical; it goes
// when step 4 deletes the rest frames.
func charmRestNote(h Host, c *Ctx, sa *cards.SA, asksBefore uint64) {
	if askCount(h) != asksBefore {
		noteUnreadParams(h, c, sa.API, CharmOf(sa).Unread)
	}
}

// villainousRunChoice runs the current victim's chosen body (names) and
// reports a legacy suspension inside it, on which it records the primitive's
// own continuation: the chosen body posed a nested mid-resolution ask
// (DBSac's sacrifice picker is the live carrier), so the remaining victims
// are still asked once that ask's chain completes, instead of being
// stranded (the enclosing Resolve loop would otherwise resume only sa.Sub,
// nil for a VillainousChoice, and the outer levels would degrade to
// no-sub-ability Notes).
func villainousRunChoice(h Host, c *Ctx, sa *cards.SA, names []string) bool {
	for _, name := range names {
		if sub := cards.ResolveSVar(c.SVars, name); sub != nil {
			Resolve(h, c, sub)
		}
		if h.Suspended() {
			h.SuspendVillainousRest(sa, VillainousRest{
				Victims: append([]state.Target(nil), c.VillainousVictims...),
				Next:    c.VillainousIndex + 1})
			return true
		}
	}
	return false
}

// charmDistinctTargetRun runs a distinct modal Charm with one target group
// per selected target-bearing mode. ModeTargets is aligned to those modes;
// non-targeting modes still run with the ordinary shared context.
func charmDistinctTargetRun(h Host, c *Ctx, sa *cards.SA, names []string) bool {
	if len(c.ModeTargets) < 2 {
		return false
	}
	offset := 0
	for _, name := range ([]string)(nil) {
		if sub := cards.ResolveSVar(c.SVars, name); sub != nil && ModeTargetSpec(sub) != "" {
			offset++
		}
	}
	for i, name := range names {
		sub := cards.ResolveSVar(c.SVars, name)
		if sub == nil {
			continue
		}
		asks := askCount(h)
		savedTargets, savedOffered, savedMarker := c.Targets, c.OfferedSA, c.TargetsOffered
		savedScope, savedScopeSA := c.CharmModeScope, c.CharmModeSA
		if ModeTargetSpec(sub) != "" {
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
		charmRestNote(h, c, sa, asks)
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
	p := CharmOf(sa)
	choices := p.Modes
	if len(choices) == 0 {
		return false
	}
	// A re-entry for an answered/continued chooser carries the cursor; the SA
	// may be reached mid-resolution with c.Modes already naming the answer.
	if c.GenericChoosers != nil {
		return charmGenericPlayersRun(h, c, sa, choices)
	}
	defined := p.Defined
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
	p := CharmOf(sa)
	tempRemember := p.TempRemember
	baselineRemembered := append([]state.Target(nil), c.Remembered...)
	// FallbackAbility$ (Forge ChooseGenericEffect): the ability resolved for a
	// chooser when NONE of the Choices$ is payable RIGHT NOW -- Forge drops a
	// choice whose UnlessCost$ the chooser cannot pay, and when that empties
	// the list it runs the fallback instead of asking.
	fallback := p.FallbackAbility
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
		asks := askCount(h)
		Resolve(h, c, sub)
		c.GenericChoosers, c.GenericChooserIndex = savedChoosers, savedIndex
		if h.Suspended() {
			return true
		}
		charmRestNote(h, c, sa, asks)
		return false
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
		available := charmRandomOffer(h, c, sa, chooser.Player, genericChoiceAvailable(h, c, chooser.Player, choices))
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
				Prompt:                    "Choose 1 to 1 mode(s)", AIRandom: aiLogicRandom(sa)}
			for i, name := range available {
				d.Options = append(d.Options, decision.Option{Index: i, Kind: "mode",
					Label: CharmModeLabel(cards.ResolveSVar(c.SVars, name), name),
					Obj:   c.Source, Player: chooser.Player})
			}
			if ans, ok := AskTape(h, d); ok {
				// The resolution kernel's answer in hand: this chooser's
				// chosen body (the "generic_players" arm's choice,
				// ResumeModes order), run below exactly as the answered
				// re-entry runs it. The legacy re-entry re-runs effCharm from
				// its first line, which emits the unread-parameter Note
				// again; mirror it (see effCharm).
				pick = ""
				if len(ans) > 0 && ans[0].Index >= 0 && ans[0].Index < len(available) {
					pick = available[ans[0].Index]
				}
				noteUnreadParams(h, c, sa.API, p.Unread)
			} else {
				if Ask(h, d) == AskAsked {
					return true
				}
				// R-9: an effects-only host has no chooser, so deterministically
				// take the first option for this chooser and continue to the next
				// -- or, for an AILogic$ Random ask, a draw from the engine rng
				// (aiRandomNoAskPick), so a Repeat re-posing it advances.
				pick = available[aiRandomNoAskPick(h, sa, len(available))]
			}
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
		raw := modeUnlessCost(sub)
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
	cp := CharmOf(sa)
	noteUnreadParams(h, c, sa.API, cp.Unread)
	if c.SVars == nil {
		return
	}
	if charmGenericPlayers(h, c, sa) {
		return
	}
	choices := cp.Modes
	if len(choices) == 0 {
		return
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
		charmRunModes(h, c, sa, names)
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
	// NumRandomChoices$ (Davriel, Soul Broker): only a random draw of the
	// eligible modes is offered. The offered list rides the ask as
	// ResumeModes, the vocabulary its answer's indices map against.
	offered := false
	if picked := charmRandomOffer(h, c, sa, c.Controller, choices); len(picked) != len(choices) {
		filteredSubs := make([]*cards.SA, len(picked))
		for i, name := range picked {
			filteredSubs[i] = cards.ResolveSVar(c.SVars, name)
		}
		choices, subs, offered = picked, filteredSubs, true
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
		Prompt: "Choose " + strconv.Itoa(min) + " to " + strconv.Itoa(max) + " mode(s)", AIRandom: aiLogicRandom(sa)}
	for i, name := range choices {
		d.Options = append(d.Options, decision.Option{
			Index: i, Kind: "mode", Label: CharmModeLabel(subs[i], name),
			Obj: c.Source, Player: c.Controller})
	}
	vocab := cp.Modes
	if offered {
		d.ResumeModes = append([]string(nil), choices...)
		vocab = choices
	}
	if ans, ok := AskTape(h, d); ok {
		// The resolution kernel's answer in hand (its record wrote the
		// ModeChosen marker): run the chosen modes, named exactly as the
		// "modes" resume arm names them (modeAnswerNames: Choices$ order,
		// or the offered ResumeModes list of a NumRandomChoices$ draw).
		names := make([]string, 0, len(ans))
		for _, o := range ans {
			if o.Index >= 0 && o.Index < len(vocab) {
				names = append(names, vocab[o.Index])
			}
		}
		// The legacy re-entry re-runs effCharm from its first line, which
		// emits the unread-parameter Note again; mirror it so the logs stay
		// identical (the duplicate goes when step 4 deletes the re-entry).
		noteUnreadParams(h, c, sa.API, cp.Unread)
		charmRunModes(h, c, sa, names)
		return
	}
	if Ask(h, d) == AskAsked {
		return // resolution suspended; the answer re-enters this effect with Ctx.Modes set.
	}
	// Fuzz/no-engine host: the deterministic first-mode default (R-9), with
	// the Note that records why the richer path did not run. (AskEmpty is
	// unreachable by construction -- charmNum is clamped to >= 1 and
	// strings.Split never yields fewer than one choice -- but the shared
	// helper owns the guard either way.)
	if min == 1 && max == 1 && !repeat && len(choices) > 1 && aiLogicRandom(sa) {
		// AILogic$ Random: the no-ask answer is an engine-rng draw, not the
		// fixed first mode (see aiRandomNoAskPick).
		idx := aiRandomNoAskPick(h, sa, len(choices))
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "chose a mode at random (no engine host to ask): " + charmModeLabel(choices, subs, idx)})
		if subs[idx] != nil {
			Resolve(h, c, subs[idx])
		}
		return
	}
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
		Text: "chose its first mode (no engine host to ask)"})
	if subs[0] != nil {
		Resolve(h, c, subs[0])
	}
}

// charmRunModes runs a Charm's chosen modes (names, in execution order) and,
// when a mode's chain suspends on a legacy ask, reports the rest as a
// charm-rest continuation. It is the answered-modes half of effCharm, shared
// by the legacy re-entry (Ctx.Modes set by the resume arm) and the
// resolution kernel's tape answer.
func charmRunModes(h Host, c *Ctx, sa *cards.SA, names []string) {
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
	for _, n := range ([]string)(nil) {
		seen[n] = true
	}
	for i, name := range names {
		asks := askCount(h)
		if sub := cards.ResolveSVar(c.SVars, name); sub != nil {
			savedOffered, savedTargets, savedMark := c.OfferedSA, c.Targets, c.TargetsOffered
			first := !seen[name]
			seen[name] = true
			if modalOffered && ModeTargetSpec(sub) != "" {
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
		charmRestNote(h, c, sa, asks)
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
	p := CharmOf(sa)
	switch p.Random {
	case "True":
		return true
	case "Compare":
		holds, evaluated := CheckSVarHolds(h, c, p.RandomCompareSVar, p.RandomCompare)
		return evaluated && holds
	}
	return false
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
