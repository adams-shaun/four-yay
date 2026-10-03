package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func effSearchLibrary(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, to state.Zone, zones []state.Zone) {
	players := searchPlayersFor(h, c, cz.fetch())
	if c.ForgetOtherReady {
		players = c.ForgetOtherOwners
	}
	if len(players) == 0 {
		return
	}
	initForgetOther(h, c, cz.Riders.ForgetOtherRemembered, players, 2)
	searchTarget := c.LibraryTarget
	searchDone := c.SearchDone
	chosen := append([]state.ObjID(nil), c.Search...)
	shuffleAnswer := c.SearchShuffle
	shufflePending := shuffleAnswer != ""
	shuffleTarget := c.LibraryTarget
	shuffleMoved := append([]state.ObjID(nil), c.SearchShuffleMoved...)
	c.Search, c.SearchDone = nil, false
	c.SearchShuffle, c.SearchShuffleMoved = "", nil
	// fx42 scoping for the Optional$ confirmation answer: consumed and
	// cleared before anything else so a nested search poses its own
	// confirmation.
	searchConfirmDone := c.SearchConfirmDone
	searchConfirmYes := strings.EqualFold(c.SearchConfirm, "yes")
	searchConfirmTarget := c.SearchConfirmTarget
	c.SearchConfirm, c.SearchConfirmDone, c.SearchConfirmTarget = "", false, 0
	start := 0
	if searchDone || shufflePending {
		start = searchTarget
	}
	if searchConfirmDone {
		start = searchConfirmTarget
	}
	g := h.Game()
	// ChooseFromDefined$ narrows the offered pool to the objects a defined
	// selector names -- the search path's twin of effHiddenPick's block, so a
	// selector-constrained search (Sanar, Innovative First-Year's
	// `Remembered.White`, The Celestial Toymaker's and Phyrexian Portal's
	// bare `Remembered`, Assemble the Team's `TopThirdOfLibrary`) offers the
	// resolved pool and never the whole library. Unresolved fails CLOSED:
	// one Note, an empty pool, no options -- never a full-library search.
	var cfdPool map[state.ObjID]bool
	cfdActive := false
	cfdResolved := true
	if raw := cz.ChooseFromDefined; raw != "" {
		cfdActive = true
		var ok bool
		if cfdPool, ok = chooseFromDefinedPool(h, c, raw); !ok {
			cfdResolved = false
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "ChangeZone ChooseFromDefined$ " + raw + " is not resolvable; nothing is offered"})
		}
	}
	for targetIndex, owner := range players {
		if targetIndex < start {
			continue
		}
		c.LibraryTarget = targetIndex
		// Forge's explicit Optional$ confirmation (ChangeZoneEffect's
		// confirmAction gate, which runs BEFORE the fetch list is consulted):
		// a hidden-origin search whose script carries the marker asks this
		// search player whether to proceed. The marker is read through the
		// ONE shared optionalConfirmMarker the hand and hidden-pick walks use
		// -- never a second parser. A decline skips this player's search, card
		// pick and search-specific shuffle/tail with the remembered set
		// intact; an accepted confirmation enters the fetch, whose Min-0 or
		// mandatory pick (and its shuffle) runs unchanged even when the
		// eligible pool is empty. The object-valued Defined$ fetch list keeps
		// its own election in moveDefinedLibraryObjects and never reaches this
		// walk, so it cannot double-confirm. ChoiceOptional$ is the pick's own
		// cardinality marker, never a yes/no gate, and a markerless text-may
		// search stays confirmation-free.
		if optionalConfirmMarker(cz) {
			// A search or may-shuffle ANSWER resume must not re-ask: this
			// player's confirmation was already consumed on the pass that
			// entered the fetch.
			resumingAnswer := (searchDone && targetIndex == searchTarget) ||
				(shufflePending && targetIndex == shuffleTarget)
			if searchConfirmDone && targetIndex < searchConfirmTarget {
				// Answered on an earlier pass; skip without re-asking.
				continue
			}
			if !searchConfirmDone && !resumingAnswer {
				chooser := searchChooser(h, c, cz)
				prompt := cz.OptionalPrompt
				if prompt == "" {
					prompt = "Proceed with searching a library?"
				}
				cd := &decision.Decision{Player: chooser, Kind: decision.KChoose,
					Min: 1, Max: 1, Source: c.Source,
					ResumeKind: "search_confirm", ResumeSA: sa, ResumeTarget: targetIndex,
					ResumeRemembered:          copyTargets(c.Remembered),
					ResumeSearchKnown:         copyTargets(c.SearchKnown),
					ResumeForgetOtherSnapshot: copyTargets(c.ForgetOtherSnapshot),
					ResumeForgetOtherOwners:   append([]state.PlayerID(nil), c.ForgetOtherOwners...),
					ResumeForgetOtherReady:    c.ForgetOtherReady,
					ResumeForgetOtherCleared:  c.ForgetOtherCleared,
					Prompt:                    prompt,
					Options: []decision.Option{
						{Index: 0, Kind: "yes", Label: "Yes", Player: chooser},
						{Index: 1, Kind: "no", Label: "No", Player: chooser},
					}}
				if Ask(h, cd) == AskAsked {
					return
				}
				// R-9: no host to ask -- play "may" as "do" deterministically,
				// then let the search path apply its own no-host pick policy.
			} else if searchConfirmDone && targetIndex == searchConfirmTarget && !searchConfirmYes {
				searchConfirmDone = false // this player's decline is consumed; later players still confirm
				continue                  // declined: keep the remembered set, skip the search/tail
			} else if searchConfirmDone && targetIndex == searchConfirmTarget {
				searchConfirmDone = false // this player's acceptance is consumed
			}
		}
		lib := zoneOf(g, state.ZLibrary, owner)
		if !zoneIn(zones, state.ZLibrary) {
			lib = nil
		}
		lookWindow := searchLibraryWindow(h, c, cz, lib)
		if len(lookWindow) < len(lib) && !cz.NoLooking {
			if cz.Reveal {
				h.Emit(events.Event{Kind: events.Note, Player: owner, IDs: append([]state.ObjID(nil), lookWindow...)})
			} else {
				emitLook(h, []state.PlayerID{searchChooser(h, c, cz)}, state.ZLibrary, lookWindow,
					"looks at the top of the library")
			}
		}
		if shufflePending && targetIndex == shuffleTarget {
			c.SearchShuffle = shuffleAnswer
			// Restore the answered tail just long enough for the shared helper
			// to consume it. The answer's owner is this target, not players[0].
			c.SearchShuffleMoved = shuffleMoved
			if searchShuffleTail(h, c, sa, cz, owner, nil, to) {
				return
			}
			shufflePending = false
			continue
		}
		if searchDone && targetIndex == searchTarget {
			c.LibraryTarget = targetIndex
			if applyLibrarySearch(h, c, sa, cz, owner, to, chosen, zones) {
				return
			}
			searchDone = false
			continue
		}

		rawSpec := cz.ChangeType
		// A hidden-library Permanent is a permanent card, not a battlefield
		// permanent. Keep the raw Forge spelling for the CR 701.23 quality
		// classification below; only candidate matching uses the contextual base.
		spec := permanentCardSpec(rawSpec)
		// Candidate order: the library first (in library order), then each public
		// origin zone in the order given by the parsed origin set. Dedupe across
		// zones so a card can never be offered twice. `eligible` is the ordered
		// list both the decision options and the R-9 stand-in read, so its order
		// is load-bearing for determinism.
		eligible := make([]state.ObjID, 0, len(lib))
		seen := make(map[state.ObjID]bool, len(lib))
		for _, id := range lookWindow {
			if MatchesSpecCtx(g, spec, id, forgetOtherSpecContext(c)) {
				eligible = append(eligible, id)
				seen[id] = true
			}
		}
		for _, z := range zones {
			if z == state.ZLibrary {
				continue
			}
			for _, id := range zoneOf(g, z, owner) {
				if seen[id] {
					continue
				}
				if MatchesSpecCtx(g, spec, id, forgetOtherSpecContext(c)) {
					eligible = append(eligible, id)
					seen[id] = true
				}
			}
		}
		// The ChooseFromDefined$ pool bounds the offered options in library
		// order (a deterministic intersection): a non-member of the resolved
		// set is never an option, and an unresolved selector leaves the pool
		// empty -- the fail-to-find tail below completes the search with
		// nothing moved, exactly like a filter that admits no card.
		if cfdActive {
			narrowed := make([]state.ObjID, 0, len(eligible))
			if cfdResolved {
				for _, id := range eligible {
					if cfdPool[id] {
						narrowed = append(narrowed, id)
					}
				}
			}
			eligible = narrowed
		}
		// WithTotalCMC$ is the cumulative mana-value budget over the found cards
		// (Protean Hulk: "any number of creature cards with total mana value 6 or
		// less"), the exact parameter effDig reads on its own window. A card
		// whose own mana value exceeds the budget can never be found, and the
		// running sum of the picks must not exceed it either; the mechanics
		// mirror effDig's (affordable filter, Decision.MaxSum + Option.Value on
		// the wire, a greedy stand-in). Absent the param the budget is 0,
		// budgetEligible == eligible and every read below is a no-op, so a
		// non-budget search emits byte-identically. The CR 701.23b/701.23d Min
		// semantics below are unchanged; only the affordable pool they are read
		// over is narrowed.
		budget, hasBudget := numResolvedText(h, c, cz.WithTotalCMC, 0)
		if budget < 0 {
			budget = 0
		}
		budgetEligible := eligible
		if hasBudget {
			budgetEligible = make([]state.ObjID, 0, len(eligible))
			for _, id := range eligible {
				if manaValueOf(g, id) <= int(budget) {
					budgetEligible = append(budgetEligible, id)
				}
			}
		}
		max := numText(h, c, cz.ChangeNum, 1)
		if max < 0 {
			max = 0
		}
		requested := max
		if max > int32(len(budgetEligible)) {
			max = int32(len(budgetEligible))
		}
		// DifferentNames$ True: one card per name can ever be found, so max is
		// at most the number of distinct names in the pool. Without the clamp a
		// quantity-only search (Extrapolate the Impossible's Card.YouOwn) forces
		// Min = Max over a pool whose every full answer repeats a name -- a
		// decision Validate's Group exclusivity refuses for EVERY answer, and the
		// match wedges. Exactly$ True ("exactly two cards ... with different
		// names": Extrapolate the Impossible, Burning-Rune Demon, Turtles
		// Forever) is all-or-nothing: when fewer than the requested number of
		// eligible cards (or of distinct names) exist, the player cannot find
		// exactly that many, so none is found (the fail-to-find tail below);
		// when they do exist, the ask below admits exactly that many or none
		// (Decision.AllowNone).
		if cz.DifferentNames {
			names := make(map[string]bool, len(budgetEligible))
			for _, id := range budgetEligible {
				if o := g.Obj(id); o != nil && o.Face() != nil {
					names[o.Face().Name] = true
				}
			}
			if distinct := int32(len(names)); max > distinct {
				max = distinct
			}
		}
		if cz.Exactly && max < requested {
			max = 0
		}
		// CR 701.23b/701.23d decide the minimum: a search whose card filter states
		// only a quantity must find that many (or as many as the zone holds), so
		// Min is forced up to Max; a stated-quality search keeps the fail-to-find
		// allowance of Min 0. `max` is already clamped to the number of eligible
		// cards, so a quantity-only search never asks for more than the library
		// holds (701.23d's "as many as possible"). This is a property of the
		// filter, not of Forge's Mandatory$ parameter.
		min := int32(0)
		if !SearchStatesQuality(rawSpec) {
			min = max
		}
		// An empty choice is not a choice: asking it suspends a real engine host
		// until it submits an empty answer, even though no answer can differ.
		// Complete the fail-to-find directly (including its required shuffle).
		if min == 0 && max == 0 {
			// A submitted search answer resumes in a fresh Ctx, so remembered
			// objects do not leak into its SubAbility chain. Preserve that existing
			// continuation contract while omitting the otherwise meaningless ask.
			c.Remembered = nil
			c.LibraryTarget = targetIndex
			if applyLibrarySearch(h, c, sa, cz, owner, to, nil, zones) {
				return
			}
			continue
		}
		// greedy is the deterministic stand-in take under the cumulative budget
		// (bound by the ChangeNum cap): with no budget every card fits and greedy
		// is exactly the first max cards of eligible -- the take the pre-budget
		// stand-in applied -- so the R-9 fallback stays byte-identical there. It
		// is computed before the Min below is finalised, because the budget can
		// strand a quantity-only search's forced Min.
		greedy := make([]state.ObjID, 0, len(budgetEligible))
		running := 0
		for _, id := range budgetEligible {
			if int32(len(greedy)) >= max {
				break
			}
			mv := manaValueOf(g, id)
			if hasBudget && running+mv > int(budget) {
				continue
			}
			running += mv
			greedy = append(greedy, id)
		}
		// A budget can strand a quantity-only search's forced Min: max was
		// clamped to len(budgetEligible), but the running sum may fit fewer than
		// that (library [3MV, 4MV], ChangeNum 2, WithTotalCMC 6 -- the greedy
		// take is one card), so Min == Max == 2 would pose an ask Decision
		// .Validate rejects for EVERY 2-pick -- a real host could never submit
		// and the match stalls. Lower the Min to the greedy count -- effDig's
		// mandatory-budget rule (cardflow.go), which its sibling effHiddenPick
		// applies too -- so a satisfying answer always exists. (Measured 0
		// corpus carriers combine a quantity-only filter with WithTotalCMC$;
		// this is general-correctness code in the direction of no wedge.)
		if hasBudget && min > int32(len(greedy)) {
			min = int32(len(greedy))
		}
		// AtRandom$ True (Geist of Regret, Tezzeret's Reckoning, Junkyard
		// Scrapper): the ENGINE picks max eligible cards from its seeded rng
		// -- Forge's changeHiddenOriginResolve takes Aggregates.random per
		// pick, with no fail-to-find choice -- so no player is asked and the
		// pick replays. The EACH, WithTotalCMC$ and DifferentNames$ shapes
		// keep the ask (measured: no AtRandom$ carrier uses them).
		if cz.AtRandom && !hasBudget && !cz.DifferentNames {
			if _, each := eachAlternatives(spec); !each {
				c.LibraryTarget = targetIndex
				if applyLibrarySearch(h, c, sa, cz, owner, to, randomObjIDs(h, budgetEligible, int(max)), zones) {
					return
				}
				continue
			}
		}
		// The prompt is built AFTER the Mandatory$ clamp below (and after the
		// EACH branch's own bounds), so the count it states can never disagree
		// with the decision's final Min/Max, and SelectPrompt$ replaces the
		// generic text exactly as effHiddenPick already does. Building it here
		// -- before the clamp -- is what made a mandatory leg advertise "up to
		// 1 card(s)" over a Min == Max == 1 decision.
		chooser := searchChooser(h, c, cz)
		// NoLooking$ True (Forge's line-1020 gate: with NoLooking the searching
		// player never LOOKS at the library -- no delayedReveal -- so the choose
		// is made over card backs): the options must not carry card names. The
		// IsRemembered legs of the Cultivate family and the seek-style shapes
		// route here; without this read the option labels leaked the library's
		// order one look at a time.
		noLooking := cz.NoLooking
		// DifferentNames$ True (Realms Uncharted): the picked cards must have
		// distinct names. One option per card name carries that name in Group, so
		// Decision.Validate's mutual-exclusion rule refuses any answer naming the
		// same card twice -- the wire enforces what Forge's one-at-a-time loop
		// (the DifferentNames fetchList filter) enforces there. The apply side
		// dedupes a host that bypassed the wire (applyLibrarySearch).
		differentNames := cz.DifferentNames
		// Forge's EACH multi-type search grammar ("EACH Forest & Plains"): the
		// pick is per-type, never a flat count over the union. One option per
		// eligible card, the sub-spec's ordinal in Option.Group, per-type
		// ChangeNum$ as Decision.GroupLimit when it is above 1 (each listed
		// type contributes up to that many), and the candidates partitioned by
		// EachTypeGroups so a card matching two listed qualities is offered
		// once and picking it can never block the other group's pick. Min: a
		// spec that states a quality -- every corpus carrier -- keeps
		// CR 701.23b's fail-to-find allowance (a listed type with no eligible
		// card simply contributes no options and no Group, and its pick is the
		// one the player cannot make); a quantity-only EACH (measured-absent)
		// keeps CR 701.23d's mandatory-find reading per type, forced to each
		// group's achievable count. The Group exclusivity contract
		// (decision.Decision.Validate) plus GroupLimit enforce the per-type cap
		// on the wire; the ordinary "search" resume arm carries the ordered
		// picks, and applyLibrarySearch re-checks each against the union
		// matcher, so no new Ctx field and no resume change. The budget is NOT
		// enforced on the structured branch (its options carry no Value, so a
		// MaxSum the wire advertises would be a cap Validate sums to 0 over --
		// meaningless, and misleading to a consumer). Clear it: 0 corpus
		// carriers combine EACH with WithTotalCMC$, and a future one needs
		// per-type budget mechanics designed, not a silent half-read.
		eachSubs, isEach := eachAlternatives(spec)
		eachStructured := isEach
		var eachGroups [][]state.ObjID
		var eachPerType int32
		d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
			Min: int(min), Max: int(max), MaxSum: int(budget), Source: c.Source,
			ResumeKind: "search", ResumeSA: sa, ResumeTarget: targetIndex,
			// The walk's Remembered rides the ask (rules restores it on the
			// resume) so the re-entered eligibility recheck and the SubAbility$
			// after this one still see the cards RememberChanged$ captured -- a
			// cast spell's mid-resolution Remembered lives only in the resolving
			// Ctx frame, and without the ride the answer's recheck (and Nissa's
			// Pilgrimage's "one onto the battlefield" leg) would re-resolve
			// IsRemembered against an empty set and move nothing. A nested hidden
			// search resumes in the same resolution too, so the fetch list built
			// by a preceding search stays available to Card.IsRemembered and
			// Defined$ Remembered in the rest of this chain.
			ResumeRemembered: copyTargets(c.Remembered),
			// The known-card set rides the ask too: this leg's answer rebuilds a
			// fresh Ctx, and the NEXT leg (or a chained sub that asks again) must
			// still label its options with the names the chooser already learned.
			ResumeSearchKnown:         copyTargets(c.SearchKnown),
			ResumeForgetOtherSnapshot: copyTargets(c.ForgetOtherSnapshot),
			ResumeForgetOtherOwners:   append([]state.PlayerID(nil), c.ForgetOtherOwners...),
			ResumeForgetOtherReady:    c.ForgetOtherReady,
			ResumeForgetOtherCleared:  c.ForgetOtherCleared}
		if eachStructured {
			eachPerType = max
			if eachPerType > 1 {
				d.GroupLimit = int(eachPerType)
			}
			eachGroups = EachTypeGroups(g, eachSubs, eligible, c.SpecContext(c.Controller))
			d.Min = 0
			d.Max = eachStructuredOptions(g, d, eachGroups, eachPerType, noLooking, owner, "search")
			if !SearchStatesQuality(rawSpec) {
				d.Min = d.Max
			}
			// The structured prompt is a fallback; SelectPrompt$ below overrides
			// it uniformly for both shapes.
			d.Prompt = "Search a library: choose one card of each listed type"
			// The budget is NOT enforced on the structured branch: its options
			// carry no Value, so a MaxSum the wire advertises would be a cap
			// Validate sums to 0 over -- meaningless, and misleading to a
			// consumer. Clear it (the Each-with-budget mechanics are designed
			// when a carrier exists, not half-read).
			d.MaxSum = 0
		} else {
			for _, id := range budgetEligible {
				name := "a card"
				var cardName string
				if o := g.Obj(id); o != nil && o.Face() != nil {
					cardName = o.Face().Name
					// NoLooking$ True means the chooser never looked at THIS search
					// window, so an option is blind unless the chain already taught
					// this chooser the card's identity (a public reveal, or an
					// earlier named ask this player answered). The Cultivate-family
					// placement legs are exactly that case: their head named or
					// revealed the cards, so the leg must not hide them again.
					if !noLooking || searchKnownTo(c, chooser, id) {
						name = cardName
					}
				}
				opt := decision.Option{Index: len(d.Options),
					Kind: "search", Label: name, Obj: id, Player: owner}
				// Only a budget search carries a Value: Option.Value is omitempty,
				// so a non-budget search's option list serialises byte-identically.
				if hasBudget {
					opt.Value = manaValueOf(g, id)
				}
				if differentNames && cardName != "" {
					opt.Group = cardName
				}
				d.Options = append(d.Options, opt)
			}
		}
		// Forge Mandatory$ removes the CR 701.23b fail-to-find option: if
		// eligible cards (or EACH groups) exist, the search must take the
		// requested number. Apply this after the EACH shape sets its bounds.
		if cz.Mandatory {
			min = max
			if hasBudget && !eachStructured {
				// Respect the cumulative budget's feasible deterministic count;
				// never post a mandatory minimum the budget cannot satisfy.
				min = int32(len(greedy))
			}
		}
		d.Min = int(min)
		// Exactly$ True ("you may reveal exactly two cards ... with different
		// names": Extrapolate the Impossible, Burning-Rune Demon, Turtles
		// Forever) is all or nothing: the find is exactly the requested number
		// (max already fell to 0 above when that many cannot exist, so this
		// ask is only posed when it can), or nothing at all -- never a partial
		// find, and never a forced one. Min == Max carries the "exactly";
		// Decision.AllowNone carries the decline, the one shape a Min/Max pair
		// alone cannot say. The flat (non-EACH) shape only: no corpus carrier
		// pairs Exactly$ with the EACH grammar.
		if cz.Exactly && !eachStructured && !hasBudget && max > 0 {
			d.Min = int(max)
			d.AllowNone = true
		}
		// The prompt is built here, AFTER the Mandatory$ clamp and the EACH
		// branch's own bounds, so its stated count always matches the decision's
		// final Min/Max -- a mandatory leg with Min == Max == 1 must not advertise
		// "up to 1 card(s)". SelectPrompt$ (Forge's ChangeZoneEffect prompt,
		// already read on the sibling hidden-pick path in effHiddenPick) replaces
		// the generic text when the SA carries it: Cultivate's legs name their own
		// "Select a card to put onto the battlefield".
		if sp := cz.SelectPrompt; sp != "" {
			d.Prompt = sp
		} else if d.AllowNone {
			d.Prompt = "Search a library: choose exactly " + strconv.Itoa(d.Max) + " card(s), or none"
		} else if d.Min == d.Max {
			d.Prompt = "Search a library: choose " + strconv.Itoa(d.Max) + " card(s)"
		} else {
			d.Prompt = "Search a library: choose up to " + strconv.Itoa(d.Max) + " card(s)"
		}
		// EACH's own prompt was only a placeholder for the counted forms; the
		// override above already replaced it when SelectPrompt$ is absent.
		if eachStructured && cz.SelectPrompt == "" {
			d.Prompt = "Search a library: choose one card of each listed type"
		}
		// The shared ask boundary (effects.Ask) refuses to post a decision whose
		// only legal answer is the empty one -- with zero eligible cards max
		// clamps to 0 and a stated-quality search's Min is already 0, so that is
		// exactly the Squadron Hawk fail-to-find shape that used to soft-lock the
		// game. AskEmpty resolves it silently through the stand-in below: the
		// search still shuffles, and a fail-to-find is legitimate under
		// CR 701.23b, so nothing is degraded and no R-9 Note is recorded.
		oc := Ask(h, d)
		if oc == AskAsked {
			return
		}
		// R-9: a host without a decision channel cannot ask a player, so it
		// supplies a deterministic answer in the player's place. For a
		// quantity-only search (CR 701.23d) the decision would refuse to find
		// fewer than Min cards, so the stand-in takes the first Min eligible
		// cards -- in the same ordered eligible list the decision's options
		// were built from -- or all of them when the library holds fewer
		// (701.23d's "as many as possible"). For a stated-quality search
		// (CR 701.23b) finding nothing is a legitimate fail-to-find, so the
		// stand-in still finds nothing, exactly as before. Either way the
		// search's unconditional shuffle still happens. An AskEmpty run takes
		// the same stand-in silently (no Note): skipping the ask is the correct
		// resolution, not a degradation.
		var picked []state.ObjID
		if eachStructured {
			// The structured stand-in: a stated-quality EACH's fail-to-find
			// (picked stays nil, CR 701.23b); a quantity-only EACH takes each
			// group's first perType candidates in group order -- the per-type
			// mirror of the flat first-Min take, and the exact take the bot's
			// group-aware fill re-derives. (A quantity-only EACH is
			// measured-absent; the arm exists so the structure never silently
			// degrades to the flat union take.)
			if !SearchStatesQuality(rawSpec) {
				for _, ids := range eachGroups {
					n := eachPerType
					if int32(len(ids)) < n {
						n = int32(len(ids))
					}
					picked = append(picked, ids[:n]...)
				}
			}
			if oc == AskNoHost {
				if SearchStatesQuality(rawSpec) {
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: chooser,
						Text: "finds no card (no engine host to ask)"})
				} else {
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: chooser,
						Text: "finds " + strconv.Itoa(len(picked)) + " card(s) (no engine host to ask)"})
				}
			}
		} else if !SearchStatesQuality(rawSpec) {
			n := int(min)
			if n > len(greedy) {
				n = len(greedy)
			}
			if n > 0 {
				// DifferentNames$ True makes the stand-in distinct-name aware too:
				// a first-Min run over duplicate names would move two same-named
				// cards the apply side would then have to silently drop under the
				// Min the decision promised. (No corpus card pairs DifferentNames$
				// with WithTotalCMC$, so the budget greedy and this walk never
				// compete; the budget's greedy is the pick when both are present.)
				if differentNames && !hasBudget {
					seen := make(map[string]bool, n)
					for _, id := range eligible {
						if len(picked) >= n {
							break
						}
						var cardName string
						if o := g.Obj(id); o != nil && o.Face() != nil {
							cardName = o.Face().Name
						}
						if seen[cardName] {
							continue
						}
						seen[cardName] = true
						picked = append(picked, id)
					}
				} else {
					picked = append(picked, greedy[:n]...)
				}
			}
			if oc == AskNoHost {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: chooser,
					Text: "finds " + strconv.Itoa(n) + " card(s) (no engine host to ask)"})
			}
		} else if oc == AskNoHost {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: chooser,
				Text: "finds no card (no engine host to ask)"})
		}
		c.LibraryTarget = targetIndex
		if applyLibrarySearch(h, c, sa, cz, owner, to, picked, zones) {
			return
		}
	}
	endForgetOtherSnapshot(c)
}

// libraryFetch is one owner and the direct-library objects moved for them.
// The slice stays in Defined$ order; a map would make emitted move/shuffle
// events nondeterministic.
type libraryFetch struct {
	owner state.PlayerID
	ids   []state.ObjID
}

// moveDefinedLibraryObjects implements Forge's hidden-origin Defined$ fetch
// list. When Defined$ resolves to object(s), those identities are the list to
// move; they do NOT select a library owner for a new search. As with the
// ordinary object path, ChangeType$ does not re-filter an already named
// object. This covers Remembered, ChosenCard, TopOfLibrary, BottomOfLibrary,
// and every future object-valued Defined selector through the same dispatch.
//
// Object selectors from Hand and Graveyard already use effChangeZone's normal
// object path. Library is the exceptional origin because it otherwise enters
// effSearchLibrary. A Defined$ yielding only player targets still belongs to
// the search-owner path below. Each touched owner is shuffled once, even when
// another sub-effect already moved every fetched object: Nissa's Pilgrimage's
// final fetch-list step is the script's shuffle point after its chosen Forest
// entered the battlefield.
//
// Optional$ True is a choice over the whole known fetch list, not permission
// to silently move it. Kenessos's DBBottom is the corpus example: after its
// player declines to put the revealed card onto the battlefield, they may put
// that card on the bottom. The yes/no decision suspends before either a move
// or a shuffle; its answer is scoped in Ctx so a nested optional fetch cannot
// inherit it. A no-host run keeps the previous deterministic mover (yes), the
// R-9 fallback used by the other optional mid-resolution effects.
//
// An unrecognised Defined$ selector is a fail-closed no-op here. In
// particular it must not pass through Defined's public source fallback: that
// fallback would make an unknown selector look like an object fetch list and
// silently consume the hidden-origin effect. A resolved player target takes
// the ordinary search-owner path below, regardless of which selector yielded
// it; the target kind, not a closed spelling list, defines the role.
func moveDefinedLibraryObjects(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, to state.Zone) bool {
	if cz.Defined == "" {
		return false
	}
	if cz.DefinedPlayer.Present {
		return false
	}
	g := h.Game()
	targets, known := knownDefinedTargets(h, c, cz.Defined)
	if !known {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unrecognised Defined library fetch " + cz.Defined})
		return true
	}
	var fetches []libraryFetch
	objectList := false
	playerList := false
	addOwner := func(p state.PlayerID) int {
		for i := range fetches {
			if fetches[i].owner == p {
				return i
			}
		}
		fetches = append(fetches, libraryFetch{owner: p})
		return len(fetches) - 1
	}
	for _, t := range targets {
		if t.IsPlayer {
			playerList = true
			continue
		}
		objectList = true
		o := g.Obj(t.Obj)
		if o == nil || int(o.Owner) >= len(g.Players) {
			continue
		}
		i := addOwner(o.Owner)
		if o.Zone == state.ZLibrary {
			fetches[i].ids = append(fetches[i].ids, o.ID)
		}
	}
	if !objectList {
		// A resolved player list identifies whose library to search. Deriving
		// that role from the resolved target kind covers every selector with a
		// player binding (Remembered, Targeted, ChosenPlayer, and future ones),
		// instead of losing an unlisted spelling to a direct-fetch no-op. An
		// empty object fetch (an empty Remembered/ChosenCard list or an empty
		// library's TopOfLibrary) is still a direct fetch and must not degrade
		// to a fresh whole-library search.
		return !playerList
	}

	optional := cz.OptionalTrue
	answer := c.DefinedLibraryMove
	c.DefinedLibraryMove = "" // fx42 scoping: a nested fetch asks for itself.
	if optional && answer == "" {
		prompt := cz.OptionalPrompt
		if prompt == "" {
			prompt = "Move the selected card(s)?"
		}
		d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
			Source: c.Source, ResumeKind: "defined_library_optional", ResumeSA: sa,
			ResumeRemembered: append([]state.Target(nil), c.Remembered...), Prompt: prompt,
			Options: []decision.Option{
				{Index: 0, Kind: "yes", Label: "Yes", Player: c.Controller},
				{Index: 1, Kind: "no", Label: "No", Player: c.Controller},
			}}
		if Ask(h, d) == AskAsked {
			return true
		}
		// AskNoHost cannot represent a decline. Preserve the prior direct-move
		// fallback rather than leaving a headless resolution suspended.
		answer = "yes"
	}
	if optional && answer == "no" {
		return true
	}
	// The fetch is entered: Forge clears the source's remembered cards before
	// the choose (ChangeZoneEffect.changeHiddenOriginResolve 1103), so an
	// accepted optional fetch that moves nothing still clears. A declined
	// Optional$ confirmation returned above without clearing.
	forgetOther(h, c, cz.Riders.ForgetOtherRemembered)

	withKind := cz.WithCountersType
	var withAmt int32
	if withKind != "" && counterDestination(to) {
		withAmt = withCounterAmount(h, c, cz)
	}
	// The AtEOT$ rider's affected set, collected across every fetch and
	// scheduled by ONE call after the loop (one Note per call, never per
	// owner).
	var ateotMoved []state.ObjID
	rider := classifyAttackingEntryText(c, cz.Riders.Attacking, to)
	for i := range fetches {
		f := &fetches[i]
		moved := make([]state.ObjID, 0, len(f.ids))
		for _, id := range f.ids {
			o := g.Obj(id)
			// Recheck at the point of movement: a malformed or stale Defined$
			// target must not move an object from a new zone.
			if o == nil || o.Zone != state.ZLibrary || o.Owner != f.owner {
				continue
			}
			settleChangeZoneMove(h, c, sa, cz, id, state.ZLibrary, to, withKind, withAmt, &rider)
			if cz.RememberChanged {
				eventRemember(h, c, id)
			}
			eventForgetChanged(h, c, sa, id)
			moved = append(moved, id)
			ateotMoved = append(ateotMoved, id)
			if to == state.ZBattlefield && cz.Tapped {
				h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: f.owner, Text: "entered tapped"})
			}
		}
		shuffleLibrary(h, cz, f.owner)
		placeLibraryObjects(h, c, cz, f.owner, moved, to)
		// Explicit Reveal$ on a Defined$ fetch list (Forge reveals movedCards
		// whenever Reveal$ names the effect, defined or not): the same public
		// Note payload applyLibrarySearch emits -- no auto-reveal here, since
		// a Defined$ list is never revealed by default.
		if cz.Reveal && len(moved) > 0 {
			h.Emit(events.Event{Kind: events.Note, Player: f.owner, IDs: moved})
		}
	}
	scheduleAtEOT(h, c, sa, ateotMoved)
	return true
}

// changeZoneFetchSelector distinguishes a fetch player from an already chosen
// object. An unbound or unknown object selector never widens to a free search.
func changeZoneFetchSelector(h Host, c *Ctx, cz *ChangeZoneParams) bool {
	if spec := cz.Defined; spec != "" {
		targets, known := knownDefinedTargets(h, c, spec)
		if !known || len(targets) == 0 {
			return false
		}
		for _, target := range targets {
			if !target.IsPlayer {
				return false
			}
		}
		return true
	}
	if cz.DefinedPlayer.Text != "" {
		return true
	}
	if cz.ValidTgts.Text != "" {
		if len(c.Targets) == 0 {
			return false
		}
		for _, target := range c.Targets {
			if !target.IsPlayer {
				return false
			}
		}
	}
	return true
}

// searchPlayers resolves whose zones are searched. DefinedPlayer$ takes
// precedence over Defined$; targeted players come next, then the controller.
func searchPlayers(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	return searchPlayersFor(h, c, fetchSelectorsParam(sa))
}

// searchPlayersFor is searchPlayers over compiled fetch selectors.
func searchPlayersFor(h Host, c *Ctx, sel fetchSelectors) []state.PlayerID {
	if sel.DefinedPlayer.Text == "" && sel.Defined.Text == "" && sel.ValidTgts.Text != "" {
		return hiddenPickPlayers(h, c, sel)
	}
	spec, explicit := sel.DefinedPlayer.Text, sel.DefinedPlayer.Present
	if !explicit {
		spec, explicit = sel.Defined.Text, sel.Defined.Present
	}
	if !explicit || strings.TrimSpace(spec) == "" {
		return []state.PlayerID{c.Controller}
	}
	// definedPlayerIDs shares the deterministic selector grammar and applies
	// Forge's getDefinedPlayers rule: a remembered CARD contributes a seat
	// only for the RememberedController/RememberedOwner spellings, never for
	// the plain Remembered family (Summon: Valefor's per-opponent loop).
	return definedPlayerIDs(h, c, spec)
}

// chooserChosenPlayer resolves a `Chooser$ ChosenPlayer` (or its
// `Player.Chosen` spelling): the player chosen earlier in the resolution, or
// the source permanent's event-backed choice. It reads the answer through the
// SAME shared grammar `Defined$ ChosenPlayer` uses (searchPlayers' path), so a
// chooser and the fetch-player lookup cannot drift apart. It reports false
// when no chosen player is bound or the bound seat has left the game; callers
// keep their own deterministic fallback rather than acting on a dead seat.
func chooserChosenPlayer(h Host, c *Ctx) (state.PlayerID, bool) {
	for _, t := range Defined(h, c, &cards.SA{Params: map[string]string{"Defined": "ChosenPlayer"}}) {
		if !t.IsPlayer {
			continue
		}
		p := PlayerOf(h, c, t)
		if int(p) < len(h.Game().Players) && !h.Game().Players[p].Lost {
			return p, true
		}
	}
	return c.Controller, false
}

// chooserPlayer resolves a non-empty Chooser$ selector through the shared
// Defined$ grammar. It deliberately returns false for an unknown or dead
// referent so each caller can preserve its own fallback.
func chooserPlayer(h Host, c *Ctx, spec string) (state.PlayerID, bool) {
	targets, ok := knownDefinedTargets(h, c, spec)
	if !ok {
		return 0, false
	}
	for _, t := range targets {
		if !t.IsPlayer && h.Game().Obj(t.Obj) == nil {
			continue
		}
		p := PlayerOf(h, c, t)
		if int(p) < len(h.Game().Players) && !h.Game().Players[p].Lost {
			return p, true
		}
	}
	return 0, false
}

// searchChooser resolves who answers the search prompt. A known Chooser$
// selector wins; an unbound or unknown selector falls back to the controller.
func searchChooser(h Host, c *Ctx, cz *ChangeZoneParams) state.PlayerID {
	if spec := cz.Chooser; spec != "" {
		if p, ok := chooserPlayer(h, c, spec); ok {
			return p
		}
	}
	return c.Controller
}

// searchKnownTo reports whether player p has already legitimately learned the
// identity of library card id during this resolution's search chain. It reads
// the Ctx.SearchKnown set that applyLibrarySearch populates: a publicly
// revealed card names every seat, a card an earlier ask offered BY NAME names
// the player who picked it. A card absent from the set is genuinely unknown to
// p and stays fail-closed -- the blind "a card" label -- so a search that
// never revealed and never named those cards cannot leak their library order.
// The list rides the ask (Decision.ResumeSearchKnown), so a planted placement
// leg still sees it after a previous leg's suspension rebuilt the Ctx.
func searchKnownTo(c *Ctx, p state.PlayerID, id state.ObjID) bool {
	if c == nil {
		return false
	}
	for _, t := range c.SearchKnown {
		if t.Obj == id && t.Player == p && !t.IsPlayer {
			return true
		}
	}
	return false
}

// searchLibraryWindow applies Forge's limited-look bound to a library search.
// MaxRevealed$ is the number of cards the search may inspect from the top of
// the library; public-origin alternatives remain outside this window. Keeping
// this helper shared by option construction and answer revalidation prevents a
// host that bypasses Decision.Validate from selecting a card below the look.
func searchLibraryWindow(h Host, c *Ctx, cz *ChangeZoneParams, lib []state.ObjID) []state.ObjID {
	if !cz.MaxRevealed.Present || cz.MaxRevealed.Text == "" || len(lib) == 0 {
		return lib
	}
	limit := numText(h, c, cz.MaxRevealed, 0)
	if limit < 0 {
		limit = 0
	}
	if limit > int32(len(lib)) {
		limit = int32(len(lib))
	}
	return lib[:limit]
}

// hiddenPickPlayers resolves whose cards the hidden-origin pick offers, the
// pick's analogue of searchPlayers: DefinedPlayer$ through the shared
// selector grammar first, then the targeted PLAYERS (Forge's
// getFirstTargetedPlayer for a usesTargeting effect), then the source
// controller -- Forge's getDefinedPlayers(null) defaults to "You".
func hiddenPickPlayers(h Host, c *Ctx, sel fetchSelectors) []state.PlayerID {
	if sel.DefinedPlayer.Text != "" {
		return searchPlayersFor(h, c, sel)
	}
	if sel.ValidTgts.Present {
		var out []state.PlayerID
		seen := make(map[state.PlayerID]bool, len(c.Targets))
		for _, t := range c.Targets {
			if !t.IsPlayer {
				continue
			}
			p := PlayerOf(h, c, t)
			if int(p) >= len(h.Game().Players) || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
		if len(out) > 0 {
			return out
		}
	}
	return []state.PlayerID{c.Controller}
}

// hiddenPickChooser resolves who answers the pick. A known Chooser$ selector
// wins; an unbound or unknown selector falls back to the fetch owner. With no
// selector, the owner remains the decider.
func hiddenPickChooser(h Host, c *Ctx, cz *ChangeZoneParams, owner state.PlayerID) state.PlayerID {
	if spec := cz.Chooser; spec != "" {
		if p, ok := chooserPlayer(h, c, spec); ok {
			return p
		}
		return owner
	}
	return owner
}

// landTypesOf lists the land subtypes o's face names, in face order: the
// supertypes (Basic, Snow) and the Land type word are stripped, so a Basic
// Forest leaves exactly Forest. The search paths this serves are
// Land.Basic-filtered (ShareLandType$ only rides hidden-library searches),
// so a face whose remaining types are empty never contributes a type.
func landTypesOf(o *state.Object) []string {
	f := o.Face()
	if f == nil {
		return nil
	}
	out := make([]string, 0, len(f.Types))
	for _, t := range f.Types {
		switch t {
		case "Land", "Basic", "Snow":
			continue
		}
		out = append(out, t)
	}
	return out
}

// SharedLandTypes reports whether every named object shares at least one
// land type: fewer than two objects is trivially true, otherwise the
// intersection of their land-subtype sets must be non-empty. Both readers of
// ShareLandType$ True go through it so the wire rule (rules.Submit) and the
// host-bypass trim (applyLibrarySearch) cannot drift on what "share" means.
func SharedLandTypes(g *state.Game, ids []state.ObjID) bool {
	if len(ids) <= 1 {
		return true
	}
	var shared map[string]bool
	for _, id := range ids {
		o := g.Obj(id)
		if o == nil {
			return false
		}
		types := landTypesOf(o)
		if shared == nil {
			shared = make(map[string]bool, len(types))
			for _, t := range types {
				shared[t] = true
			}
			continue
		}
		ok := false
		keep := make(map[string]bool, len(types))
		for _, t := range types {
			if shared[t] {
				ok = true
				keep[t] = true
			}
		}
		if !ok {
			return false
		}
		shared = keep
	}
	return true
}

// trimSharedLandTypes is SharedLandTypes' deterministic enforcement for a
// host that bypassed the wire: keep the first chosen object in answer order
// and every later one that still shares a type with everything kept before
// it.
func trimSharedLandTypes(g *state.Game, chosen []state.ObjID) []state.ObjID {
	if len(chosen) <= 1 {
		return chosen
	}
	var shared map[string]bool
	out := make([]state.ObjID, 0, len(chosen))
	for _, id := range chosen {
		o := g.Obj(id)
		if o == nil {
			continue
		}
		types := landTypesOf(o)
		if shared == nil {
			shared = make(map[string]bool, len(types))
			for _, t := range types {
				shared[t] = true
			}
			out = append(out, id)
			continue
		}
		ok := false
		keep := make(map[string]bool, len(types))
		for _, t := range types {
			if shared[t] {
				ok = true
				keep[t] = true
			}
		}
		if !ok {
			continue
		}
		out = append(out, id)
		shared = keep
	}
	return out
}

// totalCardTypesRequirement reads the constraint from the ChangeZone node
// that owns this hidden pick. ResumeSA preserves that node across an answer;
// a linked sub-ability's parameter must not constrain its parent pick.
func totalCardTypesRequirement(cz *ChangeZoneParams) (string, bool) {
	if cz == nil {
		return "", false
	}
	raw := cz.WithTotalCardTypes
	return raw, raw != ""
}

// totalCardTypesSatisfied is the hidden-search constraint used by
// WithTotalCardTypes$. Card types are the ordinary spell types, including
// Kindred and Battle; supertypes and creature subtypes in Face.Types do not count.
func totalCardTypesSatisfied(g *state.Game, ids []state.ObjID, need int) bool {
	if need <= 0 {
		return true
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		o := g.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		for _, typ := range o.Face().Types {
			switch typ {
			case "Artifact", "Battle", "Creature", "Enchantment", "Instant", "Kindred", "Land", "Planeswalker", "Sorcery":
				seen[typ] = true
			}
		}
	}
	return len(seen) >= need
}

// applyLibrarySearch returns true when the search's tail suspended on a
// may-shuffle decision. The caller must stop its per-player walk in that case;
// the resume path owns the pending decision and continues with the next
// library only after its answer has been consumed.
func applyLibrarySearch(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, owner state.PlayerID, to state.Zone, chosen []state.ObjID, zones []state.Zone) bool {
	// The search-control/replacement boundary (Opposition Agent's class):
	// the moves this function emits are the moves OF A SEARCH, and the host
	// that models that fact scopes its FoundSearchingLibrary$ replacements
	// and ControlOpponentsSearchingLibrary$ redirects to them. The optional
	// hooks keep test hosts (which do not model the state) working.
	if b, ok := h.(interface {
		BeginLibrarySearch(owner state.PlayerID)
		EndLibrarySearch()
	}); ok {
		b.BeginLibrarySearch(owner)
		defer b.EndLibrarySearch()
	}
	g := h.Game()
	spec := cz.ChangeType
	// Recheck the answer with the same hidden-zone meaning used to build the
	// option list: a library Permanent is a permanent card.
	spec = permanentCardSpec(spec)
	// DifferentNames$ True (Realms Uncharted): the options carried a Group
	// per card name, so a validated wire answer cannot repeat a name. A host
	// that bypassed the wire (bot clamp top-up, a direct resume) is deduped
	// here deterministically -- first per name in answer order -- so the
	// engine and its clients cannot drift on what the constraint means.
	if cz.DifferentNames {
		seenNames := make(map[string]bool, len(chosen))
		deduped := make([]state.ObjID, 0, len(chosen))
		for _, id := range chosen {
			name := ""
			if o := g.Obj(id); o != nil && o.Face() != nil {
				name = o.Face().Name
			}
			if name != "" && seenNames[name] {
				continue
			}
			if name != "" {
				seenNames[name] = true
			}
			deduped = append(deduped, id)
		}
		chosen = deduped
	}
	// ShareLandType$ True (Myriad Landscape): every chosen card must share at
	// least one land type with the rest ("up to two basic land cards that
	// share a land type"). A validated wire answer cannot violate it
	// (rules.Submit rejects such an intent and the decision stays pending),
	// so this trim is the host-bypass guard -- the same shape the
	// DifferentNames dedupe above serves: keep the first chosen card in
	// answer order and every later one that still shares a type with
	// everything kept before it.
	if cz.ShareLandType {
		chosen = trimSharedLandTypes(g, chosen)
	}
	// WithTotalCardTypes$ constrains the complete hidden pick, rather than
	// each option independently. Decision.Validate cannot inspect card
	// characteristics, so enforce the same constraint at the resolution
	// boundary as a conservative host-bypass guard: an underspecified answer
	// finds nothing and cannot feed the ChangeZone rider. This check follows
	// the other set-level trims so those cannot invalidate the guarantee.
	if raw, hasTotalCardTypes := totalCardTypesRequirement(cz); hasTotalCardTypes {
		// This parameter is a literal card-type cardinality in the Forge
		// grammar (Winter uses 4). Parse it directly so the hidden-search
		// continuation cannot lose the literal when it rebuilds its Ctx.
		literal, parseErr := strconv.Atoi(raw)
		need, ok := int32(literal), parseErr == nil
		if !ok || need < 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: owner,
				Text: "WithTotalCardTypes$ cannot be resolved; hidden pick fails closed"})
			chosen = nil
		} else if !totalCardTypesSatisfied(g, chosen, int(need)) {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: owner,
				Text: "hidden pick fails WithTotalCardTypes$ requirement"})
			chosen = nil
		}
	}
	window := searchLibraryWindow(h, c, cz, zoneOf(g, state.ZLibrary, owner))
	// ChooseFromDefined$ bounds the search's VALID answers the same way the
	// option list was bounded in effSearchLibrary (the two reads share the ONE
	// pool resolver, so they cannot drift): a host that bypassed the wire
	// cannot move a card outside the resolved selector, and an unresolved
	// selector with an actual answer re-fails closed loudly (the option build
	// already noted; this is the bypass guard).
	var cfdPool map[state.ObjID]bool
	cfdActive := false
	if raw := cz.ChooseFromDefined; raw != "" {
		if pool, ok := chooseFromDefinedPool(h, c, raw); ok {
			cfdPool, cfdActive = pool, true
		} else {
			if len(chosen) > 0 {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "ChangeZone ChooseFromDefined$ " + raw + " is not resolvable; nothing is offered"})
			}
			chosen = nil
		}
	}
	moved := make([]state.ObjID, 0, len(chosen))
	// Imprint$ True records the cards this search actually moved in the
	// source's persistent imprintedCards association (state.Object.Imprinted),
	// the same association the object-target path above accumulates and the
	// same one `Defined$ Imprinted` resolves later. Collected across the loop
	// and emitted as ONE events.Imprint after it, exactly like that path.
	var imprinted []state.ObjID
	// One classification for this search's whole mover loop: both branches
	// below (the public-origin settle and the library-origin direct emit)
	// share it, so a degrading rider is one Note per search, not one per card.
	rider := classifyAttackingEntryText(c, cz.Riders.Attacking, to)
	// Snapshot the rechecked pick before clearing persistent IsRemembered.
	valid := make([]state.ObjID, 0, len(chosen))
	for _, id := range chosen {
		o := g.Obj(id)
		if o != nil && o.Owner == owner && zoneIn(zones, o.Zone) &&
			(o.Zone != state.ZLibrary || containsID(window, id)) &&
			(!cfdActive || cfdPool[id]) &&
			MatchesSpecCtx(g, spec, id, forgetOtherSpecContext(c)) {
			valid = append(valid, id)
		}
	}
	// The search's fetch is entered: Forge clears the source's remembered
	// cards before the choose (ChangeZoneEffect.changeHiddenOriginResolve
	// 1103), so a search that yields no card still clears. The valid set above
	// was rechecked against the pre-clear snapshot, so a formerly remembered
	// card remains admitted here.
	forgetOther(h, c, cz.Riders.ForgetOtherRemembered)
	// A hidden search with NO Destination$ (Extrapolate the Impossible's and
	// Turtles Forever's heads, the corpus's only two: "reveal exactly two
	// cards you own ... from outside the game. An opponent chooses one of
	// them.") finds, reveals and remembers the cards for the chained sub that
	// moves the chosen ones -- whose own Origin$ names the SAME zones, so the
	// found cards must still be there. ParseZone's Graveyard default would
	// instead bin every found card before the opponent chooses. They stay put:
	// "moved" here is the found set the reveal and RememberChanged$ read.
	if cz.DestinationText == "" {
		for _, id := range valid {
			moved = append(moved, id)
			if cz.RememberChanged {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
		}
		valid = nil
	}
	for _, id := range valid {
		o := g.Obj(id)
		if o == nil || !zoneIn(zones, o.Zone) {
			continue
		}
		// A chosen candidate from a PUBLIC origin zone (OriginAlternative$
		// Graveyard/Hand/Exile) moves through the ordinary cross-zone settle:
		// no library shuffle or LibraryPosition$ placement follows it, and
		// because it never left the library nothing here can disturb the
		// library order. The library half keeps the existing direct emit, which
		// is where the exile-provenance IDs and the Imprint rider live.
		if o.Zone != state.ZLibrary {
			withKind := ""
			var withAmt int32
			if cz.WithCountersType != "" && counterDestination(to) {
				withKind = cz.WithCountersType
				withAmt = withCounterAmount(h, c, cz)
			}
			settleChangeZoneMoveAs(h, c, sa, cz, id, o.Zone, to, withKind, withAmt, owner, true, &rider)
			// AttachedTo$ on an alternative-zone pick (Boonweaver Giant's "put
			// it onto the battlefield attached to CARDNAME", Runed Crown, Arachnus
			// Web): the same rider the library branch below applies -- without it
			// an Aura found in the graveyard or hand enters unattached and the
			// CR 704.5m SBA sweeps it. GainControl$/WithCounters*/Transformed$
			// already ride settleChangeZoneMoveAs above.
			if to == state.ZBattlefield {
				changeZoneAttachedTo(h, c, sa, cz, id)
			}
			moved = append(moved, id)
			// settleChangeZoneMoveAs already appended the object to the
			// resolution's Remembered for RememberChanged$; only the persistent
			// event-backed half is left here. RememberSearched$ is not read
			// there, so its ctx append is made here too.
			if cz.RememberChanged {
				eventRemember(h, c, id)
			}
			if cz.RememberSearched {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
			eventForgetChanged(h, c, sa, id)
			if to == state.ZBattlefield && cz.Tapped {
				h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: owner, Text: "entered tapped"})
			}
			continue
		}
		ev := moveZoneEvent(c, id, state.ZLibrary, to)
		ev.Player = owner
		applyMoveFaceDown(h, c, &cz.Riders.FaceDownRiders, &ev, to)
		h.Emit(ev)
		if to == state.ZExile && c.Source != 0 {
			if o := g.Obj(id); o != nil && !o.IsToken {
				h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: []state.ObjID{id}, Text: "exiled-with"})
			}
		}
		// Imprint$ True (Distant Memories, Jace, Architect of Thought's -8,
		// Grim Reminder): the moved card joins the source's imprintedCards
		// list whatever the destination -- Forge's ChangeZoneEffect imprints
		// every card it moved, and the corpus reads the association back with
		// a later `Defined$ Imprinted` sub-ability. The move is confirmed by
		// the object's post-move zone before the id is recorded, so a skipped
		// candidate (an Origin$ miss, an in-flight replacement) is never
		// imprinted.
		if cz.Imprint && c.Source != 0 {
			if o := g.Obj(id); o != nil && o.Zone == to && !o.IsToken {
				if cz.ImprintLast {
					clearChangeZoneImprint(h, c)
					h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: []state.ObjID{id}})
				} else {
					imprinted = append(imprinted, id)
				}
			}
		}
		moved = append(moved, id)
		if cz.WithCountersType != "" && counterDestination(to) {
			h.Emit(events.Event{Kind: events.CounterChange, Obj: id,
				Counter: cz.WithCountersType, Amount: withCounterAmount(h, c, cz)})
		}
		// GainControl$ on a library search (Act on Impulse's "you may play
		// those cards" family's put-onto-battlefield relatives): same settle
		// order as every other mover -- move first, then the control change,
		// then the AttachedTo$ rider (the Origin$ Library ChangeZone lines
		// carrying it, e.g. an "return an Aura ... attached to CARDNAME" search).
		if to == state.ZBattlefield {
			applyGainControlFor(h, c, cz.Riders.GainControl, id)
			changeZoneAttachedTo(h, c, sa, cz, id)
		}
		if cz.RememberChanged {
			c.Remembered = append(c.Remembered, state.Target{Obj: id})
			eventRemember(h, c, id)
		}
		if cz.RememberSearched {
			// RememberSearched$ True (Tempt with Discovery's tempting offer):
			// the cards the search found join the resolution's Remembered --
			// the same Ctx set RememberChanged$ feeds -- so the follow-up sub
			// ("for each opponent who searched, search again") counts them
			// through Count$RememberedSize and repeats the search that many
			// times.
			c.Remembered = append(c.Remembered, state.Target{Obj: id})
			eventRemember(h, c, id)
		}
		eventForgetChanged(h, c, sa, id)
		if to == state.ZBattlefield && cz.Tapped {
			// This establishes the object's entry state; it is not the CR
			// 701.21a event of becoming tapped. Text is part of the replayed
			// event payload, so rules can distinguish it from an ordinary Tap
			// while replay folds the same tapped state.
			h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: owner, Text: "entered tapped"})
		}
		rider.apply(h, c, id, owner, to)
		// StaticEffect$ on the library-origin branch: the same rider the
		// shared settle path applied for the alternative-origin branch above.
		if to == state.ZBattlefield {
			applyStaticEffect(h, c, sa, to, []state.ObjID{id})
		}
	}
	// The Imprint$ association, one batched event after the whole mover loop
	// (the object-target path's own shape above): the source keeps the
	// moved cards' ids so a later `Defined$ Imprinted` resolves them, and the
	// association is durable state folded by events.Apply, so replay keeps it.
	if len(imprinted) > 0 {
		h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: imprinted})
	}
	// Record one completed search per library, including a search that found
	// no eligible card. This marker is distinct from the searched cards' own
	// MoveZone events so trig:SearchedLibrary cannot false-fire on ordinary
	// library movement.
	h.Emit(events.Event{Kind: events.SearchedLibrary, Obj: c.Source, Player: owner})

	// The search's reveal (hiddenreveal1): Forge's changeHiddenOriginResolve
	// reveals the moved cards when Reveal$ says so, and ALSO by default when
	// the search's ChangeType$ states a quality (anything beyond the bare
	// "Card"), the destination is not the battlefield and no Defined$ fixed
	// the list -- the "reveal it" half of a quality search (Idyllic Tutor,
	// Cultivate, Nissa's Pilgrimage; Demonic Tutor's bare Card ChangeType$
	// stays hidden). A Hidden$ move without an explicit Reveal$ suppresses
	// the default: the change is hidden. The record is the same payload the
	// Reveal primitive emits (a public Note carrying the ids; no Text --
	// view.Describe renders the names), emitted after the moves exactly where
	// Forge's own reveal call sits.
	reveal := cz.Reveal ||
		(to != state.ZBattlefield && spec != "Card" && cz.Defined == "" && !cz.NoReveal)
	if cz.Hidden && !cz.Reveal {
		reveal = false
	}
	if reveal && len(moved) > 0 {
		h.Emit(events.Event{Kind: events.Note, Player: owner, IDs: moved})
	}
	// Record which of the moved cards the choosing players have legitimately
	// learned, so a planted placement leg (NoLooking$ True, ChangeType$
	// ...IsRemembered) can label its options with real names instead of the
	// blind "a card" (effects/zone.go effSearchLibrary's ask builder; the
	// Ctx.SearchKnown field documents the criterion). Two sufficient
	// channels, both scoped to a card that ACTUALLY moved:
	//   (a) a public reveal published the card's identity to every seat
	//       (Cultivate, Kodama's Reach, Intuition, Gifts Ungiven);
	//   (b) this ask offered the card BY NAME (no NoLooking$ on this SA) and
	//       the chooser picked it, so that chooser knows it even when nothing
	//       was revealed (Final Parting, Nissa's Pilgrimage, whose heads carry
	//       no Reveal$).
	// A leg carries NoLooking$ True and so contributes no (b) entries, and a
	// card the chooser genuinely never saw stays absent from the set -- the
	// fail-closed branch that keeps a blind search from leaking library order.
	if len(moved) > 0 {
		if reveal {
			for _, id := range moved {
				for p := range g.Players {
					c.SearchKnown = append(c.SearchKnown, state.Target{Obj: id, Player: state.PlayerID(p)})
				}
			}
		}
		if !cz.NoLooking {
			chooser := searchChooser(h, c, cz)
			for _, id := range moved {
				c.SearchKnown = append(c.SearchKnown, state.Target{Obj: id, Player: chooser})
			}
		}
	}

	// AtEOT$ rides the search's moved set too. Scheduled BEFORE the
	// may-shuffle confirm: a searchShuffleTail suspension is a tail-only
	// re-entry (effSearchLibrary's SearchShuffle branch), which would never
	// reach a schedule call placed after it -- the moved cards and their
	// registrations are already game state by then.
	scheduleAtEOT(h, c, sa, moved)
	if zoneIn(zones, state.ZLibrary) && searchShuffleTail(h, c, sa, cz, owner, moved, to) {
		return true // the may-shuffle confirm suspended the resolution
	}
	return false
}
