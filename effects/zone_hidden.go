package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effHiddenPick is Forge's changeHiddenOriginResolve for a Hidden$ True
// ChangeZone whose origin zones are PUBLIC (Battlefield, Graveyard, Exile,
// Command, Stack). Sideboard is handled by the owner-scoped hidden search
// path before this dispatcher. With no Defined$ the fetch list is the origin zones'
// cards matching ChangeType$ -- game-wide for a public origin when no fetch
// player is named, the named fetch player's own zones otherwise -- and the
// chooser picks ChangeNum$ of them (Mandatory$ True makes the pick
// compulsory; otherwise Min 0, "you may"). Zones that hold hidden info are
// handled by their own walkers (Library's search, Hand's movers); this one
// never offers a hidden card by name, and a public-origin pick never
// shuffles (Forge's shuffle condition needs Library in the origin).
func effHiddenPick(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, to state.Zone, originZones []state.Zone, originAll bool, originValid bool, from string) {
	// fx42 scoping: capture and clear the answered pick (and the cursor that
	// binds it to the fetch player that asked) before anything else, so a
	// nested pick below cannot inherit them.
	ans := ([]state.ObjID)(nil)

	done := false
	cursor := int(0)

	// fx42 scoping for the Optional$ confirmation answer: consumed and cleared
	// before anything else so a nested pick poses its own confirmation (the
	// same discipline the hand walk's HandMoveConfirm answer follows).
	confirmDone := false
	confirmYes := strings.EqualFold(string(""),

		"yes")
	confirmTarget := int(0)

	hiddenPickOriginNote(h, c, originValid, from)
	players := hiddenPickPlayers(h, c, cz.fetch())
	if c.ForgetOtherReady {
		players = c.ForgetOtherOwners
	}
	initForgetOther(h, c, cz.Riders.ForgetOtherRemembered, players, 2)
	// Forge branches on the origin zones, not on the fetch player: game-wide
	// only when the origin holds no hidden-info zone and no fetch player was
	// named (Kor Skyfisher's ChangeType$ filter does the scoping).
	gameWide := !zoneIn(originZones, state.ZHand) && !zoneIn(originZones, state.ZLibrary) &&
		cz.DefinedPlayer.Text == ""
	spec := cz.ChangeType
	// Away from the battlefield, Forge's Permanent base means a permanent
	// card. Hidden graveyard/exile picks share the library search's rule;
	// without it Winter's remembered permanent is never eligible for DBReturn.
	if !zoneIn(originZones, state.ZBattlefield) {
		spec = permanentCardSpec(spec)
	}
	// The per-type groups an EACH ChangeType asks for, computed once: the
	// sub-specs are a property of the SA, not of the fetch player.
	eachSubs, isEach := eachAlternatives(spec)
	max := numText(h, c, cz.ChangeNum, 1)
	if max < 0 {
		max = 0
	}
	mandatory := cz.Mandatory
	// ChoiceOptional$ True explicitly names the Min-0 may-pick default here;
	// it does not override Mandatory$ True. False/unset leave the default unchanged.
	mayPick := cz.ChoiceOptional
	noLooking := cz.NoLooking
	withKind := cz.WithCountersType
	var withAmt int32
	if withKind != "" && counterDestination(to) {
		withAmt = withCounterAmount(h, c, cz)
	}
	// WithTotalCMC$ is the cumulative mana-value budget over the picked cards
	// (Lively Dirge's DBReturn, Technomancer, Legion's Chant, Pair o' Dice
	// Lost: "return up to N creature cards with total mana value M or less"),
	// the exact parameter effDig reads on its own window. A card whose own
	// mana value exceeds the budget can never be picked, and the running sum
	// of the picks must not exceed it either; the mechanics below mirror
	// effDig's (affordable filter, Decision.MaxSum + Option.Value on the wire,
	// a greedy stand-in take, a mandatory Min lowered to what the budget
	// affords). Absent the param the budget is 0, budgetEligible == eligible
	// and every read below is a no-op, so a non-budget pick emits
	// byte-identically. Present but unresolvable degrades to budget 0 --
	// Num's documented convention.
	budget, hasBudget := numResolvedText(h, c, cz.WithTotalCMC, 0)
	if budget < 0 {
		budget = 0
	}
	rider := classifyAttackingEntryText(c, cz.Riders.Attacking, to)
	// ChooseFromDefined$ narrows the offered pool to the objects a defined
	// selector names -- Cass, Hand of Vengeance's `ChooseFromDefined$ AttachedTo
	// TriggeredCardLKICopy.Aura` offers only the Aura cards that WERE attached
	// to the creature that died, not every Aura in the origin zone. The value
	// is a full Defined selector resolved through the ONE shared pool resolver
	// (chooseFromDefinedPool -- knownDefinedTargets, the same machinery every
	// other Defined position reads), so an unknown or unresolvable value fails
	// CLOSED (an empty pool, plus one Note) rather than silently offering the
	// whole zone. The supported spellings are the resolver's own: the dotted
	// `AttachedTo <referent>` form and every other referent it modelled
	// (TriggeredCards, TriggeredSources, Remembered/Remembered.<color>,
	// TopThirdOfLibrary, Targeted.<qualifier>, ExiledWith.<qualifier>); an
	// unresolvable spelling reaches the same fail-closed Note. It is declared
	// before the apply closure so the closure's answer recheck can bind it.
	chooseFromDefined := make(map[state.ObjID]bool)
	hasChooseFromDefined := false
	chooseFromDefinedResolved := false
	if raw := cz.ChooseFromDefined; raw != "" {
		hasChooseFromDefined = true
		if pool, ok := chooseFromDefinedPool(h, c, raw); ok {
			chooseFromDefinedResolved = true
			chooseFromDefined = pool
		}
	}
	if hasChooseFromDefined && !chooseFromDefinedResolved {
		hiddenPickChooseFromDefinedNote(h, c, cz)
	}
	apply := func(owner state.PlayerID, ids []state.ObjID) []state.ObjID {
		g := h.Game()
		// Revalidate all picks before the clear, including IsRemembered, and
		// re-bound the answer by the ChooseFromDefined$ pool: the options only
		// ever carried pool members, so a host that bypassed the wire cannot
		// move a card outside the resolved selector either (an unresolved
		// selector re-fails closed here the option build already noted).
		valid := make([]state.ObjID, 0, len(ids))
		for _, id := range ids {
			o := g.Obj(id)
			if o != nil && zoneIn(originZones, o.Zone) && MatchesSpecCtx(g, spec, id, forgetOtherSpecContext(c)) &&
				(!hasChooseFromDefined || (chooseFromDefinedResolved && chooseFromDefined[id])) {
				valid = append(valid, id)
			}
		}
		// The pick is entered: Forge clears the source's remembered cards
		// before the choose (ChangeZoneEffect.changeHiddenOriginResolve 1103),
		// so an answered pick that moves nothing still clears. The valid set
		// above was rechecked against the pre-clear snapshot, so a formerly
		// remembered card remains admitted here.
		forgetOther(h, c, cz.Riders.ForgetOtherRemembered)
		moved := make([]state.ObjID, 0, len(valid))
		for _, id := range valid {
			o := g.Obj(id)
			// Recheck at the point of movement: the answered card must still
			// sit in an origin zone and match the filter, or it stays.
			if o == nil || !zoneIn(originZones, o.Zone) {
				continue
			}
			settleChangeZoneMoveAs(h, c, sa, cz, id, o.Zone, to, withKind, withAmt, o.Owner, true, &rider)
			// AttachedTo$ on a hidden public-origin pick (Cass, Hand of
			// Vengeance's returned `AttachedTo$ Targeted` Aura; Bruna,
			// Stormkeld Curator, Sovereigns of Lost Alara): the same rider every
			// other mover applies. Without it a returned Aura enters unattached
			// and the CR 704.5m SBA sweeps it before the chained SubAbility
			// runs -- silent for all 8 corpus Hidden$+AttachedTo$ lines, and
			// the reason Cass's returned Auras would not sit on the target.
			if to == state.ZBattlefield {
				changeZoneAttachedTo(h, c, sa, cz, id)
			}
			moved = append(moved, id)
			if cz.RememberChanged {
				// settleChangeZoneMoveAs recorded the resolution-local half;
				// persist the same moved object for later resolutions.
				eventRemember(h, c, id)
			}
			eventForgetChanged(h, c, sa, id)
			if to == state.ZBattlefield && cz.Tapped {
				h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: o.Owner, Text: "entered tapped"})
			}
		}
		// Explicit Reveal$ on the pick (Karn, the Great Creator's [-2]): the
		// same public Note payload the library search's reveal emits. No
		// default auto-reveal here: the pick's origin zones are public, so
		// every offered name was already known.
		if cz.Reveal && len(moved) > 0 {
			h.Emit(events.Event{Kind: events.Note, Player: owner, IDs: moved})
		}
		// AtEOT$ rides the pick's moved set as well (latent: no corpus carrier
		// reaches the hidden pick with the param today, but a future one must
		// not be dropped silently).
		scheduleAtEOT(h, c, sa, moved)
		return moved
	}
	// "Any number" (Cass's OptionalPrompt$ text) is the absent-ChangeNum$
	// reading when ChooseFromDefined$ is present: the pool itself bounds the
	// pick. A caller that names ChangeNum$ keeps it.
	chooseFromAll := hasChooseFromDefined && cz.ChangeNum.Text == ""
	// applyAnswered applies one fetch player's answered pick: the answer
	// re-entry's branch, and the resolution kernel's served answer alike.
	applyAnswered := func(owner state.PlayerID, ids []state.ObjID) {
		if raw, ok := totalCardTypesRequirement(cz); ok {
			need, err := strconv.Atoi(raw)
			if err != nil || need < 0 || !totalCardTypesSatisfied(h.Game(), ids, need) {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: owner,
					Text: "hidden pick fails WithTotalCardTypes$ requirement"})
				return
			}
		}
		apply(owner, ids)
	}
	for i, owner := range players {
		var eligible []state.ObjID
		addPool := func(ids []state.ObjID) {
			for _, id := range ids {
				if hasChooseFromDefined && !chooseFromDefined[id] {
					continue
				}
				o := h.Game().Obj(id)
				if o == nil || !MatchesSpecCtx(h.Game(), spec, id, forgetOtherSpecContext(c)) {
					continue
				}
				eligible = append(eligible, id)
			}
		}
		if gameWide {
			// Forge's game.getCardsIn(origin): every player's cards in the
			// origin zones -- the Game.Zone accessor is per (zone, player), so
			// the game-wide pool is the union over players in seat order,
			// deterministic.
			for _, z := range originZones {
				for p := range h.Game().Players {
					addPool(h.Game().Zone(z, state.PlayerID(p)))
				}
			}
		} else {
			for _, z := range originZones {
				addPool(h.Game().Zone(z, owner))
			}
		}
		// budgetEligible is the pickable set: spec-matching AND individually
		// affordable under WithTotalCMC$ (no budget => identical to eligible).
		budgetEligible := eligible
		if hasBudget {
			budgetEligible = make([]state.ObjID, 0, len(eligible))
			for _, id := range eligible {
				if manaValueOf(h.Game(), id) <= int(budget) {
					budgetEligible = append(budgetEligible, id)
				}
			}
		}
		m := max
		if chooseFromAll {
			// "Any number" from the ChooseFromDefined$ pool: every eligible
			// card may be taken (Min stays 0 unless Mandatory$).
			m = int32(len(budgetEligible))
		}
		if m > int32(len(budgetEligible)) {
			m = int32(len(budgetEligible))
		}
		// greedy is the deterministic stand-in take under the cumulative
		// budget: walk budgetEligible in pool order and take each card only
		// while the running sum still fits, bounded by the pick count m. With
		// no budget every card fits and greedy is exactly the first m cards of
		// eligible -- the take the pre-budget stand-in applied -- so the R-9
		// fallback stays byte-identical there.
		greedy := make([]state.ObjID, 0, len(budgetEligible))
		running := 0
		for _, id := range budgetEligible {
			if int32(len(greedy)) >= m {
				break
			}
			mv := manaValueOf(h.Game(), id)
			if hasBudget && running+mv > int(budget) {
				continue
			}
			running += mv
			greedy = append(greedy, id)
		}
		if done && i < cursor {
			// This fetch player answered on an earlier pass, before a later
			// one suspended the walk (the hand walk's continuation contract).
			continue
		}
		if done && i == cursor {
			applyAnswered(owner, ans)
			continue
		}
		chooser := hiddenPickChooser(h, c, cz, owner)
		// Forge's Optional$ confirmation (the same confirmAction gate the
		// hidden-hand walk poses, which runs BEFORE the card pick): a script
		// that carries the marker asks the decider whether to proceed, even
		// when this player's eligible pool turns out empty -- a decline skips
		// this player with the remembered set intact, and only an accepted
		// confirmation reaches the pick or the empty-pool continuation below
		// (which clears). The markerless may-shapes stay confirmation-free:
		// ChoiceOptional$ names the pick's cardinality, not a yes/no gate. An
		// UNRESOLVED ChooseFromDefined$ failed closed above, so its
		// nothing-to-offer continuation never becomes a confirmation.
		if hiddenPickConfirms(cz) && !(hasChooseFromDefined && !chooseFromDefinedResolved) {
			if confirmDone && i < confirmTarget {
				// This fetch player's confirmation was already answered on an
				// earlier pass (declined, or accepted with its pick completed);
				// do not ask again and do not clear for it.
				continue
			}
			if !confirmDone {
				prompt := cz.OptionalPrompt
				if prompt == "" {
					prompt = "Proceed with moving a card?"
				}
				cd := &decision.Decision{Player: chooser, Kind: decision.KChoose,
					Min: 1, Max: 1, Source: c.Source,
					ResumeKind: "hidden_pick_confirm", ResumeSA: sa, ResumeTarget: i,
					ResumeRemembered:          copyTargets(c.Remembered),
					ResumeForgetOtherSnapshot: copyTargets(c.ForgetOtherSnapshot),
					ResumeForgetOtherOwners:   append([]state.PlayerID(nil), c.ForgetOtherOwners...),
					ResumeForgetOtherReady:    c.ForgetOtherReady,
					ResumeForgetOtherCleared:  c.ForgetOtherCleared,
					Prompt:                    prompt,
					Options: []decision.Option{
						{Index: 0, Kind: "yes", Label: "Yes", Player: chooser},
						{Index: 1, Kind: "no", Label: "No", Player: chooser},
					}}
				if ans, ok := AskTape(h, cd); ok {
					// The resolution kernel's answer in hand: the
					// "hidden_pick_confirm" re-entry's own events, then a
					// decline skips this player and an acceptance enters the
					// pick.
					hiddenPickReentryEcho(h, c, cz, to, originValid, from)
					if !tapeAnswerYes(ans) {
						continue
					}
				} else {
				}

				// R-9: no host to ask -- play "may" as "do" deterministically,
				// the same fallback the hand walk's confirmation applies.
			} else if i == confirmTarget && !confirmYes {
				confirmDone = false // this player's decline is consumed; later players still confirm
				continue            // declined: keep the remembered set
			} else if i == confirmTarget {
				confirmDone = false // this player's acceptance is consumed
			}
		}
		if len(budgetEligible) == 0 || m == 0 {
			// No eligible card, or an empty-only ChangeNum$ 0 pick: with no
			// Optional$ marker both completed silently before optionality could
			// matter; an ACCEPTED Optional$ confirmation reaches this branch
			// entered (the same AskEmpty contract the hand walk keeps for its
			// ordinary empty move). A resolved selector with an empty pool still
			// entered the fetch, so it clears the remembered set; an UNRESOLVED
			// ChooseFromDefined$ failed closed above and must not be mistaken
			// for a fetch that happened. A public origin has no shuffle to fail
			// to perform.
			if !(hasChooseFromDefined && !chooseFromDefinedResolved) {
				forgetOther(h, c, cz.Riders.ForgetOtherRemembered)
			}
			continue
		}
		// AtRandom$ True (Make a Wish, Ghoulraiser, Arcane Bombardment): the
		// ENGINE picks m eligible cards from its seeded rng -- Forge's
		// changeHiddenOriginResolve takes Aggregates.random per pick -- so no
		// player is asked and the pick replays. The EACH and WithTotalCMC$
		// shapes keep the ask (measured: no AtRandom$ carrier uses either).
		if cz.AtRandom && !isEach && !hasBudget {
			apply(owner, randomObjIDs(h, budgetEligible, int(m)))
			continue
		}
		prompt := cz.SelectPrompt
		// OptionalPrompt$ is the script's own wording for the optional pick
		// (Cass's "Select any number of Aura cards that were attached to
		// it"); it wins the default text, the same precedence the
		// library-search path gives it.
		if op := cz.OptionalPrompt; op != "" {
			prompt = op
		}
		if prompt == "" {
			prompt = "Choose up to " + strconv.Itoa(int(m)) + " card(s)"
		}
		d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
			Min: 0, Max: int(m), MaxSum: int(budget), Source: c.Source,
			// The same remembered ride the hand_move ask carries: the
			// re-entered effHiddenPick revalidates against ChangeType$, which
			// can be a ctx-Remembered predicate.
			ResumeKind: "hidden_pick", ResumeSA: sa, ResumeTarget: i,
			ResumeRemembered:          copyTargets(c.Remembered),
			ResumeForgetOtherSnapshot: copyTargets(c.ForgetOtherSnapshot),
			ResumeForgetOtherOwners:   append([]state.PlayerID(nil), c.ForgetOtherOwners...),
			ResumeForgetOtherReady:    c.ForgetOtherReady,
			ResumeForgetOtherCleared:  c.ForgetOtherCleared,
			Prompt:                    prompt}
		if mandatory {
			d.Min = int(m)
		} else if mayPick {
			// Explicit may-pick: preserve the same Min-0 default as an absent key.
			d.Min = 0
		}
		// A mandatory budget pick whose m exceeds what the budget affords must
		// not demand more picks than it can pay for: lower the Min to the
		// forced greedy count so the ask can be satisfied (effDig's rule).
		if hasBudget && d.Min > len(greedy) {
			d.Min = len(greedy)
		}
		var eachGroups [][]state.ObjID
		var eachPerType int32
		if isEach {
			// The per-type pick structure (each1's object-path fix): an EACH
			// public-origin pick is one pick of EACH listed type, never a flat
			// count over the union. The candidates join the FIRST sub-spec that
			// matches them (EachTypeGroups), so a card matching two listed
			// qualities is offered once, in one Group, and picking it cannot
			// block the other type's pick; per-type ChangeNum$ rides
			// Decision.GroupLimit when it is above 1. The budget is NOT enforced
			// on the structured branch (its options carry no Value; a MaxSum the
			// wire advertises would be a cap Validate sums to 0 over): clear it,
			// as the hidden-library search's structured branch does -- 0 corpus
			// carriers combine the two.
			eachPerType = m
			if eachPerType > 1 {
				d.GroupLimit = int(eachPerType)
			}
			eachGroups = EachTypeGroups(h.Game(), eachSubs, eligible, c.SpecContext(c.Controller))
			d.Min = 0
			d.Max = eachStructuredOptions(h.Game(), d, eachGroups, eachPerType, noLooking, owner, "hidden_pick")
			if mandatory {
				d.Min = d.Max
			}
			d.MaxSum = 0
		} else {
			for _, id := range budgetEligible {
				name := "a card"
				if o := h.Game().Obj(id); o != nil && o.Face() != nil && !noLooking {
					name = o.Face().Name
				}
				opt := decision.Option{Index: len(d.Options),
					Kind: "hidden_pick", Label: name, Obj: id, Player: owner}
				// Only a budget pick carries a Value: Option.Value is omitempty, so
				// a non-budget pick's option list serialises byte-identically.
				if hasBudget {
					opt.Value = manaValueOf(h.Game(), id)
				}
				d.Options = append(d.Options, opt)
			}
		}
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: the "hidden_pick"
			// re-entry's own events, then its answered branch.
			hiddenPickReentryEcho(h, c, cz, to, originValid, from)
			applyAnswered(owner, tapeAnswerObjs(ans))
			continue
		}
		unposable := askUnposable(d)
		// R-9: a host without a decision channel cannot ask, so it takes the
		// forced greedy take over the budget-eligible pool -- under a budget
		// the cumulative cap decides which cards fit (effDig's exact mirror);
		// without one greedy is the first m eligible cards, and the
		// DifferentNames$ variant below keeps its distinct-named-first walk
		// (no corpus card carries DifferentNames$ beside WithTotalCMC$, so the
		// two stand-ins never compete).
		var picked []state.ObjID
		if isEach {
			// The structured stand-in: each group's first perType candidates, in
			// group order -- the exact take the bot's group-aware fill
			// re-derives. R-9 plays "you may" as "do", deterministically, per
			// type, exactly as the flat stand-in plays it over the union.
			for _, ids := range eachGroups {
				n := eachPerType
				if int32(len(ids)) < n {
					n = int32(len(ids))
				}
				picked = append(picked, ids[:n]...)
			}
		} else if cz.DifferentNames && !hasBudget {
			seen := make(map[string]bool, m)
			for _, id := range eligible {
				if len(picked) >= int(m) {
					break
				}
				var cardName string
				if o := h.Game().Obj(id); o != nil && o.Face() != nil {
					cardName = o.Face().Name
				}
				if seen[cardName] {
					continue
				}
				seen[cardName] = true
				picked = append(picked, id)
			}
		} else {
			picked = append(picked, greedy...)
		}
		if !unposable {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: chooser,
				Text: "picks " + strconv.Itoa(len(picked)) + " card(s) (no engine host to ask)"})
		}
		apply(owner, picked)
	}
	endForgetOtherSnapshot(c)
}
