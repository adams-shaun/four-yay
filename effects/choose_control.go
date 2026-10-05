package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("ChooseCard", effChooseCard)
	Register("ChoosePlayer", effChoosePlayer)
	Register("ChooseSource", effChooseSource)
	Register("GainControl", effGainControl)
	Register("GainControlVariant", effGainControlVariant)
	Register("ExchangeControl", effExchangeControl)
	Register("ControlSpell", effControlSpell)
	Register("ControlPlayer", effControlPlayer)
	Register("ChangeTargets", effChangeTargets)
	Register("RepeatEach", effRepeatEach)
	Register("Branch", effBranch)
}

// choiceBounds reads Forge's shared Amount$/MinAmount$/Mandatory$ vocabulary.
// ChooseCard is optional unless Mandatory$ True says otherwise; ChoosePlayer
// has no Mandatory$ vocabulary in the corpus and requires its choice by
// default. MinAmount$ is the explicit lower bound for either primitive.
func choiceBounds(h Host, c *Ctx, sa *cards.SA, cardChoice bool) (int, int) {
	max := int(Num(h, c, sa, "Amount", 1))
	if max < 0 {
		max = 0
	}
	min := max
	if cardChoice && !strings.EqualFold(sa.ParamStr(cards.PKMandatory), "True") {
		min = 0
	}
	if _, ok := sa.Param(cards.PKMinAmount); ok {
		min = int(Num(h, c, sa, "MinAmount", 0))
	}
	if strings.EqualFold(sa.ParamStr(cards.PKMandatory), "False") || strings.EqualFold(sa.ParamStr(cards.PKOptional), "True") {
		min = 0
	}
	if min < 0 {
		min = 0
	}
	if min > max {
		min = max
	}
	return min, max
}

func choiceZones(sa *cards.SA) map[state.Zone]bool {
	if strings.EqualFold(sa.ParamStr(cards.PKAllCards), "True") {
		return nil
	}
	s := strings.TrimSpace(sa.ParamStr(cards.PKChoiceZone))
	if s == "" {
		return map[state.Zone]bool{state.ZBattlefield: true}
	}
	out := map[state.Zone]bool{}
	for z := range strings.SplitSeq(s, ",") {
		switch choiceZonesCodes.Code(string(strings.TrimSpace(z))) {
		case choiceZonesBattlefield:
			out[state.ZBattlefield] = true
		case choiceZonesHand:
			out[state.ZHand] = true
		case choiceZonesLibrary:
			out[state.ZLibrary] = true
		case choiceZonesGraveyard:
			out[state.ZGraveyard] = true
		case choiceZonesExile:
			out[state.ZExile] = true
		case choiceZonesStack:
			out[state.ZStack] = true
		}
	}
	return out
}

// definedCardPool resolves the object-set role carried by DefinedCards$.
// Unlike Defined(), an unknown role must not fall back to the resolution's
// targets: that would widen a constrained choice to unrelated objects.
func definedCardPool(g *state.Game, c *Ctx, raw string) ([]state.Target, string) {
	root, qualifier, _ := strings.Cut(strings.TrimSpace(raw), ".")
	switch definedCardPoolCodes.Code(string(root)) {
	case definedCardPoolTargeted:
		return objectsOf(c.Targets), qualifier
	case definedCardPoolParentTargeted:
		return objectsOf(parentLinkTargets(c)), qualifier
	case definedCardPoolRemembered:
		return objectsOf(c.Remembered), qualifier
	case definedCardPoolTriggeredCards:
		return objectsOf(c.Remembered), qualifier
	case definedCardPoolTriggeredSources:
		if c.TriggerSource != 0 {
			return []state.Target{{Obj: c.TriggerSource}}, qualifier
		}
		return nil, qualifier
	case definedCardPoolExiledWith:
		// Forge's hostCard.getExiledCards is the source's ChangeZone exile
		// association, not ImprintCards$ and not every card in the shared exile
		// zone. The list is event-backed by Imprint's "exiled-with"
		// discriminator and cardChoices still intersects ChoiceZone$.
		if o := g.Obj(c.Source); o != nil {
			out := make([]state.Target, 0, len(o.ExiledCards))
			for _, id := range o.ExiledCards {
				out = append(out, state.Target{Obj: id})
			}
			return out, qualifier
		}
		return nil, qualifier
	default:
		return nil, qualifier
	}
}

func definedCardQualifierMatches(h Host, g *state.Game, c *Ctx, qualifier string, o *state.Object) bool {
	if qualifier == "" {
		return true
	}
	if qualifier == "ControlledBy ChosenPlayer" {
		for _, t := range c.Chosen {
			if t.IsPlayer && t.Player == o.Controller {
				return true
			}
		}
		return false
	}
	return choiceMatches(h, g, c, "Card."+qualifier, o)
}

// chooseCardControl is the effective ControlledByPlayer$ a cardChoices walk
// reads. An explicit parameter is used as written. When the SA carries none,
// Forge's implicit default is the CHOOSER's own objects -- but only for the
// shape whose pool is otherwise unconstrained. Choices$, DefinedCards$ and
// ValidTgts$ each supply their own pool and the default is not applied to
// them: a Choices$ spec already encodes ownership through the
// chooser-perspective filter, while DefinedCards$/ValidTgts$ and AllCards$
// explicitly supply their candidate pool and may include objects another
// player controls (Wild Swing's random pick among three targeted permanents,
// Hunted by the Family's creature you don't control). Slaughter the Strong and
// Destined Confrontation ("each player chooses ... creatures they control",
// no Choices$/ControlledByPlayer$) are the corpus's only instances of the
// unconstrained shape.
func chooseCardControl(sa *cards.SA) string {
	if v := strings.TrimSpace(sa.ParamStr(cards.PKControlledByPlayer)); v != "" {
		return v
	}
	if strings.TrimSpace(sa.ParamStr(cards.PKChoices)) != "" || DefinedOf(sa).Cards.Set() {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKAllCards)), "True") {
		return ""
	}
	if TargetsOf(sa).Has(TgtValidPresent) {
		return ""
	}
	return "Chooser"
}

func cardChoices(h Host, c *Ctx, sa *cards.SA, chooser state.PlayerID) []state.Target {
	g, spec := h.Game(), sa.ParamStr(cards.PKChoices)
	control := chooseCardControl(sa)
	var candidates []state.Target
	zones := choiceZones(sa)
	if raw := DefinedOf(sa).Cards.Text; raw != "" {
		var qualifier string
		candidates, qualifier = definedCardPool(g, c, raw)
		// A DefinedCards$ set already supplies its zone. ChoiceZone$, when
		// present, remains an additional restriction on that set.
		if !sa.HasParam(cards.PKChoiceZone) {
			zones = nil
		}
		out := candidates[:0]
		for _, t := range candidates {
			o := g.Obj(t.Obj)
			if o != nil && definedCardQualifierMatches(h, g, c, qualifier, o) {
				out = append(out, t)
			}
		}
		candidates = out
	} else {
		for i := range g.Objs {
			candidates = append(candidates, state.Target{Obj: g.Objs[i].ID})
		}
	}

	var out []state.Target
	for _, t := range candidates {
		o := g.Obj(t.Obj)
		if o == nil || (zones != nil && !zones[o.Zone]) {
			continue
		}
		if !controlledByChoicePlayer(g, c, control, chooser, o) {
			continue
		}
		// The choice's filter is evaluated from the ACTIVATOR's perspective,
		// not the chooser's (Forge's ChooseCardEffect validates Choices$
		// against sa.getActivatingPlayer()): `Choices$ Card.YouOwn` asked of
		// the target opponent is the CASTER's graveyard card (Forgotten Lore,
		// Shrouded Lore, Rejoin the Fight), `Permanent.YouCtrl` asked of the
		// chosen opponent is the caster's permanent (Wormfang Crab, Demonic
		// Hordes). Every one of the 19 corpus lines whose chooser can differ
		// from the controller and whose Choices$ reads You means the
		// controller; the chooser's own cards are spelled through
		// ControlledByPlayer$/TargetControls$ or a Remembered/Targeted
		// player predicate instead. Reading the chooser here offered those
		// choosers an empty or wrong pool (Shrouded Lore's opponent was never
		// asked at all).
		if spec == "" || choiceSpecAdmits(h, g, c, spec, o) {
			out = append(out, t)
		}
	}
	return out
}

// canBeSacrificedByToken is Forge's Card.canBeSacrificedBy(player): the
// candidate is a permanent its controller could LEGALLY sacrifice. The
// corpus carries exactly one file (Eumidian Wastewaker's attack trigger:
// `Choices$ Card.inZoneHand,Permanent.CanBeSacrificedBy`), as the second
// alternative of a ChooseCard choice. The eligibility half (the CantSacrifice
// choke point) is Host.SacrificeBlocked -- the same read effSacrifice,
// effSacrificeAll and every Sac cost site go through -- which the filter
// tier cannot reach (effects sits below rules), so like the cast-provenance
// family the token is read at the ONE site that has the Host: each comma
// alternative carrying it is stripped of the token, and the alternative
// admits a candidate only when the stripped spec matches AND the Host says
// the sacrifice is legal. The controller half ("by its controller") is the
// choice walk's own ControlledByPlayer$ read; the permanent half is the
// stripped spec's own base. A negated !CanBeSacrificedBy spelling stays an
// unknown predicate in the filter (fail closed) -- the corpus carries only
// the positive form.
const canBeSacrificedByToken = "CanBeSacrificedBy"

func choiceSpecAdmits(h Host, g *state.Game, c *Ctx, spec string, o *state.Object) bool {
	if !strings.Contains(spec, canBeSacrificedByToken) {
		return choiceMatches(h, g, c, spec, o)
	}
	matched := false
	for alt := range filterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		if admits, carried := sacrificeableAlternative(h, g, c, alt, o); carried {
			matched = matched || admits
			continue
		}
		matched = matched || choiceMatches(h, g, c, alt, o)
	}
	return matched
}

// sacrificeableAlternative evaluates ONE comma alternative of a ChooseCard
// Choices$ spec that carries the canBeSacrificedBy token: the alternative is
// rewritten without the token (the remaining base and + predicates are the
// ordinary filter, so `Permanent.CanBeSacrificedBy` reduces to base
// `Permanent` -- the on-the-battlefield reading matchesBase gives it) and
// ANDed with the Host's sacrifice-eligibility read. carried is false when the
// alternative does not name the token (the caller evaluates it verbatim);
// a !-negated spelling is deliberately not carried, so it keeps the filter's
// fail-closed unknown-predicate behaviour.
func sacrificeableAlternative(h Host, g *state.Game, c *Ctx, alt string, o *state.Object) (admits, carried bool) {
	base, rest, hasRest := strings.Cut(alt, ".")
	if !hasRest {
		return false, false
	}
	var kept []string
	found := false
	for p := range strings.SplitSeq(rest, "+") {
		if p == canBeSacrificedByToken {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return false, false
	}
	stripped := base
	if len(kept) > 0 {
		stripped = base + "." + strings.Join(kept, "+")
	}
	return !h.SacrificeBlocked(o.ID, false) && choiceMatches(h, g, c, stripped, o), true
}

// controlledByChoicePlayer applies ChooseCard's ControlledByPlayer$ (or the
// implicit Chooser default chooseCardControl derives), the
// player whose objects the chooser picks among. Corpus values: Chooser 30,
// Remembered 4 (the RepeatEach subject: Winnowing, Tragic Arrogance), Left 2,
// Right 1 (Juggle the Performance), You 1. Left is the next living player in
// turn order and Right the previous one. An unrecognised value offers
// nothing rather than every object in the zone.
func controlledByChoicePlayer(g *state.Game, c *Ctx, v string, chooser state.PlayerID, o *state.Object) bool {
	switch controlledByChoicePlayerCodes.Code(string(strings.TrimSpace(v))) {
	case controlledByChoicePlayerEmpty:
		return true
	case controlledByChoicePlayerChooser:
		return o.Controller == chooser
	case controlledByChoicePlayerYou:
		return o.Controller == c.Controller
	case controlledByChoicePlayerRemembered:
		return targetIn(c.Remembered, state.Target{Player: o.Controller, IsPlayer: true})
	case controlledByChoicePlayerLeft:
		return o.Controller == g.NextAlive(chooser)
	case controlledByChoicePlayerRight:
		alive := g.AliveFrom(chooser)
		return len(alive) > 0 && o.Controller == alive[len(alive)-1]
	}
	return false
}

func randomChoices(h Host, choices []state.Target, n int) []state.Target {
	pool := append([]state.Target(nil), choices...)
	if n > len(pool) {
		n = len(pool)
	}
	picked := make([]state.Target, 0, n)
	for len(picked) < n {
		i := h.Rand(len(pool))
		picked = append(picked, pool[i])
		pool = append(pool[:i], pool[i+1:]...)
	}
	return picked
}

// choiceMatches is the ordinary object matcher evaluated from the chooser's
// perspective: the caller overrides Ctx.Controller to the chooser, so
// Choices$ Card.YouOwn means the chooser's card, not the spell's controller's.
// IsRemembered needs no resolution-local special case any more -- the general
// filter implements it against the resolution's Remembered set (plus the
// source's event-backed list), which is exactly the binding a choice's
// "a card you remembered earlier" filter wants.
// A spec that consults greatestPower binds the battlefield-wide derived-power
// table through the ONE shared helper (GreatestPowerDerivedPTs), so a
// Choices$ pool sees a pumped creature at its derived power -- the seam the
// Myrkul's Edict family reaches through DBChooseCard.
func choiceMatches(h Host, g *state.Game, c *Ctx, spec string, o *state.Object) bool {
	sc := c.SpecContext(c.Controller)
	sc.DerivedPTs = append(sc.DerivedPTs, GreatestPowerDerivedPTs(g, spec, h)...)
	return MatchesObjectCtx(g, spec, o, sc)
}

func choiceChoosers(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	seen := map[state.PlayerID]bool{}
	var out []state.PlayerID
	defined := DefinedRefOf(sa)
	plainRemembered := defined.Has(RefPlainRemembered)
	for _, t := range DefinedRef(h, c, defined, sa) {
		// Forge's getDefinedPlayers("Remembered") adds only remembered
		// PLAYERS; a remembered CARD contributes its controller/owner only
		// for the RememberedController/RememberedOwner spellings. PlayerOf
		// maps a remembered card to its controller for every spelling, so
		// without this guard a RepeatEach iteration whose Remembered holds
		// the previous iteration's RememberChosen$ card would re-ask that
		// card's controller (Summon: Valefor).
		if plainRemembered && !t.IsPlayer {
			continue
		}
		p := PlayerOf(h, c, t)
		if !seen[p] && int(p) < len(h.Game().Players) && !h.Game().Players[p].Lost {
			seen[p] = true
			out = append(out, p)
		}
	}
	if len(out) == 0 && defined.Raw == "" {
		return []state.PlayerID{c.Controller}
	}
	return out
}

// chooseCardChoosers applies ChooseCard's StartingWith$ modifier after the
// shared chooser walk has filtered duplicates and players who left the game.
// Other choice APIs retain their own unmodified Defined$ order.
func chooseCardChoosers(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	out := choiceChoosers(h, c, sa)
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKStartingWith)), "You") {
		for i, p := range out {
			if p == c.Controller {
				return append(append([]state.PlayerID(nil), out[i:]...), out[:i]...)
			}
		}
	}
	return out
}

// choiceRecord records a completed pick into the chain's chosen binding
// (Ctx.Chosen), the Remembered set and the source's event-backed lists.
func choiceRecord(h Host, c *Ctx, sa *cards.SA, picked []state.Target, playerChoice bool) {
	if playerChoice {
		// Forge's ChoosePlayerEffect calls host.setChosenPlayer(chosen) once
		// per chooser: a single player field, last chooser wins, and the card
		// entries an earlier ChooseCard chose are untouched (its separate
		// field). Drop the old player entries, keep the card entries.
		c.Chosen = append(keepChosenCards(c.Chosen), picked...)
	} else {
		c.Chosen = append(c.Chosen, picked...)
	}
	c.ChosenValid = true
	if strings.EqualFold(sa.ParamStr(cards.PKRememberChosen), "True") {
		c.Remembered = append(c.Remembered, picked...)
	}
	if c.Source == 0 {
		return
	}
	ids := make([]state.ObjID, 0, len(picked))
	for _, t := range picked {
		if t.IsPlayer {
			ids = append(ids, state.PlayerRef(t.Player))
		} else {
			ids = append(ids, t.Obj)
		}
	}
	h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "chosen", IDs: ids})
	if strings.EqualFold(sa.ParamStr(cards.PKRememberChosen), "True") {
		h.Emit(events.Event{Kind: events.Choose, Obj: c.Source, Counter: "remembered", IDs: ids})
	}
}

// ChoiceAnswerTargets is the "choice" answer's target shape, the one home
// every tape-answered choice shares: a "player" option is a player target (player zero is a real
// target), any other option naming an object is that object.
func ChoiceAnswerTargets(chosen []decision.Option) []state.Target {
	out := make([]state.Target, 0, len(chosen))
	for _, o := range chosen {
		if o.Kind == "player" {
			out = append(out, state.Target{Player: o.Player, IsPlayer: true})
		} else if o.Obj != 0 {
			out = append(out, state.Target{Obj: o.Obj})
		}
	}
	return out
}

// chooseCardRecord is the shared completion point for answered, random and
// no-host picks. ForgetChosen removes only picked objects, after recording the
// choice, so the chosen-card binding remains available to the next ability.
func chooseCardRecord(h Host, c *Ctx, sa *cards.SA, picked []state.Target) {
	choiceRecord(h, c, sa, picked, false)
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKForgetChosen)), "True") {
		for _, t := range picked {
			if !t.IsPlayer {
				forgetRememberedOne(h, c, t.Obj)
			}
		}
	}
}

// keepChosenPlayers returns only player entries, the half a ChooseCard keeps.
func keepChosenPlayers(ts []state.Target) []state.Target {
	var out []state.Target
	for _, t := range ts {
		if t.IsPlayer {
			out = append(out, t)
		}
	}
	return out
}

// keepChosenCards returns only object entries, the separate chosen-cards
// field Forge leaves untouched when ChoosePlayer replaces its chosen player.
func keepChosenCards(ts []state.Target) []state.Target {
	var out []state.Target
	for _, t := range ts {
		if !t.IsPlayer {
			out = append(out, t)
		}
	}
	return out
}

// chooseEachGroups parses ChooseEach$: the " & "-separated group specs a
// ChooseCard with ChooseEach$ picks ONE card from PER GROUP instead of one
// card total (Tragic Arrogance, Cataclysm: "choose an artifact, a creature,
// an enchantment, and a planeswalker"). Forge's ChooseCardEffect splits the
// value on " & "; the special value "Party" (Stick Together) is Forge's
// party-member expansion — "choose up to one each of Cleric, Rogue, Warrior,
// and Wizard". Each group is an extra conjunct over the Choices$ pool, so a
// card can be eligible in several groups (Liliana, Dreadhorde General's
// Artifact & Creature ...) and be picked once per group it matches.
func chooseEachGroups(sa *cards.SA) []string {
	raw := strings.TrimSpace(sa.ParamStr(cards.PKChooseEach))
	if raw == "" {
		return nil
	}
	if strings.EqualFold(raw, "Party") {
		return []string{"Cleric", "Rogue", "Warrior", "Wizard"}
	}
	var out []string
	for part := range strings.SplitSeq(raw, " & ") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// chooseEachPool narrows one group's candidate pool: the group spec is an
// additional conjunct over cardChoices' already-filtered pool, evaluated
// from the same chooser's perspective cardChoices evaluates Choices$ with
// (Choices$ Card.YouOwn means the chooser's card).
func chooseEachPool(h Host, g *state.Game, c *Ctx, pool []state.Target, chooser state.PlayerID, group string) []state.Target {
	out := make([]state.Target, 0, len(pool))
	for _, t := range pool {
		o := g.Obj(t.Obj)
		if o == nil {
			continue
		}
		cc := *c
		cc.Controller = chooser
		if choiceMatches(h, g, &cc, group, o) {
			out = append(out, t)
		}
	}
	return out
}

func effChooseCard(h Host, c *Ctx, sa *cards.SA) {
	choosers := chooseCardChoosers(h, c, sa)
	selection := *c // candidate filters read the pre-clear remembered set
	initForgetOtherSnapshot(h, c, sa, choosers, 2)
	forgetOtherRemembered(h, c, sa)
	if c.Forget.Ready {
		// The snapshot is authoritative across the asks: a later chooser's
		// pool must still match the pre-clear candidates (plus anything
		// re-remembered since) after the first move cleared the live set.
		selection.Remembered = append(append([]state.Target(nil), selection.Remembered...), c.Forget.Snapshot...)
	}
	// Reveal$ True (Planetary Annihilation's "each player chooses six lands
	// they keep" is public knowledge — CR 701.x's open choice): each chooser's
	// ANSWERED choice is revealed to every seat with the same ids-Note
	// effReveal's public reveal emits (empty Text, view.Describe renders
	// "player N reveals ...", RedactEvents passes it through unchanged). The
	// reveal fires per chooser as their choice is recorded — both on an
	// answered ask and on the no-host fallback below — so every seat
	// learns the kept set before the next chooser picks. Player entries
	// (a ChoosePlayer follow-up) reveal nothing: a player is not hidden.
	reveal := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKReveal)), "True")
	each := chooseEachGroups(sa)
	groups := 1
	if each != nil {
		groups = len(each)
	}
	// WithTotalPower$ is a cumulative power budget over the picked cards
	// (Slaughter the Strong's "each player chooses any number of creatures
	// with total power 4 or less"): a card whose own power exceeds the
	// budget can never be picked, and the running sum of one chooser's picks
	// must not exceed it either. The cap is PER CHOOSER -- each player answers
	// their own KChoose decision and the budget rides that decision
	// (Decision.MaxSum, the same wire contract Dig's WithTotalCMC$ uses), so
	// one player's picks never spend another's budget. Absent the param (the
	// corpus default) hasBudget is false and every read below is a no-op, so
	// a budget-less ChooseCard behaves byte-identically. Present but
	// unresolvable degrades to budget 0 -- NumResolved's documented
	// convention, "the card does nothing" -- and picks nothing.
	budget, hasBudget := NumResolved(h, c, sa, "WithTotalPower", 0)
	// The walk is chooser-major, group-minor: every chooser picks from ALL
	// groups before the next chooser starts, groups in ChooseEach$ order —
	// Forge's ChooseCardEffect loop shape. With ChooseEach$ the cursor i is
	// the flat index into that (chooser, group) pair list
	// (i = chooser*groups + group), so ONE ResumeTarget int carries both
	// halves — the group count comes from the SA itself.
	i := c.ChoiceTarget

	if i == 0 {
		// Fresh entry: Forge's ChooseCardEffect ends in host.setChosenCards(allChosen)
		// -- the union across THIS SA's choosers REPLACING the cards a previous
		// choice SA left. Forge's chosen player is a separate field that
		// setChosenCards does not touch, so only the object entries are reset
		// here -- the bug this closes is the same KIND accumulating across SAs
		// (a second ChooseCard's Defined$ ChosenCard follow-up saw the first
		// SA's cards too). The union across this SA's choosers is
		// choiceRecord's append (Forge accumulates allChosen the same way
		// inside one SA).
		c.Chosen = keepChosenPlayers(c.Chosen)
	}
	minBase, maxBase := choiceBounds(h, c, sa, true)
	total := len(choosers) * groups
	for ; i < total; i++ {
		chooser := choosers[i/groups]
		choices := cardChoices(h, &selection, sa, chooser)
		if each != nil {
			choices = chooseEachPool(h, h.Game(), &selection, choices, chooser, each[i%groups])
		}
		// A card whose own power exceeds the budget can never be picked,
		// however few are taken (effDig's `affordable` rule): narrow the pool
		// to the individually affordable cards before the bounds clamp. A
		// pool this leaves empty resolves silently through the ordinary
		// empty-decision guard below -- the budgeted equivalent of Choices$
		// matching nothing.
		if hasBudget {
			affordable := make([]state.Target, 0, len(choices))
			for _, t := range choices {
				if chooseCardPower(h, t.Obj) <= int(budget) {
					affordable = append(affordable, t)
				}
			}
			choices = affordable
		}
		// With ChooseEach$ the shared Amount$/MinAmount$/Mandatory$ bounds are
		// PER GROUP (revival_experiment's Amount$ 1 | MinAmount$ 0: "up to one
		// card of that type"), so one "choice" ask is answered per group and
		// choiceRecord accumulates each group's pick. A group whose narrowed pool is empty resolves
		// silently: Ask's empty-decision guard declines the shape and the
		// choice below records nothing, exactly like Forge's per-type loop
		// with no candidate of that type.
		min, max := minBase, maxBase
		if max > len(choices) {
			max = len(choices)
		}
		if min > max {
			min = max
		}
		// greedy is the deterministic forced take under the budget: walk the
		// affordable pool in its deterministic filter order and take each
		// card only while the running power sum still fits. It is the exact
		// take the no-host fallback below applies (effDig's forced greedy-take
		// shape), and what a mandatory Min is lowered to when the budget
		// cannot pay for it -- a mandatory ask whose Min exceeds the greedy
		// affordable count would have NO legal answer (Decision.Validate's
		// Min floor against the MaxSum cap) and livelock the seat.
		greedy := choices
		if hasBudget {
			greedy = budgetGreedyTake(h, choices, max, int(budget))
			if min > len(greedy) {
				min = len(greedy)
			}
		}
		if each == nil && strings.EqualFold(sa.ParamStr(cards.PKAtRandom), "True") {
			chooseCardRecord(h, c, sa, randomChoices(h, choices, max))
			continue
		}
		d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Source: c.Source, Min: min, Max: max, ResumeKind: "choice", ResumeSA: sa, ResumeTarget: i, Prompt: sa.ParamStr(cards.PKChoiceTitle)}
		if hasBudget {
			d.MaxSum, d.Budgeted = int(budget), true
		}
		for j, t := range choices {
			opt := decision.Option{Index: j, Kind: "card", Obj: t.Obj, Player: chooser}
			// Only a budget ask carries a Value: Option.Value is omitempty,
			// and setting it budget-less would put a "value" field on the wire
			// for every offered card although MaxSum is 0 and nothing reads it.
			if hasBudget {
				opt.Value = chooseCardPower(h, t.Obj)
			}
			d.Options = append(d.Options, opt)
		}
		if d.Prompt == "" {
			d.Prompt = "Choose card"
		}
		if hasBudget {
			d.Prompt += " (total power " + strconv.Itoa(int(budget)) + " or less)"
		}
		if ans, ok := AskTape(h, d); ok {
			// Answered in place: rebuild the candidate selection from the
			// Ctx as it stood at the ask (before this record), then record,
			// reveal and re-read the bounds.
			selection = *c
			if c.Forget.Ready {
				selection.Remembered = append(append([]state.Target(nil), selection.Remembered...), c.Forget.Snapshot...)
			}
			answered := ChoiceAnswerTargets(ans)
			chooseCardRecord(h, c, sa, answered)
			if reveal {
				emitChosenReveal(h, chooser, answered)
			}
			minBase, maxBase = choiceBounds(h, c, sa, true)
			continue
		}

		recorded := choices[:min]
		if hasBudget {
			recorded = greedy
		}
		chooseCardRecord(h, c, sa, recorded)
		if reveal {
			emitChosenReveal(h, chooser, recorded)
		}
	}
	// The walk completed: release the ride (the same boundary the search and
	// hidden walks end at), so a later ability in the chain cannot inherit it.
	endForgetOtherSnapshot(c)
}

// chooseCardPower is the offered card's current power -- the WithTotalPower$
// budget's per-card price. On a battlefield permanent it reads the rules'
// derived characteristics through Host.Power (the same read
// effects/count.go's refPower shares); away from the battlefield (a library
// or graveyard pool) it falls back to face power plus the summed P/T counter
// deltas (refPower's LKI-compatible fallback).
func chooseCardPower(h Host, id state.ObjID) int {
	o := h.Game().Obj(id)
	if o == nil {
		return 0
	}
	if o.Zone == state.ZBattlefield {
		return int(h.Power(id))
	}
	f := o.Face()
	if f == nil {
		return 0
	}
	dp, _ := o.CounterPTTotals()
	return int(f.Power()) + int(dp)
}

// budgetGreedyTake is the deterministic forced take under a WithTotalPower$
// cumulative budget: walk the affordable pool in its given order and take
// each card only while the running power sum still fits the cap, up to max
// picks. The no-host fallback applies exactly this take.
func budgetGreedyTake(h Host, pool []state.Target, max, budget int) []state.Target {
	out := make([]state.Target, 0, len(pool))
	running := 0
	for _, t := range pool {
		if len(out) >= max {
			break
		}
		p := chooseCardPower(h, t.Obj)
		if running+p > budget {
			continue
		}
		running += p
		out = append(out, t)
	}
	return out
}

// sourceChoices is ChooseSource's candidate pool: every damage SOURCE the
// Choices$ spec admits -- battlefield permanents and objects on the stack
// (a red instant's Lightning Bolt is as much "a red source of your choice"
// as a red creature is), in the deterministic object-registration order the
// rest of the engine scans. The filter is evaluated from the chooser's
// perspective through the same choiceMatches the ChooseCard family uses, so
// `Card.RedSource`, `Card.ChosenColorSource` and the other ChooseSource
// specs resolve with one grammar. A source this build cannot classify (the
// Emblem half of the corpus's `Card,Emblem`, or a `Card.SharesColorWith`
// qualifier) simply contributes no option: the matcher fails closed.
func sourceChoices(h Host, c *Ctx, sa *cards.SA, chooser state.PlayerID) []state.Target {
	g, spec := h.Game(), sa.ParamStr(cards.PKChoices)
	var out []state.Target
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Zone != state.ZBattlefield && o.Zone != state.ZStack {
			continue
		}
		cc := *c
		cc.Controller = chooser
		if spec == "" || choiceMatches(h, g, &cc, spec, o) {
			out = append(out, state.Target{Obj: o.ID})
		}
	}
	return out
}

// effChooseSource is Forge's ChooseSourceEffect: the player chooses a damage
// source (a permanent or a spell), recorded exactly as ChooseCard records a
// chosen card -- into Ctx.Chosen, and onto the resolution's source object
// through the event-backed Choose "chosen" fold, which is what the registered
// replacement's ValidSource$ Card.ChosenCardStrict gate and its
// Defined$ ChosenCard/ChosenCardController body read back later. The choice
// is mandatory ("a source of your choice" is not optional); a spec with no
// eligible candidate -- an empty battlefield and stack, or a filter that
// matches nothing -- asks nothing and records nothing, so the follow-up
// replacement simply has no chosen source and does not fire.
//
// The KChoose/"choice" ask shape is shared with ChooseCard: the decision
// carries ResumeSA/ResumeTarget and each chooser's ask is answered in place
// via AskTape.
func effChooseSource(h Host, c *Ctx, sa *cards.SA) {
	choosers := choiceChoosers(h, c, sa)
	i := c.ChoiceTarget

	if i == 0 {
		// Fresh entry: mirror effChooseCard's Forge setChosenCards read. The
		// chosen-source answer is a CARD entry (the Choose "chosen" fold
		// REPLACES the source object's Chosen list, events/apply.go), so the
		// ctx card half is reset the same way while a previously chosen PLAYER
		// (Forge's separate field) survives. Without this the ctx binding and
		// the event-backed object list diverge whenever a prior ChooseCard or
		// ChooseSource ran on the same Ctx/source: a later Defined$ ChosenCard
		// would read the stale card plus the new source, while the replacement
		// gate's object-backed read sees only the new one.
		c.Chosen = keepChosenPlayers(c.Chosen)
	}
	// cardChoice=false is the mandatory shape: Min defaults to Max. Only an
	// explicit MinAmount$/Optional$ True lowers it, and no corpus ChooseSource
	// carries one -- "choose a source" is a required choice.
	minBase, maxBase := choiceBounds(h, c, sa, false)
	for ; i < len(choosers); i++ {
		choices := sourceChoices(h, c, sa, choosers[i])
		min, max := minBase, maxBase
		if max > len(choices) {
			max = len(choices)
		}
		if min > max {
			min = max
		}
		d := &decision.Decision{Player: choosers[i], Kind: decision.KChoose, Source: c.Source,
			Min: min, Max: max, ResumeKind: "choice", ResumeSA: sa, ResumeTarget: i,
			Prompt: sa.ParamStr(cards.PKChoiceTitle)}
		for j, t := range choices {
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "card", Obj: t.Obj, Player: choosers[i]})
		}
		if d.Prompt == "" {
			d.Prompt = "Choose a source"
		}
		if ans, ok := AskTape(h, d); ok {
			// Answered in place: record, then re-read the bounds before the
			// next chooser.
			choiceRecord(h, c, sa, ChoiceAnswerTargets(ans), false)
			minBase, maxBase = choiceBounds(h, c, sa, false)
			continue
		}

		choiceRecord(h, c, sa, choices[:min], false)
	}
}

// emitChosenReveal is ChooseCard's Reveal$ True emission: one public ids-Note
// naming the chosen CARDS (player entries are skipped — a chosen player is
// not hidden information), the same payload shape effReveal's public reveal
// uses so every seat's transcript reads "player N reveals ...".
func emitChosenReveal(h Host, chooser state.PlayerID, picked []state.Target) {
	var ids []state.ObjID
	for _, t := range picked {
		if !t.IsPlayer && t.Obj != 0 {
			ids = append(ids, t.Obj)
		}
	}
	if len(ids) == 0 {
		return
	}
	h.Emit(events.Event{Kind: events.Note, Player: chooser, IDs: ids})
}

// choosePlayerSpec is the player restriction of a ChoosePlayer. Choices$ is
// the explicit pool; a script without it may still narrow the pool through
// ValidTgts$ (Bill Ferny's "Choose an opponent": ValidTgts$ Opponent). With
// neither, the empty spec means every living player (Valleymaker).
func choosePlayerSpec(sa *cards.SA) string {
	if spec := strings.TrimSpace(sa.ParamStr(cards.PKChoices)); spec != "" {
		return spec
	}
	return TargetsOf(sa).ValidTgts
}

func effChoosePlayer(h Host, c *Ctx, sa *cards.SA) {
	choosers := choiceChoosers(h, c, sa)
	i := c.ChoiceTarget

	minBase, maxBase := choiceBounds(h, c, sa, false)
	g, spec := h.Game(), choosePlayerSpec(sa)
	// A ChoosePlayer that carries ValidTgts$ chose its player as a target when
	// the ability was put on the stack (Bill Ferny's "target opponent"): the
	// choice is that target, if it is still a matching player.
	var targeted map[state.PlayerID]bool
	if TargetsOf(sa).Has(TgtValidPresent) && sa.ParamStr(cards.PKChoices) == "" {
		for _, t := range c.Targets {
			if t.IsPlayer && int(t.Player) < len(g.Players) && MatchesPlayerSpecWithSVars(h, c, spec, t.Player, c.Controller) {
				if targeted == nil {
					targeted = map[state.PlayerID]bool{}
				}
				targeted[t.Player] = true
			}
		}
	}
	for ; i < len(choosers); i++ {
		var choices []state.Target
		for _, p := range g.AliveFrom(choosers[i]) {
			if targeted != nil && !targeted[p] {
				continue
			}
			if spec == "" {
				// "Choose a player" with no restriction (Valleymaker): every
				// living player is a legal choice.
			} else if spec == "NonChosenPlayer" {
				seen := false
				for _, t := range c.Remembered {
					if t.IsPlayer && t.Player == p {
						seen = true
						break
					}
				}
				if seen {
					continue
				}
			} else if !MatchesPlayerSpecWithSVars(h, c, spec, p, choosers[i]) {
				// The source is passed so a compound Choices$ spec whose clauses
				// read the choosing object's own choice state resolves:
				// Territorial Hellkite's `Player.Opponent+!IsRemembered` (an
				// opponent the dragon did not attack last combat) needs the
				// source's event-backed Remembered list, which
				// MatchesPlayerSpec (source 0) cannot see and therefore failed
				// closed to an EMPTY pool.
				continue
			}
			choices = append(choices, state.Target{Player: p, IsPlayer: true})
		}
		min, max := minBase, maxBase
		if max > len(choices) {
			max = len(choices)
		}
		if min > max {
			min = max
		}
		if strings.EqualFold(sa.ParamStr(cards.PKRandom), "True") {
			choiceRecord(h, c, sa, randomChoices(h, choices, max), true)
			continue
		}
		d := &decision.Decision{Player: choosers[i], Kind: decision.KChoose, Source: c.Source, Min: min, Max: max, ResumeKind: "choice", ResumeSA: sa, ResumeTarget: i, Prompt: sa.ParamStr(cards.PKChoiceTitle)}
		for j, t := range choices {
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "player", Player: t.Player})
		}
		if d.Prompt == "" {
			d.Prompt = "Choose player"
		}
		if ans, ok := AskTape(h, d); ok {
			// Answered in place: record, then re-read the bounds before the
			// next chooser.
			choiceRecord(h, c, sa, ChoiceAnswerTargets(ans), true)
			minBase, maxBase = choiceBounds(h, c, sa, false)
			continue
		}

		choiceRecord(h, c, sa, choices[:min], true)
	}
	// Forge's ChoosePlayerEffect then runs one of the two chained riders: a
	// successful choice runs ChooseSubAbility$ (Territorial Hellkite's DBPump,
	// which registers the "attacks that player this combat if able"
	// requirement), and a choice that found no candidate -- or named no
	// chooser at all -- runs CantChooseSubAbility$ (DBTap). Most ChoosePlayer
	// carriers carry neither, so an absent param is a no-op. The rider runs
	// under the same Ctx the choice was recorded on, so it reads the just-made
	// choice through Ctx.Chosen and the source object's event-backed Chosen.
	//
	// The decision is per RESOLUTION, not per chooser: this block is reached
	// exactly once, after every chooser has answered, so a multi-chooser ChoosePlayer runs
	// its rider once (the last non-empty answer is what the source holds --
	// the same last-chooser-wins rule choiceRecord's playerChoice branch
	// already applies). A multi-chooser rider carrier is corpus-unreachable
	// (the one ChooseSubAbility carrier, Territorial Hellkite, has a single
	// Defined$ You chooser), so the once-per-resolution read is exact where it
	// is reachable and conservative where it is not.
	picked := false
	for _, t := range c.Chosen {
		if t.IsPlayer {
			picked = true
			break
		}
	}
	if picked {
		runChooseRider(h, c, sa, "ChooseSubAbility")
	} else {
		runChooseRider(h, c, sa, "CantChooseSubAbility")
	}
}

// runChooseRider resolves and runs one of a ChoosePlayer's chained riders
// (Forge's ChooseSubAbility$/CantChooseSubAbility$): the named SVar body on
// the SA's own face runs under the same Ctx the choice was recorded on. An
// absent param, a nil SVar table or an unresolvable name is the fail-closed
// no-op -- every ChoosePlayer carrier without the rider, and any name this
// build cannot resolve, simply runs nothing rather than a wrong body.
func runChooseRider(h Host, c *Ctx, sa *cards.SA, param string) {
	name := strings.TrimSpace(sa.Params[param])
	if name == "" || c.SVars == nil {
		return
	}
	sub := cards.ResolveSVar(c.SVars, name)
	if sub == nil {
		return
	}
	Resolve(h, c, sub)
}

// playerTargetIn returns the first player entry in ts, the same first-match
// order Defined's own player resolvers use.
func playerTargetIn(ts []state.Target) (state.PlayerID, bool) {
	for _, t := range ts {
		if t.IsPlayer {
			return t.Player, true
		}
	}
	return 0, false
}

// controlPlayer resolves NewController$, the player who gains control. ok is
// false when the value names nobody this resolution can bind; the control
// change then does not happen (Forge's getDefinedPlayers yields no player),
// rather than silently handing control to the effect's own controller --
// which for "that player gains control of CARDNAME" (Karona, Drooling Ogre,
// Contested War Zone) is a no-op that looks like success.
//
// When NewController$ is ABSENT and the SA targeted a player (the
// "target opponent gains control of CARDNAME" family: Sleeper Agent, Goblin
// Cadets, Avarice Amulet, ...), Forge's GainControlEffect reads the targeted
// player as the new controller; that player is bound on the Ctx as the ask's
// answer (Ctx.PickedTargets for a pre-asked chained sub, Ctx.Targets for the
// placement/announcement ask), never in Defined$'s own list, which names the
// gained object (Defined$ Self). With no player target bound the default
// stays the effect's controller -- the object-target steal shape ("gain
// control of target creature"), where that is correct.
func controlPlayer(h Host, c *Ctx, sa *cards.SA) (state.PlayerID, bool) {
	g := h.Game()
	v := strings.TrimSpace(sa.ParamStr(cards.PKNewController))
	switch controlPlayerSimpleCodes.Code(string(v)) {
	case controlPlayerSimpleEmpty:
		if p, ok := playerTargetIn(c.PickedTargets); ok {
			return p, true
		}
		if p, ok := playerTargetIn(c.Targets); ok {
			return p, true
		}
		return c.Controller, true
	case controlPlayerSimpleYou:
		return c.Controller, true
	case controlPlayerSimpleChosenPlayer:
		for _, t := range c.Chosen {
			if t.IsPlayer {
				return t.Player, true
			}
		}
		// A choice made by an earlier, independently resolving ability is
		// event-backed on its source rather than present in this fresh Ctx.
		if o := g.Obj(c.Source); o != nil {
			for _, t := range o.Chosen {
				if t.IsPlayer {
					return t.Player, true
				}
			}
		}
		return 0, false
	case controlPlayerSimplePlayerIsRemembered:
		for _, t := range c.Remembered {
			if t.IsPlayer {
				return t.Player, true
			}
		}
		return 0, false
	case controlPlayerSimpleImprintedController:
		// Forge's addPlayer(host.getImprintedCards(), "ImprintedController")
		// returns the first imprinted card's current controller.
		if src := g.Obj(c.Source); src != nil {
			for _, id := range src.Imprinted {
				if o := g.Obj(id); o != nil {
					return o.Controller, true
				}
			}
		}
		return 0, false
	case controlPlayerSimpleTriggeredSourceController:
		// DamageDone's source: "that creature's controller".
		if o := g.Obj(c.TriggerSource); o != nil {
			return o.Controller, true
		}
		return 0, false
	case controlPlayerSimpleTriggeredTarget:
		if t := c.TriggerTarget; t.IsPlayer {
			return t.Player, true
		} else if o := g.Obj(t.Obj); o != nil {
			return o.Controller, true
		}
		return 0, false
	}
	// The next seat in turn order (Forge's getNextPlayerAfter -- "the player
	// to your right" is the seat that plays BEFORE you, the last of the
	// alive seats reachable from the controller; "left" is the next one).
	// An unbound form (no other living seat) names nobody. Checked BEFORE the
	// whitelist switch below, whose default would otherwise decline these.
	if v == "NextPlayerToYourRight" || v == "NextPlayerToYourLeft" {
		alive := g.AliveFrom(c.Controller)
		if len(alive) < 2 {
			return 0, false
		}
		if v == "NextPlayerToYourRight" {
			return alive[len(alive)-1], true
		}
		return alive[1], true
	}
	if strings.HasPrefix(v, "Player.withMost") {
		// Forge resolves a Player.<property> defined player by matching the
		// property against every seat and taking the first match in seat
		// order; the withMost* family is implemented in the shared player
		// filter (MatchesPlayerSpecFrom), so this walk cannot disagree with
		// a trigger restriction or attack declaration using the same spec.
		for _, p := range g.AliveFrom(0) {
			if MatchesPlayerSpecWithSVars(h, c, v, p, c.Controller) {
				return p, true
			}
		}
		return 0, false
	}
	switch controlPlayerReferentCodes.Code(string(v)) {
	case controlPlayerReferentRemembered:
	default:
		// Defined() falls back to the resolution's targets for a form it does
		// not model; that is never a meaningful new controller.
		return 0, false
	}
	ts := DefinedSpec(h, c, v)
	// A player named directly wins over an object's controller ("target
	// player gains control of target creature" lists both targets).
	for _, t := range ts {
		if t.IsPlayer && int(t.Player) < len(g.Players) {
			return t.Player, true
		}
	}
	for _, t := range ts {
		if o := g.Obj(t.Obj); o != nil {
			return o.Controller, true
		}
	}
	return 0, false
}

func effGainControl(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	dur, unknown := ParseControlDuration(sa.ParamStr(cards.PKLoseControl))
	if unknown != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "GainControl LoseControl$ " + unknown + " unimplemented"})
		return
	}
	base := ControlGrant{You: c.Controller, Source: c.Source, Duration: dur, SVars: c.SVars,
		AddKeywords: cards.SplitKeywordList(sa.ParamStr(cards.PKAddKWs))}
	if src := g.Obj(c.Source); src != nil && src.Zone == state.ZBattlefield {
		base.SourceStamp = src.Timestamp
	}
	if dur.Unattached {
		// "For as long as that Aura is attached to it": the Aura is the
		// attaching object that fired the trigger.
		base.Aura = c.TriggerSource
		if base.Aura == 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "GainControl UntilSourceUnattached has no triggering attachment"})
			return
		}
	}
	if dur.StaticCheck {
		body, ok := c.SVars[sa.ParamStr(cards.PKStaticCommandCheckSVar)]
		cmp := strings.TrimSpace(sa.ParamStr(cards.PKStaticCommandSVarCompare))
		if !ok || len(cmp) < 3 || !strings.Contains("EQ NE LT LE GT GE", strings.ToUpper(cmp[:2])) {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "GainControl StaticCommandCheck is not evaluable"})
			return
		}
		base.CheckSVar, base.Compare = body, cmp
	}

	var ts []state.Target
	if strings.TrimSpace(sa.ParamStr(cards.PKChoices)) != "" {

		chooser := changeTargetChooser(h, c, sa)
		choices := cardChoices(h, c, sa, chooser)
		if len(choices) == 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "GainControl Choices$ has no eligible cards"})
			return
		}
		if len(choices) > 1 {
			d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Source: c.Source, Min: 1, Max: 1, ResumeKind: "choice", ResumeSA: sa, Prompt: sa.ParamStr(cards.PKChoiceTitle)}
			for i, t := range choices {
				d.Options = append(d.Options, decision.Option{Index: i, Kind: "card", Obj: t.Obj, Player: chooser})
			}
			if d.Prompt == "" {
				d.Prompt = "Choose card"
			}
			ans, ok := AskTape(h, d)
			if !ok {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "GainControl Choices$ requires a player choice"})
				return
			}
			// Answered in place: the record below, then the transfer.
			ts = ChoiceAnswerTargets(ans)
		} else {
			ts = choices
		}
		choiceRecord(h, c, sa, ts, false)

	} else if spec := sa.ParamStr(cards.PKAllValid); spec != "" {
		for i := range g.Objs {
			o := &g.Objs[i]
			if o.Zone == state.ZBattlefield && MatchesObjectCtx(g, spec, o, c.SpecContext(c.Controller)) {
				ts = append(ts, state.Target{Obj: o.ID})
			}
		}
	} else {
		ts = Defined(h, c, sa)
	}
	p, ok := controlPlayer(h, c, sa)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "GainControl NewController$ " + sa.ParamStr(cards.PKNewController) + " names no player"})
		return
	}
	for _, t := range ts {
		if t.IsPlayer {
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		gr := base
		gr.Obj, gr.ObjStamp, gr.Previous, gr.Controller = o.ID, o.Timestamp, o.Controller, p
		// CR 611.2b: an effect whose "for as long as" duration has already
		// ended when it would begin does nothing (Vedalken Shackles untapped
		// in response, Kellogg sacrificed in response).
		if ControlGrantEnded(h, gr) {
			continue
		}
		h.Emit(events.Event{Kind: events.ControlChange, Obj: o.ID, Player: p})
		h.RegisterControl(gr)
		if strings.EqualFold(sa.ParamStr(cards.PKUntap), "True") {
			h.Emit(events.Event{Kind: events.Untap, Obj: o.ID})
		}
		if strings.EqualFold(sa.ParamStr(cards.PKRememberControlled), "True") {
			// Forge (ControlGainEffect): source.addRemembered(tgtC) once per
			// gained permanent -- the persistent host-card list a later
			// Card.IsRemembered spec ("the permanents you gained control of
			// this way", e.g. Ambition's Cost's follow-up or a broker deck's
			// next trigger) matches. Recorded at ctx level for the same walk's
			// SubAbility$ and event-backed on the source for later reads. The
			// two dedupes are per level: the ctx append dedupes against the
			// walk's set, the persistent event dedupes against the source's
			// list ONLY -- a ctx entry that already names the gained object can
			// be the trigger's REFERENT capture (Kain's GainControl of itself:
			// ValidSource$ Card.Self put Kain in ctx.Remembered before this
			// leg ran), which must not suppress the persistent write a later
			// Remembered$ count/condition reads.
			tgt := state.Target{Obj: o.ID}
			if !targetIn(c.Remembered, tgt) {
				c.Remembered = append(c.Remembered, tgt)
			}
			if src := h.Game().Obj(c.Source); src == nil || !targetIn(src.Remembered, tgt) {
				eventRemember(h, c, o.ID)
			}
		}
	}
}

// effGainControlVariant implements Forge's GainControlVariant: a batch
// control effect that takes no target and enumerates every battlefield
// permanent matching AllValid$, handing each to the player ChangeController$
// names. The corpus values fall into three shapes:
//
//   - CardOwner (Alicia Masters, Trostani Discordant, Homeward Path, ...):
//     each player gains control of all permanents they own.
//   - Random (Scrambleverse): a random LIVING player is chosen for each
//     matching permanent, then each chosen player gains it.
//   - a player-selection hand-off directed by ChooseDirection or by a fixed
//     neighbour: ChooseFromPlayerToTheirRight (Inniaz, the Gale Force),
//     NextPlayerInChosenDirection (Aminatou, the Fateshifter's [-6]) and
//     ChooseNextPlayerInChosenDirection (Order of Succession).
//
// An unrecognised value is a loud Note and no transfer, the fail-closed
// direction: applying CardOwner for an unmodelled value would hand every
// permanent to its owner, a different and WRONG result.
func effGainControlVariant(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	change := strings.TrimSpace(sa.ParamStr(cards.PKChangeController))
	switch {
	case strings.EqualFold(change, "CardOwner"):
		gainControlVariantCardOwner(h, c, sa, g)
	case strings.EqualFold(change, "Random"):
		gainControlVariantRandom(h, c, sa, g)
	case strings.EqualFold(change, "ChooseFromPlayerToTheirRight"):
		gainControlVariantInniaz(h, c, sa, g)
	case strings.EqualFold(change, "NextPlayerInChosenDirection"):
		gainControlVariantAminatou(h, c, sa, g)
	case strings.EqualFold(change, "ChooseNextPlayerInChosenDirection"):
		gainControlVariantOrder(h, c, sa, g)
	default:
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "GainControlVariant ChangeController$ " + change + " unimplemented"})
	}
}

// gainControlVariantBase validates the AllValid$/LoseControl$ shape every
// value shares and builds the grant template each object's transfer fills in.
func gainControlVariantBase(h Host, c *Ctx, sa *cards.SA, g *state.Game) (string, ControlGrant, bool) {
	spec := strings.TrimSpace(sa.ParamStr(cards.PKAllValid))
	if spec == "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "GainControlVariant has no AllValid$ filter"})
		return "", ControlGrant{}, false
	}
	dur, unknown := ParseControlDuration(sa.ParamStr(cards.PKLoseControl))
	if unknown != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "GainControlVariant LoseControl$ " + unknown + " unimplemented"})
		return "", ControlGrant{}, false
	}
	base := ControlGrant{You: c.Controller, Source: c.Source, Duration: dur, SVars: c.SVars,
		AddKeywords: cards.SplitKeywordList(sa.ParamStr(cards.PKAddKWs))}
	if src := g.Obj(c.Source); src != nil && src.Zone == state.ZBattlefield {
		base.SourceStamp = src.Timestamp
	}
	return spec, base, true
}

// gainControlVariantObjects lists the battlefield permanents matching spec in
// deterministic arena order. When anyController is false only permanents
// controlled by ctrl are returned.
func gainControlVariantObjects(g *state.Game, c *Ctx, spec string, ctrl state.PlayerID, anyController bool) []state.ObjID {
	sc := c.SpecContext(c.Controller)
	var out []state.ObjID
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Zone != state.ZBattlefield {
			continue
		}
		if !anyController && o.Controller != ctrl {
			continue
		}
		if MatchesObjectCtx(g, spec, o, sc) {
			out = append(out, o.ID)
		}
	}
	return out
}

// gainControlVariantApply is the ONE control-transfer site every variant
// uses: it emits the ControlChange only on a visible move and registers the
// grant. A permanent already under the new controller still gets the grant
// record (CR 613.7: the newest control effect becomes the latest), the
// CardOwner contract.
func gainControlVariantApply(h Host, base ControlGrant, o *state.Object, to state.PlayerID) {
	gr := base
	gr.Obj, gr.ObjStamp, gr.Previous, gr.Controller = o.ID, o.Timestamp, o.Controller, to
	if ControlGrantEnded(h, gr) {
		return
	}
	if o.Controller != to {
		h.Emit(events.Event{Kind: events.ControlChange, Obj: o.ID, Player: to})
	}
	h.RegisterControl(gr)
}

// gainControlVariantCardOwner implements ChangeController$ CardOwner.
func gainControlVariantCardOwner(h Host, c *Ctx, sa *cards.SA, g *state.Game) {
	spec, base, ok := gainControlVariantBase(h, c, sa, g)
	if !ok {
		return
	}
	// The dense object arena is creation order, so the walk (and therefore
	// the emitted ControlChange sequence and its ControlGrant records) is
	// deterministic across a replay.
	for _, id := range gainControlVariantObjects(g, c, spec, 0, true) {
		o := g.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		// The effect is applied to EVERY matching permanent, including one
		// its owner already controls (CR 613.7); only a visible change of
		// controller emits the ControlChange event.
		gainControlVariantApply(h, base, o, o.Owner)
	}
}

// gainControlVariantRandom implements ChangeController$ Random
// (Scrambleverse): one random LIVING player per matching permanent, drawn
// from the seeded host generator BEFORE any transfer, so the resulting
// ControlChange sequence replays identically.
func gainControlVariantRandom(h Host, c *Ctx, sa *cards.SA, g *state.Game) {
	spec, base, ok := gainControlVariantBase(h, c, sa, g)
	if !ok {
		return
	}
	alive := g.AliveFrom(0)
	if len(alive) == 0 {
		return
	}
	objs := gainControlVariantObjects(g, c, spec, 0, true)
	picks := make([]state.PlayerID, len(objs))
	for i := range objs {
		picks[i] = alive[h.Rand(len(alive))]
	}
	// Scrambleverse's SubAbility$ DBUntap runs after this returns, through
	// Resolve's ordinary sa.Sub walk -- including a permanent whose random
	// pick left its controller unchanged.
	for i, id := range objs {
		if o := g.Obj(id); o != nil && o.Zone == state.ZBattlefield {
			gainControlVariantApply(h, base, o, picks[i])
		}
	}
}

// gainControlVariantInniaz implements ChangeController$
// ChooseFromPlayerToTheirRight (Inniaz, the Gale Force): for EVERY player,
// the effect's controller chooses one matching permanent controlled by the
// player to that player's right, and that player gains it. The chooser is the
// caster for every recipient -- the shape that distinguishes Inniaz from
// Order of Succession below, where each recipient chooses for themself.
func gainControlVariantInniaz(h Host, c *Ctx, sa *cards.SA, g *state.Game) {
	spec, base, ok := gainControlVariantBase(h, c, sa, g)
	if !ok {
		return
	}
	recipients := g.AliveFrom(c.Controller)
	gainControlVariantAskLoop(h, c, sa, base, recipients,
		func(state.PlayerID) state.PlayerID { return c.Controller },
		func(R state.PlayerID) []state.ObjID {
			right, ok := gainControlNeighbor(g, R, directionRight)
			if !ok {
				return nil
			}
			return gainControlVariantObjects(g, c, spec, right, false)
		},
		"Choose a nonland permanent controlled by the player to that player's right")
}

// gainControlVariantAminatou implements ChangeController$
// NextPlayerInChosenDirection (Aminatou, the Fateshifter's [-6]): each player
// gains control of all matching permanents controlled by the next player in
// the chosen direction. Every recipient's pool is read from the PRE-transfer
// controllers and the transfers are applied afterwards, so two recipients can
// never be offered the same permanent once control has moved.
func gainControlVariantAminatou(h Host, c *Ctx, sa *cards.SA, g *state.Game) {
	spec, base, ok := gainControlVariantBase(h, c, sa, g)
	if !ok {
		return
	}
	dir, ok := gainControlVariantDirection(h, c, sa)
	if !ok {
		return
	}
	type transfer struct {
		id state.ObjID
		to state.PlayerID
	}
	var gains []transfer
	for _, R := range g.AliveFrom(0) {
		next, ok := gainControlNeighbor(g, R, dir)
		if !ok {
			continue
		}
		for _, id := range gainControlVariantObjects(g, c, spec, next, false) {
			gains = append(gains, transfer{id: id, to: R})
		}
	}
	for _, tr := range gains {
		if o := g.Obj(tr.id); o != nil && o.Zone == state.ZBattlefield {
			gainControlVariantApply(h, base, o, tr.to)
		}
	}
}

// gainControlVariantOrder implements ChangeController$
// ChooseNextPlayerInChosenDirection (Order of Succession): starting with the
// caster and proceeding in the chosen direction, each player chooses one
// matching permanent controlled by the next player in that direction, and
// gains it. Each recipient is its OWN chooser.
func gainControlVariantOrder(h Host, c *Ctx, sa *cards.SA, g *state.Game) {
	spec, base, ok := gainControlVariantBase(h, c, sa, g)
	if !ok {
		return
	}
	dir, ok := gainControlVariantDirection(h, c, sa)
	if !ok {
		return
	}
	recipients := gainControlDirectionRing(g, c.Controller, dir)
	gainControlVariantAskLoop(h, c, sa, base, recipients,
		func(R state.PlayerID) state.PlayerID { return R },
		func(R state.PlayerID) []state.ObjID {
			next, ok := gainControlNeighbor(g, R, dir)
			if !ok {
				return nil
			}
			return gainControlVariantObjects(g, c, spec, next, false)
		},
		"Choose a permanent controlled by the next player in the chosen direction")
}

// gainControlVariantAskLoop runs the ordered per-recipient choice loop the
// Inniaz and Order shapes share. For each recipient with at least one
// eligible permanent, chooserFor names who picks (the caster for Inniaz, the
// recipient for Order) and poolFor lists that recipient's eligible
// permanents. A recipient with no eligible permanent is skipped with a
// positional empty pick so the cursor stays aligned with recipients.
//
// The picks are gathered across every ask BEFORE any transfer is applied, so
// an earlier hand-off cannot change a later recipient's pool. Each ask is
// answered in place via AskTape; the cursor rides Decision.ResumeTarget.
func gainControlVariantAskLoop(h Host, c *Ctx, sa *cards.SA, base ControlGrant,
	recipients []state.PlayerID,
	chooserFor func(state.PlayerID) state.PlayerID,
	poolFor func(state.PlayerID) []state.ObjID,
	prompt string) {
	i := c.ChoiceTarget
	var picks []state.Target

	if i > 0 {
		picks = append([]state.Target(nil), c.Chosen...)
	}
	for ; i < len(recipients); i++ {
		pool := poolFor(recipients[i])
		if len(pool) == 0 {
			picks = append(picks, state.Target{})
			continue
		}
		if len(pool) == 1 {
			picks = append(picks, state.Target{Obj: pool[0]})
			c.Chosen = picks
			continue
		}
		chooser := chooserFor(recipients[i])
		c.ChoiceTarget, c.Chosen = i, picks
		d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Min: 1, Max: 1,
			Source: c.Source, ResumeKind: "choice", ResumeSA: sa, ResumeTarget: i,
			Prompt: prompt}
		for j, id := range pool {
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "card", Obj: id, Player: chooser})
		}
		if ans, ok := AskTape(h, d); ok {
			// Answered in place: append the pick (an empty answer keeps the
			// positional blank).
			pick := state.Target{}
			if ts := ChoiceAnswerTargets(ans); len(ts) > 0 {
				pick = ts[0]
			}
			picks = append(picks[:len(picks):len(picks)], pick)
			continue
		}

		picks = append(picks, state.Target{Obj: pool[0]})
		c.Chosen = picks
	}
	for k, R := range recipients {
		if k >= len(picks) || picks[k].Obj == 0 {
			continue
		}
		if o := h.Game().Obj(picks[k].Obj); o != nil && o.Zone == state.ZBattlefield {
			gainControlVariantApply(h, base, o, R)
		}
	}
	c.ChoiceTarget = 0
}

func effControlSpell(h Host, c *Ctx, sa *cards.SA) {
	// Mode$ (Commandeer's "Gain"): what the control transfer targets. "Gain"
	// — the corpus's only value — takes control of the target SPELL on the
	// stack (the ControlChange below is already stack-scoped), which is the
	// behaviour this primitive always had; Forge's ControlSpellEffect reads
	// the same param and branches on it (Gain vs the permanent shapes). An
	// unrecognised value is a loud Note and no transfer, the fail-closed
	// direction — a control change applied to the wrong kind of object is
	// not recoverable.
	mode := strings.TrimSpace(sa.ParamStr(cards.PKMode))
	switch effControlSpellCodes.Code(string(mode)) {
	case effControlSpellGain:
	default:
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unhandled ControlSpell Mode$ " + mode})
		return
	}
	p, ok := controlPlayer(h, c, sa)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "ControlSpell NewController$ " + sa.ParamStr(cards.PKNewController) + " names no player"})
		return
	}
	for _, t := range Defined(h, c, sa) {
		if !t.IsPlayer {
			if o := h.Game().Obj(t.Obj); o != nil && o.Zone == state.ZStack {
				h.Emit(events.Event{Kind: events.ControlChange, Obj: o.ID, Player: p})
			}
		}
	}
}

// effControlPlayer implements CR 720's "you control target opponent during
// their next turn" (Mindslaver, Sorin Markov, The Dominion Bracelet's granted
// ability; `api:ControlPlayer`). The target player is the CONTROLLED seat and
// the resolving ability's controller is the seat controlling them. The grant
// is one ControlPlayerChange event folded into state.Game.ControlledBy; the
// engine redirects that player's decisions to their controller while the
// grant is live (rules/engine.go ask) and expires it at the end of that
// player's next turn (rules/turn.go beginTurn). The duration is Forge's fixed
// "next turn" reading -- none of the corpus carriers carries a Duration$.
func effControlPlayer(h Host, c *Ctx, sa *cards.SA) {
	subj, ok := playerTargetIn(c.Targets)
	if !ok {
		subj, ok = playerTargetIn(c.PickedTargets)
	}
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "ControlPlayer has no opponent target player"})
		return
	}
	if subj == c.Controller {
		// CR 720.6: a player cannot control themselves. The one corpus shape
		// that could read this way is an unrestricted ValidTgts$ whose answer
		// is the controller; fail closed with no grant rather than no-op
		// silently.
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "ControlPlayer target is the source's own controller"})
		return
	}
	h.Emit(events.Event{Kind: events.ControlPlayerChange, Player: c.Controller,
		IDs: []state.ObjID{state.PlayerRef(subj)}, Amount: 1})
}

func effectSA(o *state.Object) *cards.SA {
	if o.Ability != nil {
		return o.Ability
	}
	if f := o.Face(); f != nil {
		return f.SpellAbility()
	}
	return nil
}

func targetIn(ts []state.Target, want state.Target) bool {
	for _, t := range ts {
		if t.IsPlayer == want.IsPlayer && ((t.IsPlayer && t.Player == want.Player) || (!t.IsPlayer && t.Obj == want.Obj)) {
			return true
		}
	}
	return false
}

func targetAllowed(h Host, c *Ctx, restriction string, t state.Target) bool {
	if restriction == "" {
		return true
	}
	if t.IsPlayer {
		return MatchesPlayerSpec(h.Game(), restriction, t.Player, c.Controller)
	}
	return MatchesObjectCtx(h.Game(), restriction, h.Game().Obj(t.Obj), c.SpecContext(c.Controller))
}

func recordTargets(h Host, obj state.ObjID, ts []state.Target) {
	for i, t := range ts {
		amount := int32(0)
		if i > 0 {
			amount = 2
		}
		ev := events.Event{Kind: events.TargetsChosen, Obj: obj, Amount: amount}
		if t.IsPlayer {
			ev.Player = t.Player
			ev.Amount++
		} else {
			ev.IDs = []state.ObjID{t.Obj}
		}
		h.Emit(ev)
	}
}

func changeTargetChooser(h Host, c *Ctx, sa *cards.SA) state.PlayerID {
	v := strings.TrimSpace(sa.ParamStr(cards.PKChooser))
	if v == "" || v == "You" {
		return c.Controller
	}
	// definedPlayerIDs keeps the plain Remembered family players-only, so a
	// remembered CARD cannot hand the redirect chooser to its controller.
	if ps := definedPlayerIDs(h, c, v); len(ps) > 0 {
		return ps[0]
	}
	return c.Controller
}

// effChangeTargets derives candidates through Host.LegalTargets, the exact
// rules-side target census used at announcement. The redirect's own
// TargetRestriction narrows that set; it can never widen the subject spell's
// legality. Fixed magnets and random redirects use the same intersection.
func effChangeTargets(h Host, c *Ctx, sa *cards.SA) {
	var target *state.Object
	for _, t := range Defined(h, c, sa) {
		if !t.IsPlayer {
			if o := h.Game().Obj(t.Obj); o != nil && o.Zone == state.ZStack {
				target = o
				break
			}
		}
	}
	if target == nil {
		return
	}

	subject := effectSA(target)
	if subject == nil || len(target.Targets) == 0 {
		return
	}
	chooser := changeTargetChooser(h, c, sa)
	restriction := strings.TrimSpace(sa.ParamStr(cards.PKTargetRestriction))
	if strings.EqualFold(sa.ParamStr(cards.PKRandomTarget), "True") && sa.ParamStr(cards.PKRandomTargetRestriction) != "" {
		restriction = sa.ParamStr(cards.PKRandomTargetRestriction)
	}
	var candidates []state.Target
	// CR 115.7: a changed target must be one the spell or ability could
	// legally target, which is decided from its own controller's side
	// ("target creature an opponent controls" names the redirected spell's
	// opponents, not the redirecting player's).
	for _, t := range h.LegalTargets(target.Controller, target.ID, subject) {
		if targetAllowed(h, c, restriction, t) {
			candidates = append(candidates, t)
		}
	}

	if magnet := strings.TrimSpace(sa.ParamStr(cards.PKDefinedMagnet)); magnet != "" {
		var picked []state.Target
		for _, t := range DefinedSpec(h, c, magnet) {
			if targetIn(candidates, t) {
				picked = append(picked, t)
				break
			}
		}
		if len(picked) > 0 {
			picked = append(picked, target.Targets[1:]...)
			recordTargets(h, target.ID, picked)
		}
		return
	}

	count := len(target.Targets)
	if strings.EqualFold(sa.ParamStr(cards.PKChangeSingleTarget), "True") {
		count = 1
	}
	if strings.EqualFold(sa.ParamStr(cards.PKRandomTarget), "True") {
		pool := append([]state.Target(nil), candidates...)
		var picked []state.Target
		for len(picked) < count && len(pool) > 0 {
			i := h.Rand(len(pool))
			picked = append(picked, pool[i])
			pool = append(pool[:i], pool[i+1:]...)
		}
		if len(picked) > 0 {
			if len(picked) < len(target.Targets) {
				picked = append(picked, target.Targets[len(picked):]...)
			}
			recordTargets(h, target.ID, picked)
		}
		return
	}

	min, max := count, count
	if strings.EqualFold(sa.ParamStr(cards.PKOptional), "True") {
		min = 0
	}
	if max > len(candidates) {
		max = len(candidates)
		if min > max {
			min = max
		}
	}
	d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Source: c.Source, Min: min, Max: max, ResumeKind: "choice", ResumeSA: sa, Prompt: "Choose new target"}
	for _, t := range candidates {
		o := decision.Option{Index: len(d.Options)}
		if t.IsPlayer {
			o.Kind, o.Player = "player", t.Player
		} else {
			o.Kind, o.Obj = "card", t.Obj
			if obj := h.Game().Obj(t.Obj); obj != nil {
				o.Player = obj.Controller
			}
		}
		d.Options = append(d.Options, o)
	}
	if ans, ok := AskTape(h, d); ok {
		// Answered in place: apply the redirect.
		changeTargetsApply(h, sa, target, ChoiceAnswerTargets(ans))
	}
}

// changeTargetsApply records an answered ChangeTargets redirect: the chosen
// new targets, padded with the subject's remaining old ones. An empty
// (Optional) answer keeps every target.
func changeTargetsApply(h Host, sa *cards.SA, target *state.Object, choice []state.Target) {
	if len(choice) == 0 {
		return
	}
	out := append([]state.Target(nil), choice...)
	if strings.EqualFold(sa.ParamStr(cards.PKChangeSingleTarget), "True") || len(out) < len(target.Targets) {
		out = append(out, target.Targets[len(out):]...)
	}
	recordTargets(h, target.ID, out)
}

func repeatPlayers(h Host, c *Ctx, spec string) ([]state.PlayerID, bool) {
	selected := map[state.PlayerID]bool{}
	add := func(ts []state.Target) {
		for _, t := range ts {
			p := PlayerOf(h, c, t)
			if int(p) >= 0 && int(p) < len(h.Game().Players) && !h.Game().Players[p].Lost {
				selected[p] = true
			}
		}
	}
	switch repeatPlayersCodes.Code(string(spec)) {
	case repeatPlayersPlayer:
		for _, p := range h.Game().AliveFrom(c.Controller) {
			selected[p] = true
		}
	case repeatPlayersOpponent:
		for _, p := range h.Game().AliveFrom(c.Controller) {
			selected[p] = p != c.Controller
		}
	case repeatPlayersYou:
		selected[c.Controller] = true
	case repeatPlayersTargeted:
		add(c.Targets)
	case repeatPlayersTargetedAndYou:
		add(c.Targets)
		selected[c.Controller] = true
	case repeatPlayersRemembered:
		add(c.Remembered)
	case repeatPlayersNonTargetedController:
		add(c.Targets)
		for _, p := range h.Game().AliveFrom(c.Controller) {
			selected[p] = !selected[p]
		}
	case repeatPlayersOppNonRememberedController:
		add(c.Remembered)
		for _, p := range h.Game().AliveFrom(c.Controller) {
			selected[p] = p != c.Controller && !selected[p]
		}
	case repeatPlayersOppNonTriggeredDefender:
		// Attacks triggers capture the player being attacked separately from
		// the player whose action/event caused the trigger. These carriers
		// copy the attacker for each OTHER opponent: omit the captured
		// defender, not TriggerPlayer/AttackingPlayer.
		if !c.DefendingPlayer.IsPlayer || int(c.DefendingPlayer.Player) >= len(h.Game().Players) {
			break
		}
		for _, p := range h.Game().AliveFrom(c.Controller) {
			selected[p] = p != c.Controller && p != c.DefendingPlayer.Player
		}
	case repeatPlayersChosenYou:
		add(c.Chosen)
		selected[c.Controller] = true
	default:
		// The shared player filter covers Player.Chosen and other qualifiers
		// for which the engine has state. Unknown qualifiers fail closed and
		// are reported rather than silently broadening the loop.
		for _, p := range h.Game().AliveFrom(c.Controller) {
			if MatchesPlayerSpecWithSVars(h, c, spec, p, c.Controller) {
				selected[p] = true
			}
		}
		if len(selected) == 0 {
			return nil, false
		}
	}
	var out []state.PlayerID
	for _, p := range h.Game().AliveFrom(c.Controller) {
		if selected[p] {
			out = append(out, p)
		}
	}
	return out, true
}

// iterationBase is what an iteration's Remembered holds besides its subject.
// Forge's RepeatEachEffect swaps only remembered PLAYERS out for a player
// loop, so the cards the resolution remembered stay visible to the body
// (Braids's "a permanent that shares a card type with it"). The event object
// a trigger captured is not part of that list in Forge and is left out here.
// A card or spell loop binds its subject alone.
func iterationBase(c *Ctx, subject state.Target) []state.Target {
	if !subject.IsPlayer {
		return nil
	}
	captured := copyTargets(c.Captured)
	var out []state.Target
	for _, t := range c.Remembered {
		if t.IsPlayer {
			continue
		}
		if i := indexTarget(captured, t); i >= 0 {
			captured = append(captured[:i], captured[i+1:]...)
			continue
		}
		out = append(out, t)
	}
	return out
}

func indexTarget(ts []state.Target, want state.Target) int {
	for i, t := range ts {
		if t == want {
			return i
		}
	}
	return -1
}

// rememberIteration folds what one RepeatEach iteration remembered back into
// the loop's own Remembered. Forge adds the subject to the host's remembered
// list for the iteration and removes only the subject afterwards, so
// anything the iteration remembered (RememberChosen$, RememberDiscarded$, ...)
// is still remembered by the sub-abilities after the loop. body is the
// iteration's final Remembered; base (the entries it started with besides
// the subject) and the subject are not additions. The result is a fresh
// slice: outer may share a backing array with a stack object.
func rememberIteration(outer, body, base []state.Target, subject state.Target) []state.Target {
	out := copyTargets(outer)
	start := append(copyTargets(base), subject)
	// Any entry the iteration STARTED with (base+subject) that the body no
	// longer holds was removed by the body (ForgetChosen$,
	// forgetRememberedOne): that removal must persist on the outer set, else
	// an outer Repeat's RepeatDefined$ Remembered gate never shrinks.
	for _, t := range start {
		if indexTarget(body, t) >= 0 {
			continue
		}
		if i := indexTarget(out, t); i >= 0 {
			out = append(out[:i], out[i+1:]...)
		}
	}
	for _, t := range body {
		if indexTarget(start, t) >= 0 {
			continue
		}
		if indexTarget(out, t) >= 0 {
			continue
		}
		out = append(out, t)
	}
	return out
}
func effBranch(h Host, c *Ctx, sa *cards.SA) {
	if c.SVars == nil {
		return
	}
	v := Num(h, c, &cards.SA{Params: map[string]string{"condition": sa.ParamStr(cards.PKBranchConditionSVar)}}, "condition", 0)
	// Forge's BranchEffect defaults an absent BranchConditionSVarCompare$ to
	// GE1 (31 of the corpus's Branch lines rely on it: "if X is at least
	// one"). An operator this build does not know takes the false arm.
	cmp := strings.TrimSpace(sa.ParamStr(cards.PKBranchConditionSVarCompare))
	if cmp == "" {
		cmp = "GE1"
	}
	op := ""
	if len(cmp) >= 2 {
		op, cmp = strings.ToUpper(cmp[:2]), cmp[2:]
	}
	n, err := strconv.Atoi(cmp)
	if err != nil {
		// Branch uses the same literal/SVar count vocabulary as its left side.
		// Inline PlayerCount...$Amount is Forge's spelling for a count head.
		raw := strings.TrimSuffix(cmp, "$Amount")
		if body, found := c.SVars[raw]; found {
			n = int(EvalCount(h, c, body))
		} else {
			n = int(EvalCount(h, c, "Count$"+raw))
		}
	}
	yes := compareCount(op, int(v), n)
	name := sa.ParamStr(cards.PKFalseSubAbility)
	if yes {
		name = sa.ParamStr(cards.PKTrueSubAbility)
	}
	if sub := cards.ResolveSVar(c.SVars, name); sub != nil {
		Resolve(h, c, sub)
	}
}

type choiceZonesCode uint16

const (
	choiceZonesBattlefield choiceZonesCode = iota + 1
	choiceZonesHand
	choiceZonesLibrary
	choiceZonesGraveyard
	choiceZonesExile
	choiceZonesStack
)

var choiceZonesCodes = state.NewStrCodes(
	state.StrEntry[choiceZonesCode]{Key: "Battlefield", Val: choiceZonesBattlefield},
	state.StrEntry[choiceZonesCode]{Key: "Hand", Val: choiceZonesHand},
	state.StrEntry[choiceZonesCode]{Key: "Library", Val: choiceZonesLibrary},
	state.StrEntry[choiceZonesCode]{Key: "Graveyard", Val: choiceZonesGraveyard},
	state.StrEntry[choiceZonesCode]{Key: "Exile", Val: choiceZonesExile},
	state.StrEntry[choiceZonesCode]{Key: "Stack", Val: choiceZonesStack},
)

type definedCardPoolCode uint16

const (
	definedCardPoolTargeted definedCardPoolCode = iota + 1
	definedCardPoolParentTargeted
	definedCardPoolRemembered
	definedCardPoolTriggeredCards
	definedCardPoolTriggeredSources
	definedCardPoolExiledWith
)

var definedCardPoolCodes = state.NewStrCodes(
	state.StrEntry[definedCardPoolCode]{Key: "Targeted", Val: definedCardPoolTargeted},
	state.StrEntry[definedCardPoolCode]{Key: "TargetedCard", Val: definedCardPoolTargeted},
	state.StrEntry[definedCardPoolCode]{Key: "ParentTargeted", Val: definedCardPoolParentTargeted},
	state.StrEntry[definedCardPoolCode]{Key: "Remembered", Val: definedCardPoolRemembered},
	state.StrEntry[definedCardPoolCode]{Key: "RememberedLKI", Val: definedCardPoolRemembered},
	state.StrEntry[definedCardPoolCode]{Key: "TriggeredCards", Val: definedCardPoolTriggeredCards},
	state.StrEntry[definedCardPoolCode]{Key: "TriggeredAttackers", Val: definedCardPoolTriggeredCards},
	state.StrEntry[definedCardPoolCode]{Key: "TriggeredBlockers", Val: definedCardPoolTriggeredCards},
	state.StrEntry[definedCardPoolCode]{Key: "TriggeredSources", Val: definedCardPoolTriggeredSources},
	state.StrEntry[definedCardPoolCode]{Key: "ExiledWith", Val: definedCardPoolExiledWith},
)

type controlledByChoicePlayerCode uint16

const (
	controlledByChoicePlayerEmpty controlledByChoicePlayerCode = iota + 1
	controlledByChoicePlayerChooser
	controlledByChoicePlayerYou
	controlledByChoicePlayerRemembered
	controlledByChoicePlayerLeft
	controlledByChoicePlayerRight
)

var controlledByChoicePlayerCodes = state.NewStrCodes(
	state.StrEntry[controlledByChoicePlayerCode]{Key: "", Val: controlledByChoicePlayerEmpty},
	state.StrEntry[controlledByChoicePlayerCode]{Key: "Chooser", Val: controlledByChoicePlayerChooser},
	state.StrEntry[controlledByChoicePlayerCode]{Key: "You", Val: controlledByChoicePlayerYou},
	state.StrEntry[controlledByChoicePlayerCode]{Key: "Remembered", Val: controlledByChoicePlayerRemembered},
	state.StrEntry[controlledByChoicePlayerCode]{Key: "Left", Val: controlledByChoicePlayerLeft},
	state.StrEntry[controlledByChoicePlayerCode]{Key: "Right", Val: controlledByChoicePlayerRight},
)

type controlPlayerSimpleCode uint16

const (
	controlPlayerSimpleEmpty controlPlayerSimpleCode = iota + 1
	controlPlayerSimpleYou
	controlPlayerSimpleChosenPlayer
	controlPlayerSimplePlayerIsRemembered
	controlPlayerSimpleImprintedController
	controlPlayerSimpleTriggeredSourceController
	controlPlayerSimpleTriggeredTarget
)

var controlPlayerSimpleCodes = state.NewStrCodes(
	state.StrEntry[controlPlayerSimpleCode]{Key: "", Val: controlPlayerSimpleEmpty},
	state.StrEntry[controlPlayerSimpleCode]{Key: "You", Val: controlPlayerSimpleYou},
	state.StrEntry[controlPlayerSimpleCode]{Key: "True", Val: controlPlayerSimpleYou},
	state.StrEntry[controlPlayerSimpleCode]{Key: "ChosenPlayer", Val: controlPlayerSimpleChosenPlayer},
	state.StrEntry[controlPlayerSimpleCode]{Key: "Player.Chosen", Val: controlPlayerSimpleChosenPlayer},
	state.StrEntry[controlPlayerSimpleCode]{Key: "Player.IsRemembered", Val: controlPlayerSimplePlayerIsRemembered},
	state.StrEntry[controlPlayerSimpleCode]{Key: "ImprintedController", Val: controlPlayerSimpleImprintedController},
	state.StrEntry[controlPlayerSimpleCode]{Key: "TriggeredSourceController", Val: controlPlayerSimpleTriggeredSourceController},
	state.StrEntry[controlPlayerSimpleCode]{Key: "TriggeredTarget", Val: controlPlayerSimpleTriggeredTarget},
)

type controlPlayerReferentCode uint16

const (
	controlPlayerReferentRemembered controlPlayerReferentCode = iota + 1
)

var controlPlayerReferentCodes = state.NewStrCodes(
	state.StrEntry[controlPlayerReferentCode]{Key: "Remembered", Val: controlPlayerReferentRemembered},
	state.StrEntry[controlPlayerReferentCode]{Key: "RememberedController", Val: controlPlayerReferentRemembered},
	state.StrEntry[controlPlayerReferentCode]{Key: "TriggeredPlayer", Val: controlPlayerReferentRemembered},
	state.StrEntry[controlPlayerReferentCode]{Key: "TriggeredActivator", Val: controlPlayerReferentRemembered},
	state.StrEntry[controlPlayerReferentCode]{Key: "TriggeredAttackingPlayer", Val: controlPlayerReferentRemembered},
	state.StrEntry[controlPlayerReferentCode]{Key: "TriggeredDefendingPlayer", Val: controlPlayerReferentRemembered},
	state.StrEntry[controlPlayerReferentCode]{Key: "TriggeredCardController", Val: controlPlayerReferentRemembered},
	state.StrEntry[controlPlayerReferentCode]{Key: "Opponent", Val: controlPlayerReferentRemembered},
	state.StrEntry[controlPlayerReferentCode]{Key: "Player.Opponent", Val: controlPlayerReferentRemembered},
	state.StrEntry[controlPlayerReferentCode]{Key: "Targeted", Val: controlPlayerReferentRemembered},
	state.StrEntry[controlPlayerReferentCode]{Key: "TargetedPlayer", Val: controlPlayerReferentRemembered},
	state.StrEntry[controlPlayerReferentCode]{Key: "TargetedController", Val: controlPlayerReferentRemembered},
	state.StrEntry[controlPlayerReferentCode]{Key: "ParentTarget", Val: controlPlayerReferentRemembered},
)

type effControlSpellCode uint16

const (
	effControlSpellGain effControlSpellCode = iota + 1
)

var effControlSpellCodes = state.NewStrCodes(
	state.StrEntry[effControlSpellCode]{Key: "", Val: effControlSpellGain},
	state.StrEntry[effControlSpellCode]{Key: "Gain", Val: effControlSpellGain},
)

type repeatPlayersCode uint16

const (
	repeatPlayersPlayer repeatPlayersCode = iota + 1
	repeatPlayersOpponent
	repeatPlayersYou
	repeatPlayersTargeted
	repeatPlayersTargetedAndYou
	repeatPlayersRemembered
	repeatPlayersNonTargetedController
	repeatPlayersOppNonRememberedController
	repeatPlayersOppNonTriggeredDefender
	repeatPlayersChosenYou
)

var repeatPlayersCodes = state.NewStrCodes(
	state.StrEntry[repeatPlayersCode]{Key: "Player", Val: repeatPlayersPlayer},
	state.StrEntry[repeatPlayersCode]{Key: "Opponent", Val: repeatPlayersOpponent},
	state.StrEntry[repeatPlayersCode]{Key: "Player.Opponent", Val: repeatPlayersOpponent},
	state.StrEntry[repeatPlayersCode]{Key: "You", Val: repeatPlayersYou},
	state.StrEntry[repeatPlayersCode]{Key: "NonOpponent", Val: repeatPlayersYou},
	state.StrEntry[repeatPlayersCode]{Key: "Targeted", Val: repeatPlayersTargeted},
	state.StrEntry[repeatPlayersCode]{Key: "TargetedPlayer", Val: repeatPlayersTargeted},
	state.StrEntry[repeatPlayersCode]{Key: "TargetedController", Val: repeatPlayersTargeted},
	state.StrEntry[repeatPlayersCode]{Key: "TargetedAndYou", Val: repeatPlayersTargetedAndYou},
	state.StrEntry[repeatPlayersCode]{Key: "Remembered", Val: repeatPlayersRemembered},
	state.StrEntry[repeatPlayersCode]{Key: "RememberedController", Val: repeatPlayersRemembered},
	state.StrEntry[repeatPlayersCode]{Key: "NonTargetedController", Val: repeatPlayersNonTargetedController},
	state.StrEntry[repeatPlayersCode]{Key: "OppNonRememberedController", Val: repeatPlayersOppNonRememberedController},
	state.StrEntry[repeatPlayersCode]{Key: "OppNonTriggeredDefender", Val: repeatPlayersOppNonTriggeredDefender},
	state.StrEntry[repeatPlayersCode]{Key: ".Chosen,You", Val: repeatPlayersChosenYou},
	state.StrEntry[repeatPlayersCode]{Key: "Chosen,You", Val: repeatPlayersChosenYou},
)
