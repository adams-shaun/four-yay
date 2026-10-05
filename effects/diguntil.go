package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effDigUntil implements Forge's reveal-until search (DigUntilEffect; task
// diguntil1) — a DIFFERENT primitive from Dig, with its own param family:
// reveal cards from the top of Defined$'s library (default: the resolving
// controller) in zone order until one matches Valid$, publicly reveal every
// card turned over INCLUDING the found one (the same non-Secret ids-Note
// shape effDig's Reveal$ arm emits), move the found card(s) to
// FoundDestination$ (absent: the found card stays part of the revealed pile
// and goes wherever it goes -- Forge's reading, see foundWithRevealed) and
// the revealed rest to
// RevealedDestination$ (default Library; RevealedLibraryPosition$ "-1" =
// bottom via Move's zone append, "0"/absent = the stay-in-place default so
// no event). The cards AFTER the found card stay in the library untouched —
// the scan stops at the amount-th match, unlike Dig's fixed window.
//
// OptionalFoundMove$ True (4 corpus lines — Songbirds' Blessing) makes the
// found move a real yes/no ask to the library's owner (the attach_optional
// KChoose shape): "yes" moves to FoundDestination$, "no" — the decline —
// to OptionalNoDestination$ when the SA carries one, else the found card
// JOINS the revealed pile (the corpus oracles all say "then put all cards
// revealed this way that weren't put onto the battlefield on the bottom":
// Genesis Storm, Hei Bai, Aurora Awakener). No answer declines
// deterministically (R-9); botpolicy's clamp fallback answers option 0 =
// "yes". RevealRandomOrder$ True (54 corpus lines) shuffles the pile's
// RETURN order to the bottom of the library through the engine's seeded
// generator (h.Rand), so it replays exactly; the public reveal Note and the
// Remembered capture stay in scan order because reveal order is a reveal-time
// fact. A stay-in-place placement (RevealedLibraryPosition$ "0"/absent, e.g.
// Indomitable Creativity) cannot express a random order at all, so it keeps
// the existing order behind one loud Note.
//
// Riders implemented: RememberFound$ / RememberRevealed$ (the ctx-level
// Remembered discipline digRemember uses), Tapped$ (the MoveZone-then-Tap
// pair effDig's battlefield take emits) and GainControl$ (the found
// permanent enters under the resolving controller via the ordinary
// ControlChange event). A found card with an AURA face put onto the
// battlefield gets the CR 303.4f non-cast-entry attach, degraded to the
// deterministic stand-in: the first permanent in the controller's
// battlefield zone order that satisfies the Enchant keyword's own spec. If
// more than one permanent qualifies, the controller chooses the bearer; a
// sole candidate is taken without an answer. With NO eligible bearer the Aura
// stays in the library (CR 303.4f's remain-in-current-zone) rather than
// entering unattached and dying to the CR 704.5m SBA. Riders withheld with
// one loud Note per parameter and the core move still running: a non-literal
// Amount$ token is resolved as an SVar name through the count evaluator
// (literals 1..5 ARE honoured as "keep revealing until N matches", literal 0
// means the scan reveals nothing); Shuffle$ True shuffles the dug library
// after the moves (ShuffleCondition$ NoneFound restricts it to a scan that
// found nothing); NoMoveFound$ True leaves the found card in the library;
// FoundLibraryPosition$ places a library-destination found card at the bottom
// ("-1") or leaves it on top ("0"/absent, no event); ImprintFound$ /
// ImprintRevealed$ record the found / all-revealed cards on the source's
// imprint association (the Seek "seek-found" list); NoneFoundDestination$ /
// NoneFoundLibraryPosition$ give the nothing-found branch its own
// destination. What remains withheld: Amount$ whose SVar is absent or
// unresolvable (amount 1 then) and DigZone$ (every corpus value is
// PlanarDeck, and this build has no planar tier). RevealRandomOrder$ True is
// implemented for the library-bottom return (h.Rand, seeded and replay-exact);
// a stay-in-place placement keeps the existing order behind one loud Note.
func effDigUntil(h Host, c *Ctx, sa *cards.SA) {
	dp := DigUntilOf(sa)
	spec := dp.Spec
	revDest := dp.RevDest
	// An absent FoundDestination$ is Forge's "the found card is one of the
	// revealed cards": it goes wherever the revealed pile goes -- the
	// RevealedDestination$ (at RevealedLibraryPosition$, in the pile's random
	// order), or nowhere under NoMoveRevealed$ True. Measured at the
	// FORGE_REF pin, 25 corpus DigUntil lines carry no FoundDestination$ and
	// every one's Oracle text agrees: the 18 with a RevealedDestination$ put
	// "those cards" / "all cards revealed this way" -- the found land or
	// creature included -- into the graveyard (Consuming Aberration,
	// Balustrade Spy, Mind Funeral, Undercity Informer), onto the bottom
	// (Goblin Charbelcher, Erratic Explosion, Amplifire) or into the hand
	// (Treasure Hunt), and the 7 with neither (NoMoveRevealed$: Treasure
	// Keeper, Spellshift, Plargg, The Crimson Avenger, Underdark Beholder,
	// Neera, Dance, Pathetic Marionette) leave the found card in the library
	// for the chained cast/choose that reads it as Remembered. The old Hand
	// default handed the library's owner a free card in all 25.
	foundWithRevealed := dp.FoundWithRevealed
	foundDest := dp.FoundDest
	revPos := dp.RevPos
	optionalMove := dp.OptionalMove
	noMoveRevealed := dp.NoMoveRevealed
	revealRandomOrder := dp.RevealRandomOrder
	tapped := dp.Tapped
	gainControl := dp.GainControl
	rememberFound := dp.RememberFound
	rememberRevealed := dp.RememberRevealed
	amount := int32(1)
	// amountWithheld is the one rider decided at resolution: a non-literal
	// Amount$ token whose SVar is absent or unresolvable. It is noted ahead
	// of the compiled withheld list (dp.Withheld), the historical order.
	amountWithheld := ""
	if raw := dp.AmountRaw; raw != "" && raw != "1" {
		if dp.AmountLit > 0 {
			amount = dp.AmountLit
		} else if n, resolved := digUntilAmountSVar(h, c, raw); resolved {
			// A non-literal token names an SVar (X/MassX/Y/VoteNum); its body
			// is read by the count evaluator, the same read effDig's DigNum$ X
			// arm uses. A zero tally is legitimate (Selvala's Stampede with no
			// wild vote reveals nothing).
			amount = n
		} else {
			// Absent or unresolvable SVar: fail-safe to amount 1, and keep the
			// loud-unimplemented Note contract.
			amountWithheld = "Amount$ " + raw
		}
	}
	// DigZone$ stays withheld (dp.Withheld): every corpus value is
	// PlanarDeck, and this build has no planar deck or planar zone (the
	// planechase approximation). The ordinary library scan remains the
	// deterministic core move.
	noMoveFound := dp.NoMoveFound
	shuffle := dp.Shuffle
	shuffleNoneFound := dp.ShuffleNoneFound
	// MinTotalCMC$ N (Dream Harvest, Improvisation Capstone, Tasha's Hideous
	// Laughter): reveal until the MATCHING cards' cumulative mana value
	// reaches N, instead of stopping at Amount$ matches. Zero (absent or an
	// unresolvable value) keeps the ordinary Amount$-bounded scan.
	minTotalCMC := int32(0)
	if dp.MinTotalCMC.Present {
		minTotalCMC = numText(h, c, dp.MinTotalCMC, 0)
	}
	imprintFound := dp.ImprintFound
	imprintRevealed := dp.ImprintRevealed
	foundPos := dp.FoundPos
	// NoneFound* is the nothing-found branch (Tunnel Vision carries both):
	// when the scan finds nothing its revealed cards go to
	// NoneFoundDestination$ at NoneFoundLibraryPosition$ instead of the
	// RevealedDestination$/RevealedLibraryPosition$ pair. Absent keys leave the
	// ordinary revealed destination in force.
	noneFoundSet := dp.NoneFoundSet
	noneFoundDest := dp.NoneFoundDest
	noneFoundPos := dp.NoneFoundPos
	// The found-move election: asked once (for the first player whose walk
	// reaches it) and then governing every later player's walk; moveDone
	// also suppresses the reveal Note of every player after that ask.
	moveAns := "no"
	moveDone := false
	g := h.Game()
	noteUnreadParams(h, c, "DigUntil", dp.Unread)
	if amountWithheld != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "DigUntil withholds " + amountWithheld + "; the core move runs without it"})
	}
	for _, param := range dp.Withheld {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "DigUntil withholds " + param + "; the core move runs without it"})
	}
	targets := DefinedRef(h, c, dp.Defined, sa)
	if dp.Defined.Raw == "" && !TargetsOf(sa).Targeted() {
		// Forge's default for a reveal-until with no Defined$ and no targets:
		// the resolving controller's own library (Songbirds' Blessing's
		// trigger). Defined's source-object fallback is wrong here — the
		// source is the resolving permanent, not a player.
		targets = []state.Target{{Player: c.Controller, IsPlayer: true}}
	}
	// DigZone$ is withheld above. The ordinary library scan remains the
	// deterministic core move even for the PlanarDeck carriers; their full
	// planar-zone semantics are outside this primitive.
	// declineDest is where the found card goes when the optional move is
	// declined: OptionalNoDestination$ when the SA carries one, else the
	// revealed pile (the corpus oracles' "put all cards revealed this way
	// that weren't put onto the battlefield ...").
	declineDest := dp.DeclineDest
	// One classification for the whole DigUntil call, before the player walk.
	// The rider is only ever delivered on a battlefield entry (apply gates on
	// the destination it is handed), so it is classified against that zone
	// rather than against either of the two destinations the walk picks
	// between.
	rider := classifyAttackingEntry(c, sa, state.ZBattlefield)
	players := playerIDsFromTargets(h, c, dp.Defined.Raw, targets)
	selection := *c // Valid$ Card.IsRemembered uses the pre-clear set.
	// The snapshot arms for any player count, a single library included.
	initForgetOtherSnapshot(h, c, sa, players, 1)
	forgetOtherRemembered(h, c, sa)
	// RememberFound$ replaces the resolution's Remembered set with found
	// cards, or with all revealed cards when RememberRevealed$ is also set.
	// Trigger referents remain in Ctx.Captured. Accumulate across the
	// player walk so a later player's reveal does not erase earlier ones.
	var digRemembered []state.Target
	var imprintObjs []state.ObjID
	for _, p := range players {
		lib := zoneOf(g, state.ZLibrary, p)
		if len(lib) == 0 {
			continue
		}
		var revealed, found []state.ObjID
		// A zero Amount$ (an SVar tally of 0) reveals nothing: the loop's own
		// `len(found) >= amount` would otherwise stop on the first card.
		if amount > 0 {
			cmcSum := int32(0)
			for _, id := range lib {
				revealed = append(revealed, id)
				if MatchesSpecCtx(g, spec, id, forgetOtherPreClearContext(&selection, c)) {
					found = append(found, id)
					if minTotalCMC > 0 {
						// MinTotalCMC$: keep revealing until the matching cards'
						// cumulative mana value reaches the threshold (XMage's
						// loop and the Oracles' "until they have exiled cards
						// with total mana value 5 or greater this way").
						if o := g.Obj(id); o != nil && o.Face() != nil {
							cmcSum += o.Face().ManaValue()
						}
						if cmcSum >= minTotalCMC {
							break
						}
						continue
					}
					if int32(len(found)) >= amount {
						break
					}
				}
			}
		}
		// The reveal is PUBLIC (the same non-Secret ids-Note effDig's Reveal$
		// arm emits), recorded before the ask; it is not emitted once the
		// found-move election has been answered.
		if !moveDone && len(revealed) > 0 {
			h.Emit(events.Event{Kind: events.Note, Player: p, IDs: revealed})
		}
		if optionalMove && !moveDone {
			verb := digDestPhrase(foundDest)
			d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
				Source:     c.Source,
				ResumeKind: "diguntil_move", ResumeSA: sa,
				Prompt: "Put the revealed matching card(s) onto " + verb + "?",
				Options: []decision.Option{
					{Index: 0, Kind: "yes", Label: "Yes — put into " + verb, Player: p},
					{Index: 1, Kind: "no", Label: "No", Player: p},
				}}
			if ans, ok := AskTape(h, d); ok {
				// The answer governs this walk from here on.
				moveDone = true
				moveAns = "no"
				if answerYes(ans) {
					moveAns = "yes"
				}
			} else {
				// No answer: the deterministic decline (R-9) — the found
				// card(s) join the decline destination.
				moveDone = true
				moveAns = "no"
			}
		}
		switch {
		case rememberRevealed:
			// The revealed set already includes every found card. Alone,
			// RememberRevealed$ retains its append semantics; paired with
			// RememberFound$ it replaces the trigger capture at the end.
			for _, id := range revealed {
				if rememberFound {
					digRemembered = append(digRemembered, state.Target{Obj: id})
				} else {
					c.Remembered = append(c.Remembered, state.Target{Obj: id})
				}
			}
		case rememberFound:
			for _, id := range found {
				digRemembered = append(digRemembered, state.Target{Obj: id})
			}
		}
		foundJoinedRevealed := false
		if len(found) > 0 {
			dest := foundDest
			toMove := found
			if noMoveFound {
				// NoMoveFound$ True: the found card is not moved to its
				// destination. Its effective destination is the library, so a
				// FoundLibraryPosition$ still places it within the library.
				dest = state.ZLibrary
			}
			if optionalMove && !noMoveFound && moveAns != "yes" {
				dest = declineDest
				foundJoinedRevealed = dest == revDest
			}
			if foundWithRevealed && !noMoveFound {
				// No FoundDestination$: the found card(s) travel with the
				// revealed pile below (see foundWithRevealed above), so the
				// pile's NoMoveRevealed$, position and random order apply and
				// there is no separate found move.
				foundJoinedRevealed = true
				toMove = nil
			}
			for _, id := range toMove {
				if dest == state.ZBattlefield {
					// An AURA face put onto the battlefield by a non-cast effect
					// never got a cast-time target. With NO eligible bearer the
					// card stays in the library (CR 303.4f's
					// remain-in-current-zone) instead of entering unattached and
					// dying to the CR 704.5m SBA.
					bearer, isAuraFace := auraEntryBearer(g, id, p)
					if isAuraFace {
						bearers, _ := auraEntryBearers(g, id, p)
						switch {
						case len(bearers) == 0:
							bearer = 0
						case len(bearers) == 1:
							bearer = bearers[0]
						default:
							d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
								Source: c.Source, ResumeKind: "diguntil_aura", ResumeSA: sa,
								Prompt: "Choose a permanent for the revealed Aura to enchant"}
							for i, candidate := range bearers {
								d.Options = append(d.Options, decision.Option{Index: i, Kind: "card", Obj: candidate, Player: p})
							}
							if ans, ok := AskTape(h, d); ok {
								// The answered bearer, revalidated against
								// the current battlefield.
								answered := state.ObjID(0)
								if len(ans) > 0 {
									answered = ans[0].Obj
								}
								bearer = auraAnsweredBearer(bearers, answered)
							} else {
								// R-9: a host without an answer takes the
								// deterministic first candidate.
								bearer = bearers[0]
							}
						}
					}
					if isAuraFace && bearer == 0 {
						h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
							Text: "no permanent the revealed Aura can enchant; it stays in the library (CR 303.4f)"})
						continue
					}
					ev := moveZoneEvent(c, id, state.ZLibrary, dest)
					ev.Player, ev.Secret = p, true
					if bearer != 0 {
						// The revealed Aura's bearer is this effect's own
						// (selected above): the marked entry attaches it as
						// the Aura enters, with no second CR 303.4f choice.
						events.MarkNamedAttachEntry(&ev, []state.ObjID{bearer})
					}
					h.Emit(ev)
					if tapped {
						h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: p, Text: "entered tapped"})
					}
					rider.apply(h, c, id, p, dest)
					if gainControl {
						h.Emit(events.Event{Kind: events.ControlChange, Obj: id, Player: c.Controller})
					}
					if o := g.Obj(id); bearer != 0 && o != nil && o.Zone == state.ZBattlefield && o.AttachedTo == 0 {
						// CR 303.4f: the selected permanent is the Aura's
						// chosen bearer on this non-cast battlefield entry
						// (a host whose entry did not settle it).
						emitAttach(h, id, bearer)
					}
					// StaticEffect$ on a DigUntil battlefield take: the same
					// rider registration every ChangeZone mover applies (no
					// corpus carrier rides a DigUntil today; hooked so the
					// class cannot miss one).
					applyStaticEffect(h, c, sa, dest, []state.ObjID{id})
					continue
				}
				if dest == state.ZLibrary {
					// The found card's destination IS the library (an explicit
					// FoundDestination$ Library, or NoMoveFound$ True).
					// FoundLibraryPosition$ "-1" puts it on the bottom via a real
					// library-to-library move (Move's zone append — the same
					// contract the revealed-rest "-1" arm below uses); "0"/absent
					// is the stay-in-place top default, so no event.
					if foundPos == "-1" {
						h.Emit(events.Event{Kind: events.MoveZone, Obj: id,
							From: state.ZLibrary, To: state.ZLibrary, Player: p, Secret: true})
					}
					continue
				}
				ev := moveZoneEvent(c, id, state.ZLibrary, dest)
				ev.Player, ev.Secret = p, true
				h.Emit(ev)
			}
		}
		// The revealed rest (plus a found card whose decline joined the pile)
		// move to RevealedDestination$; when the scan found NOTHING and the SA
		// carries a NoneFound* key they move to NoneFoundDestination$ at
		// NoneFoundLibraryPosition$ instead. NoMoveRevealed$ True (8 corpus
		// lines) leaves them where they are.
		restDest, restPos := revDest, revPos
		if len(found) == 0 && noneFoundSet {
			restDest, restPos = noneFoundDest, noneFoundPos
		}
		if !noMoveRevealed {
			// The rest is the revealed pile minus any found card that really
			// left the pile; a library-bottom random return shuffles exactly
			// THIS list (the order the per-card Secret MoveZone events are
			// emitted in IS the returned bottom order — zone append lands each
			// card at the bottom in emit order). Reveal order is NOT shuffled:
			// the public Note and the Remembered capture above stay in scan
			// order.
			toReturn := make([]state.ObjID, 0, len(revealed))
			for _, id := range revealed {
				isFound := false
				for _, fid := range found {
					if fid == id {
						isFound = true
						break
					}
				}
				if isFound && !foundJoinedRevealed {
					continue
				}
				toReturn = append(toReturn, id)
			}
			if restDest == state.ZLibrary && revealRandomOrder {
				switch {
				case restPos == "-1":
					// A full Fisher-Yates over the return list (the h.Rand idiom
					// the random pick/discard arms use) draws once per position,
					// so the seeded generator replays byte-identically. This is
					// the engine's seeded randomness, not a library shuffle:
					// T:Mode$ Shuffled triggers must not fire for a bottom return
					// that merely happens to be random.
					for i := 0; i < len(toReturn); i++ {
						j := i + h.Rand(len(toReturn)-i)
						toReturn[i], toReturn[j] = toReturn[j], toReturn[i]
					}
				case restPos == "" || restPos == "0":
					// Stay-in-place placement keeps the existing order (no
					// library randomisation is expressible there); name the
					// limitation once.
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
						Text: "RevealRandomOrder$ with stay-in-place placement keeps existing order"})
				}
			}
			for _, id := range toReturn {
				if restDest == state.ZLibrary {
					// Library placement: "-1" (bottom) is a real
					// library-to-library move (Move's zone append lands it at the
					// bottom — the exact contract effDig's LibraryPosition2$ "-1"
					// arm documents); "0"/absent is the engine's stay-in-place
					// default (the cards already sit on top in their existing
					// relative order) so no event; anything else is named loudly
					// and the card stays.
					if restPos == "-1" {
						h.Emit(events.Event{Kind: events.MoveZone, Obj: id,
							From: state.ZLibrary, To: state.ZLibrary, Player: p, Secret: true})
					} else if restPos != "" && restPos != "0" {
						h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
							Text: "RevealedLibraryPosition$ " + restPos + " is not implemented; the card stays on top"})
					}
					continue
				}
				ev := moveZoneEvent(c, id, state.ZLibrary, restDest)
				ev.Player, ev.Secret = p, true
				h.Emit(ev)
			}
		}
		// Shuffle$ True: after the found move and the revealed-rest moves,
		// shuffle the dug player's library (the same Fisher-Yates +
		// Secret events.Shuffle contract effShuffle emits). ShuffleCondition$
		// NoneFound restricts it to a scan that found nothing.
		if shuffle && (!shuffleNoneFound || len(found) == 0) {
			order := h.ShuffleLibrary(p, g.Zone(state.ZLibrary, p))
			h.Emit(events.Event{Kind: events.Shuffle, Player: p, IDs: order, Secret: true})
		}
		// ImprintFound$/ImprintRevealed$ (Forge's addImprintedLists) are
		// accumulated across the player walk and emitted once after it.
		if imprintFound && len(found) > 0 {
			imprintObjs = append(imprintObjs, found...)
		}
		if imprintRevealed {
			imprintObjs = append(imprintObjs, revealed...)
		}
	}
	// The walk completed: release the ride (the same boundary the search and
	// hidden walks end at), so a later ability in the chain cannot inherit it.
	endForgetOtherSnapshot(c)
	if len(imprintObjs) > 0 && c.Source != 0 {
		// Forge's addImprintedLists links the found (or all revealed) cards to
		// the resolving source. It rides the Seek "seek-found" list, not the
		// ordinary Imprinted one: DigUntil's continuation readers (Defined$
		// Imprinted, Card.IsImprinted) read the association wherever the card
		// currently sits — library, battlefield — where the ordinary list's CR
		// 607.2a exile-only filter would hide it.
		h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source,
			IDs: append([]state.ObjID(nil), imprintObjs...), Text: "seek-found"})
	}
	if rememberFound {
		c.Remembered = digRemembered
	}
}

// auraAnsweredBearer revalidates an answered DigUntil Aura bearer against the
// current eligible bearers: the answer if it is still one of them, else 0
// (no bearer -- the Aura stays in the library).
func auraAnsweredBearer(bearers []state.ObjID, answered state.ObjID) state.ObjID {
	for _, candidate := range bearers {
		if candidate == answered {
			return candidate
		}
	}
	return 0
}

// digUntilAmountSVar resolves a non-literal Amount$ token as an SVar name
// (empty_the_laboratory's Y, kindred_summons' X, mass_polymorph's MassX,
// selvalas_stampede's runtime VoteNum). The body is evaluated with the count
// evaluator -- the same read effDig's DigNum$ X arm uses; an absent SVar table
// or name, or an unmodelled count head, reports not-resolved so the caller
// keeps its fail-safe amount 1.
func digUntilAmountSVar(h Host, c *Ctx, token string) (int32, bool) {
	if c == nil || c.SVars == nil {
		return 0, false
	}
	body, ok := c.SVars[token]
	if !ok {
		return 0, false
	}
	return EvalCountOK(h, c, body)
}

// auraEntryBearer resolves a non-cast battlefield entry's Aura bearer: the
// first permanent in seat order, then battlefield order, that satisfies the
// face's Enchant keyword spec (or any permanent when the face
// carries no Enchant keyword — nothing in the corpus prints one, the same
// convention rules/attach.go's auraStillMatchesEnchant uses). aura is false
// when the face is not an Aura (no attach needed); aura && bearer == 0
// means the face IS an Aura but no eligible bearer exists. The filter read
// is the shared MatchesSpecFrom, so a compound spec (Enchant:
// Creature.YouCtrl) evaluates exactly like an attach-time legality check.
func auraEntryBearer(g *state.Game, id state.ObjID, p state.PlayerID) (state.ObjID, bool) {
	bearers, isAura := auraEntryBearers(g, id, p)
	if len(bearers) > 0 {
		return bearers[0], true
	}
	return 0, isAura
}

func auraEntryBearers(g *state.Game, id state.ObjID, p state.PlayerID) ([]state.ObjID, bool) {
	o := g.Obj(id)
	if o == nil || o.Face() == nil {
		return nil, false
	}
	isAura := false
	for _, t := range o.Face().Types {
		if strings.EqualFold(t, "Aura") {
			isAura = true
			break
		}
	}
	if !isAura {
		return nil, false
	}
	spec := "Permanent"
	if param, ok := o.Face().KeywordParam("Enchant"); ok && strings.TrimSpace(param) != "" {
		spec, _, _ = strings.Cut(param, ":")
	}
	spec = strings.TrimSpace(spec)
	var bearers []state.ObjID
	for seat := range g.Players {
		for _, bid := range g.Zone(state.ZBattlefield, state.PlayerID(seat)) {
			if bid == id {
				continue
			}
			if MatchesSpecFrom(g, spec, bid, p, id) {
				bearers = append(bearers, bid)
			}
		}
	}
	return bearers, true
}
