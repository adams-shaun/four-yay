package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effDig implements Forge's Dig: look at the top DigNum cards of Defined$'s
// library, move up to ChangeNum of the ones matching ChangeValid$ (default
// "Card") to DestinationZone$ (default "Hand"), and put everything else on
// the BOTTOM of the library in an order the player picks (the default Forge
// leaves unwritten; measured at the current pin, 518 of the corpus's 737 Dig
// lines carry no DestinationZone2$ and no SkipReorder$ and take this
// default). The remainder's second destination DOES exist in the corpus as
// "DestinationZone2$" (with "LibraryPosition2$" placing it in a library) --
// an earlier note here wrongly claimed the parameter does not exist; it is
// read below. The primary LibraryPosition$ is applied after the primary
// pile settles, including across an ordered-bottom remainder ask.
//
// A real card can also write "ChangeNum$ All" (e.g. Goblin Guide's own Dig)
// to mean every matching card within the DigNum look, with no cap short of
// that. changeNum's own upper bound is already the size of the dug slice, so
// defaulting it to digNum and only overriding that default for a literal or
// SVar ChangeNum$ handles "All" for free: it is simply the case where
// nothing narrows the cap below the number of cards looked at.
//
// The look-and-take ask (dig1): where the top DigNum window holds STRICTLY
// more ChangeValid$-eligible cards than ChangeNum, the pick is a real
// decision and effDig poses it -- the same strict-supersets rule effDiscard's
// TgtChoose arm already uses, so a decision nobody could answer differently
// is never emitted. ChangeNum$ 0 (the corpus's three reveal-machinery digs:
// birthing_ritual, sanity_grinding, stomping_slabs) takes nothing, so the
// ask gate also requires changeNum > 0 -- otherwise a zero cap with any
// eligible card would pose a Min==Max==0 KChoose whose only legal answer is
// the empty one, a decision nobody could answer differently by definition.
// ChangeNum$ Any is the third cap: Forge's any-number take (Jace, the Mind
// Sculptor's "You may put that card on the bottom", Through the Forest
// Gate's "put any number of land cards"), so it caps at the window and
// lowers the ask's Min to 0 whenever an eligible card exists -- a real
// choice, unlike what an earlier note here claimed take-all (measured: the
// unresolvable-value Num read degraded Any to 0 and those digs took
// NOTHING). The look is recorded first as a Secret Note carrying the
// window's ids (only the library's owner may know what sat on top; the same
// channel effRearrangeTopOfLibrary uses, with the ids added so the owner's
// client can render what was seen -- view/redact.go rule (1) passes a Secret
// event's payload to its own Player and strips it from everyone else). The
// decision is a KChoose over the ELIGIBLE cards only, in library order: an
// ineligible card must not be pickable, so it is not offered (the window
// itself is on the look Note; the prompt names the card text). Min honours
// Optional$ -- 0 when the take is optional, ChangeNum when it is not -- and
// Max is ChangeNum. The ask is answered in place (ResumeKind "dig", with the
// asking target's index as ResumeTarget).
//
// A host that cannot answer (the fuzz/no-engine stand-in, R-9) and the
// no-choice path (eligible <= ChangeNum) keep the take deterministic: the
// first ChangeNum eligible cards in zone order move, and the remainder takes
// its second destination -- by default the bottom, ordered (asked; offered
// order without a host). Each target of a multi-target Dig poses its own
// take ask in turn, except that once a target's ordered-bottom arrange has
// been answered every later target keeps the deterministic take. The
// no-choice path asks NO take decision, but it is not event-free when the
// remainder moves: a default-remainder Dig still moves its untaken cards to
// the bottom (asking for that order when two or more remain), so only a game
// that never reaches a Dig whose remainder moves replays byte-identically to
// the pre-dig1 engine.
//
// The variant params (task inbox-paramcensus-dig-variants), each read
// below:
//
//   - Reveal$ True reveals the dug window to the whole table BEFORE any
//     take -- a non-Secret Note carrying the window's ids, the same shape
//     effReveal's public arm emits (Ad Nauseam "Reveal the top card", Chaos
//     Warp, Goblin Guide, Matter Reshaper). It replaces the ask path's
//     private look when a real choice follows the reveal.
//   - NoReveal$ True suppresses that window reveal. A guard, not a
//     behaviour change: the corpus's 84 NoReveal lines carry no Reveal$, and
//     the dig's moves were already Secret before this task (Impulse).
//   - ForceRevealToController$ True reveals each MOVED card publicly before
//     its Secret move -- Ancient Stirrings' "you may reveal a colorless card
//     from among them and put it into your hand" (the window itself stays
//     private). Suppressed when the window was already revealed.
//   - Tapped$ True taps a card the primary move sends to the battlefield,
//     the same MoveZone-then-Tap pair effChangeZone's Tapped$ movers emit
//     (Through the Forest Gate: "put any number of land cards ... onto the
//     battlefield tapped").
//   - DestinationZone2$ (with LibraryPosition2$) is the remainder's second
//     destination: every window card the primary move did not take goes
//     there (Chaos Warp and Goblin Guide's unmatched card back to the
//     library, Matter Reshaper's unmatched card to the hand). OMITTED -- the
//     corpus's default -- it is the library bottom (Ancient Stirrings' own
//     Oracle: "put the rest on the bottom of your library in any order").
//   - LibraryPosition2$ places a library DestinationZone2$: "0" = top,
//     which leaves the cards exactly where they are, so the placement
//     emits nothing; "-1" = bottom, the ordered-bottom KArrange ask (or,
//     for a one-card remainder, the deterministic move -- one card has
//     exactly one possible order); anything else is named in a loud Note
//     and the card stays (the corpus carries only "0" and "-1").
//   - SkipReorder$ True suppresses both the bottom default and a
//     DestinationZone2$ remainder placement: the untaken cards never move,
//     so they stay on top in their existing relative order (the corpus
//     never pairs the two; Through the Forest Gate carries it without one).
//   - WithMayLook$ True (Ixhel, Scion of Atraxa; Gonti; Thief of Sanity;
//     Scarlet Witch) grants the exiling effect's controller -- never the
//     exiled card's owner -- a lasting look at the face of every card the
//     Dig exiles face down. It is applied by the shared applyFaceDownMarker
//     (effects/zone.go) on the primary move's MoveZone, so the Dig calls the
//     one read every other face-down mover uses: the looker rides the
//     "exiled_with_face_down_maylook" marker's Amount and the projection
//     (view/cardViews) admits exactly that player. Absent the param the
//     marker is unchanged, so every non-maylook Dig emits byte-identically.
//
// RestRandomOrder$ True (the bottom pile returns shuffled from the engine's
// seeded generator, with no ask) is read below. Still unread here: the
// exotic DestinationZone2 values (PlanarDeck). The primary optional
// election, primary LibraryPosition$, Choser$ and DigNum$ X are handled by
// this walk.
func effDig(h Host, c *Ctx, sa *cards.SA) {
	dp := DigOf(sa)
	digNum := numText(h, c, dp.DigNum, 1)
	// Forge's DigNum$ X names the resolving X value when one was paid, but
	// trigger bodies also use the same spelling for their face SVar (Keldon
	// Flamesage's SVar:X:Count$CardPower). A zero paid-X slot is not enough to
	// distinguish those forms, so use the named SVar as the trigger fallback
	// when the resolution carries one and its count body resolves.
	if dp.DigNumX && c.X == 0 && c.SVars != nil {
		if body, ok := c.SVars["X"]; ok {
			if n, resolved := EvalCountOK(h, c, body); resolved {
				digNum = n
			}
		}
	}
	if digNum < 0 {
		digNum = 0
	}
	changeNum := digNum
	anyNum := false
	if dp.ChangeNumAny || dp.ChangeNumCapped {
		if dp.ChangeNumAny {
			// Forge's any-number cap: the take is uncapped within the window
			// and the answer may be empty -- a real choice whenever any
			// eligible card exists, which the ask gate and Min below read.
			anyNum = true
		} else {
			changeNum = numText(h, c, dp.ChangeNum, digNum)
		}
	}
	if changeNum < 0 {
		changeNum = 0
	}
	// WithTotalCMC$ is a cumulative mana-value budget over the picked cards
	// ("put any number of nonland permanent cards with total mana value 4 or
	// less"): a card whose own mana value exceeds it can never be picked, and
	// the running sum of the picks must not exceed it either. Absent the
	// param (the corpus default) the budget is 0 and every read below is a
	// no-op, so a non-budget Dig emits byte-identically. Present but
	// unresolvable degrades to budget 0 -- Num's documented convention, "the
	// card does nothing" -- and takes nothing.
	budget, hasBudget := numResolvedText(h, c, dp.WithTotalCMC, 0)
	if budget < 0 {
		budget = 0
	}
	spec := dp.Spec
	dest := dp.Dest
	optional := dp.Optional
	promptToSkipOptional := dp.PromptToSkipOptional
	// The variant params (see the comment block above the function for what
	// each means and which corpus card carries it).
	revealWin := dp.RevealWin
	noLooking := dp.NoLooking
	forceReveal := dp.ForceReveal
	skipReorder := dp.SkipReorder
	tapped := dp.Tapped
	// FromBottom$ True (task scrybottom): the Dig window is the BOTTOM DigNum
	// cards of the library rather than the top. The Temporal Anchor's
	// "exile that many cards from the bottom of your library" is the corpus
	// carrier (`/usr/bin/grep -rlE 'FromBottom\$'` = 2 files); everything
	// after the window (the primary move, the remainder placement) is
	// unchanged, so a non-FromBottom Dig emits byte-identically.
	fromBottom := dp.FromBottom
	lookText := "looks at the top of the library"
	lookWhere := "top"
	if fromBottom {
		lookText = "looks at the bottom of the library"
		lookWhere = "bottom"
	}
	primaryPos := dp.PrimaryPos
	// Forge's omitted second destination means bottom-of-library remainder.
	// An EXPLICIT DestinationZone2$ Library with no LibraryPosition2$ is the
	// same bottom default: the corpus's older single-position spelling writes
	// `LibraryPosition$ -1` (Squad Rallier, Keldon Flamesage, Kaalia,
	// Winota -- 26 files) and leaves LibraryPosition2$ unset, so without this
	// fallback the remainder stayed on top. Only "-1" (bottom) and "0" (top)
	// appear in the corpus, so a library second destination with no explicit
	// second position is always the bottom. compileDig applies both defaults.
	dest2Name, pos2 := dp.Dest2Name, dp.Pos2
	// bottomRest says the remainder moves to the library bottom (the default,
	// or an explicit Library + LibraryPosition2$ "-1"), so a no-choice tail
	// with two or more untaken cards must record the look before the ordered
	// bottom ask -- the same record the take-ask path makes before ITS ask.
	bottomRest := !skipReorder && strings.EqualFold(strings.TrimSpace(dest2Name), "Library") && pos2 == "-1"
	// RestRandomOrder$ True (task fdn-dig-rest-random-order): the remainder
	// goes to the library bottom in a RANDOM order drawn from the engine's
	// seeded generator, and the controller is NOT asked to order it -- the
	// offered order is not even shown. Drivers: Squad Rallier, Loot,
	// Exuberant Explorer (209 corpus scripts). Absent the param the walk is
	// unchanged, so every pre-existing game replays byte-identically.
	restRandomOrder := dp.RestRandomOrder
	// arrangeThrough is the index of the last target whose ordered-bottom
	// arrange was answered (the answer record already applied its
	// LibraryOrder); every later target keeps the deterministic take, with no
	// take ask of its own.
	arrangeThrough := -1

	// Zone-batch bracket (Mode$ ChangesZoneAll/PhaseOutAll): ONE api:Dig
	// resolution is ONE zone-change action even when it moves several cards
	// (Wild Wasteland's "exile the top two cards of your library"), so the
	// "one or more" batch trigger observes the whole resolution as one batch
	// instead of queueing once per moved card. Every ask inside the walk is
	// answered in place, so the bracket opens here and the deferred close
	// ends it when the walk returns. A stray close is a no-op in
	// closeZoneBatch's depth guard, zoneBatchDepth is reentrant (a Dig
	// inside a RepeatEach ChangeZoneTable$ loop nests inside that loop's own
	// bracket), and a host double without the bracket interface simply queues
	// per move (the batch-of-one pre-fix reading, the mill bracket's shape).
	// The bracket is engine memory folded identically on replay -- no event
	// schema change; the first trigger's queueing position does not move,
	// later matching events stop queueing, so the event stream shrinks by
	// N-1 trigger resolutions for an N-card Dig.
	noteUnreadParams(h, c, "Dig", dp.Unread)
	if b, ok := h.(interface {
		BeginZoneBatch()
		EndZoneBatch()
	}); ok {
		b.BeginZoneBatch()
		defer b.EndZoneBatch()
	}
	g := h.Game()
	players := definedPlayers(h, c, sa)
	selection := *c // IsRemembered in ChangeValid reads the pre-clear set.
	initForgetOtherSnapshot(h, c, sa, players, 2)
	forgetOtherRemembered(h, c, sa)
	// One classification for the whole Dig call, before the target walk: a
	// degrading Attacking$ rider is one Note per dig, not one per taken card
	// (nor one per Defined$ library).
	rider := classifyAttackingEntry(c, sa, dest)
	for targetIndex, p := range players {
		lib := zoneOf(g, state.ZLibrary, p)
		n := digNum
		if int32(len(lib)) < n {
			n = int32(len(lib))
		}
		top := append([]state.ObjID(nil), lib[:n]...)
		if fromBottom {
			top = append([]state.ObjID(nil), lib[int32(len(lib))-n:]...)
		}
		// primaryMoved is the temporary library pile for a primary
		// DestinationZone$ Library move. It is placed after the remainder has
		// settled, so the primary LibraryPosition$ cannot be lost to the
		// remainder's ordered-bottom ask.
		primaryMoved := make([]state.ObjID, 0, len(top))
		// tapeArranged: this target's ordered-bottom arrange was answered,
		// and the KArrange answer record (arrangeAnswerRecord's dig_bottom)
		// already placed the primary pile -- the target is complete.
		tapeArranged := false
		placePrimary := func() {
			if tapeArranged || dest != state.ZLibrary || len(primaryMoved) == 0 {
				return
			}
			switch effDig4c61Codes.Code(string(primaryPos)) {
			case effDig4c61Empty:
				libraryOrderPlacement(h, p, primaryMoved, false)
			case effDig4c611:
				// MoveZone already appends the primary pile at the bottom.
			default:
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
					Text: "LibraryPosition$ " + primaryPos + " is not implemented; the cards sit at the BOTTOM of the library"})
			}
		}
		// take moves one window card to the primary destination, revealing
		// it first when ForceRevealToController$ asks (a public Note naming
		// the card, then the Secret move -- the same reveal-then-secret-move
		// shape effChangeZone's Reveal$ fetch emits; suppressed when Reveal$
		// already made the whole window public) and tapping it right after a
		// Tapped$ True battlefield entry.
		take := func(id state.ObjID) {
			if forceReveal && !revealWin {
				h.Emit(events.Event{Kind: events.Note, Player: p, IDs: []state.ObjID{id}})
			}
			ev := moveZoneEvent(c, id, state.ZLibrary, dest)
			ev.Player, ev.Secret = p, true
			applyFaceDownMarker(h, sa, c, &ev, dest)
			if dest == state.ZLibrary {
				primaryMoved = append(primaryMoved, id)
			}
			// ExileFaceDown$ True with an exile destination (Ugin, the
			// Ineffable's [+1]: "Exile the top card of your library face down
			// and look at it") carries the same face-down exile payload
			// Hideaway's move uses -- the exiling source rides in Amount --
			// so a replay derives FaceDown identically and a projection
			// withholds the card.
			// applyFaceDownMarker preserves ExileFaceDown$'s source-carrying
			// payload (Counter, Amount and nil IDs), while also stamping
			// battlefield FaceDown$ entries.
			h.Emit(ev)
			if dp.Imprint && c.Source != 0 {
				if moved := g.Obj(id); moved != nil && moved.Zone == dest && !moved.IsToken {
					h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: []state.ObjID{id}})
				}
			}
			digRemember(c, dp, id)
			if tapped && dest == state.ZBattlefield {
				h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: p, Text: "entered tapped"})
			}
			rider.apply(h, c, id, p, dest)
			if dest == state.ZBattlefield && dp.GainControl {
				h.Emit(events.Event{Kind: events.ControlChange, Obj: id, Player: c.Controller})
			}
			// StaticEffect$ on a battlefield take (Arbiter of the Ideal's
			// "put it onto the battlefield ... it's an enchantment"): the same
			// rider registration every ChangeZone mover applies.
			if dest == state.ZBattlefield {
				applyStaticEffect(h, c, sa, dest, []state.ObjID{id})
			}
		}
		// rest moves the window cards the primary move did not take to the
		// second destination (DestinationZone2$, placed by LibraryPosition2$).
		// With DestinationZone2$ Library / "-1" -- the omitted default -- the
		// cards go to the bottom, in the answered order when two or more remain
		// (the ask above) and in their existing order otherwise.
		rest := func(ids []state.ObjID) {
			if skipReorder {
				return
			}
			dest2 := ParseZone(dest2Name)
			if dest2 == state.ZLibrary && pos2 == "-1" {
				if len(ids) == 0 {
					return
				}
				// RestRandomOrder$ True: shuffle the remainder itself (the engine's
				// seeded h.Rand, recorded as one LibraryOrder through
				// moveRestToBottom) and pose NO ask -- the controller never chooses
				// or sees the bottom order. A one-card remainder has exactly one
				// order either way, so the shuffle is a no-op there (the helper
				// skips it) and the same branch serves both counts.
				if restRandomOrder {
					moveRestToBottom(h, g, p, ids, true)
					return
				}
				// A one-card remainder has exactly one possible order, so no
				// decision anybody could answer differently is posed -- the same
				// rule the take ask's ChangeNum$ 0 gate applies. It moves to the
				// bottom directly (skipped when it already sits there).
				if len(ids) == 1 {
					moveRestToBottom(h, g, p, ids, false)
					return
				}
				// The ordered-bottom ask: Min == Max == len(ids), so the answer
				// is a full permutation -- every remaining card is placed, and
				// the ANSWER order is the bottom order (the hideaway contract;
				// handleArrange's "dig_bottom" case applies it as
				// untouched-library + answered remainder). ResumeKind
				// "dig_arrange" is this ask's OWN continuation: reusing the
				// take ask's "dig" would feed the answered order back through
				// the "dig" arm as a TAKE answer and re-dig the next window.
				d := &decision.Decision{Player: p, Kind: decision.KArrange, Min: len(ids), Max: len(ids), Source: c.Source,
					ResumeKind: "dig_arrange", ResumeSA: sa, ResumeTarget: targetIndex,
					Prompt:           "Put the remaining cards on the bottom of your library in any order",
					ResumeDigPrimary: append([]state.ObjID(nil), primaryMoved...),
				}
				for i, id := range ids {
					name := "a card"
					if !noLooking || revealWin {
						if o := g.Obj(id); o != nil && o.Face() != nil {
							name = o.Face().Name
						}
					}
					d.Options = append(d.Options, decision.Option{Index: i, Kind: "dig_bottom", Label: name, Obj: id, Player: p})
				}
				if _, ok := AskTapeIntent(h, d); ok {
					// The answer record applied the order; the later targets
					// keep the deterministic take.
					tapeArranged = true
					arrangeThrough = targetIndex
					return
				}

				// R-9 no-host stand-in: the OFFERED order is the bottom order --
				// the exact permutation botpolicy's clamp top-up answers, so the
				// two deterministic readers cannot drift.
				moveRestToBottom(h, g, p, ids, false)
				return
			}
			for _, id := range ids {
				if dest2 == state.ZLibrary {
					if pos2 != "" && pos2 != "0" {
						h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
							Text: "LibraryPosition2$ " + pos2 + " is not implemented; the card stays on top"})
					}
					continue
				}
				if forceReveal && !revealWin {
					h.Emit(events.Event{Kind: events.Note, Player: p, IDs: []state.ObjID{id}})
				}
				ev := moveZoneEvent(c, id, state.ZLibrary, dest2)
				ev.Player, ev.Secret = p, true
				h.Emit(ev)
				digRemember(c, dp, id)
			}
		}
		// takeAnswered applies an answered take for this target: move exactly
		// the answered cards that still sit in the ASKING target's window (a
		// per-window filter keeps a stray answer from moving an object that
		// left the window meanwhile), in the player's answer order; the rest
		// of the window goes to the second destination.
		takeAnswered := func(ans []state.ObjID) {
			picked := make(map[state.ObjID]bool, len(ans))
			for _, id := range ans {
				if !containsID(top, id) {
					continue
				}
				picked[id] = true
				take(id)
			}
			restIDs := make([]state.ObjID, 0, len(top))
			for _, id := range top {
				if !picked[id] {
					restIDs = append(restIDs, id)
				}
			}
			rest(restIDs)
			placePrimary()
		}
		eligible := make([]state.ObjID, 0, len(top))
		for _, id := range top {
			if MatchesSpecCtx(g, spec, id, forgetOtherPreClearContext(&selection, c)) {
				eligible = append(eligible, id)
			}
		}
		// affordable says whether a card may be picked at all under
		// WithTotalCMC$: a card whose own mana value exceeds the budget can
		// never fit, however few are taken.
		affordable := func(id state.ObjID) bool { return !hasBudget || manaValueOf(g, id) <= int(budget) }
		// budgetEligible is the pickable set: spec-matching AND individually
		// affordable (no budget => identical to eligible).
		budgetEligible := eligible
		if hasBudget {
			budgetEligible = make([]state.ObjID, 0, len(eligible))
			for _, id := range eligible {
				if affordable(id) {
					budgetEligible = append(budgetEligible, id)
				}
			}
		}
		// greedy is the deterministic forced take under the cumulative budget:
		// walk budgetEligible in zone order and take each card only while the
		// running sum of mana values still fits. This is the exact take the
		// no-choice tail and the R-9 no-host fallback apply, and it is also
		// what decides whether a choice exists (below).
		greedy := make([]state.ObjID, 0, len(budgetEligible))
		running := 0
		for _, id := range budgetEligible {
			if int32(len(greedy)) >= changeNum {
				break
			}
			mv := manaValueOf(g, id)
			if hasBudget && running+mv > int(budget) {
				continue
			}
			running += mv
			greedy = append(greedy, id)
		}
		// forcedAll says the forced greedy take consumes every budget-eligible
		// card, so the answer cannot differ from it and no ask is warranted.
		forcedAll := len(greedy) == len(budgetEligible)
		askBudget := hasBudget && len(budgetEligible) > 0 && !forcedAll
		// The ask gate: every target past arrangeThrough keeps the
		// deterministic take.
		optionalChoice := (optional || promptToSkipOptional) && len(budgetEligible) > 0 && changeNum > 0
		takeChoice := int32(len(budgetEligible)) > changeNum || anyNum && len(budgetEligible) > 0
		chooser := p
		if rawChooser := dp.Choser; rawChooser != "" {
			if cp, ok := chooserPlayer(h, c, rawChooser); ok {
				chooser = cp
			}
		}
		if arrangeThrough < 0 && changeNum > 0 && (takeChoice || optionalChoice || askBudget) {
			// A real choice: record the look, then ask the library's owner.
			// Reveal$ True makes the record a PUBLIC reveal of the window (the
			// same non-Secret ids-Note shape effReveal's public arm emits);
			// otherwise the look stays private to the library's owner.
			if revealWin {
				h.Emit(events.Event{Kind: events.Note, Player: p, IDs: top})
			} else if !noLooking {
				emitLook(h, []state.PlayerID{p}, state.ZLibrary, top, lookText)
			}
			minv := int32(0)
			if !optional && !promptToSkipOptional && !anyNum {
				minv = changeNum
			}
			// A mandatory budget dig whose changeNum exceeds what the budget
			// affords must not demand more picks than it can pay for: lower the
			// Min to the forced affordable count so the ask can be satisfied.
			// (Corpus carriers are all Min 0; this is general-correctness code.)
			if hasBudget && minv > int32(len(greedy)) {
				minv = int32(len(greedy))
			}
			maxv := int(changeNum)
			if maxv > len(budgetEligible) {
				maxv = len(budgetEligible)
			}
			verb := "you may put up to "
			if !optional && !promptToSkipOptional && !anyNum {
				verb = "put "
			}
			look := "Look at the " + lookWhere + " " + strconv.Itoa(int(n)) + " card(s) of your library"
			if noLooking && !revealWin {
				look = "Choose from the " + lookWhere + " " + strconv.Itoa(int(n)) + " card(s) of your library"
			}
			// A self-contained library move (Jace, the Mind Sculptor's "you
			// may put it on the bottom" shape) is represented by an optional
			// KChoose.  "Choose" alone is ambiguous here: the selected card
			// moves to the bottom, while an unselected card does not move.  Say
			// both sides of that choice in the prompt and on each option.
			bottomChoice := dest == state.ZLibrary && primaryPos == "-1" && skipReorder
			prompt := look + ": " + verb + strconv.Itoa(int(changeNum)) + " matching card(s) into " + digDestPhrase(dest)
			if bottomChoice {
				selectVerb := "Select up to "
				if !optional && !promptToSkipOptional && !anyNum {
					selectVerb = "Select "
				}
				prompt = look + ": " + selectVerb + strconv.Itoa(int(changeNum)) + " matching card(s) to put on the bottom of your library. Leave unselected card(s) on top."
			}
			if hasBudget {
				prompt += " (total mana value " + strconv.Itoa(int(budget)) + " or less)"
			}
			d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
				Min:          int(minv),
				Max:          maxv,
				MaxSum:       int(budget),
				Source:       c.Source,
				ResumeKind:   "dig",
				ResumeSA:     sa,
				ResumeTarget: targetIndex,
				Prompt:       prompt,
			}
			for _, id := range budgetEligible {
				name := "a card"
				if !noLooking || revealWin {
					if o := g.Obj(id); o != nil && o.Face() != nil {
						name = o.Face().Name
					}
				}
				label := name
				if bottomChoice {
					label = "Put " + name + " on bottom"
				}
				opt := decision.Option{Index: len(d.Options),
					Kind: "dig", Label: label, Obj: id, Player: p}
				// Only a budget Dig carries a Value: Option.Value is
				// omitempty, and setting it on a budget-less Dig would put a
				// "value" field on the wire for every offered card although
				// MaxSum is 0 and nothing reads it. Keeping it budget-only
				// leaves every existing (non-budget) option list serialising
				// byte-identically.
				if hasBudget {
					opt.Value = manaValueOf(g, id)
				}
				d.Options = append(d.Options, opt)
			}
			if ans, ok := AskTape(h, d); ok {
				// An empty answer to the optional-ability election declines
				// the whole Dig for this target: the looked-at window stays in
				// place, with no remainder, reveal or destination side effects.
				picks := answerObjs(ans)
				if promptToSkipOptional && len(picks) == 0 {
					continue
				}
				takeAnswered(picks)
				continue
			}

			// Fuzz/no-engine host: the deterministic stand-in (R-9) takes the
			// greedy affordable set -- the exact mirror of the budget-aware bot
			// arm -- with the Note that records why the richer path did not run.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
				Text: "takes the first matching card(s) (no engine host to ask)", Secret: true})
			taken := make(map[state.ObjID]bool, len(greedy))
			for _, id := range greedy {
				take(id)
				taken[id] = true
			}
			restIDs := make([]state.ObjID, 0, len(top))
			for _, id := range top {
				if !taken[id] {
					restIDs = append(restIDs, id)
				}
			}
			rest(restIDs)
			placePrimary()
			continue
		}
		// No take decision to ask about (eligible <= ChangeNum): the M1 silent
		// TAKE runs, but the tail may still act -- a Reveal$ window reveal, a
		// Tapped$ Tap, a look Note ahead of an ordered-bottom ask, or a
		// remainder move can each add an event, and a default-remainder card
		// emits one. The forced greedy take moves in window order while the
		// cumulative budget (WithTotalCMC$) allows; everything else in the
		// window -- unmatched, over-budget and beyond the cap alike -- goes to
		// the second destination (by default the bottom, ordered) or stays
		// exactly where it is (SkipReorder$, or a top LibraryPosition2$).
		if revealWin && len(top) > 0 {
			h.Emit(events.Event{Kind: events.Note, Player: p, IDs: top})
		}
		if bottomRest && !revealWin && !noLooking && int32(len(top)-len(greedy)) >= 2 {
			// The ordered-bottom ask is coming: the look that authorises it is
			// recorded here, the same Secret owner's Note the take-ask path
			// emits before ITS ask (a Reveal$ window is already public).
			emitLook(h, []state.PlayerID{p}, state.ZLibrary, top, lookText)
		}
		taken := make(map[state.ObjID]bool, len(greedy))
		for _, id := range greedy {
			take(id)
			taken[id] = true
		}
		restIDs := make([]state.ObjID, 0, len(top))
		for _, id := range top {
			if !taken[id] {
				restIDs = append(restIDs, id)
			}
		}
		rest(restIDs)
		placePrimary()
	}
	// The walk completed: release the ride (the same boundary the search and
	// hidden walks end at), so a later ability in the chain cannot inherit it.
	endForgetOtherSnapshot(c)
}

// digRemember honours a Dig's RememberChanged$ True: each card the dig moved
// joins the resolution's Remembered, where a chained SubAbility$ reads it --
// Atsushi's DBEffect RememberObjects$ RememberedCard seeds the registered
// may-play grant's Remembered from exactly this list. Absent the parameter
// (the corpus default) the walk adds nothing, so every pre-existing game
// replays byte-identically.
func digRemember(c *Ctx, dp *DigParams, id state.ObjID) {
	if dp.RememberChanged {
		c.Remembered = append(c.Remembered, state.Target{Obj: id})
	}
}

const (
	effDig4c61Empty uint16 = 1 // "", "0"
	effDig4c611     uint16 = 2 // "-1"
)

var effDig4c61Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "", Val: effDig4c61Empty},
	state.StrEntry[uint16]{Key: "0", Val: effDig4c61Empty},
	state.StrEntry[uint16]{Key: "-1", Val: effDig4c611},
)
