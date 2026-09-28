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
	Register("ChangeZone", effChangeZone)
	Register("ChangeZoneAll", effChangeZoneAll)
	Register("Destroy", effDestroy)
	Register("DestroyAll", effDestroyAll)
	Register("Sacrifice", effSacrifice)
	Register("Manifest", effManifest)
	Register("ManifestDread", effManifestDread)
	Register("Cloak", effCloak)
	Register("Seek", effSeek)
}

// effSeek implements Alchemy's random library-to-hand seek. Unlike a hidden
// library search, seek neither reveals nor shuffles: it samples the eligible
// pool without replacement using the host's seeded RNG.
func effSeek(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	// RememberFound$ makes the found card(s) the resolution's Remembered set
	// (Forge's SeekEffect rememberFound), REPLACING whatever the resolution
	// started with -- the same rule the DigUntil fix (c1d996d4) landed for
	// DB$ DigUntil. A triggered resolution's ctx Remembered already carries
	// the trigger's captured referent (Goblin Trapfinder's own dying card),
	// so appending would make a chained Defined$ Remembered act on it too.
	// The trigger referents survive in Ctx.Captured, the separate channel.
	// Accumulate across the multi-player walk and assign once, so a second
	// player's found cards do not clobber the first's.
	rememberFound := strings.EqualFold(strings.TrimSpace(sa.Params["RememberFound"]), "True")
	var seekRemembered []state.Target
	players := Defined(h, c, sa)
	if sa.Params["Defined"] == "" {
		players = []state.Target{{Player: c.Controller, IsPlayer: true}}
	}
	for _, target := range players {
		if !target.IsPlayer || int(target.Player) >= len(g.Players) {
			continue
		}
		owner := target.Player
		pool := zoneOf(g, state.ZLibrary, owner)
		if raw := strings.TrimSpace(sa.Params["DefinedCards"]); raw != "" {
			if raw != "Top_10_OfLibrary" {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "Seek withholds DefinedCards$ " + raw + "; no cards moved"})
				continue
			}
			if len(pool) > 10 {
				pool = pool[:10]
			}
		}

		spec := strings.TrimSpace(sa.Params["Type"])
		if spec == "" {
			spec = "Card"
		}
		types := strings.Split(strings.TrimSpace(sa.Params["Types"]), ",")
		if strings.TrimSpace(sa.Params["Types"]) == "" {
			types = []string{spec}
		}
		selected := make([]state.ObjID, 0)
		used := make(map[state.ObjID]bool)
		for _, typeSpec := range types {
			typeSpec = permanentCardSpec(strings.TrimSpace(typeSpec))
			eligible := make([]state.ObjID, 0, len(pool))
			for _, id := range pool {
				if used[id] || !MatchesSpecCtx(g, typeSpec, id, c.SpecContext(c.Controller)) {
					continue
				}
				eligible = append(eligible, id)
			}
			n := int32(1)
			if len(types) == 1 {
				n = Num(h, c, sa, "Num", 1)
			}
			if n < 0 {
				n = 0
			}
			if n > int32(len(eligible)) {
				n = int32(len(eligible))
			}
			for i := int32(0); i < n; i++ {
				j := h.Rand(len(eligible))
				id := eligible[j]
				selected = append(selected, id)
				used[id] = true
				eligible = append(eligible[:j], eligible[j+1:]...)
			}
		}
		if len(selected) == 0 {
			continue
		}
		for _, id := range selected {
			h.Emit(moveZoneEvent(c, id, state.ZLibrary, state.ZHand))
			if rememberFound {
				seekRemembered = append(seekRemembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
		}
		if strings.EqualFold(strings.TrimSpace(sa.Params["ImprintFound"]), "True") && c.Source != 0 {
			// ImprintFound$ is Forge's seek imprint (SeekEffect writes
			// imprintedCards). It rides the separate SeekFound list -- not the
			// ordinary Imprinted one -- because the found cards sit in a hand
			// at continuation time, where `Defined$ Imprinted`'s CR 607.2a
			// exiled-only reader would hide them; a chained Origin$ Hand body
			// (Spawning Pod, Gitrog, Kardum, Puppet Raiser) reads them here.
			h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: append([]state.ObjID(nil), selected...), Text: "seek-found"})
		}
		h.Emit(events.Event{Kind: events.Seek, Player: owner, Obj: c.Source})
	}
	if rememberFound {
		c.Remembered = seekRemembered
	}
}

// ParseZone maps a Forge zone name to a state.Zone. Unknown names resolve to
// the graveyard, which is where the overwhelming majority of movement goes
// and is a safe default for an unmodelled destination.
func ParseZone(s string) state.Zone {
	z, ok := parseZone(s)
	if !ok {
		return state.ZGraveyard
	}
	return z
}

// ZoneWord maps a state.Zone back to the canonical Forge zone word ParseZone
// reads (ParseZone's exact-match vocabulary). It is the reverse direction the
// move-driven lifetimes need when the zone is taken from the OBJECT at grant
// time rather than named by the script: a Duration$ Permanent Animate/Pump
// grant records its object's current zone in ExileOnMoved so the grant ends
// when that object leaves the zone it was granted in (CR 400.7 -- a zone
// change makes it a new object). An out-of-range or unnamed zone yields "",
// which ParseZone can never match, so the sweep simply never fires -- the
// honest no-op for a zone this vocabulary does not carry.
func ZoneWord(z state.Zone) string {
	switch z {
	case state.ZHand:
		return "Hand"
	case state.ZBattlefield:
		return "Battlefield"
	case state.ZLibrary:
		return "Library"
	case state.ZGraveyard:
		return "Graveyard"
	case state.ZExile:
		return "Exile"
	case state.ZStack:
		return "Stack"
	case state.ZCommand:
		return "Command"
	case state.ZSideboard:
		return "Sideboard"
	case state.ZCeased:
		return "Ceased"
	}
	return ""
}

// ParseZoneWord is parseZone's exported form for callers that must react to
// an UNKNOWN zone word (fail closed) rather than silently degrading to a
// graveyard the way ParseZone does: the trigger-side PresentZone$ clause's
// recognised-vocabulary check shares this one classification with every
// other zone word so the two cannot disagree.
func ParseZoneWord(s string) (state.Zone, bool) {
	return parseZone(s)
}

func parseZone(s string) (state.Zone, bool) {
	switch strings.TrimSpace(s) {
	case "Hand":
		return state.ZHand, true
	case "Battlefield":
		return state.ZBattlefield, true
	case "Library":
		return state.ZLibrary, true
	case "Graveyard":
		return state.ZGraveyard, true
	case "Exile":
		return state.ZExile, true
	case "Stack":
		return state.ZStack, true
	case "Command":
		return state.ZCommand, true
	case "Sideboard":
		return state.ZSideboard, true
	case "Ceased":
		return state.ZCeased, true
	}
	return 0, false
}

// ParseZones parses an Origin$ zone set. Any and All are wildcards. A false
// result means at least one token was unknown; callers must not treat it as a
// graveyard origin.
func ParseZones(s string) (zones []state.Zone, all, ok bool) {
	ok = true
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if part == "Any" || part == "All" {
			all = true
			continue
		}
		z, known := parseZone(part)
		if !known {
			ok = false
			continue
		}
		if !zoneIn(zones, z) {
			zones = append(zones, z)
		}
	}
	return zones, all, ok
}

func zoneIn(zones []state.Zone, want state.Zone) bool {
	for _, z := range zones {
		if z == want {
			return true
		}
	}
	return false
}

// mixedOriginIncludesHand identifies every explicit multi-zone Origin$ that
// includes Hand. Such an effect needs one origin-aware hidden-zone chooser;
// the exact-Hand and exact-Library walkers cannot safely stand in for it.
func mixedOriginIncludesHand(zones []state.Zone, all bool) bool {
	return !all && len(zones) > 1 && zoneIn(zones, state.ZHand)
}

// changeZoneAltDestination resolves ChangeZone's conditional alternate
// destination (Forge's ChangeZoneEffect.handleAltDest): DestAltSVar$ names an
// SVar (or inline count expression) evaluated against the resolving host card
// and compared under DestAltSVarCompare$ (default GE1, i.e. truthy). When the
// condition holds, the move takes DestinationAlternative$ instead of
// Destination$.
//
// The optional "MANDATORY " prefix is stripped. Forge reads MANDATORY as the
// difference between forcing the alternate and offering the player a
// confirmAction; this engine has no destination-confirm ask, so BOTH branches
// take the alternate deterministically when the condition holds, and the
// non-mandatory shape records one Note disclosing the dropped confirm (the
// expansion-specific riders of six corpus carriers, all Destination$ Hand ->
// DestinationAlternative$ Battlefield). MANDATORY itself therefore changes no
// behaviour today; it is parsed so the two spellings cannot drift.
//
// Unlike CheckSVarHolds's other call sites, an unreadable condition here fails
// CLOSED to the primary destination (plus a Note): moving a card to a zone the
// condition cannot justify would be a silently wrong board, whereas keeping
// the primary is the pre-existing behaviour and therefore replay-safe.
func changeZoneAltDestination(h Host, c *Ctx, sa *cards.SA, primary state.Zone) state.Zone {
	cond := strings.TrimSpace(sa.Params["DestAltSVar"])
	if cond == "" {
		return primary
	}
	mandatory := false
	if rest, ok := strings.CutPrefix(cond, "MANDATORY "); ok {
		mandatory = true
		cond = strings.TrimSpace(rest)
	}
	holds, evaluated := CheckSVarHolds(h, c, cond, strings.TrimSpace(sa.Params["DestAltSVarCompare"]))
	if !evaluated {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "DestAltSVar$ " + strings.TrimSpace(sa.Params["DestAltSVar"]) +
				" is not a condition this engine can evaluate; the move takes the primary destination"})
		return primary
	}
	if !holds {
		return primary
	}
	alt, ok := ParseZoneWord(sa.Params["DestinationAlternative"])
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "DestAltSVar$ " + strings.TrimSpace(sa.Params["DestAltSVar"]) +
				" holds but DestinationAlternative$ " + strings.TrimSpace(sa.Params["DestinationAlternative"]) +
				" is not a zone this engine models; the move takes the primary destination"})
		return primary
	}
	if !mandatory {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "DestAltSVar$ " + strings.TrimSpace(sa.Params["DestAltSVar"]) +
				" holds: the alternate destination " + strings.TrimSpace(sa.Params["DestinationAlternative"]) +
				" is taken (Forge would ask which destination; this engine does not ask)"})
	}
	return alt
}

// clearChangeZoneImprint uses the same event as Cleanup's ClearImprinted rider.
// In particular, ImprintLast replaces rather than appends on every mover.
func clearChangeZoneImprint(h Host, c *Ctx) {
	if c.Source == 0 {
		return
	}
	if o := h.Game().Obj(c.Source); o != nil && (len(o.Imprinted) > 0 || len(o.ImprintTokens) > 0 || len(o.SeekFound) > 0) {
		h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, Text: "clear"})
	}
}

func effChangeZone(h Host, c *Ctx, sa *cards.SA) {
	// Unimprint is a pre-move operation, even when no candidate is moved.
	// Re-entering after a choice may clear an already empty list; the fold
	// remains replayable and the later successful move supplies the new card.
	if strings.EqualFold(strings.TrimSpace(sa.Params["Unimprint"]), "True") {
		clearChangeZoneImprint(h, c)
	}
	to := changeZoneAltDestination(h, c, sa, ParseZone(sa.Params["Destination"]))
	// Set only when an explicit multi-zone Origin$ including Hand falls
	// through the dedicated walkers above to the object path; the diagnostic
	// for a resolution that ends up moving nothing is emitted after the move
	// loop, where `moved` knows the truth.
	mixedOriginNoteFrom := ""
	var originZones []state.Zone
	var originAll bool
	if from, present := sa.Params["Origin"]; present {
		var valid bool
		originZones, originAll, valid = ParseZones(from)
		// OriginAlternative$ is Forge's "and/or" second origin: the zones
		// named there join Origin$ into ONE candidate set at the
		// choose-a-card-from-any-of-these-zones step ("search your graveyard,
		// hand, and/or library"). Every one of the corpus's 62 carriers pairs
		// it with Origin$ Library; without this merge the exact-Library branch
		// below sees a library-only origin and silently searches just that.
		// Compound Origin$ spells use the same union. An object-valued
		// selector (Eladamri's ChosenCard) has already made its choice, while
		// a player-valued selector needs a choice from that player's zones.
		// Parse with the same vocabulary as Origin$. A zone word ParseZones
		// does not model an origin is noted loudly and dropped from the
		// merged set while every KNOWN zone keeps searching -- bailing the
		// whole effect (folding altValid into `valid`) would lose the library
		// half of invasion_of_arcavios's "library, graveyard, and/or outside
		// the game", a regression over the pre-OriginAlternative engine,
		// which still searched the library.
		if alt, hasAlt := sa.Params["OriginAlternative"]; hasAlt {
			altZones, altAll, altValid := ParseZones(alt)
			for _, z := range altZones {
				if !zoneIn(originZones, z) {
					originZones = append(originZones, z)
				}
			}
			originAll = originAll || altAll
			if !altValid {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "unrecognised ChangeZone OriginAlternative " + alt})
			}
		}
		hidden := strings.EqualFold(strings.TrimSpace(sa.Params["Hidden"]), "True")
		// ... and the branch excludes every origin the dedicated walkers own:
		// exactly-Library is the search below, exactly-Hand the hand movers,
		// a mixed-Hand origin the loud note -- and this branch must sit BEFORE
		// the unrecognised-Origin bail, because some hidden origins may still
		// resolve (Burning Wish's wish, whose
		// SubAbility$ self-exile must run). Origin$ All stays on the object
		// path too -- every no-Defined$ corpus line naming it carries Defined$
		// (all eight are Dauthi-shaped replacements), and a game-wide all-zones
		// pick would offer hidden hand/library cards by name. When a Defined$
		// DOES name the objects, Forge's resolver takes them without a choose
		// ask, reveals nothing and shuffles nothing (`!defined` fails both the
		// reveal and the shuffle conditions) -- exactly what the object path
		// below already performs, which is why Dauthi Voidwalker's Hidden$
		// "exile it instead" replacement carries no behaviour of its own
		// beyond this read. A Hidden$ compound origin naming Sideboard skips
		// this branch too: the search below is its chooser (Karn, the Great
		// Creator's -2), and effHiddenPick's game-wide or owner-public fetch
		// list is the wrong shape for an owner-private sideboard union.
		if hidden && sa.Params["Defined"] == "" &&
			!strings.EqualFold(strings.TrimSpace(sa.Params["Imprint"]), "True") &&
			!originAll && !mixedOriginIncludesHand(originZones, originAll) &&
			!zoneIn(originZones, state.ZLibrary) &&
			!zoneIn(originZones, state.ZSideboard) &&
			!(len(originZones) == 1 && originZones[0] == state.ZHand) {
			effHiddenPick(h, c, sa, to, originZones, originAll, valid, from)
			return
		}
		// hiddenpick1: Forge's SpellAbility.isHidden() (hasParam("Hidden") ||
		// the origin zones hold hidden info) routes the resolution through the
		// hidden-origin resolver (ChangeZoneEffect.changeHiddenOriginResolve)
		// even when the origin zones are PUBLIC. There the objects are not the
		// source default: with no Defined$ the fetch list is the origin zones'
		// cards matching ChangeType$ -- game-wide for a public origin when no
		// fetch player is named (Kor Skyfisher's bounce, Temur Sabertooth's
		// "another creature", the graveyard/exile mill follow-ups) or the
		// DefinedPlayer$/targeted player's own zones (Relic of Progenitus) --
		// and the resolver asks the chooser to pick ChangeNum$ of them. The
		// object path below would instead move the Defined() source default
		// silently (a self-bounce) or skip the player fetchers entirely (a
		// silent no-op). Hidden sideboard searches resolve here (Burning Wish's
		// wish, whose SubAbility$ self-exile
		// must run), which is why the branch sits before the
		// unrecognised-Origin bail. Origin$ All stays on the object path too
		// -- every no-Defined$ corpus line naming it carries Defined$ (all
		// eight are Dauthi-shaped replacements), and a game-wide all-zones
		// pick would offer hidden hand/library cards by name. When a Defined$
		// DOES name the objects, Forge's resolver takes them without a choose
		// ask, reveals nothing and shuffles nothing (`!defined` fails both the
		// reveal and the shuffle conditions) -- exactly what the object path
		// below already performs, which is why Dauthi Voidwalker's Hidden$
		// "exile it instead" replacement carries no behaviour of its own
		// beyond this read.
		if !valid {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unrecognised ChangeZone Origin " + from})
			return
		}
		// A hidden-origin fetch offers the union from Origin$ and
		// OriginAlternative$, including a player-selected hand/other-zone pair.
		// Concrete object selectors stay on the already-answered object path:
		// Eladamri's ChosenCard was picked by ChooseCard, not this search.
		// The searching player may fail to find a card with the stated quality (Min
		// is always zero), and the answer resumes this same effect before its
		// SubAbility runs. The exact-Library spelling is the single-zone case of
		// the same path; the alternatives are PUBLIC zones (Graveyard, Exile,
		// Hand) whose candidates join the library's in one option list. A
		// mixed-Hand alternative (Gate to the Afterlife's Graveyard,Hand) is
		// deliberately OWNED here rather than by the mixed-origin note below,
		// because the search IS the origin-aware chooser that note says does not
		// exist: the fetch player sees their own hand, so no hidden information
		// is exposed by offering it by name.
		fetchSelector := changeZoneFetchSelector(h, c, sa)
		if !originAll && (len(originZones) == 1 || fetchSelector) &&
			(zoneIn(originZones, state.ZLibrary) || zoneIn(originZones, state.ZSideboard) ||
				(len(originZones) > 1 && zoneIn(originZones, state.ZHand))) &&
			(!zoneIn(originZones, state.ZBattlefield) || zoneIn(originZones, state.ZHand)) {
			// Forge treats a Defined$ that resolves to objects in a hidden
			// library as the already-selected fetch list, not as the owner of a
			// fresh whole-library search. This is structural rather than keyed to
			// Remembered: ChosenCard, TopOfLibrary once resolved, and future
			// object-valued Defined selectors share the same dispatcher. Only the
			// single-zone case takes it: with OriginAlternative$ present the
			// corpus carries no Defined$ (measured 0 of 62), so this is latent
			// rather than live.
			if len(originZones) == 1 && moveDefinedLibraryObjects(h, c, sa, to) {
				return
			}
			effSearchLibrary(h, c, sa, to, originZones)
			return
		}
		// An unbound concrete object selector in a mixed-Hand origin must not
		// become a free search. Keep its object path -- the chooser question the
		// old note here claimed was unimplemented is answered by the search
		// path above (one private option list across the named origins, per
		// fetch player's own zones) and by this object path for a Defined$
		// that already names its objects -- and leave the diagnostic to the
		// post-move-loop emission, so a resolution whose objects DID move (a
		// chosen card, a remembered pair) is not slandered by a note.
		if mixedOriginIncludesHand(originZones, originAll) {
			mixedOriginNoteFrom = from
		}
		// A ChangeZone from exactly Hand with no object selector is Forge's
		// hidden-origin hand put-back: the chooser picks ChangeNum$ cards (a
		// ChangeType$ filter narrows the pool; its absence -- Brainstorm's "put
		// two cards from your hand on top of your library", Jace, the Mind
		// Sculptor's [0] -- offers the whole hand). With no Defined$/
		// DefinedPlayer$/ValidTgts$ the object path below would resolve Defined
		// to the SOURCE default and then skip every candidate on the Origin$
		// precondition -- the silent no-op the handmove1 fix replaces with a
		// real hand choice (the rv2b extension drops handmove1's ChangeType$
		// requirement: the whole 239-line no-selector Origin$ Hand population
		// routes here now, 19 of it untyped).
		// An SVar or inline count expression is evaluated through Num where the
		// count grammar supports it (for example Wrenn and Seven's SVar X counts
		// lands in hand). An unknown count remains loud rather than falling through
		// to the old source-default no-op: it emits a Note and moves nothing.
		if len(originZones) == 1 && originZones[0] == state.ZHand && !originAll &&
			sa.Params["Defined"] == "" &&
			sa.Params["DefinedPlayer"] == "" && sa.Params["ValidTgts"] == "" &&
			!strings.EqualFold(sa.Params["Imprint"], "True") {
			if _, supported := handMoveCountOf(h, c, sa); supported {
				effChangeZoneHand(h, c, sa, to)
				return
			}
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "cannot choose ChangeNum$ " + strings.TrimSpace(sa.Params["ChangeNum"]) +
					" cards from hand (a non-literal count is not a bound this engine can evaluate)"})
			return
		}
		// A ChangeZone from exactly Hand whose hand OWNER is selected --
		// DefinedPlayer$-alone (Kynaios and Tiro's "each player may put a land
		// card from their hand onto the battlefield", Braids, Conjurer Adept,
		// Mindleech Ghoul) or ValidTgts$-alone naming the players (Karn
		// Liberated's "[+4]: Target player exiles a card from their hand",
		// Kyoki, Sanity's Eclipse) -- is the per-owner hidden-hand shape: one
		// chooser ask per hand owner, chained through the persisted
		// Ctx.HandMoveTarget cursor. The rv2b r2 finding: these shapes used to
		// fall through to the object path, where Defined() resolved to the
		// source (or to targets the Origin$ precondition then skipped because
		// they are PLAYERS, not hand cards) -- another silent no-op.
		if len(originZones) == 1 && originZones[0] == state.ZHand && !originAll &&
			sa.Params["Defined"] == "" &&
			(sa.Params["DefinedPlayer"] != "" || sa.Params["ValidTgts"] != "") {
			effChangeZoneHandOwners(h, c, sa, to)
			return
		}
		// DefinedPlayer$ alongside a Defined$ that names concrete objects
		// (Wilt-Leaf Liege's DefinedPlayer$ ReplacedPlayer + Defined$
		// ReplacedCard, 1 corpus line) keeps the object path -- the moved
		// objects are already named -- but the owner parameter is unread
		// there, so the shape is loud about it rather than silent.
		if len(originZones) == 1 && originZones[0] == state.ZHand && !originAll &&
			sa.Params["Defined"] != "" && sa.Params["DefinedPlayer"] != "" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "DefinedPlayer$ " + sa.Params["DefinedPlayer"] +
					" is unread next to Defined$ " + sa.Params["Defined"] +
					" (the move goes to the named objects alone)"})
		}
	}
	// Answered ShuffleNonMandatory$ confirm re-entry for the OBJECT path
	// (searchmay1): a hidden-library search's own re-entry is handled inside
	// effSearchLibrary above and returns, so reaching here with an answer
	// means the SA moved objects from a public origin (a graveyard/top
	// shuffle-in) and those moves already landed on the first pass. Run only
	// the answered tail -- consume the answer, shuffle on "yes" -- and stop:
	// re-resolving targets would re-run the move pass and re-pose the
	// pre-asks below against objects that have left their origin zone.
	if c.SearchShuffle != "" && objectPathShuffleOwed(sa) {
		objectPathShuffleTail(h, c, sa, nil)
		return
	}
	// WithCountersType$/WithCountersAmount$ make the move put counters on the
	// object it lands with -- the Undying expansion's "return to the battlefield
	// with a +1/+1 counter" (cards/keywords.go) and a card exiled with TIME
	// counters (suspend). The CounterChange is emitted AFTER the MoveZone, so it
	// lands on the moved (new) object's back at its destination, exactly as Move
	// waiting to run first would want, and the counter survives onto the object
	// because it is added post-move. Counter (not the Move carrying it along) is
	// what keeps events/apply.go's Move from knowing anything about counters.
	// counterDestination is the one gate every mover shares: a destination that
	// cannot carry the counters neither parses the amount nor emits anything.
	withKind := sa.Params["WithCountersType"]
	var withAmt int32
	if withKind != "" && counterDestination(to) {
		withAmt = withCounterAmount(h, c, sa)
	}
	targets := Defined(h, c, sa)
	targetAskPending := false
	// ValidTgts$ targeting whose ask was never offered: the placement ask
	// (rules' pushTrigger) reads only the trigger's OWN Execute SA, so a
	// deeper sub's ValidTgts$ -- the "when you do" family's shape (Forum
	// Filibuster's `TrigReturn`, 134 raw corpus T: chains reaching one) --
	// arrives here with no chosen targets and used to move nothing silently.
	// Offer the targets now, through the same Host.LegalTargets census the
	// announcement ask uses (targetZones' Origin$-implied graveyard included),
	// as a KChoose over the shared "choice" resume arm; the answer lands in
	// Ctx.Choice and the re-entered pass consumes it (fx42 scoping -- a nested
	// ChangeZone in the same chain poses its own ask). A host that cannot ask
	// takes the deterministic first-max stand-in (R-9, the same mirror the
	// effDig ask's botpolicy arm answers with option 0). The chosen bounds are
	// TargetMin$/TargetMax$ through the ordinary Num grammar (TrigReturn's
	// TargetMin$ 0 / TargetMax$ 1 -- "up to one"), clamped to the eligible
	// count; a bound pair that admits nothing (Min == Max == 0, or no eligible
	// candidate) poses no ask and moves nothing -- a decision nobody could
	// answer differently is never emitted.
	if ans, ok := changeZoneChosenTargets(h, c, sa); ok {
		targets = ans
		// A suspension (nil answer, ok) leaves the chooser pending: the note
		// below must wait for the answering re-entry, which moves the targets.
		targetAskPending = ans == nil
	}
	// The O-Ring return shape (Journey to Nowhere, Leonin Relic-Warder): the
	// LEAVE-battlefield trigger's Execute is `DB$ ChangeZone | Defined$
	// Remembered`, and Forge reads the HOST CARD's remembered list there --
	// the cross-resolution memory RememberTargets$ wrote -- not this build's
	// Ctx binding. This build's trigger resolutions seed Ctx.Remembered with
	// the event capture (triggerRemembered), which for a self-trigger is
	// exactly [{source}], so the ctx set is distinguishable: when the
	// resolved set is exactly that capture and the source's persistent
	// Remembered is non-empty, the card's list is what the script meant.
	// Mid-chain readings are unaffected: a chain that remembered its own
	// source through the object path below wrote BOTH halves (ctx and
	// persistent), so the replacement is the same set.
	if sa.Params["Defined"] == "Remembered" {
		if len(targets) == 1 && !targets[0].IsPlayer && targets[0].Obj == c.Source {
			if src := h.Game().Obj(c.Source); src != nil && len(src.Remembered) > 0 {
				targets = append([]state.Target(nil), src.Remembered...)
			}
		}
	}
	// AlternativeDecider$ chooses which library position receives the
	// targeted card. The owner, not the spell's controller, answers (the
	// referent may be the OPPONENT of the caster, and the ask goes to that
	// seat). Keep the ask after target resolution so TargetedOwner is bound to
	// the actual referent, and before any move so replay re-entry cannot
	// partially apply. The one shape modelled is the corpus's uniform
	// AlternativeDecider shape: ONE targeted card moving to a library whose
	// primary position is the TOP (or second from top) and whose alternative
	// position is `-1` (bottom). Other shapes stay LOUD and take the
	// pre-existing deterministic placement rather than silently offering a
	// choice the script never posed.
	altDecider := strings.TrimSpace(sa.Params["AlternativeDecider"])
	altAnswer := c.ChangeZoneAlternative
	c.ChangeZoneAlternative = ""
	altBottom := false
	altEngaged := false
	if altDecider != "" && len(targets) > 0 {
		primaryPosition := strings.TrimSpace(sa.Params["LibraryPosition"])
		altPosition := strings.TrimSpace(sa.Params["LibraryPositionAlternative"])
		shapeOK := to == state.ZLibrary && len(targets) == 1 && !targets[0].IsPlayer &&
			altPosition == "-1" && (primaryPosition == "" || primaryPosition == "0" || primaryPosition == "1")
		switch {
		case !shapeOK:
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "AlternativeDecider$ " + altDecider + " is not the top-or-bottom library shape this engine can ask; the primary destination is taken"})
		default:
			target := h.Game().Obj(targets[0].Obj)
			var chooser state.PlayerID
			chooserOK := false
			switch altDecider {
			case "TargetedOwner":
				if target != nil {
					chooser, chooserOK = target.Owner, true
				}
			}
			if !chooserOK || int(chooser) >= len(h.Game().Players) {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
					Text: "AlternativeDecider$ " + altDecider + " cannot resolve an object owner"})
			} else if altAnswer == "" {
				primaryLabel := "top"
				if primaryPosition == "1" {
					primaryLabel = "second from top"
				}
				d := &decision.Decision{Player: chooser, Kind: decision.KChoose, Min: 1, Max: 1,
					Source: c.Source, ResumeKind: "changezone_alternative", ResumeSA: sa,
					Prompt: "Choose your library position",
					Options: []decision.Option{
						{Index: 0, Kind: "primary", Label: primaryLabel},
						{Index: 1, Kind: "bottom", Label: "bottom"},
					}}
				if Ask(h, d) == AskAsked {
					return
				}
				// R-9 no-ask host: the primary placement, deterministically.
				altAnswer = "top"
				altBottom = false
				altEngaged = true
			} else {
				altBottom = altAnswer == "bottom"
				altEngaged = true
			}
		}
	}
	forgetOtherRemembered(h, c, sa)
	// ForgetOtherTargets$ True (Journey to Nowhere, Leonin Relic-Warder):
	// Forge's ChangeZoneEffect.forgetOtherTargets -- forget every previously
	// remembered object before this effect resolves, so a source that
	// remembered something earlier (a re-entered O-Ring exiling a second
	// creature) remembers only its own targets and the return trigger
	// returns exactly this effect's set. Both halves clear: the resolution's
	// ctx list and the source's event-backed persistent one.
	if strings.EqualFold(strings.TrimSpace(sa.Params["ForgetOtherTargets"]), "True") {
		c.Remembered = nil
		clearEventRemembered(h, c)
	}
	// Imprint effects such as Chrome Mox select eligible cards from their
	// controller's hand. Keep them out of the generic hand mover so their
	// successful exile can be recorded in the replayable Imprint event.
	if len(targets) == 1 && targets[0].Obj == c.Source && !targets[0].IsPlayer &&
		len(originZones) == 1 && originZones[0] == state.ZHand && !originAll &&
		strings.EqualFold(sa.Params["Imprint"], "True") {
		targets = nil
		if c.ImprintDone {
			for _, id := range c.Imprint {
				targets = append(targets, state.Target{Obj: id})
			}
			c.Imprint, c.ImprintDone = nil, false
		} else {
			spec := sa.Params["ChangeType"]
			if spec == "" {
				spec = "Card"
			}
			for _, id := range h.Game().Zone(state.ZHand, c.Controller) {
				if o := h.Game().Obj(id); o != nil && MatchesSpecCtx(h.Game(), spec, id, c.SpecContext(c.Controller)) {
					targets = append(targets, state.Target{Obj: id})
				}
			}
			max := Num(h, c, sa, "ChangeNum", 1)
			if int32(len(targets)) > max {
				d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: int(max), Max: int(max), Source: c.Source,
					ResumeKind: "imprint", ResumeSA: sa, Prompt: "Choose a card to imprint"}
				for _, target := range targets {
					o := h.Game().Obj(target.Obj)
					d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "imprint", Obj: target.Obj, Label: o.Face().Name})
				}
				if h.Ask(d) {
					return
				}
				targets = targets[:max]
			}
		}
	}
	var imprinted []state.ObjID
	// Forge keeps every DB$ Effect in an implicit "effect" object in the
	// Command zone, and the corpus's one-shot idiom `DB$ ChangeZone | Defined$
	// Self | Origin$ Command | Destination$ Exile` is that effect object
	// exiling itself -- ending the effect after one use (Deflecting Palm's
	// RPreventNextFromSource: "the NEXT time the chosen source would deal
	// damage"). This build has no effect object, so when the chain resolves
	// inside an Effect-created replacement's body (Ctx.EffectFrame is bound
	// by rules' seedEffectReplCtx) and the ChangeZone names that frame's own
	// source, the shape ends exactly that registration. Everywhere else the
	// ordinary move below runs unchanged (it moves nothing: the named source
	// is not in the Command zone), so no other resolution changes.
	if f := c.EffectFrame; f.Source != 0 && f.Source == c.Source && to == state.ZExile && !originAll &&
		len(originZones) == 1 && originZones[0] == state.ZCommand &&
		len(targets) == 1 && !targets[0].IsPlayer && targets[0].Obj == c.Source {
		if f.Stamp != 0 {
			h.EndEffect(f.Source, f.Stamp)
		} else {
			// A source-scoped frame (no per-registration stamp): the idiom ran
			// from an Effect's OWN body -- its Triggers$ body, or the chain of
			// the spell/ability that registered it -- so end every Effect-created
			// registration from that source. The ender's FromEffect marker keeps
			// the source's printed statics out of it.
			h.EndEffectSource(f.Source)
		}
		return
	}
	// The ImprintOnHost$ ender (task param:api:Effect.ImprintOnHost): the
	// same "exile the implicit effect object" idiom as the block above, but
	// keyed on the HOST's imprint instead of the effect's own self-exile.
	// Forge's DB$ Effect | ImprintOnHost$ True imprints the created effect
	// token on the host card and moves the token to the Command zone; the
	// corpus's `DB$ ChangeZone | Defined$ Imprinted | Origin$ Command |
	// Destination$ Exile` (Superior Foes of Spider-Man, Furious Rise,
	// Unstable Amulet, Word of Command, Semester's End -- 5 files) exiles
	// that token, ending the effect it carries ("you may play that card
	// until you exile another card with this creature" -- the second dig's
	// trigger exiles the FIRST effect's token before the new Effect
	// registers). This build has no effect-token object, so the marker
	// rides the registrations (state.ContinuousEffect.ImprintOnHost) and
	// the idiom ends exactly those through Host.EndImprintedEffects. The
	// ordinary move below still runs: the source's real imprinted cards
	// (Chrome Mox's) are never in the Command zone in this build, so the
	// Origin$ precondition skips them exactly as it did before.
	if to == state.ZExile && !originAll && len(originZones) == 1 &&
		originZones[0] == state.ZCommand &&
		strings.TrimSpace(sa.Params["Defined"]) == "Imprinted" {
		h.EndImprintedEffects(c.Source)
	}
	// The objects the move loop actually moved, in move order: ChangeZone's
	// AtEOT$ affected set is the MOVED objects (some carriers carry
	// RememberChanged$ and some do not, so the moved set is collected here
	// rather than read back out of Remembered).
	var moved []state.ObjID
	// The Attacking$ entry rider is classified ONCE for the whole call, before
	// the mover loop: its degrades are one Note per ChangeZone, not one per
	// moved object.
	rider := classifyAttackingEntry(c, sa, to)
	for _, t := range targets {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil {
			continue
		}
		// Origin$, when given, is a precondition: the object must actually be
		// where the script expects, or the movement does not happen. This is
		// also this build's only CR 608.2b guard for ChangeZone: a target
		// moved away by an earlier effect in the same resolution, or by a
		// response that has already resolved, is simply skipped rather than
		// moved a second time or moved from the wrong zone.
		if _, present := sa.Params["Origin"]; present && !originAll && !zoneIn(originZones, o.Zone) {
			continue
		}
		// A CantExile restriction (The Master, Multiplied: "Triggered abilities
		// you control can't cause you to ... exile creature tokens you
		// control") withholds the object from this exile entirely: it never
		// leaves the battlefield, no MoveZone is emitted and none of the
		// inlined riders (exiled-with, RememberChanged, exile-return) run. The
		// shared settle path (settleChangeZoneMoveAs) carries the same guard
		// for every other ChangeZone mover.
		if to == state.ZExile && h.ExileBlocked(o.ID, false) {
			continue
		}
		// Inlined rather than routed through settleChangeZoneMove: this loop
		// carries the exiled-with association and the RememberChanged$
		// event-backed rider (eventRemember) in a specific order (MoveZone,
		// exiled-with, RememberChanged, WithCounters) that predates the
		// shared settle helper, and neither is shared with that helper's
		// other callers (see settleChangeZoneMoveAs's doc comment). The
		// MoveZone event itself still goes through moveZoneEvent (every exile
		// mover shares that one constructor) plus the same Imprint$True/
		// ExiledWithSource-static IDs augmentation settleChangeZoneMoveAs
		// applies, so events.Apply's ExiledWith-scalar derivation (o.ExiledWith
		// = e.IDs[0]) fires here exactly as it does on that path -- Chrome
		// Mox's own DefinedCards$ ExiledWith read needs it, not just the
		// distinct ExiledCards list exiledWithAssociation below maintains.
		ev := moveZoneEvent(c, o.ID, o.Zone, to)
		// Capture the LKI before the emit: events.Apply's Move fold resets a
		// battlefield departure's controller to its owner (CR 400.7), so this
		// is the last point the pre-move controller is readable.
		if strings.EqualFold(sa.Params["RememberLKI"], "True") {
			c.ChangeZoneLKI = append(c.ChangeZoneLKI, state.LKIObject{Obj: o.ID, Controller: o.Controller, Owner: o.Owner})
		}
		if to == state.ZExile && len(ev.IDs) == 0 && (faceStaticsNameExiledWithSource(h, c.Source) || strings.EqualFold(strings.TrimSpace(sa.Params["Imprint"]), "True")) {
			ev.IDs = []state.ObjID{c.Source}
		}
		applyFaceDownMarker(h, sa, c, &ev, to)
		fromZone := o.Zone
		// A Transformed$ True entry flips to the back face BEFORE the MoveZone
		// is folded, so events.Apply's Move grants CR 306.5b loyalty for the
		// face the permanent enters with. See applyTransformed.
		if to == state.ZBattlefield {
			applyTransformed(h, c, sa, o.ID)
		}
		h.Emit(ev)
		moved = append(moved, o.ID)
		exiledWithAssociation(h, c, o.ID, to)
		if to == state.ZExile {
			recordExileReturn(h, c, sa, o.ID, fromZone, to)
		}
		// RememberLKI$ True (Reanimate's "creature card" whose mana value the
		// chained lose-life SVar reads, RememberedLKI$CardManaCost) joins the
		// moved object to the ability's Remembered -- a resolution-local Ctx
		// value, replayed identically because replay re-runs the same SA. The
		// two flags stack; an object is not remembered twice.
		if strings.EqualFold(sa.Params["RememberLKI"], "True") &&
			!strings.EqualFold(sa.Params["RememberChanged"], "True") {
			c.Remembered = append(c.Remembered, state.Target{Obj: o.ID})
		}
		if strings.EqualFold(sa.Params["RememberChanged"], "True") {
			c.Remembered = append(c.Remembered, state.Target{Obj: o.ID})
			eventRemember(h, c, o.ID)
		}
		// RememberTargets$ True (Journey to Nowhere's exile trigger, Bile
		// Blight's Pump sibling): the CHOSEN TARGETS join the ability's
		// Remembered, in both halves -- the ctx list the chain's later
		// sub-abilities read (Bile Blight's PumpAll Remembered.sameName) and
		// the source's event-backed persistent list, which a LATER, separate
		// resolution reads through Defined$ Remembered via the O-Ring rescue
		// above (Journey's leave-battlefield return trigger). Only a target
		// the move actually moved is remembered: a target skipped by the
		// Origin$ precondition was never exiled and must never come back.
		if strings.EqualFold(strings.TrimSpace(sa.Params["RememberTargets"]), "True") {
			c.Remembered = append(c.Remembered, state.Target{Obj: o.ID})
			eventRemember(h, c, o.ID)
		}
		eventForgetChanged(h, c, sa, o.ID)
		if withKind != "" && counterDestination(to) {
			h.Emit(events.Event{Kind: events.CounterChange, Obj: o.ID, Counter: withKind, Amount: withAmt})
		}
		// GainControl$ hands the moved object to the named player (Reanimate:
		// "return target creature card... to the battlefield under your
		// control"). Only a battlefield entry can carry a control change (CR
		// 701.22a controls permanents); a card moved to a hidden or public
		// non-battlefield zone keeps its owner. Not part of the "inlined
		// rather than settleChangeZoneMove" scoping above -- GainControl$ is
		// unconditional on the move landing on the battlefield, the same as
		// settleChangeZoneMoveAs's own tail call.
		if to == state.ZBattlefield {
			applyGainControl(h, c, sa, o.ID)
			changeZoneAttachedTo(h, c, sa, o.ID)
		}
		// Tapped$ True (CR 110.5's entry state): the moved permanent enters
		// tapped. The object path did not apply this rider before, so a
		// targeted graveyard/exile return carrying it (Zuko's Conviction's
		// kicked alternate, every "return it to the battlefield tapped"
		// spell) entered untapped -- the same Tap event applyLibrarySearch
		// and the hand movers emit, so the entry state is a real event and
		// replay derives it.
		if to == state.ZBattlefield && strings.EqualFold(sa.Params["Tapped"], "True") {
			h.Emit(events.Event{Kind: events.Tap, Obj: o.ID, Player: c.Controller, Text: "entered tapped"})
		}
		rider.apply(h, c, o.ID, c.Controller, to)
		// StaticEffect$ on the inlined object path: the same rider the shared
		// settle path applies for every other mover (the main loop deliberately
		// predates settleChangeZoneMoveAs and is not routed through it).
		if to == state.ZBattlefield {
			applyStaticEffect(h, c, sa, to, []state.ObjID{o.ID})
			// LeaveBattlefield$ Exile on the inlined object path (Isareth the
			// Awakener, From the Catacombs): the same promise the shared settle
			// path registers, on the object this move just landed, after its
			// entry riders are settled.
			registerLeaveExile(h, c, o.ID, sa.Params["LeaveBattlefield"], "", true)
		}
		if strings.EqualFold(sa.Params["Imprint"], "True") &&
			(to == state.ZExile || strings.EqualFold(strings.TrimSpace(sa.Params["ImprintLast"]), "True")) {
			if landed := h.Game().Obj(o.ID); landed != nil && landed.Zone == to &&
				(to == state.ZExile || !landed.IsToken) {
				imprinted = append(imprinted, o.ID)
			}
		}
	}
	if len(imprinted) > 0 {
		if strings.EqualFold(strings.TrimSpace(sa.Params["ImprintLast"]), "True") {
			clearChangeZoneImprint(h, c)
			imprinted = imprinted[len(imprinted)-1:]
		}
		h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: imprinted})
	}
	// The mixed-Hand diagnostic, now that the pass's truth is known: every
	// no-selector/player-selector mixed origin was answered by the search
	// path's chooser above, and a Defined$ naming its objects moves them
	// through the Origin$-preconditioned loop -- so only a mixed-Hand
	// resolution that moved nothing AND posed no pending target ask is left
	// loud (a suspended ask emits on its answering re-entry, which moves).
	if mixedOriginNoteFrom != "" && len(moved) == 0 && !targetAskPending {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "no object of the mixed ChangeZone Origin$ " + mixedOriginNoteFrom +
				" resolution was eligible to move"})
	}
	// AtEOT$ (Puppeteer Clique's reanimation: "at the beginning of your next
	// end step, exile it"): schedule the end-step departure for every object
	// this move actually moved. Scheduled BEFORE the library shuffle tail:
	// a ShuffleNonMandatory$ confirm suspension returns out of the tail, and
	// the re-entry's early-return branch (c.SearchShuffle above) would never
	// reach a schedule call placed after it -- the same order
	// applyLibrarySearch uses for its own hidden-origin tail.
	scheduleAtEOT(h, c, sa, moved)
	// Object-path library shuffle tail (searchmay1): a ChangeZone that moved
	// objects INTO a library and states Shuffle$ True now shuffles. This is
	// the tail the AGENTS.md row named as "the object-path shuffle": the
	// path previously shuffled nothing at all. Four corpus lines also set
	// ShuffleNonMandatory$; three SP-parented DB subs (Cathartic Parting,
	// Devious Cover-Up, Put Away) still inherit the parent's targets and
	// cannot reach this tail until sub-ability targeting is separated. The 76
	// mandatory carriers (Turn the Earth, Quandrix Command, Rite of Renewal,
	// Stream of Consciousness, the death-trigger "shuffle CARDNAME into its
	// owner's library" family) shuffled nothing either and now shuffle. Only
	// the explicit Shuffle$ True is read: the corpus's LibraryPosition$
	// "put it on top" movers state no Shuffle$ and must NOT shuffle. Each
	// distinct card owner's library is shuffled once (a cross-graveyard mover
	// like Turn the Earth touches several players), in first-move order so
	// the event stream stays deterministic.
	if to == state.ZLibrary && len(moved) > 0 && objectPathShuffleOwed(sa) {
		if objectPathShuffleTail(h, c, sa, moved) {
			return
		}
	}
	if altEngaged && len(moved) > 0 {
		position := int32(0)
		if altBottom {
			position = -1
		} else if strings.TrimSpace(sa.Params["LibraryPosition"]) == "1" {
			position = 1
		}
		libraryOrderPlacementAt(h, h.Game().Obj(moved[0]).Owner, moved, position)
	}
}

// changeZoneAttachedTo implements ChangeZone's AttachedTo$ param: "the moved
// card enters the battlefield attached to the resolved target" (Forum
// Filibuster's `AttachedTo$ DelayTriggerRememberedLKI` -- attach the returned
// Aura to the remembered token; the 42 raw corpus ChangeZone lines carrying
// the param: Self x15 is the dominant form, "return an Aura ... attached to
// CARDNAME"). The value is a Defined$-grammar selector, resolved with the
// ordinary resolver against a shallow SA that carries it in Defined$ (the
// same shape effToken's own AttachedTo$ rider takes), so every corpus
// spelling (Self, ParentTarget, TriggeredCardLKICopy, ChosenCard,
// DelayTriggerRememberedLKI, a card filter, ...) resolves without a second
// resolver. The Attach event is the same shape that rider emits: Obj is the
// MOVED card (it takes the AttachedTo back-reference), IDs[0] the target it
// attaches to. A value that resolves to no object -- a selector this grammar
// cannot evaluate, or a target that left play -- is ONE loud Note and the
// card enters unattached (an Aura's unattached state), never a guessed
// target and never a silent skip. Battlefield destinations only: the param
// on a move that does not enter the battlefield has no CR meaning (nothing
// can be attached in a hidden zone) and is left unread.
func changeZoneAttachedTo(h Host, c *Ctx, sa *cards.SA, moved state.ObjID) {
	val := strings.TrimSpace(sa.Params["AttachedTo"])
	playerVal := strings.TrimSpace(sa.Params["AttachedToPlayer"])
	if moved == 0 || (val == "" && playerVal == "") {
		return
	}
	if playerVal != "" {
		changeZoneAttachedToPlayer(h, c, sa, moved, playerVal)
		return
	}
	sub := *sa
	sub.Params = map[string]string{"Defined": val}
	// A bare card-filter spelling ("Creature" -- Retether's mass return;
	// "Creature.YouCtrl" -- One Last Job, Storm Herald, Nomad Mythmaker;
	// "Creature.sharesCreatureTypeWith <ref>" -- Runed Crown) is not a
	// Defined$ referent (definedSpec has no case for it) and MUST NOT ride
	// Defined's source fallback: that would fasten the moved Aura to the
	// resolving spell/ability itself, an attach the CR 704.5m SBA then
	// sweeps the moment the source leaves play. When knownDefinedTargets
	// cannot classify the value, resolve it as a battlefield card filter --
	// the same walk the Valid-prefixed branch runs -- so a spelling this
	// grammar cannot evaluate fails closed to the loud Note below, never to
	// a guessed attach. A value that is already the Valid-prefixed filter
	// form (Mantle of the Ancients' "Valid Creature.EnchantedBy") keeps its
	// own branch.
	if val != "Valid" && !strings.HasPrefix(val, "Valid ") {
		if _, ok := knownDefinedTargets(h, c, val); !ok {
			sub.Params["Defined"] = "Valid " + val
		}
	}
	var to state.ObjID
	for _, t := range Defined(h, c, &sub) {
		if !t.IsPlayer {
			to = t.Obj
			break
		}
	}
	if to == 0 || h.Game().Obj(to) == nil {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "ChangeZone AttachedTo$ " + val + " resolved to nothing; the card enters unattached"})
		return
	}
	emitAttach(h, moved, to)
}

// changeZoneAttachedToPlayer implements ChangeZone's AttachedToPlayer$ param:
// "the moved card enters the battlefield attached to the resolved PLAYER"
// (Lynde, Cheerful Tormentor's `AttachedToPlayer$ You`, Curse of Misfortunes'
// `EnchantedPlayer`, Bitterheart Witch's `Targeted`, the two Trandformed$
// Curses' `ParentTarget`). It is the player-destination twin of
// changeZoneAttachedTo above; state.Object cannot carry both a permanent and
// a player bearer, and events.Attach's player branch is what folds the seat
// into AttachedPlayer/HasAttachedPlayer. The value is resolved with the
// ordinary resolver against a shallow SA carrying it in Defined$ (the same
// shape the object twin takes and definedSpec already supports: You,
// Targeted, ParentTarget, EnchantedPlayer), and the FIRST living player it
// names is the bearer. A value that resolves to no living seat -- an
// unmodelled selector, or a target that left -- is ONE loud Note and the card
// enters unattached, never a guessed seat. Battlefield destinations only (the
// caller gates on that); nothing can be attached in a hidden zone.
func changeZoneAttachedToPlayer(h Host, c *Ctx, sa *cards.SA, moved state.ObjID, val string) {
	sub := *sa
	sub.Params = map[string]string{"Defined": val}
	var seat state.PlayerID
	found := false
	for _, t := range Defined(h, c, &sub) {
		if t.IsPlayer && int(t.Player) < len(h.Game().Players) && !h.Game().Players[t.Player].Lost {
			seat, found = t.Player, true
			break
		}
	}
	if !found {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "ChangeZone AttachedToPlayer$ " + val + " resolved to nothing; the card enters unattached"})
		return
	}
	// The raw player-attach event, exactly the shape effAttach's own
	// Enchant:Player branch emits: Obj is the MOVED card, Player the seat.
	// emitAttach is deliberately NOT reused -- its Unattached detach half
	// reads o.AttachedTo (the permanent link) only, and the Attach fold's
	// player branch already clears AttachedTo, so a re-attach from one
	// player to another or from a permanent to a player is handled by the
	// one event (CR 701.3b: the new attachment supersedes the old).
	h.Emit(events.Event{Kind: events.Attach, Obj: moved, Player: seat, Text: "attach to player"})
}

// applyFaceDownMarker stamps a just-built ChangeZone MoveZone with the
// face-down encoding the card text asks for. Two spellings reach it, and they
// mean different CR things:
//
//   - ExileFaceDown$ True (Necropotence's "exile the top card of your library
//     face down"): the "exiled_with_face_down" decode sets Object.FaceDown AND
//     records the exiling source as the ExiledWith association -- the same
//     encoding Hideaway's face-down exile uses. The IDs provenance payload is
//     cleared so the two carriers cannot disagree on one event.
//
//   - FaceDown$ True (Yedora, Grave Gardener; the manifest marker's own
//     spelling) on a battlefield entry: the "entered_face_down" decode folds
//     Object.FaceDown plus the optional FaceDownSetType$/FaceDownPower$/
//     FaceDownToughness$ payload the card text names, exactly as a Manifest
//     does. A hand/library-origin face-down entry is marked Secret (its face
//     would otherwise leak through the transcript), matching the Manifest
//     precedent; a graveyard-origin one stays public (CR 708.9 already
//     revealed it on leaving the battlefield).
//
//   - FaceDown$ True on an exile destination (Tezzeret's Reckoning): the card
//     is put into exile face down WITHOUT an ExiledWith association, so the
//     bare spelling uses its own "face_down" marker rather than borrowing
//     ExileFaceDown$'s source-carrying one.
//
//   - WithMayLook$ True with an exile face-down destination (Ixhel, Scion of
//     Atraxa; Gonti; Thief of Sanity): the exiling effect's controller may
//     look at the exiled card's face for as long as it remains exiled, even
//     though the card's owner may NOT. It rides the same MoveZone as
//     ExileFaceDown$, but with the looker in Amount and the exiling source in
//     IDs (the marker's own layout, decoded under the
//     "exiled_with_face_down_maylook" Counter value), so replay folds both
//     the look permission and the ExiledWith provenance with no new event kind
//     and no Event field change. The pair composes with Foretold$ (a distinct
//     marker) because Forge keeps the two designations independent; measured
//     at the current corpus pin no card carries both, but the decode keeps
//     them separable rather than silently dropping one.
//
// It is called from every ChangeZone mover (the object path, the shared
// settle helper the hand/library routes use, and applyLibrarySearch), so the
// read composes with each without a second caller-side branch. The Dig
// mover (effects/cardflow.go effDig) calls it too, so the same WithMayLook$
// read serves both APIs from this one choke point.
//
// Unearth$ True (cards/kw_unearth.go's K:Unearth expansion) also lands on
// the battlefield entry here: it stamps the "entered_unearthed" counter so
// rules' entry hook (rules/unearth.go, reached from checkTriggers) can tell
// an unearth return from every other battlefield entry and apply CR
// 702.84a's haste grant and end-step exile promise. The counter is not a
// face-down marker (IsFaceDownEntry returns false for it), so the two
// encodings never collide; no corpus line combines Unearth with
// FaceDown$/ExileFaceDown$.
func applyFaceDownMarker(h Host, sa *cards.SA, c *Ctx, ev *events.Event, to state.Zone) {
	faceDown := strings.EqualFold(strings.TrimSpace(sa.Params["FaceDown"]), "True")
	exileFaceDown := strings.EqualFold(strings.TrimSpace(sa.Params["ExileFaceDown"]), "True")
	withMayLook := strings.EqualFold(strings.TrimSpace(sa.Params["WithMayLook"]), "True")
	foretold := strings.EqualFold(strings.TrimSpace(sa.Params["Foretold"]), "True")
	if to == state.ZBattlefield && strings.EqualFold(strings.TrimSpace(sa.Params["Unearth"]), "True") {
		ev.Counter = events.UnearthEntryCounter
	}
	switch {
	case to == state.ZExile && exileFaceDown && withMayLook:
		// The may-look layout: Amount carries the looker (the exiling
		// effect's controller), IDs carries the exiling source. The looker
		// is authoritative, so the card's owner is NOT admitted by the
		// view's default controller check.
		ev.Counter = "exiled_with_face_down_maylook"
		if foretold {
			ev.Counter = "exiled_with_face_down_maylook_foretold"
		}
		ev.Amount = int32(c.Controller)
		ev.IDs = []state.ObjID{c.Source}
	case to == state.ZExile && exileFaceDown:
		if foretold {
			ev.Counter = "exiled_with_face_down_foretold"
		} else {
			ev.Counter = "exiled_with_face_down"
		}
		ev.Amount = int32(c.Source)
		ev.IDs = nil
	case to == state.ZExile && faceDown:
		ev.Counter = "face_down"
		ev.Amount = 0
		ev.IDs = nil
	case to == state.ZBattlefield && faceDown:
		setType := strings.TrimSpace(sa.Params["FaceDownSetType"])
		power, hasPower := NumResolved(h, c, sa, "FaceDownPower", 0)
		toughness, hasTough := NumResolved(h, c, sa, "FaceDownToughness", 0)
		hasPT := hasPower || hasTough
		ev.Counter = events.FaceDownEntryCounterFor(setType, power, toughness, hasPT)
		if ev.From == state.ZHand || ev.From == state.ZLibrary {
			ev.Secret = true
		}
	}
}

// settleChangeZoneMove is the one settle path every ChangeZone mover shares:
// the MoveZone itself, then RememberChanged$ (the moved object joins the
// ability's Remembered -- a DelayedTrigger running as a later SubAbility of
// the same chain captures it, and the value is a parameter of the ongoing
// resolution (Ctx), not game state, so mutating it here is fine), then the
// WithCountersType$/WithCountersAmount$ entry counters when the move lands on
// a counter-bearing destination (battlefield or exile -- counterDestination).
// Keeping the object path and the hand-choice path on this one helper means
// the two cannot drift apart on any of the three.
func settleChangeZoneMove(h Host, c *Ctx, sa *cards.SA, id state.ObjID, from, to state.Zone, withKind string, withAmt int32, rider *attackingEntry) {
	settleChangeZoneMoveAs(h, c, sa, id, from, to, withKind, withAmt, 0, false, rider)
}

// settleChangeZoneMoveAs is the one settle path every ChangeZone mover shares
// (settleChangeZoneMove is its event-Player-unset form), plus the explicit
// event-Player form: a hidden-zone move of ANOTHER player's card carries that
// player as the event's Player -- the same attribution the library search's
// move applies -- so the view layer's hidden-card redaction sees the move the
// way the owner does. The MoveZone itself, then RememberChanged$ (the moved
// object joins the ability's Remembered -- a DelayedTrigger running as a
// later SubAbility of the same chain captures it, and the value is a
// parameter of the ongoing resolution (Ctx), not game state, so mutating it
// here is fine), then the WithCountersType$/WithCountersAmount$ entry
// counters when the move lands on a counter-bearing destination (battlefield
// or exile -- counterDestination). Keeping the object path and the hand-choice
// path on this one helper means the two cannot drift apart on any of the
// three. Tapped$ True is event-backed for EVERY shape through this path:
// this settle's own tail emits the hand-origin entry Tap (the object path,
// the library search's library-origin branch and the Dig windows carry their
// own), so no card this helper moves onto the battlefield silently enters
// untapped.
//
// The exiled-with association and the RememberChanged$ event-backed rider
// (eventRemember) are NOT done here: they are scoped to the two ORIGINAL
// ChangeZone movers that carried them before this helper existed (the
// object-target loop in effChangeZone and applyLibrarySearch's hidden-search
// mover), not to every caller of this now-shared settle path -- widening
// their scope here would move acceptance-game replay hashes beyond the
// reviewed change.

// gainControlOf resolves a ChangeZone SA's GainControl$ parameter (Reanimate's
// "onto the battlefield under your control", Control Magic-family Steal
// effects' "under your control") and returns the player the moved object must
// come under the control of. Forge's ChangeZoneEffect names the gain target
// in that one parameter: "True" and "You" both mean the resolving
// controller (the corpus's 287 True lines and 43 You lines); every other
// value is a player selector resolved through the shared Defined grammar
// (ChosenPlayer, Targeted, Player.IsRemembered, ParentTarget, ...), taking
// the first resolved player deterministically. The third return reports
// whether the parameter is PRESENT at all; the second whether the value
// resolved. An unresolvable value (a spec whose resolution names no player,
// or one the Defined grammar does not know) is false and the caller is loud
// rather than silently keeping the owner -- the same fail-closed convention
// every unread parameter here follows.
func gainControlOf(h Host, c *Ctx, sa *cards.SA) (state.PlayerID, bool, bool) {
	raw, present := sa.Params["GainControl"]
	if !present || strings.TrimSpace(raw) == "" {
		return 0, false, true
	}
	if strings.EqualFold(raw, "True") || strings.EqualFold(raw, "You") {
		return c.Controller, true, true
	}
	targets, known := definedSpec(h, c, raw)
	if !known {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "unrecognised ChangeZone GainControl$ " + raw})
		return 0, false, true
	}
	for _, t := range targets {
		if t.IsPlayer {
			return t.Player, true, true
		}
		if o := h.Game().Obj(t.Obj); o != nil {
			return o.Controller, true, true
		}
	}
	// The selector resolved (its grammar is known) but named no living
	// player: nothing to hand control to, and no silent owner-keep either.
	return 0, false, true
}

// applyGainControl emits the ControlChange that hands a just-moved object to
// the GainControl$ player, after the Move has placed it. Order matters: the
// move establishes the entry (controller = owner, CR 400.7), the control
// change is the CR 701.22a "gains control" step on top, and replay folds the
// two events in the same order live does.
func applyGainControl(h Host, c *Ctx, sa *cards.SA, id state.ObjID) {
	p, ok, present := gainControlOf(h, c, sa)
	if !present || !ok {
		return
	}
	if o := h.Game().Obj(id); o != nil && o.Controller != p {
		h.Emit(events.Event{Kind: events.ControlChange, Obj: id, Player: p,
			Text: "GainControl"})
	}
}

// applyTransformed implements ChangeZone's Transformed$ True: a double-faced
// card this effect moves to the battlefield enters TRANSFORMED (CR 711.10a:
// a transforming double-faced card enters with its back face up when an
// effect says so) — the Ojer Axonil death trigger's "return it to the
// battlefield tapped and transformed", the Kytheon/Kumano "return it
// transformed" returns. The flip is the one FlipFace event effSetState
// emits, to the face AFTER the one the card carries out of its zone.
//
// Callers emit this BEFORE the MoveZone, while the card is still in its
// origin zone, so events.Apply's Move sees the face the permanent actually
// enters with. That matters when the back face is a planeswalker: CR 306.5b
// grants loyalty counters on the ENTRY face, and Move reads o.Face(). Flipping
// after the move (the old order) granted nothing, so the walker entered at 0
// loyalty and rules/sba.go killed it. Flip-then-move is the same order the
// modal-land play path uses (rules/legal.go) and the order the CR 712.4d
// land-back test pins. A card with fewer than two faces is not a transform
// and is left alone.
func applyTransformed(h Host, c *Ctx, sa *cards.SA, id state.ObjID) {
	if !strings.EqualFold(strings.TrimSpace(sa.Params["Transformed"]), "True") {
		return
	}
	o := h.Game().Obj(id)
	if o == nil || o.Card == nil || len(o.Card.Faces) < 2 {
		return
	}
	next := (int(o.FaceIdx) + 1) % len(o.Card.Faces)
	h.Emit(events.Event{Kind: events.FlipFace, Obj: id, Amount: int32(next), Text: "Transformed"})
}

// attackingEntryKind is what a move body's `Attacking$` rider resolved to for
// one call: see attackingEntry.
type attackingEntryKind uint8

const (
	attackingEntryNone        attackingEntryKind = iota // no rider, or a non-battlefield destination
	attackingEntryAttacks                               // literal True with a bound trigger defender
	attackingEntryNoDefender                            // literal True, but no defender in context
	attackingEntryUnsupported                           // a selector form (Remembered, TriggeredDefender, ...)
)

// attackingEntry is ONE move call's classification of its `Attacking$` entry
// rider (Alesha's "tapped and attacking" graveyard return, Preeminent
// Captain's Soldier, the Dig "onto the battlefield attacking" family).
//
// The rider is a property of the EFFECT, not of each card the effect moves, so
// every mover classifies it ONCE before its loop and the loop then applies only
// the derived entry state. That is what keeps the degrades honest: a call with
// no defending player in context, or one carrying a selector form this build
// does not model, emits exactly ONE deterministic loud Note for the whole call
// -- never one per moved object, which is what a multi-object ChangeZone or a
// Dig window would otherwise produce. It mirrors effects/token.go's
// TokenAttacking$ read, whose single-mint shape gets that for free.
type attackingEntry struct {
	kind     attackingEntryKind
	defender state.PlayerID
	note     string
	noted    bool
}

func classifyAttackingEntry(c *Ctx, sa *cards.SA, to state.Zone) attackingEntry {
	if to != state.ZBattlefield {
		return attackingEntry{}
	}
	attack := strings.TrimSpace(sa.Params["Attacking"])
	if attack == "" {
		return attackingEntry{}
	}
	if strings.EqualFold(attack, "True") {
		if c.DefendingPlayer.IsPlayer {
			return attackingEntry{kind: attackingEntryAttacks, defender: c.DefendingPlayer.Player}
		}
		return attackingEntry{kind: attackingEntryNoDefender,
			note: "Attacking$ with no defending player in context; the permanent enters tapped but does not attack"}
	}
	return attackingEntry{kind: attackingEntryUnsupported,
		note: "Attacking$ " + attack + " is not implemented; the permanent enters but does not attack"}
}

// apply delivers the classified entry state for one moved object. A nil
// receiver is the "no rider" case every non-hoisting caller can pass.
func (a *attackingEntry) apply(h Host, c *Ctx, id state.ObjID, player state.PlayerID, to state.Zone) {
	if a == nil || a.kind == attackingEntryNone || to != state.ZBattlefield {
		return
	}
	if a.kind == attackingEntryAttacks {
		h.Emit(events.Event{Kind: events.TokenAttacks, Obj: id, Player: player,
			IDs: []state.ObjID{state.ObjID(a.defender)}, Text: "entered attacking"})
		return
	}
	if a.kind == attackingEntryNoDefender {
		// An "enters attacking" object must enter TAPPED even when the
		// trigger context cannot identify a defender. Keep that entry state
		// while degrading only the attack assignment; callers that already
		// emitted their Tapped$ entry event do not get a duplicate Tap.
		if o := h.Game().Obj(id); o != nil && !o.Tapped {
			h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: player, Text: "entered tapped"})
		}
	}
	if a.noted {
		return
	}
	a.noted = true
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller, Text: a.note})
}

func settleChangeZoneMoveAs(h Host, c *Ctx, sa *cards.SA, id state.ObjID, from, to state.Zone, withKind string, withAmt int32, player state.PlayerID, hasPlayer bool, rider *attackingEntry) {
	// A CantExile restriction (The Master, Multiplied) swallows the exile
	// before it happens: the object stays where it is, no MoveZone event is
	// emitted and none of this settle path's riders (exiled-with, exile-return,
	// imprint) run. This is the shared ChangeZone settle every mover below the
	// two inlined paths (effChangeZone's object loop and applyLibrarySearch's
	// library-origin move) funnels through, so a battlefield token can never be
	// taken by an exile that reached here instead.
	if to == state.ZExile && h.ExileBlocked(id, false) {
		return
	}
	ev := moveZoneEvent(c, id, from, to)
	if strings.EqualFold(sa.Params["RememberLKI"], "True") {
		if o := h.Game().Obj(id); o != nil {
			c.ChangeZoneLKI = append(c.ChangeZoneLKI, state.LKIObject{Obj: id, Controller: o.Controller, Owner: o.Owner})
		}
	}
	if to == state.ZExile && len(ev.IDs) == 0 && (faceStaticsNameExiledWithSource(h, c.Source) || strings.EqualFold(strings.TrimSpace(sa.Params["Imprint"]), "True")) {
		// The S: static spelling of the same provenance need: a source whose
		// own Static lines name ExiledWithSource (Intellect Devourer's
		// MayPlay+ExiledWithSource grant) tracks its exiles exactly like the
		// SVar shapes exileProvenanceNeeded covers; Imprint$ True is the
		// Chrome Mox spelling, feeding the Defined.Imprinted reflected-mana
		// selector. Extra IDs on an exile move are inert for every consumer
		// that never reads them.
		ev.IDs = []state.ObjID{c.Source}
	}
	if hasPlayer {
		ev.Player = player
	}
	applyFaceDownMarker(h, sa, c, &ev, to)
	// A Transformed$ True entry flips to the back face BEFORE the MoveZone is
	// folded, so events.Apply's Move grants CR 306.5b loyalty for the face the
	// permanent enters with. See applyTransformed.
	if to == state.ZBattlefield {
		applyTransformed(h, c, sa, id)
	}
	h.Emit(ev)
	if to == state.ZExile {
		recordExileReturn(h, c, sa, id, from, to)
	}
	// Imprint$ True on the shared settle path (Dakra Mystic's DBPutRevealed:
	// `Defined$ Remembered | Origin$ Library | Destination$ Graveyard |
	// Imprint$ True`): Forge records every card a ChangeZone moved in the
	// source's persistent imprintedCards association, whatever the
	// destination, and a later `Defined$ Imprinted` sub reads it back
	// (Dakra's follow-up draw is gated `ConditionDefined$ Imprinted ...
	// EQ0`). The card joins the source's association through the ordinary
	// events.Imprint association, so replay folds it. Confirmed by the
	// object's post-move zone: a skipped candidate is never imprinted, and a
	// token never is. This is the one settle path every movement route
	// shares; the two inlined movers that predate it (the object-target loop
	// and applyLibrarySearch's library-origin branch) keep their own
	// collection and do not call through here, so nothing is recorded twice.
	if strings.EqualFold(strings.TrimSpace(sa.Params["Imprint"]), "True") && c.Source != 0 {
		if o := h.Game().Obj(id); o != nil && o.Zone == to && !o.IsToken {
			if strings.EqualFold(strings.TrimSpace(sa.Params["ImprintLast"]), "True") {
				clearChangeZoneImprint(h, c)
			}
			h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: []state.ObjID{id}})
		}
	}
	// RememberLKI$ True (the corpus's 77 ChangeZone lines -- Reanimate's
	// "creature card" whose mana value the chained lose-life SVar reads,
	// RememberedLKI$CardManaCost) joins the moved object to the ability's
	// Remembered. Same Ctx binding RememberChanged$ uses: a resolution-local
	// value, replayed identically because replay re-runs the same SA. The two
	// flags stack; an object is not remembered twice. RememberLKI$
	// Targeted (2 lines, a different capture point -- the CHOSEN target, not
	// the moved object) is left to its own work and is not silently folded
	// into this read.
	if strings.EqualFold(sa.Params["RememberLKI"], "True") &&
		!strings.EqualFold(sa.Params["RememberChanged"], "True") {
		c.Remembered = append(c.Remembered, state.Target{Obj: id})
	}
	if strings.EqualFold(sa.Params["RememberChanged"], "True") {
		c.Remembered = append(c.Remembered, state.Target{Obj: id})
	}
	if withKind != "" && counterDestination(to) {
		h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: withKind, Amount: withAmt})
	}
	// GainControl$ hands the moved object to the named player. Only a
	// battlefield entry can carry a control change (CR 701.22a controls
	// permanents); a card moved to a hidden or public non-battlefield zone
	// keeps its owner.
	if to == state.ZBattlefield {
		applyGainControl(h, c, sa, id)
		// Tapped$ True (CR 110.5's entry state) for the hand-origin movers:
		// the same "entered tapped" Tap the object path, the library search's
		// library-origin branch and the Dig windows emit. Gated on the hand
		// origin because this helper's OTHER callers (the hidden pick, the
		// library search's alternative-origin branch) emit their own Tap after
		// the call and a second one here would double-emit.
		if from == state.ZHand && strings.EqualFold(sa.Params["Tapped"], "True") {
			h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: player, Text: "entered tapped"})
		}
		rider.apply(h, c, id, player, to)
		// StaticEffect$ <name> (the "return it ... It's a Spirit Detective"
		// rider): the named Continuous static registers onto the moved card
		// once its move and entry riders are settled. A no-op on every SA
		// without the parameter.
		applyStaticEffect(h, c, sa, to, []state.ObjID{id})
		// LeaveBattlefield$ Exile (Isareth the Awakener, From the Catacombs):
		// the promise rides the entered object for as long as it stays on the
		// battlefield (effects/leavebattlefield.go) -- no Duration$ on either
		// carrier, and the move sweep ends it on the departure itself.
		registerLeaveExile(h, c, id, sa.Params["LeaveBattlefield"], "", true)
	}
}

// exiledWithAssociation emits Forge's ChangeZoneEffect.handleExiledWith
// association for a non-token card this effect just exiled: the host's
// distinct exiledCards collection. It is deliberately NOT an ImprintCards$
// association: DefinedCards$ ExiledWith consumes this list, while
// ImprintedController only consumes explicit ImprintCards$ entries. Scoped to
// the object-target loop and applyLibrarySearch, the two movers that carried
// this association originally.
func exiledWithAssociation(h Host, c *Ctx, id state.ObjID, to state.Zone) {
	if to != state.ZExile || c.Source == 0 {
		return
	}
	if o := h.Game().Obj(id); o != nil && !o.IsToken {
		h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: []state.ObjID{id}, Text: "exiled-with"})
	}
}

// recordExileReturn reads ChangeZone's Duration$ parameter. The corpus's
// whole ChangeZone Duration$ population (118 raw .cards/cardsfolder lines:
// 111 Origin$ Battlefield, 6 Hand, 1 Graveyard, plus 7 on ChangeZoneAll)
// carries the single value UntilHostLeavesPlay -- the Oblivion Ring /
// Banisher Priest pattern, "exile ... until CARDNAME leaves the battlefield".
// For it, the exiled object joins the source's event-backed ExileReturn
// association carrying the zone it was exiled from; when the source leaves
// the battlefield the rules sweep (Engine.sweepExileReturn) returns every
// object still in exile to that zone under its owner's control, which is
// Forge's own return semantic for the duration (a battlefield-origin exile
// comes back to the battlefield, a hand-origin one to the hand). A card is
// only recorded when the move actually landed in exile, and a token is never
// recorded: a token that left the battlefield has ceased to exist (CR 111.7)
// and must not come back. Any other Duration$ value is loud (a Note) rather
// than silently inert, the convention every unread parameter here follows.
func recordExileReturn(h Host, c *Ctx, sa *cards.SA, id state.ObjID, from, to state.Zone) {
	raw, present := sa.Params["Duration"]
	if !present || strings.TrimSpace(raw) == "" {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(raw), "UntilHostLeavesPlay") {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "unmodelled ChangeZone Duration$ " + strings.TrimSpace(raw)})
		return
	}
	if to != state.ZExile || c.Source == 0 {
		return
	}
	if o := h.Game().Obj(id); o == nil || o.Zone != state.ZExile || o.IsToken {
		return
	}
	h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: []state.ObjID{id},
		Amount: int32(from), Text: "until-host-leaves"})
}

// handChangeNum reads the SA's ChangeNum$ as a plain integer literal
// (absent = 1, Forge's ChangeZoneEffect default for this shape). The second
// return is false for anything else -- a non-integer, negative, or value
// outside a decision count's signed 32-bit range -- and the caller routes
// that SA to the pre-existing object path
// instead: evaluating SVar/Count$ count expressions here is a scoped-out
// follow-up, not part of handmove1.
func handChangeNum(sa *cards.SA) (int32, bool) {
	v, present := sa.Params["ChangeNum"]
	if !present || v == "" {
		return 1, true
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil || n < 0 {
		return 0, false
	}
	return int32(n), true
}

// effChangeZoneHand is the whole-hand shape (handmove1/rv2b r1): Origin$ Hand
// with no player selector -- the resolving controller's own hand is the one
// owner, and the controller is its own chooser (Brainstorm, Jace the Mind
// Sculptor's [0], Sawtooth Loon, Burgeoning). The mechanics -- the ask gate
// (the dig1/effDiscard strict-supersets rule), explicit markers plus real
// card/script text for markerless optionality (never an assumed "may"), the
// fx42 re-entry scoping, the R-9 stand-in, the Destination$ Library placement
// (absent LibraryPosition$ = TOP in answer order; Shuffle$ True randomises instead)
// and the unread-parameter list -- are handMoveOwnersWalk's, which this
// delegates to with the one-owner, chooser==owner, no-random configuration.
// Only a literal ChangeNum$ (or its absent default 1) reaches here: the
// routing in effChangeZone Notes a non-literal before this is ever called.
func forgetOtherRemembered(h Host, c *Ctx, sa *cards.SA) {
	if strings.EqualFold(strings.TrimSpace(sa.Params["ForgetOtherRemembered"]), "True") && !c.ForgetOtherCleared {
		c.Remembered = nil
		clearEventRemembered(h, c)
		if c.ForgetOtherReady {
			c.ForgetOtherCleared = true
		}
	}
}

// A walk that re-runs its candidate filter across an ask must match every
// owner's candidates against the memory from BEFORE the first move. The
// actual remembered list is still cleared at the first move and rebuilt by
// events; this snapshot is only a filter input. It rides the owner cursor
// across asks, including the answered owner's recheck. minOwners is the
// walk's own continuation shape: 2 for a walk whose filter is only read for
// owners AFTER an answered ask, 1 for a walk whose answered owner's filter
// re-runs on re-entry (effDigUntil's re-scan). A walk whose answered
// revalidation instead reads the ask's ResumeRemembered ride never arms it.
func initForgetOtherSnapshot(h Host, c *Ctx, sa *cards.SA, owners []state.PlayerID, minOwners int) {
	if len(owners) < minOwners || c.ForgetOtherReady || !strings.EqualFold(strings.TrimSpace(sa.Params["ForgetOtherRemembered"]), "True") {
		return
	}
	c.ForgetOtherReady = true
	c.ForgetOtherOwners = append([]state.PlayerID(nil), owners...)
	c.ForgetOtherSnapshot = append([]state.Target(nil), c.Remembered...)
	if src := h.Game().Obj(c.Source); src != nil {
		c.ForgetOtherSnapshot = append(c.ForgetOtherSnapshot, src.Remembered...)
	}
}

func endForgetOtherSnapshot(c *Ctx) {
	c.ForgetOtherSnapshot = nil
	c.ForgetOtherOwners = nil
	c.ForgetOtherReady, c.ForgetOtherCleared = false, false
}

func forgetOtherSpecContext(c *Ctx) SpecContext {
	sc := c.SpecContext(c.Controller)
	if c.ForgetOtherReady {
		sc.Remembered = append(append([]state.Target(nil), sc.Remembered...), c.ForgetOtherSnapshot...)
	}
	return sc
}

// forgetOtherPreClearContext is the ONE spec-context read every
// ForgetOtherRemembered$ walk's candidate filter goes through: before the
// snapshot exists the walk's own pre-clear shallow copy is authoritative
// (the set the first pass matched its options under), and once
// initForgetOtherSnapshot has armed the Ctx the snapshot is authoritative,
// riding every ask so a resumed walk re-matches the pre-clear candidates
// against memory the first move cleared. One read for every affected
// primitive so the two carriers cannot drift.
func forgetOtherPreClearContext(sel, c *Ctx) SpecContext {
	if c.ForgetOtherReady {
		return forgetOtherSpecContext(c)
	}
	return sel.SpecContext(sel.Controller)
}

func effChangeZoneHand(h Host, c *Ctx, sa *cards.SA, to state.Zone) {
	count, _ := handMoveCountOf(h, c, sa)
	handMoveOwnersWalk(h, c, sa, to, []state.PlayerID{c.Controller}, count, false, nil, false)
}

// handMoveCount is the ChangeNum$ bound one hidden-hand walk carries: either
// a fixed bound resolved once for the whole resolution, or the per-owner
// eligible count (ChangeNum$ NumInHand / HandSize -- Forge's "all matching
// cards in that hand" texts: Eradicate, Extirpate, Kotose, Lost Legacy, The
// Great Aurora).
type handMoveCount struct {
	fixed    int32
	perOwner bool
}

// handMoveCountOf classifies a hidden-hand walk's ChangeNum$. Absent reads as
// 1 (Forge's ChangeZoneEffect default). "NumInHand"/"HandSize" are the
// per-owner spellings. A plain integer literal is that literal. An SVar-named
// count (ChangeNum$ X / Y over an SVar: body) or a bare "X" (the paid X,
// CR 107.3i) resolves through the ordinary count evaluator, bound to the
// resolving context. Anything else returns false and the caller is loud (a
// Note) rather than degrading to a silent zero-count no-op.
func handMoveCountOf(h Host, c *Ctx, sa *cards.SA) (handMoveCount, bool) {
	raw := strings.TrimSpace(sa.Params["ChangeNum"])
	if raw == "" {
		return handMoveCount{fixed: 1}, true
	}
	if strings.EqualFold(raw, "NumInHand") || strings.EqualFold(raw, "HandSize") {
		return handMoveCount{perOwner: true}, true
	}
	if n, ok := handChangeNum(sa); ok {
		return handMoveCount{fixed: n}, true
	}
	resolvable := raw == "X" || strings.HasPrefix(raw, "Count$") ||
		strings.HasPrefix(raw, "Sacrificed$") || strings.HasPrefix(raw, "TriggerCount$") ||
		strings.HasPrefix(raw, "TriggerCountMax$")
	if c != nil && c.SVars != nil {
		if _, exists := c.SVars[raw]; exists {
			resolvable = true
		}
	}
	if !resolvable {
		return handMoveCount{}, false
	}
	n := Num(h, c, sa, "ChangeNum", 1)
	if n < 0 {
		n = 0
	}
	return handMoveCount{fixed: n}, true
}

// effChangeZoneHandOwners implements the owner-SELECTED hidden-hand shape
// (rv2b r2): Origin$ Hand with DefinedPlayer$-alone (Kynaios and Tiro's "each
// player may put a land card from their hand onto the battlefield", Braids,
// Conjurer Adept, Mindleech Ghoul) or ValidTgts$-alone naming the players
// whose hand moves (Karn Liberated's "[+4]: Target player exiles a card from
// their hand", Kyoki, Sanity's Eclipse). One chooser ask per hand owner,
// chained across owners through the persisted Ctx.HandMoveTarget cursor --
// the walk restarts on every answer, skips the owners already answered, and
// asks the next one -- exactly effDig's per-target continuation, but with a
// REAL ask for every later owner rather than a deterministic stand-in (the
// hand owners are few and each ask is short). Whose hand and who answers are
// the two selectors this shape carries: the owners come from
// DefinedPlayer$/ValidTgts$, the chooser from Chooser$ (Forge's default is
// the hand owner). Every shape this function cannot model emits a Note and
// moves nothing -- the finding's floor: never a silent no-op.
func effChangeZoneHandOwners(h Host, c *Ctx, sa *cards.SA, to state.Zone) {
	// On a resume of a multi-owner walk, the owner cursor's captured list is
	// authoritative: the first move may have cleared the remembered set the
	// owner selector reads (DefinedPlayer$ RememberedOwner with
	// ForgetOtherRemembered$ and no RememberChanged$), so recomputing here
	// would return no owners and the empty-owner guard below would return
	// before handMoveOwnersWalk can restore the list -- dropping the later
	// owner's already-answered move. handMoveOwnersWalk's own entry restores
	// the same list; this restores it early enough to survive the guards.
	owners, ok := c.ForgetOtherOwners, true
	if !c.ForgetOtherReady {
		owners, ok = handMoveOwners(h, c, sa)
	}
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "cannot resolve the hand owner (DefinedPlayer$ " + strings.TrimSpace(sa.Params["DefinedPlayer"]) +
				"); no hand card moves"})
		return
	}
	if len(owners) == 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "the hand owner selector names no player this engine can resolve; no hand card moves"})
		return
	}
	count, ok := handMoveCountOf(h, c, sa)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "cannot choose ChangeNum$ " + strings.TrimSpace(sa.Params["ChangeNum"]) +
				" cards from a selected hand (a count this engine cannot evaluate)"})
		return
	}
	choosers := make([]state.PlayerID, len(owners))
	for i, owner := range owners {
		ch, ok := handMoveChooserFor(h, c, sa, owner)
		if !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "Chooser$ " + strings.TrimSpace(sa.Params["Chooser"]) +
					" is not a chooser this engine can resolve; no hand card moves"})
			return
		}
		choosers[i] = ch
	}
	random := strings.EqualFold(strings.TrimSpace(sa.Params["AtRandom"]), "True")
	handMoveOwnersWalk(h, c, sa, to, owners, count, random, func(_ Host, _ *Ctx, _ *cards.SA, owner state.PlayerID) (state.PlayerID, bool) {
		return choosers[ownerIndex(owners, owner)], true
	}, true)
}

// ownerIndex is the position of owner in owners (owners is small and built
// without duplicates).
func ownerIndex(owners []state.PlayerID, owner state.PlayerID) int {
	for i, p := range owners {
		if p == owner {
			return i
		}
	}
	return 0
}

// handMoveOwners resolves whose hands an owner-selected hidden-hand ChangeZone
// moves from. DefinedPlayer$ takes precedence and resolves through the same
// deterministic selector grammar the library search uses (searchPlayers); a
// ValidTgts$-alone line's chosen targets are the hand owners. It fails
// CLOSED: a player spec this build does not model returns ok=false and the
// caller emits its loud Note -- degrading an unmodelled selector to the
// resolving controller's hand would move (and reveal) cards from the WRONG
// player's hidden hand, which is worse than moving none.
func handMoveOwners(h Host, c *Ctx, sa *cards.SA) ([]state.PlayerID, bool) {
	if spec := strings.TrimSpace(sa.Params["DefinedPlayer"]); spec != "" {
		if _, modelled := definedSpec(h, c, spec); !modelled {
			return nil, false
		}
		return searchPlayers(h, c, sa), true
	}
	if plainRememberedSelector(sa.Params["Defined"]) {
		// A remembered PLAYER is a legitimate hand owner; the plain family no
		// longer drops it just because a remembered CARD coexists in the set.
		return definedPlayers(h, c, sa), true
	}
	// ValidTgts$-alone: Defined's own rule names the chosen targets.
	owners := make([]state.PlayerID, 0, len(c.Targets))
	seen := make(map[state.PlayerID]bool, len(c.Targets))
	for _, t := range Defined(h, c, sa) {
		if !t.IsPlayer {
			// A hand-ownership selector that resolved to a card is not a shape
			// this chooser can honour.
			return nil, false
		}
		p := PlayerOf(h, c, t)
		if int(p) >= len(h.Game().Players) || seen[p] {
			continue
		}
		seen[p] = true
		owners = append(owners, p)
	}
	return owners, true
}

// handMoveChooserFor resolves who answers one owner's hidden-hand ask.
// Forge's default for the shape is the hand owner (Kynaios and Tiro's "each
// player may put", Mindleech Ghoul's "defending player exiles a card from
// their hand"); Chooser$ You is the caster picking out of another player's
// hand (Kitesail Freebooter, Witness the End), Chooser$ Targeted the chosen
// target (Karn Liberated), and the TriggeredTarget/TriggeredPlayer spellings
// the causing event's bound player (Kheru Mind Eater, Widespread Panic). An
// unmodelled value fails closed (ok=false) so the caller is loud rather than
// handing the ask to a guessed seat.
func handMoveChooserFor(h Host, c *Ctx, sa *cards.SA, owner state.PlayerID) (state.PlayerID, bool) {
	switch strings.TrimSpace(sa.Params["Chooser"]) {
	case "", "Owner":
		return owner, true
	case "You":
		return c.Controller, true
	case "Targeted":
		if len(c.Targets) > 0 {
			return PlayerOf(h, c, c.Targets[0]), true
		}
		return owner, true
	case "TriggeredTarget":
		if c.TriggerTarget.IsPlayer {
			return c.TriggerTarget.Player, true
		}
		if c.TriggerTarget.Obj != 0 {
			if o := h.Game().Obj(c.TriggerTarget.Obj); o != nil {
				return o.Controller, true
			}
		}
		return owner, true
	case "TriggeredPlayer":
		if c.TriggerPlayer.IsPlayer {
			return c.TriggerPlayer.Player, true
		}
		return owner, true
	case "ChosenPlayer", "Player.Chosen":
		// The chosen player, resolved through the SAME shared read
		// searchChooser/hiddenPickChooser use. With none bound or the seat
		// gone, fail closed (never fall to the hand owner: a hidden-hand
		// move from the wrong seat is worse than moving none).
		if p, ok := chooserChosenPlayer(h, c); ok {
			return p, true
		}
		return owner, false
	}
	return owner, false
}

// handMoveOwnersWalk is the ONE hidden-hand mover both shapes share: for each
// owner in order the eligible pool is that owner's hand filtered through
// ChangeType$ (absent: the whole hand), the bound is the count (perOwner:
// that pool's own size), and the pick is one of three shapes -- the chained
// ask (STRICTLY more eligible cards than the bound: a real KChoose to the
// chooser, Min 0 when the take is optional else the bound, Max the bound, the
// answer re-entering through ResumeKind "hand_move" with ResumeTarget
// binding it to this owner), the no-choice deterministic take (a REQUIRED
// move with eligible <= bound: every eligible card moves, in hand order, no
// ask), or AtRandom$'s engine-random pick (no ask: randomness, not a player
// choice, picks). An OPTIONAL move takes the choice path whenever there is
// at least one eligible card, including eligible <= bound: declining remains
// a meaningful answer even when taking every card is the only nonempty pick
// (an empty-only ChangeNum$ 0 still resolves through AskEmpty). The re-entry
// contract (fx42 scoping): Ctx.HandMove/HandMoveDone/HandMoveTarget are captured and
// cleared at the top of the walk; owners before the cursor completed before a
// later owner suspended and are skipped, the cursor's owner consumes the
// answer (moved exactly as answered, revalidated against the CURRENT hand
// and filter), and owners after it continue the chain.
//
// The whole-hand shape (handmove1/rv2b r1) is the one-owner case of this
// walk, with the chooser == the owner -- the r1 contracts (ask shape, card
// text/explicit-marker optionality, R-9 stand-in text, zero-eligible silence,
// library tail) are this walk's contracts, unchanged. Still unread here, each
// a scoped-out follow-up:
// Destination$ Hand/Sideboard oddities (2 lines), and any ConditionPresent$/
// ConditionDefined$ gate (the engine-wide Condition* gap).
func handMoveOwnersWalk(h Host, c *Ctx, sa *cards.SA, to state.Zone, owners []state.PlayerID,
	count handMoveCount, random bool, chooserFor func(Host, *Ctx, *cards.SA, state.PlayerID) (state.PlayerID, bool),
	eventPlayer bool) {
	spec := sa.Params["ChangeType"]
	if spec == "" {
		spec = "Card" // the whole hand: Brainstorm, Jace's [0], Sawtooth Loon
	}
	// A leading `Permanent` base in a HAND-origin move must read Forge's
	// "permanent CARD" (nta1): every candidate here is a card in a hand, so
	// the shared matcher's on-the-battlefield base reading (matchesBase)
	// can never be what the script meant -- `ChangeType$ Permanent...` from
	// hand matched NOTHING and the whole walk was a silent no-op (Kodama of
	// the East Tree's ETB rider, Kona Rescue Beastie, Mind into Matter; 29
	// raw corpus lines on an exact `Origin$ Hand`). The rewrite is the same
	// zone-aware normalizer the Dig windows (permanentCardSpec) and
	// rules/stack.go's targetSpecForZone already apply -- one leading token,
	// every qualifier riding along -- and it is safe for ALL of this
	// function's callers (whole-hand, owner-selected, random) because a
	// hand move has no on-battlefield candidates to mis-read.
	spec = permanentCardSpec(spec)
	// The per-type groups an EACH ChangeType asks for, computed once: the
	// sub-specs are a property of the SA, not of the hand owner.
	eachSubs, isEach := eachAlternatives(spec)
	if c.ForgetOtherReady {
		owners = c.ForgetOtherOwners
	}
	g := h.Game()
	// fx42 scoping: capture and clear the answered pick (and the cursor that
	// binds it to the owner that asked) BEFORE anything else, so a nested
	// hand-move ask below cannot inherit them.
	ans := c.HandMove
	done := c.HandMoveDone
	cursor := c.HandMoveTarget
	c.HandMove, c.HandMoveDone, c.HandMoveTarget = nil, false, 0
	// fx42 scoping for the Optional$ confirmation answer: consumed and cleared
	// before anything else so a nested hand move poses its own confirmation.
	confirmDone := c.HandMoveConfirmDone
	confirmYes := strings.EqualFold(c.HandMoveConfirm, "yes")
	confirmTarget := c.HandMoveConfirmTarget
	c.HandMoveConfirm, c.HandMoveConfirmDone, c.HandMoveConfirmTarget = "", false, 0
	withKind := sa.Params["WithCountersType"]
	var withAmt int32
	if withKind != "" && counterDestination(to) {
		withAmt = withCounterAmount(h, c, sa)
	}
	// settleHandMove settles one chosen card: exactly the shared ChangeZone
	// mover; on the owner-SELECTED shapes the Move event also carries the
	// hand's OWNER as its Player (a hidden-zone move of another player's
	// card -- the same attribution the library search's move carries),
	// while the whole-hand shape keeps its historical event shape
	// (eventPlayer false, the r1 golden contract). settleChangeZoneMoveAs's
	// tail is also the one Tapped$ True entry-state emitter for every
	// hand-origin mover, so concrete Defined$ objects and future hand-owner
	// selectors cannot silently miss the tapped entry.
	rider := classifyAttackingEntry(c, sa, to)
	// The pre-clear remembered snapshot rides the walk's asks: owner B's
	// candidates must still match the set the walk started with after
	// owner A's settle cleared both halves of the remembered state. It is
	// armed for a single owner too now that the walk clears BEFORE its first
	// ask (an accepted Optional$ confirmation or a mandatory entry), so the
	// answered re-entry's eligibility filter still sees the pre-clear set.
	initForgetOtherSnapshot(h, c, sa, owners, 1)
	// Snapshot eligibility before forgetting: an IsRemembered filter must
	// still admit an answered card after the old set has been cleared.
	eligibleByOwner := make([][]state.ObjID, len(owners))
	for i, owner := range owners {
		for _, id := range zoneOf(g, state.ZHand, owner) {
			if MatchesSpecCtx(g, spec, id, forgetOtherSpecContext(c)) {
				eligibleByOwner[i] = append(eligibleByOwner[i], id)
			}
		}
	}
	forgot := false
	settleHandMove := func(id state.ObjID, owner state.PlayerID) {
		if !forgot {
			forgetOtherRemembered(h, c, sa)
			forgot = true
		}
		settleChangeZoneMoveAs(h, c, sa, id, state.ZHand, to, withKind, withAmt, owner, eventPlayer, &rider)
		if strings.EqualFold(sa.Params["RememberChanged"], "True") {
			eventRemember(h, c, id)
		}
	}
	for i, owner := range owners {
		hand := zoneOf(g, state.ZHand, owner)
		eligible := eligibleByOwner[i]
		if done && i < cursor {
			// This owner answered on an earlier pass, before a later owner
			// suspended the walk. Re-running it could move a second batch, so
			// skip it (effDig's per-target continuation contract).
			continue
		}
		if done && i == cursor {
			// Re-entry: move exactly the answered cards that still sit in THIS
			// owner's hand and still match the filter (a stray answer must not
			// move an object that left the hand meanwhile), in the player's
			// answer order. Reaching this branch at all means the fetch was
			// ENTERED -- a mandatory move, or an accepted Optional$
			// confirmation -- so the event-backed memory is cleared exactly
			// once here, before the answered cards are settled, even when the
			// answer picked nothing (Forge clears at
			// ChangeZoneEffect.changeHiddenOriginResolve 1103 before the
			// choose, so an accepted search that finds nothing still clears).
			forgetOtherRemembered(h, c, sa)
			var moved []state.ObjID
			for _, id := range ans {
				if !containsID(hand, id) {
					continue
				}
				o := g.Obj(id)
				if o == nil || o.Zone != state.ZHand {
					continue
				}
				if !containsID(eligible, id) {
					continue
				}
				settleHandMove(id, owner)
				moved = append(moved, id)
			}
			handLibraryTail(h, g, sa, c.Source, owner, moved, to)
			// AtEOT$ on the hand walk: the owner's answered batch is the
			// affected set, scheduled per owner BEFORE the walk can suspend on a
			// later owner's ask (a suspension must not lose this batch's
			// registrations -- the re-entry skips already-answered owners and
			// never re-schedules them).
			scheduleAtEOT(h, c, sa, moved)
			continue
		}
		n := count.fixed
		if count.perOwner {
			n = int32(len(eligible))
		}
		var moved []state.ObjID
		// The chooser is a property of the SA and the owner, not of the
		// eligible pool, so it is resolved once here for both the Optional$
		// confirmation and the card pick below.
		chooser := owner
		if chooserFor != nil {
			chooser, _ = chooserFor(h, c, sa, owner)
		}
		if random {
			// AtRandom$ True: the engine picks, not a player -- ChangeNum$
			// random distinct eligible cards (corpus: always 1, mandatory),
			// through the seeded generator, so the pick replays. No player can
			// decline an engine pick, so the fetch is entered unconditionally
			// here and the event-backed memory is cleared even when no card can
			// be drawn.
			forgetOtherRemembered(h, c, sa)
			if len(eligible) == 0 || n == 0 {
				continue
			}
			pool := append([]state.ObjID(nil), eligible...)
			for k := int32(0); k < n && len(pool) > 0; k++ {
				j := h.Rand(len(pool))
				settleHandMove(pool[j], owner)
				moved = append(moved, pool[j])
				pool = append(pool[:j], pool[j+1:]...)
			}
			handLibraryTail(h, g, sa, c.Source, owner, moved, to)
			scheduleAtEOT(h, c, sa, moved)
			continue
		}
		// Forge's Optional$ confirmation (ChangeZoneEffect's confirmAction gate,
		// which runs BEFORE the card pick): a script that carries the marker asks
		// the decider whether to proceed. A decline skips this owner with the
		// remembered set intact; only an accepted confirmation enters the fetch.
		// The markerless may-shapes handTakeOptional recognises stay
		// confirmation-free: Forge expresses their may as a null pick, not as a
		// confirmation, so their empty answer is an accepted fetch that clears.
		if handMoveConfirms(sa) {
			if confirmDone && i < confirmTarget {
				// This owner's confirmation was already answered on an earlier
				// pass (declined, or accepted with its pick completed); do not
				// ask again and do not clear for it.
				continue
			}
			if !confirmDone {
				prompt := strings.TrimSpace(sa.Params["OptionalPrompt"])
				if prompt == "" {
					prompt = "Proceed with moving a card from hand?"
				}
				cd := &decision.Decision{Player: chooser, Kind: decision.KChoose,
					Min: 1, Max: 1, Source: c.Source,
					ResumeKind: "hand_move_confirm", ResumeSA: sa, ResumeTarget: i,
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
				if Ask(h, cd) == AskAsked {
					return
				}
				// R-9: no host to ask -- play "may" as "do" deterministically,
				// the same fallback moveDefinedLibraryObjects applies.
			} else if i == confirmTarget && !confirmYes {
				confirmDone = false // this owner's decline is consumed; later owners still confirm
				continue            // declined: keep the remembered set
			} else if i == confirmTarget {
				confirmDone = false // this owner's acceptance is consumed
			}
		}
		if len(eligible) == 0 || n == 0 {
			// No eligible card, or an empty-only ChangeNum$ 0 choice. An
			// ordinary (non-ForgetOther) empty hand move completes silently
			// before optionality is read -- AskEmpty's contract. A
			// ForgetOtherRemembered$ fetch that is entered (mandatory, a
			// markerless may-shape, or an accepted Optional$ confirmation)
			// clears its remembered set: Forge clears before the choose and
			// does not require a nonempty fetchList.
			if strings.EqualFold(strings.TrimSpace(sa.Params["ForgetOtherRemembered"]), "True") {
				forgetOtherRemembered(h, c, sa)
			}
			continue
		}
		// NumInHand/HandSize means "all matching cards in that hand", an
		// intrinsically required all-cards move (Eradicate, Extirpate, The
		// Great Aurora). Its count semantics settle optionality even when the
		// script has neither marker nor explanatory text; preserve an explicit
		// Optional$ marker should a future script carry one.
		intrinsicAll := count.perOwner && strings.TrimSpace(sa.Params["Optional"]) == "" && strings.TrimSpace(sa.Params["Mandatory"]) == ""
		optional, optionalKnown := handTakeOptional(h, c, sa, to)
		if intrinsicAll {
			optional, optionalKnown = false, true
		}
		if !optionalKnown {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "cannot determine whether the markerless hand move is optional; no hand card moves"})
			return
		}
		// The per-type pick structure (each1's object-path fix): an EACH hand
		// move is one move of EACH listed type, never a flat count over the
		// union -- Michelangelo Improvisers' "EACH Creature & Land" must be
		// able to move a creature AND a land, not one card from the union. The
		// candidates join the FIRST sub-spec that matches them
		// (EachTypeGroups), so a card matching two listed qualities is offered
		// once, in one Group, and picking it cannot block the other type's
		// pick; per-type ChangeNum$ rides Decision.GroupLimit when it is above
		// 1. AtRandom$ and the all-matching perOwner shapes keep their flat
		// walks (measured-absent with EACH; the count semantics are their own).
		structured := isEach && !random && !count.perOwner
		var eachGroups [][]state.ObjID
		var eachPerType int32
		var ceiling int
		if structured {
			eachPerType = n
			eachGroups = EachTypeGroups(g, eachSubs, eligible, c.SpecContext(c.Controller))
			for _, ids := range eachGroups {
				k := int32(len(ids))
				if k > eachPerType {
					k = eachPerType
				}
				ceiling += int(k)
			}
			if !optional && ceiling == len(eligible) {
				// A required structured move with no selection alternative: every
				// group's pool fits its per-type count, so the only legal answer
				// takes all of eligible -- the same deterministic take-all the
				// flat shape takes below, in the same (hand) order.
				for _, id := range eligible {
					settleHandMove(id, owner)
					moved = append(moved, id)
				}
				handLibraryTail(h, g, sa, c.Source, owner, moved, to)
				scheduleAtEOT(h, c, sa, moved)
				continue
			}
		}
		if !structured && int32(len(eligible)) <= n && !optional {
			// A required move with no possible nonempty selection alternative
			// takes every eligible card deterministically. An OPTIONAL move
			// must still ask here: declining is a distinct, legal answer even
			// when every nonempty answer takes all eligible cards.
			for _, id := range eligible {
				settleHandMove(id, owner)
				moved = append(moved, id)
			}
			handLibraryTail(h, g, sa, c.Source, owner, moved, to)
			scheduleAtEOT(h, c, sa, moved)
			continue
		}
		min := int(n)
		if optional {
			min = 0 // "you may put": none is a legal answer
		}
		d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
			Min: min, Max: int(n), Source: c.Source,
			ResumeKind: "hand_move", ResumeSA: sa, ResumeTarget: i,
			// The re-entered walk revalidates the answered cards against the
			// SAME filter it offered them under (Card.IsRemembered in Vizkopa
			// Confessor's PickOne, whose remembered population is ctx-level
			// only -- RememberRevealed$), so the ask must RIDE that set the way
			// every other mid-resolution ask boundary does (attach.go,
			// counters.go, play.go): without it the rebuild loses the ctx-level
			// Remembered and the revalidation re-eligible-matches nothing.
			// The ForgetOtherRemembered$ pre-clear snapshot rides with it: a
			// LATER owner's pool (and any answered revalidation after an
			// earlier owner's settle cleared the live set) still reads the
			// candidates the walk started with.
			ResumeRemembered:          copyTargets(c.Remembered),
			ResumeForgetOtherSnapshot: copyTargets(c.ForgetOtherSnapshot),
			ResumeForgetOtherOwners:   append([]state.PlayerID(nil), c.ForgetOtherOwners...),
			ResumeForgetOtherReady:    c.ForgetOtherReady,
			ResumeForgetOtherCleared:  c.ForgetOtherCleared,
			Prompt:                    handMovePromptFor(sa, to, int(n), chooser == owner)}
		for _, id := range eligible {
			name := "a card"
			if o := g.Obj(id); o != nil && o.Face() != nil {
				name = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "hand_move", Label: name, Obj: id, Player: owner})
		}
		if structured {
			// Replace the flat range and option list with the per-type one: the
			// ceiling (each group's min(perType, size) summed) bounds the ask,
			// a required move demands it whole, and one option per candidate
			// carries its group's ordinal. The flat loop above has already
			// appended the union options -- rebuild from scratch.
			d.Min = 0
			d.Max = 0
			d.Options = nil
			if !optional {
				d.Min = ceiling
			}
			d.Max = ceiling
			if eachPerType > 1 {
				d.GroupLimit = int(eachPerType)
			}
			eachStructuredOptions(g, d, eachGroups, eachPerType, false, owner, "hand_move")
		}
		// The iteration is entered (a mandatory move, a markerless may-shape, or
		// an accepted Optional$ confirmation): Forge clears the source's
		// remembered cards here, before the choose, so this applies even when
		// the answer picks nothing. The decision above already captured the
		// pre-clear Remembered, so the resumed revalidation keeps matching.
		forgetOtherRemembered(h, c, sa)
		// The shared ask boundary (effects.Ask): a ChangeNum$ 0 pick over a
		// nonempty eligible hand is Min == Max == 0 -- the empty-answer-only
		// shape -- so it is never posted; AskEmpty resolves silently through
		// the stand-in below, which moves zero cards.
		oc := Ask(h, d)
		if oc == AskAsked {
			return // resolution suspended; the answer re-enters with Ctx.HandMove set.
		}
		// R-9: a host without a decision channel cannot ask a player, so it
		// supplies the deterministic answer in the player's place -- the first
		// ChangeNum eligible cards in the same ordered eligible list the
		// decision's options were built from.
		if oc == AskNoHost {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: chooser,
				Text: "moves the first matching card(s) from hand (no engine host to ask)"})
		}
		if structured {
			// The structured stand-in: each group's first perType candidates, in
			// group order -- the exact take the bot's group-aware fill
			// re-derives, and the per-type mirror of the flat first-n take.
			for _, ids := range eachGroups {
				k := eachPerType
				if int32(len(ids)) < k {
					k = int32(len(ids))
				}
				for _, id := range ids[:k] {
					settleHandMove(id, owner)
					moved = append(moved, id)
				}
			}
		} else {
			for k := int32(0); k < n && int(k) < len(eligible); k++ {
				settleHandMove(eligible[k], owner)
				moved = append(moved, eligible[k])
			}
		}
		handLibraryTail(h, g, sa, c.Source, owner, moved, to)
		scheduleAtEOT(h, c, sa, moved)
	}
	// The walk completed: release the ride. A later ability in the same
	// chain must not inherit this walk's snapshot (the same boundary the
	// search and hidden walks end at).
	endForgetOtherSnapshot(c)
}

// optionalConfirmMarker is the ONE reader of Forge's explicit Optional$
// confirmation marker (the confirmAction gate in
// ChangeZoneEffect.changeHiddenOriginResolve, which runs before any card is
// picked and whose decline does not clear the source's remembered cards).
// Only a positive marker asks the decider: Optional$ True, or the older
// Optional$ You spelling Cauldron Dance carries. An explicit Optional$ False
// and an absent marker pose no confirmation. The markerless may-shapes
// handTakeOptional recognises from card text stay confirmation-free -- Forge
// expresses their may as a null pick, not as a confirmation -- so an empty
// answer there is an accepted fetch that clears. ChoiceOptional$ is
// deliberately NOT this marker: it names the pick's own cardinality (the
// Min-0 may-pick default), not a yes/no gate.
func optionalConfirmMarker(sa *cards.SA) bool {
	o := strings.TrimSpace(sa.Params["Optional"])
	return strings.EqualFold(o, "True") || strings.EqualFold(o, "You")
}

// handMoveConfirms reports whether a hidden-hand ChangeZone poses Forge's
// Optional$ confirmation before its card pick. The ordinary (non-ForgetOther)
// Optional$ fetch confirms exactly like the ForgetOtherRemembered$ one: the
// decline-vs-accept split is observable in the ask sequence itself (a decline
// poses no card pick at all, where an accepted Min-0 pick would), which is
// Forge's confirm-before-pick order in changeHiddenOriginResolve.
func handMoveConfirms(sa *cards.SA) bool {
	return optionalConfirmMarker(sa)
}

// hiddenPickConfirms reports whether a Hidden$ True public-origin ChangeZone
// pick poses Forge's Optional$ confirmation before its pick: the same
// optionalConfirmMarker the hidden-hand walk reads, for the same
// confirmAction gate. effHiddenPick poses it per fetch player, including when
// that player's eligible pool turns out empty -- a decline skips the player
// with the remembered set intact, and only an accepted confirmation reaches
// the pick or the empty-pool continuation that clears.
func hiddenPickConfirms(sa *cards.SA) bool {
	return optionalConfirmMarker(sa)
}

// handTakeOptional reads Forge's optional-vs-mandatory markers for a
// hidden-origin hand move. A missing marker is NOT an optional default:
// Volrath's Dungeon is markerless but requires its target to put a card back.
// Forge does not encode that distinction in ChangeZone's parameters, so the
// markerless may-shapes are recognised from their card/script text (Burgeoning,
// Oviya, Volcanic Spite); text we cannot classify fails closed and loudly at
// the caller rather than granting an invented decline.
func handTakeOptional(h Host, c *Ctx, sa *cards.SA, to state.Zone) (optional, known bool) {
	if strings.EqualFold(strings.TrimSpace(sa.Params["Mandatory"]), "True") {
		return false, true
	}
	if o := strings.TrimSpace(sa.Params["Optional"]); o != "" {
		return strings.EqualFold(o, "True") || strings.EqualFold(o, "You"), true
	}
	text := sa.Params["SpellDescription"]
	if o := h.Game().Obj(c.Source); o != nil && o.Face() != nil {
		text += "\n" + o.Face().Oracle
	}
	if strings.TrimSpace(text) == "" {
		return false, false
	}
	return handMoveTextOptional(text, to)
}

// handMoveTextOptional recognises the actual English may-forms for the one
// ChangeZone move being resolved. "Put any number" is optional even without
// the word may. It requires the destination's action verb and a hand
// reference IN THE SAME sentence: an unrelated "may put" elsewhere on a
// multi-ability card is not evidence that this move may be declined.
// Conversely, text that lacks a matching action is unknown, so the caller
// emits its fail-closed Note.
func handMoveTextOptional(text string, to state.Zone) (optional, known bool) {
	text = strings.ToLower(text)
	var action string
	switch to {
	case state.ZBattlefield, state.ZLibrary:
		action = "put"
	case state.ZExile:
		action = "exile"
	case state.ZGraveyard:
		action = "discard"
	case state.ZHand:
		action = "return"
	default:
		return false, false
	}
	if handMovePhraseMentionsHand(text, "may "+action) ||
		handMovePhraseFollowsHandReveal(text, "may "+action) ||
		(to == state.ZLibrary && handMovePhraseMentionsHand(text, "may shuffle")) ||
		(handMovePhraseMentionsHand(text, "any number") && handMovePhraseMentionsHand(text, action)) {
		return true, true
	}
	if handMovePhraseSupportsRequiredMove(text, action) ||
		handMovePhraseFollowsHandChoice(text, action) ||
		(to == state.ZLibrary && handMovePhraseSupportsRequiredMove(text, "shuffle")) {
		return false, true
	}
	return false, false
}

// handMovePhraseSupportsRequiredMove also accepts "choose" in the action's
// sentence: a preceding RevealHand can make the later "You choose ... and
// exile that card" sentence omit the word hand (Thought-Knot Seer), but it is
// still an unambiguously required chooser action.
func handMovePhraseSupportsRequiredMove(text, phrase string) bool {
	for start := 0; ; {
		i := strings.Index(text[start:], phrase)
		if i < 0 {
			return false
		}
		i += start
		begin := 0
		if j := strings.LastIndexAny(text[:i], ".;"); j >= 0 {
			begin = j + 1
		}
		end := len(text)
		if j := strings.IndexAny(text[i:], ".;"); j >= 0 {
			end = i + j
		}
		if strings.Contains(text[begin:end], "hand") || strings.Contains(text[begin:end], "choose") {
			return true
		}
		start = i + len(phrase)
	}
}

// handMovePhraseFollowsHandChoice recognises the same hidden-hand sequence
// when Forge split its selection and movement into sentences: "reveal their
// hand. You choose a card from it. Exile that card" (Kitesail Freebooter).
// It scans only the action sentence and its three predecessors, all of which
// must establish the hand -> choice -> pronoun chain.
func handMovePhraseFollowsHandChoice(text, phrase string) bool {
	for start := 0; ; {
		i := strings.Index(text[start:], phrase)
		if i < 0 {
			return false
		}
		i += start
		begin := 0
		if j := strings.LastIndexAny(text[:i], ".;"); j >= 0 {
			begin = j + 1
		}
		end := len(text)
		if j := strings.IndexAny(text[i:], ".;"); j >= 0 {
			end = i + j
		}
		contextStart := begin
		for n := 0; n < 3 && contextStart > 0; n++ {
			prior := strings.TrimRight(text[:contextStart], ".; ")
			if j := strings.LastIndexAny(prior, ".;"); j >= 0 {
				contextStart = j + 1
			} else {
				contextStart = 0
			}
		}
		context := text[contextStart:end]
		if (strings.Contains(text[begin:end], "that card") || strings.Contains(text[begin:end], " it")) &&
			strings.Contains(context, "hand") && strings.Contains(context, "choose") && strings.Contains(context, "it") {
			return true
		}
		start = i + len(phrase)
	}
}

// handMovePhraseFollowsHandReveal recognises the usual two-sentence hidden
// hand wording: "Target player reveals their hand. You may put ... from it."
// The pronoun is enough only immediately after a hand-reveal sentence, so an
// unrelated optional action elsewhere cannot make this move optional.
func handMovePhraseFollowsHandReveal(text, phrase string) bool {
	for start := 0; ; {
		i := strings.Index(text[start:], phrase)
		if i < 0 {
			return false
		}
		i += start
		begin := 0
		if j := strings.LastIndexAny(text[:i], ".;"); j >= 0 {
			begin = j + 1
		}
		end := len(text)
		if j := strings.IndexAny(text[i:], ".;"); j >= 0 {
			end = i + j
		}
		prior := strings.TrimRight(text[:begin], ".; ")
		if j := strings.LastIndexAny(prior, ".;"); j >= 0 {
			prior = prior[j+1:]
		}
		if strings.Contains(prior, "hand") && strings.Contains(text[begin:end], "it") {
			return true
		}
		start = i + len(phrase)
	}
}

// handMovePhraseMentionsHand keeps text classification local to the sentence
// carrying a candidate action. Forge's Oracle text uses periods for sentence
// boundaries; semicolons also separate instructions often enough to be a safe
// boundary here.
func handMovePhraseMentionsHand(text, phrase string) bool {
	for start := 0; ; {
		i := strings.Index(text[start:], phrase)
		if i < 0 {
			return false
		}
		i += start
		end := len(text)
		if j := strings.IndexAny(text[i:], ".;"); j >= 0 {
			end = i + j
		}
		if strings.Contains(text[i:end], "hand") {
			return true
		}
		start = i + len(phrase)
	}
}

// handMovePrompt builds the human-readable ask text, naming the top/bottom
// placement when the destination is the library (the one destination where
// WHERE matters to the chooser), and whose hand it is when the chooser is
// not the hand's owner (Chooser$ You: the caster picks out of another
// player's hand).
func handMovePromptFor(sa *cards.SA, to state.Zone, n int, own bool) string {
	dest := handDestPhrase(to)
	if to == state.ZLibrary {
		if strings.TrimSpace(sa.Params["LibraryPosition"]) == "-1" {
			dest = "the bottom of your library"
		} else {
			dest = "the top of your library"
		}
	}
	whose := "your hand"
	if !own {
		whose = "that player's hand"
		if to == state.ZLibrary {
			if strings.TrimSpace(sa.Params["LibraryPosition"]) == "-1" {
				dest = "the bottom of that player's library"
			} else {
				dest = "the top of that player's library"
			}
		}
	}
	return "Choose " + strconv.Itoa(n) + " card(s) from " + whose + ": they move to " + dest
}

// handLibraryTail is the post-move library placement both hidden-origin
// movers end with. A hand put-back only shuffles when its own script says
// so (Shuffle$ True -- Slowtrip), while a library search shuffles by
// default; the placement itself is the shared libraryOrderPlacement helper,
// with Forge's absent-LibraryPosition$ default (TOP) applied for the hand
// path -- Brainstorm and Jace's [0] name no LibraryPosition$ and their
// oracle puts the cards on top.
func handLibraryTail(h Host, _ *state.Game, sa *cards.SA, source state.ObjID, owner state.PlayerID, moved []state.ObjID, to state.Zone) {
	if to != state.ZLibrary || len(moved) == 0 {
		return
	}
	// DestinationAlternative$/LibraryPositionAlternative$ (Dream Cache's "both
	// on top of your library or both on the bottom", 1 raw line) is a modal
	// destination choice this engine cannot yet ask: the alternative is named
	// in a Note and the primary destination/position is taken
	// deterministically, so the unsupported shape is never silent.
	if alt := strings.TrimSpace(sa.Params["DestinationAlternative"]); alt != "" || strings.TrimSpace(sa.Params["LibraryPositionAlternative"]) != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: source, Player: owner,
			Text: "DestinationAlternative$ " + alt + " is not a choice this engine can ask; the cards take the primary destination"})
	}
	if strings.EqualFold(strings.TrimSpace(sa.Params["Shuffle"]), "True") &&
		!strings.EqualFold(strings.TrimSpace(sa.Params["NoShuffle"]), "True") {
		shuffleLibraryExplicit(h, sa, owner)
		return // a shuffled library has no meaningful LibraryPosition$
	}
	position := strings.TrimSpace(sa.Params["LibraryPosition"])
	if position != "" && position != "0" && position != "-1" {
		h.Emit(events.Event{Kind: events.Note, Obj: source, Player: owner,
			Text: "LibraryPosition$ " + position + " is not implemented; the cards go on top"})
	}
	libraryOrderPlacement(h, owner, moved, position == "-1")
}

// handDestPhrase names the hand-move destination in the human-readable
// prompt; its own vocabulary so it cannot drift into digDestPhrase's or
// destinationPhrase's.
func handDestPhrase(to state.Zone) string {
	switch to {
	case state.ZBattlefield:
		return "the battlefield"
	case state.ZGraveyard:
		return "the graveyard"
	case state.ZExile:
		return "exile"
	case state.ZLibrary:
		return "the library"
	case state.ZHand:
		return "the hand"
	default:
		return "its destination"
	}
}

// counterDestination reports whether a ChangeZone destination can carry the
// WithCountersType$/WithCountersAmount$ entry counters. They land on a
// permanent entering the battlefield (the Undying expansion) or on a card
// exiled with them (suspend's TIME counters); a counter on a moved card in any
// other zone is never read by anything, so such a destination must not parse
// the amount (which would emit a malformed-amount Note for a dynamic value)
// and must not emit a CounterChange. Measured at the corpus pin: every
// ChangeZone-family `WithCountersType$` line names exactly these two
// destinations -- Battlefield 99, Exile 38 (137 total) -- so the gate admits
// the whole measured population and nothing else. This is the one gate every
// ChangeZone mover shares (the other APIs that carry the parameter,
// CopyPermanent and Token, read it in their own primitives).
func counterDestination(to state.Zone) bool {
	return to == state.ZBattlefield || to == state.ZExile
}

// withCounterAmount parses WithCountersAmount$ (default 1). Malformed values
// must be loud, not silently default to 1 (the reviewer's item): a wrong
// counter count on a Returning permanent is a hard-to-spot board-shape bug. A
// Note event (the way Resolve surfaces an unimplemented API) keeps this
// deterministic and replay-log-visible rather than dropping to a log line the
// event log cannot account for. The movement still proceeds with the safe
// default 1.
func withCounterAmount(h Host, c *Ctx, sa *cards.SA) int32 {
	v := strings.TrimSpace(sa.Params["WithCountersAmount"])
	if v == "" {
		return 1
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "malformed WithCountersAmount " + v})
		return 1
	}
	return int32(n)
}

// effSearchLibrary implements the hidden-origin ChangeZone shape. The option
// list is rebuilt deterministically from library order and ChangeType$, while
// the answer is carried only as option indices and object ids through the
// ordinary KChoose/resume mechanism.
//
// zones is the full origin set (Origin$ merged with OriginAlternative$):
// Library is the hidden half, and any public zones in the set (Graveyard,
// Exile, Hand) contribute their owner's matching cards to the SAME option
// list, exactly Forge's choose-a-card-from-any-of-these-zones step. The
// library is searched first in candidate order so a pure-library search's
// option list -- and therefore its chain heads -- is unchanged.
//
// The per-library answer cursor rides the same resume point as every other
// mid-resolution choice. That makes a multi-player search continue after the
// owner whose answer suspended the effect, rather than rebuilding from the
// first owner on every re-entry.
// chooseFromDefinedPool is the ONE resolver for the ChangeZone
// ChooseFromDefined$ selector: the value is a full Defined selector, resolved
// through the shared Defined machinery (knownDefinedTargets) into the
// eligible-object pool every dispatch path bounds its offered set with --
// effHiddenPick's public-origin pick, effSearchLibrary's option list and
// applyLibrarySearch's answer recheck. An unknown, unresolvable or
// player-only result fails CLOSED: ok=false means the caller offers and
// moves NOTHING (plus its loud Note), never the whole origin zone.
func chooseFromDefinedPool(h Host, c *Ctx, raw string) (map[state.ObjID]bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	ts, ok := knownDefinedTargets(h, c, raw)
	if !ok {
		return nil, false
	}
	pool := make(map[state.ObjID]bool, len(ts))
	for _, t := range ts {
		if !t.IsPlayer && t.Obj != 0 {
			pool[t.Obj] = true
		}
	}
	return pool, true
}

func effSearchLibrary(h Host, c *Ctx, sa *cards.SA, to state.Zone, zones []state.Zone) {
	players := searchPlayers(h, c, sa)
	if c.ForgetOtherReady {
		players = c.ForgetOtherOwners
	}
	if len(players) == 0 {
		return
	}
	initForgetOtherSnapshot(h, c, sa, players, 2)
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
	if raw := strings.TrimSpace(sa.Params["ChooseFromDefined"]); raw != "" {
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
		if optionalConfirmMarker(sa) {
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
				chooser := searchChooser(h, c, sa)
				prompt := strings.TrimSpace(sa.Params["OptionalPrompt"])
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
		lookWindow := searchLibraryWindow(h, c, sa, lib)
		if len(lookWindow) < len(lib) && !strings.EqualFold(strings.TrimSpace(sa.Params["NoLooking"]), "True") {
			if strings.EqualFold(strings.TrimSpace(sa.Params["Reveal"]), "True") {
				h.Emit(events.Event{Kind: events.Note, Player: owner, IDs: append([]state.ObjID(nil), lookWindow...)})
			} else {
				emitLook(h, []state.PlayerID{searchChooser(h, c, sa)}, state.ZLibrary, lookWindow,
					"looks at the top of the library")
			}
		}
		if shufflePending && targetIndex == shuffleTarget {
			c.SearchShuffle = shuffleAnswer
			// Restore the answered tail just long enough for the shared helper
			// to consume it. The answer's owner is this target, not players[0].
			c.SearchShuffleMoved = shuffleMoved
			if searchShuffleTail(h, c, sa, owner, nil, to) {
				return
			}
			shufflePending = false
			continue
		}
		if searchDone && targetIndex == searchTarget {
			c.LibraryTarget = targetIndex
			if applyLibrarySearch(h, c, sa, owner, to, chosen, zones) {
				return
			}
			searchDone = false
			continue
		}

		rawSpec := sa.Params["ChangeType"]
		if rawSpec == "" {
			rawSpec = "Card"
		}
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
		budget, hasBudget := NumResolved(h, c, sa, "WithTotalCMC", 0)
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
		max := Num(h, c, sa, "ChangeNum", 1)
		if max < 0 {
			max = 0
		}
		if max > int32(len(budgetEligible)) {
			max = int32(len(budgetEligible))
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
			if applyLibrarySearch(h, c, sa, owner, to, nil, zones) {
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
		// The prompt is built AFTER the Mandatory$ clamp below (and after the
		// EACH branch's own bounds), so the count it states can never disagree
		// with the decision's final Min/Max, and SelectPrompt$ replaces the
		// generic text exactly as effHiddenPick already does. Building it here
		// -- before the clamp -- is what made a mandatory leg advertise "up to
		// 1 card(s)" over a Min == Max == 1 decision.
		chooser := searchChooser(h, c, sa)
		// NoLooking$ True (Forge's line-1020 gate: with NoLooking the searching
		// player never LOOKS at the library -- no delayedReveal -- so the choose
		// is made over card backs): the options must not carry card names. The
		// IsRemembered legs of the Cultivate family and the seek-style shapes
		// route here; without this read the option labels leaked the library's
		// order one look at a time.
		noLooking := strings.EqualFold(strings.TrimSpace(sa.Params["NoLooking"]), "True")
		// DifferentNames$ True (Realms Uncharted): the picked cards must have
		// distinct names. One option per card name carries that name in Group, so
		// Decision.Validate's mutual-exclusion rule refuses any answer naming the
		// same card twice -- the wire enforces what Forge's one-at-a-time loop
		// (the DifferentNames fetchList filter) enforces there. The apply side
		// dedupes a host that bypassed the wire (applyLibrarySearch).
		differentNames := strings.EqualFold(strings.TrimSpace(sa.Params["DifferentNames"]), "True")
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
		if strings.EqualFold(strings.TrimSpace(sa.Params["Mandatory"]), "True") {
			min = max
			if hasBudget && !eachStructured {
				// Respect the cumulative budget's feasible deterministic count;
				// never post a mandatory minimum the budget cannot satisfy.
				min = int32(len(greedy))
			}
		}
		d.Min = int(min)
		// The prompt is built here, AFTER the Mandatory$ clamp and the EACH
		// branch's own bounds, so its stated count always matches the decision's
		// final Min/Max -- a mandatory leg with Min == Max == 1 must not advertise
		// "up to 1 card(s)". SelectPrompt$ (Forge's ChangeZoneEffect prompt,
		// already read on the sibling hidden-pick path in effHiddenPick) replaces
		// the generic text when the SA carries it: Cultivate's legs name their own
		// "Select a card to put onto the battlefield".
		if sp := strings.TrimSpace(sa.Params["SelectPrompt"]); sp != "" {
			d.Prompt = sp
		} else if d.Min == d.Max {
			d.Prompt = "Search a library: choose " + strconv.Itoa(d.Max) + " card(s)"
		} else {
			d.Prompt = "Search a library: choose up to " + strconv.Itoa(d.Max) + " card(s)"
		}
		// EACH's own prompt was only a placeholder for the counted forms; the
		// override above already replaced it when SelectPrompt$ is absent.
		if eachStructured && strings.TrimSpace(sa.Params["SelectPrompt"]) == "" {
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
		if applyLibrarySearch(h, c, sa, owner, to, picked, zones) {
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
func moveDefinedLibraryObjects(h Host, c *Ctx, sa *cards.SA, to state.Zone) bool {
	if strings.TrimSpace(sa.Params["Defined"]) == "" {
		return false
	}
	if _, owner := sa.Params["DefinedPlayer"]; owner {
		return false
	}
	g := h.Game()
	targets, known := knownDefinedTargets(h, c, sa.Params["Defined"])
	if !known {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unrecognised Defined library fetch " + sa.Params["Defined"]})
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

	optional := strings.EqualFold(strings.TrimSpace(sa.Params["Optional"]), "True")
	answer := c.DefinedLibraryMove
	c.DefinedLibraryMove = "" // fx42 scoping: a nested fetch asks for itself.
	if optional && answer == "" {
		prompt := strings.TrimSpace(sa.Params["OptionalPrompt"])
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
	forgetOtherRemembered(h, c, sa)

	withKind := sa.Params["WithCountersType"]
	var withAmt int32
	if withKind != "" && counterDestination(to) {
		withAmt = withCounterAmount(h, c, sa)
	}
	// The AtEOT$ rider's affected set, collected across every fetch and
	// scheduled by ONE call after the loop (one Note per call, never per
	// owner).
	var ateotMoved []state.ObjID
	rider := classifyAttackingEntry(c, sa, to)
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
			settleChangeZoneMove(h, c, sa, id, state.ZLibrary, to, withKind, withAmt, &rider)
			if strings.EqualFold(sa.Params["RememberChanged"], "True") {
				eventRemember(h, c, id)
			}
			eventForgetChanged(h, c, sa, id)
			moved = append(moved, id)
			ateotMoved = append(ateotMoved, id)
			if to == state.ZBattlefield && strings.EqualFold(sa.Params["Tapped"], "True") {
				h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: f.owner, Text: "entered tapped"})
			}
		}
		shuffleLibrary(h, sa, f.owner)
		placeLibraryObjects(h, sa, f.owner, moved, to)
		// Explicit Reveal$ on a Defined$ fetch list (Forge reveals movedCards
		// whenever Reveal$ names the effect, defined or not): the same public
		// Note payload applyLibrarySearch emits -- no auto-reveal here, since
		// a Defined$ list is never revealed by default.
		if strings.EqualFold(strings.TrimSpace(sa.Params["Reveal"]), "True") && len(moved) > 0 {
			h.Emit(events.Event{Kind: events.Note, Player: f.owner, IDs: moved})
		}
	}
	scheduleAtEOT(h, c, sa, ateotMoved)
	return true
}

// effManifest implements Forge's Manifest primitive (Reality Shift's
// "its controller manifests the top card of their library", Whisperwood
// Elemental's bare `DB$ Manifest` trigger body): move the top card of each
// named player's library onto the battlefield FACE DOWN (CR 708.5). The move
// is the REAL card object -- never a token mint: the manifested 2/2 keeps
// the object's identity, so if it dies it reaches the graveyard as itself
// (CR 708.9's reveal is the FaceDown clear on leaving the battlefield, and
// the view's FaceDown redaction hides the face from non-controllers while it
// stays in play). Each move is one Secret MoveZone with Player set to the
// manifesting player: Secret is what keeps the event's Obj out of every
// other seat's projection (redaction rule 1) -- a library-to-battlefield
// move would otherwise stay public under rule 2 and leak the face through
// the transcript.
//
// Scope, measured over the corpus's 33 plain-Manifest lines: the default
// top-card shape (the 9 bare `DB$ Manifest` trigger bodies), a
// `DefinedPlayer$` selector through searchPlayers's grammar (Reality
// Shift's `TargetedController`) and a literal/SVar `Amount$` (default 1;
// a value resolving to <= 0 manifests nothing, no event) are implemented.
// Every other shape -- `Defined$` object manifests, the `Choices$`
// chooser forms, `RememberManifested$ True`, an unresolvable `Amount$`
// body (Y, or X outside a cast's own X-value) -- emits the SAME loud
// "unimplemented API Manifest" note the unimplemented-API fallback emits
// and moves nothing: fail loud, never silently move the wrong card.
// Turning a face-down permanent face up (CR 708.6) is not implemented
// anywhere (AGENTS.md's manifest row).
func effManifest(h Host, c *Ctx, sa *cards.SA) {
	if strings.TrimSpace(sa.Params["Defined"]) != "" ||
		strings.TrimSpace(sa.Params["Choices"]) != "" ||
		strings.EqualFold(strings.TrimSpace(sa.Params["RememberManifested"]), "True") {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented API Manifest"})
		return
	}
	amount := int32(1)
	if raw, present := sa.Params["Amount"]; present {
		// X/Y (and any body Num's grammar cannot resolve) are out of scope:
		// loud, never a degraded count silently moving a wrong number of
		// cards.
		if raw == "X" || raw == "Y" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented API Manifest"})
			return
		}
		n, ok := NumResolved(h, c, sa, "Amount", 1)
		if !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unimplemented API Manifest"})
			return
		}
		amount = n
	}
	if amount <= 0 {
		return
	}
	g := h.Game()
	for _, p := range searchPlayers(h, c, sa) {
		if int(p) >= len(g.Players) {
			continue
		}
		n := amount
		if l := int32(len(g.Zone(state.ZLibrary, p))); l < n {
			n = l
		}
		for i := int32(0); i < n; i++ {
			// Index 0 is the TOP of the library (the end a Draw takes). The
			// MoveZone fold removes the object as it lands, so the zone is
			// re-read each iteration.
			top := g.Zone(state.ZLibrary, p)[0]
			h.Emit(events.Event{Kind: events.MoveZone, Obj: top, Player: p,
				From: state.ZLibrary, To: state.ZBattlefield,
				Counter: "entered_face_down", Secret: true})
		}
	}
}

// effManifestDread implements CR 701.61's two-card library operation. The
// private look is recorded before the choice; the offered identities are
// visible only to the library's player. If the host cannot ask, choose the
// top card deterministically, matching the engine's R-9 fallback contract.
//
// Scope, measured over the corpus's 37 ManifestDread lines: the plain top-two
// body (Zimone, Mystery Unraveler and 26 others) and `Amount$ 2` (identical
// to the default). Every other parameter family -- `Amount$ 1` (a count this
// build does not implement), `DefinedPlayer$` (only the resolving
// controller's library is supported) and `RememberManifested$ True` (the
// DBAttach/DBPutCounter rider family needs the manifested object remembered)
// -- emits the SAME loud "unimplemented API ManifestDread" note the
// unimplemented-API fallback emits and moves nothing: fail loud, never
// silently look at the wrong count, the wrong player's library, or lose the
// remembered card a rider needs.
func effManifestDread(h Host, c *Ctx, sa *cards.SA) {
	if strings.TrimSpace(sa.Params["DefinedPlayer"]) != "" ||
		strings.EqualFold(strings.TrimSpace(sa.Params["RememberManifested"]), "True") ||
		sa.Params["Choices"] != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented API ManifestDread"})
		return
	}
	if raw, present := sa.Params["Amount"]; present && strings.TrimSpace(raw) != "2" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented API ManifestDread"})
		return
	}
	g := h.Game()
	p := c.ManifestDreadPlayer
	picked := c.ManifestDreadPick
	done := c.ManifestDreadDone
	c.ManifestDreadPick, c.ManifestDreadDone = 0, false
	if int(p) >= len(g.Players) {
		p = c.Controller
	}
	if done {
		if o := g.Obj(picked); o != nil && o.Zone == state.ZLibrary && o.Owner == p {
			window := []state.ObjID{picked}
			for _, id := range g.Zone(state.ZLibrary, p) {
				if id != picked && len(window) < 2 {
					window = append(window, id)
				}
			}
			manifestDreadMove(h, c, p, window, picked)
		}
		return
	}
	p = c.Controller
	lib := g.Zone(state.ZLibrary, p)
	if len(lib) == 0 {
		return
	}
	window := append([]state.ObjID(nil), lib[:min(2, len(lib))]...)
	emitLook(h, []state.PlayerID{p}, state.ZLibrary, window, "looks at the top two cards of the library")
	if len(window) == 1 {
		manifestDreadMove(h, c, p, window, window[0])
		return
	}
	d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "manifest_dread", ResumeSA: sa,
		Prompt: "Choose a card to manifest dread"}
	for _, id := range window {
		label := "a card"
		if o := g.Obj(id); o != nil && o.Face() != nil {
			label = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "manifest_dread", Label: label, Obj: id, Player: p})
	}
	c.ManifestDreadPlayer = p
	if Ask(h, d) == AskAsked {
		return
	}
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: p,
		Text: "manifests the top card (no engine host to ask)", Secret: true})
	manifestDreadMove(h, c, p, window, window[0])
}

// manifestDreadMove applies CR 701.61's two destinations. The chosen card's
// move onto the battlefield face down is Secret (the private look must not
// leak which card was manifested); the unchosen card's move to the graveyard
// is PUBLIC -- a graveyard is a public zone, so every seat and spectator
// learns which card went there, exactly as applyNonlandExplore's
// library-to-graveyard move does. Marking it Secret would strip Obj from
// every non-owner projection, leaving the transcript a nameless move even
// though the card's identity is public the moment it lands.
func manifestDreadMove(h Host, c *Ctx, p state.PlayerID, window []state.ObjID, chosen state.ObjID) {
	for _, id := range window {
		if id == chosen {
			h.Emit(events.Event{Kind: events.MoveZone, Obj: id, Player: p, From: state.ZLibrary,
				To: state.ZBattlefield, Counter: "entered_face_down", Secret: true})
		} else {
			o := h.Game().Obj(id)
			if o != nil {
				h.Emit(events.Event{Kind: events.MoveZone, Obj: id, Player: o.Owner, From: state.ZLibrary, To: state.ZGraveyard})
			}
		}
	}
}

// effCloak implements Forge's Cloak primitive (veiled_ascension's upkeep
// trigger, unexplained_absence's per-player cloak, cryptic_coat's ETB cloak):
// CR 708.5's cloak variant -- the named card objects move onto the
// battlefield FACE DOWN as 2/2 creatures with ward {2}. The move is the REAL
// card object, the effManifest shape with a different Counter value (one
// Secret MoveZone per card, "entered_cloaked" instead of
// "entered_face_down" -- the marker events/apply.go folds into
// state.Object.Cloaked, which rules/layers.go and rules/trigger_match.go
// read for the ward {2}; the view's FaceDown redaction covers both
// variants). A cloak's Defined$ names card objects that may sit in the
// library, exile or hand (the Remembered carriers move cards that just
// arrived there), so unlike effManifest the object shapes resolve directly.
//
// Scope, measured over the corpus's 11 Cloak lines: the per-player top-card
// shapes (DefinedPlayer$, or Defined$ TopOfLibrary whose listed player's OWN
// library is taken -- never the resolver's), the ctx-Remembered object
// shapes (become_anonymous, hide_in_plain_sight, expose_the_culprit), a
// literal/SVar Amount$ (default 1; a value resolving to <= 0 cloaks
// nothing, no event) and the riders Tapped$ (enter tapped, the
// MoveZone-then-Tap pair), Shuffle$ (the standard Secret shuffle of each
// affected player's library afterwards) and RememberCloaked$ (each cloaked
// object joins the resolution's Remembered -- cryptic_coat's chained attach)
// are implemented. SubAbility$ is free (the ordinary chain). Every other
// shape -- the Choices$ cloak-from-hand chooser (vannifar), Defined$
// ValidLibrary (etrata), an unresolvable Amount$ body -- emits the SAME loud
// "unimplemented API Cloak" note the unimplemented fallback emits and moves
// nothing: fail loud, never silently move the wrong card. Turning a cloaked
// card face up (CR 708.6) is not implemented anywhere (the
// Morph/Megamorph/Disguise ticket owns the shared turn-face-up path).
func effCloak(h Host, c *Ctx, sa *cards.SA) {
	loud := func() {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unimplemented API Cloak"})
	}
	defined := strings.TrimSpace(sa.Params["Defined"])
	if strings.TrimSpace(sa.Params["Choices"]) != "" ||
		strings.Contains(defined, "ValidLibrary") {
		loud()
		return
	}
	amount := int32(1)
	if raw, present := sa.Params["Amount"]; present {
		// X/Y (and any body Num's grammar cannot resolve) are out of scope:
		// loud, never a degraded count silently moving a wrong number of
		// cards.
		if raw == "X" || raw == "Y" {
			loud()
			return
		}
		n, ok := NumResolved(h, c, sa, "Amount", 1)
		if !ok {
			loud()
			return
		}
		amount = n
	}
	if amount <= 0 {
		return
	}
	tapped := strings.EqualFold(strings.TrimSpace(sa.Params["Tapped"]), "True")
	shuffle := strings.EqualFold(strings.TrimSpace(sa.Params["Shuffle"]), "True")
	remember := strings.EqualFold(strings.TrimSpace(sa.Params["RememberCloaked"]), "True")
	g := h.Game()
	shuffled := make(map[state.PlayerID]bool)
	cloak := func(id state.ObjID) {
		o := g.Obj(id)
		if o == nil || o.Zone == state.ZBattlefield {
			// A missing object moves nothing; a card already on the
			// battlefield is not a cloak candidate (every corpus shape sources
			// from library/exile/hand).
			return
		}
		from := o.Zone
		// Player rides the CLOAKED card's controller: Secret is what keeps
		// the event's Obj out of every other seat's projection (redaction
		// rule 1), and the seat that may look at a face-down card is its
		// controller (CR 708.5).
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id, Player: o.Controller,
			From: from, To: state.ZBattlefield,
			Counter: "entered_cloaked", Secret: true})
		if remember {
			// RememberCloaked$ is ctx level (the cryptic_coat attach chain
			// reads it within the same resolution); the persistent list is
			// left alone, the RememberChanged$ convention.
			c.Remembered = append(c.Remembered, state.Target{Obj: id})
		}
		if tapped {
			h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: o.Controller,
				Text: "entered tapped"})
		}
		shuffled[o.Owner] = true
	}
	if strings.TrimSpace(sa.Params["DefinedPlayer"]) != "" {
		// The per-player top-card shape (unexplained_absence's
		// "Defined$ TopOfLibrary | DefinedPlayer$ RememberedController"):
		// each listed player's OWN top Amount$ cards -- searchPlayers's
		// DefinedPlayer$ precedence, the effManifest loop's move shape.
		for _, p := range searchPlayers(h, c, sa) {
			if int(p) >= len(g.Players) {
				continue
			}
			n := amount
			if l := int32(len(g.Zone(state.ZLibrary, p))); l < n {
				n = l
			}
			for i := int32(0); i < n; i++ {
				// Index 0 is the TOP of the library (the end a Draw takes).
				// The MoveZone fold removes the object as it lands, so the
				// zone is re-read each iteration.
				top := g.Zone(state.ZLibrary, p)[0]
				cloak(top)
			}
		}
	} else {
		switch defined {
		case "", "TopOfLibrary":
			// The bare top-card shape (veiled_ascension, ransom_note,
			// cryptic_coat): the resolving controller's top card, the
			// TopOfLibrary selector's own anchor (effects/context.go).
			targets, ok := definedSpec(h, c, "TopOfLibrary")
			if !ok {
				loud()
				return
			}
			for _, t := range targets {
				if t.IsPlayer {
					continue
				}
				cloak(t.Obj)
			}
		case "Remembered":
			// The Remembered-object shape (become_anonymous,
			// hide_in_plain_sight, expose_the_culprit): each remembered card
			// object cloaks from wherever it sits now (library top, exile,
			// hand).
			for _, t := range objectsOf(copyTargets(c.Remembered)) {
				cloak(t.Obj)
			}
		default:
			loud()
			return
		}
	}
	if shuffle {
		// Shuffle$ True: the standard Secret events.Shuffle for each player
		// whose library lost a card (become_anonymous and expose_the_culprit
		// carry it; the deterministic order comes from the host's own rng
		// path every other library shuffle uses).
		for _, p := range g.AliveFrom(c.Controller) {
			if shuffled[p] {
				shuffleLibraryOrder(h, p)
			}
		}
	}
}

// changeZoneFetchSelector distinguishes a fetch player from an already chosen
// object. An unbound or unknown object selector never widens to a free search.
func changeZoneFetchSelector(h Host, c *Ctx, sa *cards.SA) bool {
	if spec := strings.TrimSpace(sa.Params["Defined"]); spec != "" {
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
	if sa.Params["DefinedPlayer"] != "" {
		return true
	}
	if sa.Params["ValidTgts"] != "" {
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
	if sa.Params["DefinedPlayer"] == "" && sa.Params["Defined"] == "" && sa.Params["ValidTgts"] != "" {
		return hiddenPickPlayers(h, c, sa)
	}
	spec, explicit := sa.Params["DefinedPlayer"]
	if !explicit {
		spec, explicit = sa.Params["Defined"]
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
func searchChooser(h Host, c *Ctx, sa *cards.SA) state.PlayerID {
	if spec := strings.TrimSpace(sa.Params["Chooser"]); spec != "" {
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
func searchLibraryWindow(h Host, c *Ctx, sa *cards.SA, lib []state.ObjID) []state.ObjID {
	raw, present := sa.Params["MaxRevealed"]
	if !present || strings.TrimSpace(raw) == "" || len(lib) == 0 {
		return lib
	}
	limit := Num(h, c, sa, "MaxRevealed", 0)
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
func hiddenPickPlayers(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	if strings.TrimSpace(sa.Params["DefinedPlayer"]) != "" {
		return searchPlayers(h, c, sa)
	}
	if _, targeted := sa.Params["ValidTgts"]; targeted {
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
func hiddenPickChooser(h Host, c *Ctx, sa *cards.SA, owner state.PlayerID) state.PlayerID {
	if spec := strings.TrimSpace(sa.Params["Chooser"]); spec != "" {
		if p, ok := chooserPlayer(h, c, spec); ok {
			return p
		}
		return owner
	}
	return owner
}

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
func effHiddenPick(h Host, c *Ctx, sa *cards.SA, to state.Zone, originZones []state.Zone, originAll bool, originValid bool, from string) {
	// fx42 scoping: capture and clear the answered pick (and the cursor that
	// binds it to the fetch player that asked) before anything else, so a
	// nested pick below cannot inherit them.
	ans := c.HiddenPick
	done := c.HiddenPickDone
	cursor := c.HiddenPickTarget
	c.HiddenPick, c.HiddenPickDone, c.HiddenPickTarget = nil, false, 0
	// fx42 scoping for the Optional$ confirmation answer: consumed and cleared
	// before anything else so a nested pick poses its own confirmation (the
	// same discipline the hand walk's HandMoveConfirm answer follows).
	confirmDone := c.HiddenPickConfirmDone
	confirmYes := strings.EqualFold(c.HiddenPickConfirm, "yes")
	confirmTarget := c.HiddenPickConfirmTarget
	c.HiddenPickConfirm, c.HiddenPickConfirmDone, c.HiddenPickConfirmTarget = "", false, 0
	if !originValid {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "ChangeZone Origin$ " + from + " includes a zone this engine does not model (no outside-the-game cards exist); nothing is offered from it"})
	}
	players := hiddenPickPlayers(h, c, sa)
	if c.ForgetOtherReady {
		players = c.ForgetOtherOwners
	}
	initForgetOtherSnapshot(h, c, sa, players, 2)
	// Forge branches on the origin zones, not on the fetch player: game-wide
	// only when the origin holds no hidden-info zone and no fetch player was
	// named (Kor Skyfisher's ChangeType$ filter does the scoping).
	gameWide := !zoneIn(originZones, state.ZHand) && !zoneIn(originZones, state.ZLibrary) &&
		strings.TrimSpace(sa.Params["DefinedPlayer"]) == ""
	spec := sa.Params["ChangeType"]
	if spec == "" {
		spec = "Card"
	}
	// Away from the battlefield, Forge's Permanent base means a permanent
	// card. Hidden graveyard/exile picks share the library search's rule;
	// without it Winter's remembered permanent is never eligible for DBReturn.
	if !zoneIn(originZones, state.ZBattlefield) {
		spec = permanentCardSpec(spec)
	}
	// The per-type groups an EACH ChangeType asks for, computed once: the
	// sub-specs are a property of the SA, not of the fetch player.
	eachSubs, isEach := eachAlternatives(spec)
	max := Num(h, c, sa, "ChangeNum", 1)
	if max < 0 {
		max = 0
	}
	mandatory := strings.EqualFold(strings.TrimSpace(sa.Params["Mandatory"]), "True")
	// ChoiceOptional$ True explicitly names the Min-0 may-pick default here;
	// it does not override Mandatory$ True. False/unset leave the default unchanged.
	mayPick := strings.EqualFold(strings.TrimSpace(sa.Params["ChoiceOptional"]), "True")
	noLooking := strings.EqualFold(strings.TrimSpace(sa.Params["NoLooking"]), "True")
	withKind := sa.Params["WithCountersType"]
	var withAmt int32
	if withKind != "" && counterDestination(to) {
		withAmt = withCounterAmount(h, c, sa)
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
	budget, hasBudget := NumResolved(h, c, sa, "WithTotalCMC", 0)
	if budget < 0 {
		budget = 0
	}
	rider := classifyAttackingEntry(c, sa, to)
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
	if raw := strings.TrimSpace(sa.Params["ChooseFromDefined"]); raw != "" {
		hasChooseFromDefined = true
		if pool, ok := chooseFromDefinedPool(h, c, raw); ok {
			chooseFromDefinedResolved = true
			chooseFromDefined = pool
		}
	}
	if hasChooseFromDefined && !chooseFromDefinedResolved {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "ChangeZone ChooseFromDefined$ " + strings.TrimSpace(sa.Params["ChooseFromDefined"]) + " is not resolvable; nothing is offered"})
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
		forgetOtherRemembered(h, c, sa)
		moved := make([]state.ObjID, 0, len(valid))
		for _, id := range valid {
			o := g.Obj(id)
			// Recheck at the point of movement: the answered card must still
			// sit in an origin zone and match the filter, or it stays.
			if o == nil || !zoneIn(originZones, o.Zone) {
				continue
			}
			settleChangeZoneMoveAs(h, c, sa, id, o.Zone, to, withKind, withAmt, o.Owner, true, &rider)
			// AttachedTo$ on a hidden public-origin pick (Cass, Hand of
			// Vengeance's returned `AttachedTo$ Targeted` Aura; Bruna,
			// Stormkeld Curator, Sovereigns of Lost Alara): the same rider every
			// other mover applies. Without it a returned Aura enters unattached
			// and the CR 704.5m SBA sweeps it before the chained SubAbility
			// runs -- silent for all 8 corpus Hidden$+AttachedTo$ lines, and
			// the reason Cass's returned Auras would not sit on the target.
			if to == state.ZBattlefield {
				changeZoneAttachedTo(h, c, sa, id)
			}
			moved = append(moved, id)
			if strings.EqualFold(sa.Params["RememberChanged"], "True") {
				// settleChangeZoneMoveAs recorded the resolution-local half;
				// persist the same moved object for later resolutions.
				eventRemember(h, c, id)
			}
			eventForgetChanged(h, c, sa, id)
			if to == state.ZBattlefield && strings.EqualFold(sa.Params["Tapped"], "True") {
				h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: o.Owner, Text: "entered tapped"})
			}
		}
		// Explicit Reveal$ on the pick (Karn, the Great Creator's [-2]): the
		// same public Note payload the library search's reveal emits. No
		// default auto-reveal here: the pick's origin zones are public, so
		// every offered name was already known.
		if strings.EqualFold(strings.TrimSpace(sa.Params["Reveal"]), "True") && len(moved) > 0 {
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
	chooseFromAll := hasChooseFromDefined && strings.TrimSpace(sa.Params["ChangeNum"]) == ""
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
			if raw, ok := totalCardTypesRequirement(sa); ok {
				need, err := strconv.Atoi(raw)
				if err != nil || need < 0 || !totalCardTypesSatisfied(h.Game(), ans, need) {
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: owner,
						Text: "hidden pick fails WithTotalCardTypes$ requirement"})
					continue
				}
			}
			apply(owner, ans)
			continue
		}
		chooser := hiddenPickChooser(h, c, sa, owner)
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
		if hiddenPickConfirms(sa) && !(hasChooseFromDefined && !chooseFromDefinedResolved) {
			if confirmDone && i < confirmTarget {
				// This fetch player's confirmation was already answered on an
				// earlier pass (declined, or accepted with its pick completed);
				// do not ask again and do not clear for it.
				continue
			}
			if !confirmDone {
				prompt := strings.TrimSpace(sa.Params["OptionalPrompt"])
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
				if Ask(h, cd) == AskAsked {
					return
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
				forgetOtherRemembered(h, c, sa)
			}
			continue
		}
		prompt := strings.TrimSpace(sa.Params["SelectPrompt"])
		// OptionalPrompt$ is the script's own wording for the optional pick
		// (Cass's "Select any number of Aura cards that were attached to
		// it"); it wins the default text, the same precedence the
		// library-search path gives it.
		if op := strings.TrimSpace(sa.Params["OptionalPrompt"]); op != "" {
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
		oc := Ask(h, d)
		if oc == AskAsked {
			return
		}
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
		} else if differentNamesEnabled(sa) && !hasBudget {
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
		if oc == AskNoHost {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: chooser,
				Text: "picks " + strconv.Itoa(len(picked)) + " card(s) (no engine host to ask)"})
		}
		apply(owner, picked)
	}
	endForgetOtherSnapshot(c)
}

// differentNamesEnabled is the one DifferentNames$ read shared by the two
// hidden-origin walkers that enforce it, so their option/answer contract
// cannot drift.
func differentNamesEnabled(sa *cards.SA) bool {
	return strings.EqualFold(strings.TrimSpace(sa.Params["DifferentNames"]), "True")
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
func totalCardTypesRequirement(sa *cards.SA) (string, bool) {
	if sa == nil {
		return "", false
	}
	raw := strings.TrimSpace(sa.Params["WithTotalCardTypes"])
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
func applyLibrarySearch(h Host, c *Ctx, sa *cards.SA, owner state.PlayerID, to state.Zone, chosen []state.ObjID, zones []state.Zone) bool {
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
	spec := sa.Params["ChangeType"]
	if spec == "" {
		spec = "Card"
	}
	// Recheck the answer with the same hidden-zone meaning used to build the
	// option list: a library Permanent is a permanent card.
	spec = permanentCardSpec(spec)
	// DifferentNames$ True (Realms Uncharted): the options carried a Group
	// per card name, so a validated wire answer cannot repeat a name. A host
	// that bypassed the wire (bot clamp top-up, a direct resume) is deduped
	// here deterministically -- first per name in answer order -- so the
	// engine and its clients cannot drift on what the constraint means.
	if strings.EqualFold(strings.TrimSpace(sa.Params["DifferentNames"]), "True") {
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
	if strings.EqualFold(strings.TrimSpace(sa.Params["ShareLandType"]), "True") {
		chosen = trimSharedLandTypes(g, chosen)
	}
	// WithTotalCardTypes$ constrains the complete hidden pick, rather than
	// each option independently. Decision.Validate cannot inspect card
	// characteristics, so enforce the same constraint at the resolution
	// boundary as a conservative host-bypass guard: an underspecified answer
	// finds nothing and cannot feed the ChangeZone rider. This check follows
	// the other set-level trims so those cannot invalidate the guarantee.
	if raw, hasTotalCardTypes := totalCardTypesRequirement(sa); hasTotalCardTypes {
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
	window := searchLibraryWindow(h, c, sa, zoneOf(g, state.ZLibrary, owner))
	// ChooseFromDefined$ bounds the search's VALID answers the same way the
	// option list was bounded in effSearchLibrary (the two reads share the ONE
	// pool resolver, so they cannot drift): a host that bypassed the wire
	// cannot move a card outside the resolved selector, and an unresolved
	// selector with an actual answer re-fails closed loudly (the option build
	// already noted; this is the bypass guard).
	var cfdPool map[state.ObjID]bool
	cfdActive := false
	if raw := strings.TrimSpace(sa.Params["ChooseFromDefined"]); raw != "" {
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
	rider := classifyAttackingEntry(c, sa, to)
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
	forgetOtherRemembered(h, c, sa)
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
			if sa.Params["WithCountersType"] != "" && counterDestination(to) {
				withKind = sa.Params["WithCountersType"]
				withAmt = withCounterAmount(h, c, sa)
			}
			settleChangeZoneMoveAs(h, c, sa, id, o.Zone, to, withKind, withAmt, owner, true, &rider)
			// AttachedTo$ on an alternative-zone pick (Boonweaver Giant's "put
			// it onto the battlefield attached to CARDNAME", Runed Crown, Arachnus
			// Web): the same rider the library branch below applies -- without it
			// an Aura found in the graveyard or hand enters unattached and the
			// CR 704.5m SBA sweeps it. GainControl$/WithCounters*/Transformed$
			// already ride settleChangeZoneMoveAs above.
			if to == state.ZBattlefield {
				changeZoneAttachedTo(h, c, sa, id)
			}
			moved = append(moved, id)
			// settleChangeZoneMoveAs already appended the object to the
			// resolution's Remembered for RememberChanged$; only the persistent
			// event-backed half is left here. RememberSearched$ is not read
			// there, so its ctx append is made here too.
			if strings.EqualFold(sa.Params["RememberChanged"], "True") {
				eventRemember(h, c, id)
			}
			if strings.EqualFold(strings.TrimSpace(sa.Params["RememberSearched"]), "True") {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
			eventForgetChanged(h, c, sa, id)
			if to == state.ZBattlefield && strings.EqualFold(sa.Params["Tapped"], "True") {
				h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: owner, Text: "entered tapped"})
			}
			continue
		}
		ev := moveZoneEvent(c, id, state.ZLibrary, to)
		ev.Player = owner
		applyFaceDownMarker(h, sa, c, &ev, to)
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
		if strings.EqualFold(strings.TrimSpace(sa.Params["Imprint"]), "True") && c.Source != 0 {
			if o := g.Obj(id); o != nil && o.Zone == to && !o.IsToken {
				if strings.EqualFold(strings.TrimSpace(sa.Params["ImprintLast"]), "True") {
					clearChangeZoneImprint(h, c)
					h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: []state.ObjID{id}})
				} else {
					imprinted = append(imprinted, id)
				}
			}
		}
		moved = append(moved, id)
		if sa.Params["WithCountersType"] != "" && counterDestination(to) {
			h.Emit(events.Event{Kind: events.CounterChange, Obj: id,
				Counter: sa.Params["WithCountersType"], Amount: withCounterAmount(h, c, sa)})
		}
		// GainControl$ on a library search (Act on Impulse's "you may play
		// those cards" family's put-onto-battlefield relatives): same settle
		// order as every other mover -- move first, then the control change,
		// then the AttachedTo$ rider (the Origin$ Library ChangeZone lines
		// carrying it, e.g. an "return an Aura ... attached to CARDNAME" search).
		if to == state.ZBattlefield {
			applyGainControl(h, c, sa, id)
			changeZoneAttachedTo(h, c, sa, id)
		}
		if strings.EqualFold(sa.Params["RememberChanged"], "True") {
			c.Remembered = append(c.Remembered, state.Target{Obj: id})
			eventRemember(h, c, id)
		}
		if strings.EqualFold(strings.TrimSpace(sa.Params["RememberSearched"]), "True") {
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
		if to == state.ZBattlefield && strings.EqualFold(sa.Params["Tapped"], "True") {
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
	reveal := strings.EqualFold(strings.TrimSpace(sa.Params["Reveal"]), "True") ||
		(to != state.ZBattlefield && spec != "Card" &&
			strings.TrimSpace(sa.Params["Defined"]) == "" &&
			!strings.EqualFold(strings.TrimSpace(sa.Params["NoReveal"]), "True"))
	if strings.EqualFold(strings.TrimSpace(sa.Params["Hidden"]), "True") &&
		!strings.EqualFold(strings.TrimSpace(sa.Params["Reveal"]), "True") {
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
		if !strings.EqualFold(strings.TrimSpace(sa.Params["NoLooking"]), "True") {
			chooser := searchChooser(h, c, sa)
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
	if zoneIn(zones, state.ZLibrary) && searchShuffleTail(h, c, sa, owner, moved, to) {
		return true // the may-shuffle confirm suspended the resolution
	}
	return false
}

// shuffleLibrary applies the default hidden-library shuffle used by searches
// and direct Defined$ fetches: the CR 701.23d shuffle, unless the SA opts out
// (NoShuffle$ True / Shuffle$ False). A hand put-back is different: it
// shuffles only when its own SA explicitly says Shuffle$ True, and uses
// shuffleLibraryExplicit below. ShuffleNonMandatory$ True -- Forge's "Do you
// want to shuffle the library?" confirm, an information-mercy so a player may
// keep the library order a search just taught them -- is NOT read here: this
// helper is the mandatory path, and the confirm belongs to the search's own
// tail, searchShuffleTail below.
func shuffleLibrary(h Host, sa *cards.SA, owner state.PlayerID) {
	if strings.EqualFold(sa.Params["NoShuffle"], "True") || strings.EqualFold(sa.Params["Shuffle"], "False") {
		return
	}
	shuffleLibraryOrder(h, owner)
}

// objectPathShuffleOwed reports whether an object-target ChangeZone that
// moved objects into a library states the explicit Shuffle$ True (the
// graveyard/battlefield/exile "shuffle it into their library" family). The
// object path reads only the explicit flag: the 66 corpus lines that move a
// card into a library with no Shuffle$ parameter are LibraryPosition$ "put
// it on top of your library" movers, which must not shuffle. NoShuffle$
// True is honoured exactly as shuffleLibrary reads it.
func objectPathShuffleOwed(sa *cards.SA) bool {
	return strings.EqualFold(strings.TrimSpace(sa.Params["Shuffle"]), "True") &&
		!strings.EqualFold(sa.Params["NoShuffle"], "True")
}

// objectPathShuffleTail finishes an object-target ChangeZone that moved
// objects into a library, after the moves landed in effChangeZone. It
// shuffles each distinct card owner's library once, and when the SA also sets
// ShuffleNonMandatory$ it poses Forge's may-shuffle confirm first. Today's
// flag-bearing corpus lines target their controller's own graveyard, so the
// controller is the owner; this is not a per-owner election for future
// multi-owner movers. SP-parented DB carriers reach this tail since task
// spcz1: their own targeting is offered by changeZoneChosenTargets's ask
// (rules/ pins the live path on Put Away and Cathartic Parting).
// Returns true when the confirm
// suspended the resolution; the answer re-enters effChangeZone, whose
// SearchShuffle early-return calls this again with moved == nil. A host that
// cannot ask takes the deterministic decline (R-9), the same stand-in every
// other may-shuffle confirm uses.
func objectPathShuffleTail(h Host, c *Ctx, sa *cards.SA, moved []state.ObjID) bool {
	if c.SearchShuffle != "" {
		ans, placed := c.SearchShuffle, c.SearchShuffleMoved
		c.SearchShuffle, c.SearchShuffleMoved = "", nil
		if ans == "yes" {
			objectPathShuffleOwners(h, placed)
		}
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(sa.Params["ShuffleNonMandatory"]), "True") {
		objectPathShuffleOwners(h, moved)
		return false
	}
	d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "search_mayshuffle", ResumeSA: sa,
		ResumeMoved: append([]state.ObjID(nil), moved...),
		Prompt:      "Shuffle your library?",
		Options: []decision.Option{
			{Index: 0, Kind: "yes", Label: "Yes — shuffle", Player: c.Controller},
			{Index: 1, Kind: "no", Label: "No — keep the order", Player: c.Controller},
		}}
	if Ask(h, d) == AskAsked {
		return true
	}
	// No-host stand-in (R-9): decline the shuffle, keep the order.
	return false
}

// objectPathShuffleOwners shuffles the library of every distinct owner among
// the moved objects, each once, in first-move order (deterministic; never a
// map range). An object that has already left the game is skipped.
func objectPathShuffleOwners(h Host, moved []state.ObjID) {
	g := h.Game()
	seen := make(map[state.PlayerID]bool, len(moved))
	for _, id := range moved {
		o := g.Obj(id)
		if o == nil || seen[o.Owner] {
			continue
		}
		seen[o.Owner] = true
		shuffleLibraryOrder(h, o.Owner)
	}
}

// searchShuffleTail is a hidden-library search's shuffle-and-place tail, with
// the ShuffleNonMandatory$ read (Path to Exile, Stoneforge Mystic, Squadron
// Hawk, Boggart Harbinger -- 209 exact-Origin$ Library corpus lines carry the
// flag). When the flag is set, even if the search moved no cards, the
// searcher is offered Forge's may-shuffle confirm -- "Shuffle your
// library?" -- instead of the unconditional shuffle: declining keeps the
// library order the search's option list (offered in library order) just
// taught them. The confirm is offered whether or not the search moved a
// card (searchmay1): the fail-to-find shape asks too, because the search's
// mandatory shuffle is exactly what the confirm may spare, and a player who
// failed to find has just as much reason to keep the order they know. This
// is also the tail an object-target ChangeZone into a library calls
// (searchmay1), so a graveyard shuffle-in poses the same confirm.
//
// The confirm suspends the resolution after the moves: the answer re-enters
// through the "search_mayshuffle" resume arm (rules' resumeResolution), which
// restores the answer into Ctx.SearchShuffle and the moved list into
// Ctx.SearchShuffleMoved (ridden on the ask via Decision.ResumeMoved, the
// same runtime-continuation class as ResumeRemembered -- the re-entry's
// LibraryPosition$ placement needs the list the suspension lost). The
// re-entered effSearchLibrary consumes both at its top and finishes here:
// "yes" emits the same Secret events.Shuffle every library shuffle emits,
// "no" keeps the order, and either way placeLibraryObjects runs after the
// shuffle point exactly as the unconditional path ordered it. A host that
// cannot ask takes the deterministic no-host stand-in (R-9): decline, keep
// the order -- the same stand-in the arrange_mayshuffle confirm falls back
// to. Returns true when the confirm suspended the resolution (the caller
// must stop; the re-entry owns the tail), false when the tail completed.
func searchShuffleTail(h Host, c *Ctx, sa *cards.SA, owner state.PlayerID, moved []state.ObjID, to state.Zone) bool {
	if c.SearchShuffle != "" {
		// Re-entry after the answered confirm: the moves happened in the
		// first pass, so this pass places only. Consume and clear before
		// continuing (fx42 scoping), so a nested search poses its own confirm.
		ans, placed := c.SearchShuffle, c.SearchShuffleMoved
		c.SearchShuffle, c.SearchShuffleMoved = "", nil
		if ans == "yes" {
			shuffleLibraryOrder(h, owner)
		}
		placeLibraryObjects(h, sa, owner, placed, to)
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(sa.Params["ShuffleNonMandatory"]), "True") {
		shuffleLibrary(h, sa, owner)
		placeLibraryObjects(h, sa, owner, moved, to)
		return false
	}
	d := &decision.Decision{Player: owner, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: c.Source, ResumeKind: "search_mayshuffle", ResumeSA: sa,
		ResumeTarget: c.LibraryTarget, ResumeRemembered: copyTargets(c.Remembered),
		ResumeForgetOtherSnapshot: copyTargets(c.ForgetOtherSnapshot),
		ResumeForgetOtherOwners:   append([]state.PlayerID(nil), c.ForgetOtherOwners...),
		ResumeForgetOtherReady:    c.ForgetOtherReady, ResumeForgetOtherCleared: c.ForgetOtherCleared,
		ResumeMoved: append([]state.ObjID(nil), moved...),
		Prompt:      "Shuffle your library?",
		Options: []decision.Option{
			{Index: 0, Kind: "yes", Label: "Yes — shuffle", Player: owner},
			{Index: 1, Kind: "no", Label: "No — keep the order", Player: owner},
		}}
	if Ask(h, d) == AskAsked {
		return true // suspended; the answer re-enters with Ctx.SearchShuffle set.
	}
	// No-host stand-in (R-9): decline the shuffle, keep the order.
	placeLibraryObjects(h, sa, owner, moved, to)
	return false
}

func shuffleLibraryExplicit(h Host, sa *cards.SA, owner state.PlayerID) {
	if strings.EqualFold(sa.Params["Shuffle"], "True") &&
		!strings.EqualFold(sa.Params["NoShuffle"], "True") {
		shuffleLibraryOrder(h, owner)
	}
}

func shuffleLibraryOrder(h Host, owner state.PlayerID) {
	order := h.ShuffleLibrary(owner, h.Game().Zone(state.ZLibrary, owner))
	h.Emit(events.Event{Kind: events.Shuffle, Player: owner, IDs: order, Secret: true})
}

// placeLibraryObjects implements LibraryPosition$ after its source library
// was shuffled. It is shared by a searched subset and a Defined$ fetch list.
// Reorder$ True (Goblin Recruiter's "put those cards on top in any order",
// Brainstorm's put-back) is the marker that the ANSWER order is the
// placement order: the branch below pins the chosen cards on top in exactly
// the order the player's answer carried them (libraryOrderPlacement), never
// a re-sorted one.
func placeLibraryObjects(h Host, sa *cards.SA, owner state.PlayerID, moved []state.ObjID, to state.Zone) {
	if strings.EqualFold(strings.TrimSpace(sa.Params["Reorder"]), "True") && to == state.ZLibrary {
		position := strings.TrimSpace(sa.Params["LibraryPosition"])
		if len(moved) > 0 && (position == "0" || position == "-1") {
			libraryOrderPlacement(h, owner, moved, position == "-1")
		}
		return
	}
	position := strings.TrimSpace(sa.Params["LibraryPosition"])
	if to != state.ZLibrary || len(moved) == 0 || (position != "0" && position != "-1") {
		return
	}
	libraryOrderPlacement(h, owner, moved, position == "-1")
}

// libraryOrderPlacement is the one LibraryPosition$ placement both
// hidden-origin movers (the library search's tutor-back and the hand
// put-back) share. The moved cards are already in the library (Move appended
// them at the bottom, in settle order); this one Secret LibraryOrder makes
// position exact: bottom=false puts the chosen cards on TOP in chosen order,
// bottom=true leaves them at the bottom in that same order, with the rest of
// the library beneath/above them respectively. Secret so the full order is
// visible only to the library's owner (redaction rule (1)).
func libraryOrderPlacement(h Host, owner state.PlayerID, moved []state.ObjID, bottom bool) {
	selected := make(map[state.ObjID]bool, len(moved))
	for _, id := range moved {
		selected[id] = true
	}
	lib := h.Game().Zone(state.ZLibrary, owner)
	rest := make([]state.ObjID, 0, len(lib)-len(moved))
	placed := make([]state.ObjID, 0, len(moved))
	for _, id := range moved {
		if containsID(lib, id) {
			placed = append(placed, id)
		}
	}
	for _, id := range lib {
		if !selected[id] {
			rest = append(rest, id)
		}
	}
	order := make([]state.ObjID, 0, len(lib))
	if !bottom {
		order = append(order, placed...)
		order = append(order, rest...)
	} else {
		order = append(order, rest...)
		order = append(order, placed...)
	}
	h.Emit(events.Event{Kind: events.LibraryOrder, Player: owner, IDs: order, Secret: true})
}

// libraryOrderPlacementAt places an already-moved subset at an exact library
// position. Negative positions count from the bottom (-1 is the bottom); a
// positive position is a zero-based offset from the top. AlternativeDecider
// uses position 1 for the corpus's second-from-top vs bottom choices.
func libraryOrderPlacementAt(h Host, owner state.PlayerID, moved []state.ObjID, position int32) {
	selected := make(map[state.ObjID]bool, len(moved))
	for _, id := range moved {
		selected[id] = true
	}
	lib := h.Game().Zone(state.ZLibrary, owner)
	rest := make([]state.ObjID, 0, len(lib)-len(moved))
	placed := make([]state.ObjID, 0, len(moved))
	for _, id := range moved {
		if containsID(lib, id) {
			placed = append(placed, id)
		}
	}
	for _, id := range lib {
		if !selected[id] {
			rest = append(rest, id)
		}
	}
	order := make([]state.ObjID, 0, len(lib))
	if position < 0 {
		order = append(order, rest...)
		order = append(order, placed...)
	} else {
		offset := int(position)
		if offset > len(rest) {
			offset = len(rest)
		}
		order = append(order, rest[:offset]...)
		order = append(order, placed...)
		order = append(order, rest[offset:]...)
	}
	h.Emit(events.Event{Kind: events.LibraryOrder, Player: owner, IDs: order, Secret: true})
}

// changeZoneAllPlayers resolves the player scope Forge's ChangeZoneAllEffect
// applies. A card that says "exile all cards from target player's graveyard"
// (Bojuka Bog, Tormod's Crypt, Nihil Spellbomb, ...) names ONE player and must
// move only that player's cards; without this scope the effect swept every
// player's zones. Forge's rule:
//
//	if ((!sa.usesTargeting() && !sa.hasParam("Defined")) || UseAllOriginZones$ True)
//	    -> every player
//	else
//	    -> the chosen target players when the ability uses targeting, else the
//	       Defined$ players (getTargetPlayers; targeting wins when both ride)
//
// Both scope forms resolve through the shared player-target vocabulary
// (`Defined` / `definedSpec`), so `ValidTgts$ Player`, `ValidTgts$ Opponent`,
// `Defined$ You`, `Defined$ TargetedController` and the rest all work without a
// second spelling table. A selector we cannot resolve to a PLAYER (an unknown
// spelling, or an object-only one) keeps the pre-fix all-players sweep rather
// than silently moving nothing, and emits a Note saying so: the unscoped sweep
// is the previous behaviour, so an unmodelled card is never quietly inert, and
// a card we DO understand is correctly restricted. `Origin$` handling is not
// touched -- ParseZones already splits `Hand,Graveyard` into two zones and the
// Any/All wildcard is resolved by the caller before this runs.
func changeZoneAllPlayers(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	g := h.Game()
	if strings.EqualFold(strings.TrimSpace(sa.Params["UseAllOriginZones"]), "True") {
		return g.AliveFrom(0)
	}
	_, targeting := sa.Params["ValidTgts"]
	_, defined := sa.Params["Defined"]
	if !targeting && !defined {
		return g.AliveFrom(0)
	}
	var chosen []state.Target
	if targeting {
		// Forge's getTargetPlayers reads the SA's ANSWERED target players when
		// it uses targeting, never Defined$; the pre-ask answered set outranks
		// the resolution list exactly as Defined's own targeting branch does.
		if c.PickedTargets != nil {
			chosen = c.PickedTargets
		} else {
			chosen = c.Targets
		}
	} else {
		chosen = Defined(h, c, sa)
	}
	seen := make(map[state.PlayerID]bool, len(chosen))
	out := make([]state.PlayerID, 0, len(chosen))
	for _, t := range chosen {
		if !t.IsPlayer || seen[t.Player] {
			continue
		}
		seen[t.Player] = true
		out = append(out, t.Player)
	}
	if len(out) == 0 {
		sel := sa.Params["Defined"]
		if targeting {
			sel = sa.Params["ValidTgts"]
		}
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "ChangeZoneAll could not resolve a player scope from " + sel + "; sweeping all players"})
		return g.AliveFrom(0)
	}
	return out
}

func effChangeZoneAll(h Host, c *Ctx, sa *cards.SA) {
	from, all, valid := ParseZones(sa.Params["Origin"])
	if !valid {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unrecognised ChangeZoneAll Origin " + sa.Params["Origin"]})
		return
	}
	if all {
		from = []state.Zone{
			state.ZLibrary, state.ZHand, state.ZBattlefield, state.ZGraveyard,
			state.ZExile, state.ZStack, state.ZCommand, state.ZCeased,
		}
	}
	to := ParseZone(sa.Params["Destination"])
	spec := sa.Params["ChangeType"]
	if spec == "" {
		spec = "Card"
	}
	// ChangeNum$ caps the sweep (expert_level_safe's DBOpenSafe writes "All",
	// bone_dancer's DBChangeZone writes "1"): an omitted value or "All" moves
	// every matching card -- the behaviour the primitive always had -- while a
	// numeric cap (a literal, or an SVar/X reference through Num) moves at
	// most that many, in the sweep's own scan order (zone-major, seat-minor;
	// a RandomOrder$ sweep's shuffle picks WHICH candidates sit under the
	// cap, since the shuffle only sets the move order). A value Num cannot
	// resolve degrades to 0 by Num's own documented convention -- "the card
	// did nothing", the fail-closed direction.
	changeCap := int32(-1) // -1: uncapped
	if raw := strings.TrimSpace(sa.Params["ChangeNum"]); raw != "" && !strings.EqualFold(raw, "All") {
		changeCap = Num(h, c, sa, "ChangeNum", 0)
		if changeCap < 0 {
			changeCap = 0
		}
	}
	g := h.Game()
	// LibraryPosition$ (Terminus' "put all creatures on the bottom of their
	// owners' libraries") and Shuffle$ (Jace, the Mind Sculptor's [-12]
	// "shuffles their hand into their library", Gomazoa's "put on top ... then
	// those players shuffle") both act on the DESTINATION libraries, which are
	// each object's OWNER's library — a battlefield creature controlled by
	// another player (the Gomazoa / Vortex Elemental blocking shapes) still
	// returns to its owner's library, because the MoveZone keeps its owner.
	// The move loop therefore records every destination-library OWNER that had
	// a card moved (read off the object, not the source-zone player), in the
	// loop's own deterministic (zone-major, AliveFrom(0)-minor) order.
	position := strings.TrimSpace(sa.Params["LibraryPosition"])
	shuffle := strings.EqualFold(sa.Params["Shuffle"], "True")
	type ownerMoved struct {
		owner state.PlayerID
		ids   []state.ObjID
	}
	var placements []ownerMoved
	// AtEOT$'s affected set for ChangeZoneAll is the objects the sweep
	// actually moved, collected in move order.
	var moved []state.ObjID
	findOwnerMoved := func(owner state.PlayerID) *ownerMoved {
		for i := range placements {
			if placements[i].owner == owner {
				return &placements[i]
			}
		}
		placements = append(placements, ownerMoved{owner: owner})
		return &placements[len(placements)-1]
	}
	players := changeZoneAllPlayers(h, c, sa)
	// ForgetOtherRemembered$ True (The Mimeoplasm's MimeoExile, 11 corpus
	// ChangeZoneAll carriers): Forge forgets every previously remembered
	// object before this effect resolves, so a setup that remembered its own
	// candidates (the ChooseCard's RememberChosen$) plus stale memory from an
	// earlier resolution leaves exactly the moved set behind (RememberChanged$
	// re-remembers it). The ChangeType$ Card.IsRemembered selector reads the
	// very memory the clear drops, so the matched set is snapshotted BEFORE
	// the clear and the sweep below matches against the snapshot -- matching
	// after the clear would sweep nothing.
	var preMatched map[state.ObjID]bool
	if strings.EqualFold(strings.TrimSpace(sa.Params["ForgetOtherRemembered"]), "True") {
		preMatched = make(map[state.ObjID]bool)
		for _, z := range from {
			for _, p := range players {
				for _, id := range g.Zone(z, p) {
					if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
						preMatched[id] = true
					}
				}
			}
		}
		c.Remembered = nil
		clearEventRemembered(h, c)
	}
	// RandomOrder$ True (task mordorparams1, Gríma, Saruman's Footman's
	// "Then that player puts the exiled cards that weren't cast this way on
	// the bottom of their library in a random order"): the destination
	// placement order is a real shuffle, not the engine's scan order. The
	// cards are COLLECTED first (the same zone-major/seat-minor scan, no
	// emission), Fisher-Yates'd per destination-library owner through the
	// seeded engine rng (the randomChoices/Host.Rand precedent — a replay
	// re-derives the identical order), and only then emitted, so the
	// MoveZone appends settle in the shuffled order. The shuffle only sets
	// the MOVE ORDER; the LibraryPosition$/Shuffle$ tail below still applies
	// on top of it (Triumph of Saint Katherine's `LibraryPosition$ 0` after
	// a RandomOrder$ sweep puts the shuffled pile on TOP, not the bottom).
	// The Dig/RestRandomOrder$/RevealRandomOrder$ variants are their own rows
	// and are not touched here.
	randomOrder := strings.EqualFold(strings.TrimSpace(sa.Params["RandomOrder"]), "True")
	rider := classifyAttackingEntry(c, sa, to)
	emitMove := func(id state.ObjID, z state.Zone, p state.PlayerID) {
		// A CantExile restriction withholds the object from a battlefield exile
		// before the MoveZone (and the moved bookkeeping) is produced -- the
		// ChangeZoneAll half of the same guard effChangeZone's object loop and
		// settleChangeZoneMoveAs carry.
		if to == state.ZExile && h.ExileBlocked(id, false) {
			return
		}
		// Capture before MoveZone folds: battlefield departure resets control,
		// clears counters and removes battlefield-derived characteristics.
		if strings.EqualFold(sa.Params["RememberLKI"], "True") {
			if o := g.Obj(id); o != nil {
				snapshot := o.CloneDeep()
				c.ChangeZoneLKI = append(c.ChangeZoneLKI, state.LKIObject{
					Obj: id, Controller: o.Controller, Owner: o.Owner, Snapshot: snapshot,
				})
			}
		}
		ev := moveZoneEvent(c, id, z, to)
		applyFaceDownMarker(h, sa, c, &ev, to)
		h.Emit(ev)
		moved = append(moved, id)
		// Tapped$ True (Splendid Reclamation's "Return all land cards
		// ... tapped"): a battlefield entry is followed by the same
		// "entered tapped" Tap event every other Tapped$ zone-change
		// path emits -- an entry state, not the CR 701.21a event of
		// becoming tapped.
		if to == state.ZBattlefield && strings.EqualFold(strings.TrimSpace(sa.Params["Tapped"]), "True") {
			h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: p, Text: "entered tapped"})
		}
		rider.apply(h, c, id, p, to)
		if to == state.ZExile {
			recordExileReturn(h, c, sa, id, z, to)
		}
		// GainControl$ hands the moved object to the named player
		// (Karn Liberated's ReturnFromExile, Cold Storage, Ghost
		// Vacuum). Only a battlefield entry can carry a control
		// change (CR 701.22a controls permanents), the same rule the
		// ChangeZone path applies; the shared resolver is loud rather
		// than silent on an unresolvable selector.
		if to == state.ZBattlefield {
			applyGainControl(h, c, sa, id)
			// StaticEffect$ (ChangeZoneAll's carriers -- Ghost Vacuum, Grimoire
			// of the Dead, Storm of Souls, Shilgengar): the same per-card rider
			// registration every other ChangeZone mover applies.
			applyStaticEffect(h, c, sa, to, []state.ObjID{id})
		}
		if to == state.ZLibrary {
			owner := p
			if o := g.Obj(id); o != nil {
				owner = o.Owner
			}
			findOwnerMoved(owner).ids = append(findOwnerMoved(owner).ids, id)
		}
		// RememberChanged$ True re-remembers the moved cards in both halves
		// (the ctx list the chain's later sub-abilities read and the source's
		// event-backed persistent list a later resolution's IsRemembered /
		// Remembered$ head reads -- The Mimeoplasm's MimeoChooseCopy, Gift of
		// Immortality's return trigger). Previously this recorded the ctx
		// entries alone and only for the ExiledWithSource provenance shape
		// (Valakut Exploration); the persistent half is what the Mimeoplasm
		// chain's IsRemembered/Remembered$CardPower reads need.
		if strings.EqualFold(sa.Params["RememberLKI"], "True") &&
			!strings.EqualFold(sa.Params["RememberChanged"], "True") {
			c.Remembered = append(c.Remembered, state.Target{Obj: id})
		}
		if strings.EqualFold(sa.Params["RememberChanged"], "True") {
			c.Remembered = append(c.Remembered, state.Target{Obj: id})
			eventRemember(h, c, id)
		}
	}
	if randomOrder {
		type pendingMove struct {
			id state.ObjID
			z  state.Zone
			p  state.PlayerID
		}
		var owners []state.PlayerID
		byOwner := make(map[state.PlayerID][]pendingMove)
		for _, z := range from {
			for qi, p := range players {
				// The shared stack (state/game.go Zone) is snapshotted once,
				// under the first player in the resolved scope. Without this an
				// N-player sweep enqueues the same stack object N times and
				// emits N MoveZones for one card. Other origins stay per-player.
				if z == state.ZStack && qi > 0 {
					continue
				}
				// Snapshot the zone exactly like the emit loop does.
				ids := append([]state.ObjID(nil), g.Zone(z, p)...)
				for _, id := range ids {
					if preMatched != nil {
						if !preMatched[id] {
							continue
						}
					} else if !MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
						continue
					}
					owner := p
					if o := g.Obj(id); o != nil {
						owner = o.Owner
					}
					if _, seen := byOwner[owner]; !seen {
						owners = append(owners, owner)
					}
					byOwner[owner] = append(byOwner[owner], pendingMove{id: id, z: z, p: p})
				}
			}
		}
		for _, owner := range owners {
			list := byOwner[owner]
			for i := len(list) - 1; i > 0; i-- {
				j := h.Rand(i + 1)
				list[i], list[j] = list[j], list[i]
			}
			byOwner[owner] = list
		}
	emitLoop:
		for _, owner := range owners {
			for _, pm := range byOwner[owner] {
				if changeCap >= 0 && int32(len(moved)) >= changeCap {
					break emitLoop
				}
				emitMove(pm.id, pm.z, pm.p)
			}
		}
	} else {
	sweep:
		for _, z := range from {
			for qi, p := range players {
				// Same shared-stack guard as the RandomOrder$ branch: one
				// snapshot of the stack, taken under the first scoped player.
				if z == state.ZStack && qi > 0 {
					continue
				}
				// Snapshot the zone: emitting move events mutates it underneath us.
				ids := append([]state.ObjID(nil), g.Zone(z, p)...)
				for _, id := range ids {
					if changeCap >= 0 && int32(len(moved)) >= changeCap {
						break sweep
					}
					matched := false
					if preMatched != nil {
						matched = preMatched[id]
					} else {
						matched = MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller))
					}
					if matched {
						emitMove(id, z, p)
					}
				}
			}
		}
	}
	// The post-placement tail runs for BOTH branches: the shuffled order is
	// only the ORDER the moves settle in, so `LibraryPosition$ 0` (Triumph of
	// Saint Katherine's "shuffle that pile and put it back on TOP of your
	// library") and `Shuffle$` must still apply after a RandomOrder$ sweep.
	if to == state.ZLibrary && len(placements) > 0 {
		// LibraryPosition$: MoveZone already appends at the bottom of the
		// destination library in settle order, so "-1" (Terminus) is exactly the
		// move order and needs no extra event; "0" pins the moved cards on TOP
		// via the one Secret LibraryOrder placement every library placement
		// shares. Any other value is loud rather than silently inert.
		switch position {
		case "", "-1":
		case "0":
			for _, pm := range placements {
				libraryOrderPlacement(h, pm.owner, pm.ids, false)
			}
		default:
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "LibraryPosition$ " + position + " is not implemented; the cards sit at the BOTTOM of their owners' libraries (the MoveZone append)"})
		}
		// Shuffle$ True shuffles each destination library that received a card,
		// AFTER the placement (Gomazoa's "put on top ..., then those players
		// shuffle" order), through the same Secret events.Shuffle every other
		// library shuffle emits.
		if shuffle {
			for _, pm := range placements {
				shuffleLibraryOrder(h, pm.owner)
			}
		}
	}
	scheduleAtEOT(h, c, sa, moved)
}

// effDestroy is a single-target removal effect: exactly the shape CR 608.2b
// target rechecking exists for. Today the only recheck is "does the target
// still exist, and is it still on the battlefield" -- a target that stayed on
// the battlefield but became newly ineligible some other way (e.g. it gained
// Indestructible in response, or protection from the source) between
// targeting and resolution is not rechecked. See the Task 18 report.
func effDestroy(h Host, c *Ctx, sa *cards.SA) {
	// Forge's ForgetOtherTargets$ replaces the prior remembered set before
	// this Destroy, while RememberTargets$ records only objects that actually
	// leave the battlefield (not targets spared by regeneration or
	// indestructibility).  Keep both the resolution-local and event-backed
	// halves in sync, as the chained sub-ability may read either one.
	if strings.EqualFold(strings.TrimSpace(sa.Params["ForgetOtherTargets"]), "True") {
		c.Remembered = nil
		clearEventRemembered(h, c)
	}
	// RememberTargets$ records only objects that actually leave the
	// battlefield. RememberDestroyed$ True (Transforming Flourish) is Forge's
	// spelling of the same "this permanent was destroyed this way" record.
	// RememberLKI$ True (Noxious Gearhulk) does BOTH -- it records the object
	// and captures its last-known-information snapshot (CR 603.10 look-back),
	// because the chained read (RememberedLKI$CardToughness) needs the
	// battlefield P/T that events.Apply's Move clears. The snapshot rides
	// Ctx.LKI/LKIPower/LKIToughness, the same fields rules' triggerLKI publishes
	// and evalRefProperty reads for a zone-change trigger; evalRefProperty
	// applies it only when the snapshot names the referenced object, so no
	// other remembered read is affected.
	rememberTargets := strings.EqualFold(strings.TrimSpace(sa.Params["RememberTargets"]), "True")
	rememberDestroyed := strings.EqualFold(strings.TrimSpace(sa.Params["RememberDestroyed"]), "True")
	rememberLKI := strings.EqualFold(strings.TrimSpace(sa.Params["RememberLKI"]), "True")
	// Same pre-batch discipline as effDestroyAll: the targets Defined
	// resolves are destroyed as one simultaneous batch (a multi-target
	// Destroy over a lifelink Equipment and its bearer must not make the
	// bearer's LKI depend on battlefield order), so the snapshot covers all
	// of them before the first move.
	var victims []state.ObjID
	for _, t := range Defined(h, c, sa) {
		o := h.Game().Obj(t.Obj)
		if t.IsPlayer || o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		if h.HasKeyword(o.ID, "Indestructible") {
			continue
		}
		victims = append(victims, o.ID)
	}
	if len(victims) > 0 {
		h.BatchDepartures(victims)
		defer h.EndBatchDepartures()
	}
	for _, id := range victims {
		o := h.Game().Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		// NoRegen$ is compared against "True", not against empty: an explicit
		// NoRegen$ False PERMITS regeneration, and reading it as "set, so
		// suppress" would invert the card. The corpus splits 144 True / 1
		// False (creepy_doll.txt), and that one is unreachable today because
		// cards/link.go auto-links only SubAbility$, not the WinSubAbility$ it
		// hangs off -- so this is correctness insurance for when that changes,
		// not a live fix.
		if sa.Params["NoRegen"] != "True" && ReplaceDestruction(h, id) {
			continue
		}
		// Umbra armor (CR 702.90) applies even when NoRegen$ suppresses
		// regeneration — it is its own replacement, not a shield. Consuming
		// the Aura leaves it in the graveyard; when the loop reaches the Aura
		// itself (a DestroyAll that named it too) the zone guard above skips it.
		if ReplaceUmbraArmor(h, id) {
			continue
		}
		// Capture the last-known information BEFORE the Move folds: the
		// snapshot must see the battlefield permanent (its counters, pump
		// layers and printed toughness), not the graveyard card the move
		// leaves behind. The zone guard above proved o is on the battlefield.
		var lki *state.Object
		var lkiPower, lkiToughness int32
		var lkiValid bool
		if rememberLKI {
			cp := o.CloneDeep()
			lki = &cp
			lkiPower, lkiToughness = h.Power(id), h.Toughness(id)
			lkiValid = true
		}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id,
			From: state.ZBattlefield, To: state.ZGraveyard, Text: "destroyed"})
		// Host.Emit applies move replacements before folding the move. Only
		// remember a permanent that actually ended up in the graveyard; a
		// replacement such as exile must not feed a later IsRemembered search.
		if rememberTargets || rememberDestroyed || rememberLKI {
			if moved := h.Game().Obj(id); moved != nil && moved.Zone == state.ZGraveyard {
				if rememberLKI {
					c.LKI = lki
					c.LKIPower, c.LKIToughness, c.LKIPTValid = lkiPower, lkiToughness, lkiValid
				}
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
		}
	}
}

func effDestroyAll(h Host, c *Ctx, sa *cards.SA) {
	spec := sa.Params["ValidCards"]
	if spec == "" {
		spec = "Permanent"
	}
	zone := state.ZBattlefield
	if raw := strings.TrimSpace(sa.Params["Zone"]); raw != "" {
		var ok bool
		zone, ok = ParseZoneWord(raw)
		if !ok {
			return
		}
	}
	g := h.Game()
	remember := strings.EqualFold(strings.TrimSpace(sa.Params["RememberDestroyed"]), "True")
	// One pre-batch victim list across every player, then ONE departure
	// snapshot, then the emit loop (CR 704.3 simultaneity, as far as the
	// sequential emit model can express it): the CR 603.10a lifelink LKI a
	// later victim's departure capture reads must be the state from
	// immediately before the FIRST move -- a destroy-all over a
	// lifelink-granting Equipment and its bearer must not make the bearer's
	// own lifelink LKI depend on battlefield order.
	var victims []state.ObjID
	sc := c.SpecContext(c.Controller)
	sc.CombatDamageHits = h.CombatDamageToPlayersThisTurn()
	for _, p := range g.AliveFrom(0) {
		ids := append([]state.ObjID(nil), g.Zone(zone, p)...)
		for _, id := range ids {
			if zone == state.ZBattlefield && h.HasKeyword(id, "Indestructible") {
				continue
			}
			if MatchesSpecCtx(g, spec, id, sc) {
				victims = append(victims, id)
			}
		}
	}
	if len(victims) > 0 && zone == state.ZBattlefield {
		h.BatchDepartures(victims)
		defer h.EndBatchDepartures()
	}
	for _, id := range victims {
		if g.Obj(id) == nil || g.Obj(id).Zone != zone {
			continue
		}
		if zone == state.ZBattlefield {
			// NoRegen$ != "True", not == "": see effDestroy's note above.
			if sa.Params["NoRegen"] != "True" && ReplaceDestruction(h, id) {
				continue
			}
			// Umbra armor after the shield: see effDestroy's note.
			if ReplaceUmbraArmor(h, id) {
				continue
			}
		}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id,
			From: zone, To: state.ZGraveyard, Text: "destroyed"})
		if remember {
			// Forge's RememberDestroyed$ adds only cards that actually
			// reached the graveyard; a move replacement may redirect it.
			if moved := h.Game().Obj(id); moved != nil && moved.Zone == state.ZGraveyard {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
		}
	}
}

// sacTargetCardReferent resolves a SacValid$ spec that names the resolution's
// card TARGET rather than the resolving source. Forge writes this referent as
// the base `TargetedCard` with the `.Self` property (“the card this ability
// targeted”), e.g. Enchanter's Bane's `SacValid$ TargetedCard.Self`: the
// sacrifice is aimed at the targeted enchantment's controller, and the only
// eligible permanent is that enchanted enchantment itself. The engine's filter
// grammar reads a bare `Self` relative to the resolving SOURCE, so this base
// is unrecognised there and fails closed; resolving it here, against the
// resolution's object targets, is what admits the actual targeted card. Any
// other base (including `Self`/`Card.Self`, whose subject IS the source) and any
// qualifier other than `.Self` are not this referent and return ok=false,
// leaving MatchesSpecCtx's own reading in place -- fail closed, never widened.
// The caller checks the returned id against the pool object, so a referent that
// is not in the asked player's battlefield never becomes eligible.
func sacTargetCardReferent(spec string, c *Ctx) (state.ObjID, bool) {
	base, qual, _ := strings.Cut(strings.TrimSpace(spec), ".")
	if base != "TargetedCard" || qual != "Self" {
		return 0, false
	}
	for _, t := range c.Targets {
		if !t.IsPlayer && t.Obj != 0 {
			return t.Obj, true
		}
	}
	return 0, false
}

// effSacrifice moves permanents to the graveyard. Sacrifice ignores
// Indestructible: sacrificing is not destruction (CR 701.16), so no
// HasKeyword/Indestructible gate and no ReplaceDestruction/regeneration
// consultation -- a regenerated creature does not survive being sacrificed.
// Same CR 608.2b caveat as effDestroy: only existence-and-zone is rechecked.
//
// A sacrifice aimed at a PLAYER now asks that player (CR 701.21a: "its
// controller chooses one") through a real KChoose over their matching
// permanents: Amount$ (default 1) sizes the ask, Optional$ True makes it
// "may sacrifice" (Min 0), and a hand of fewer eligible permanents than
// Amount$ sacrifices everything it has without asking (there is no choice
// to record, the effDiscard TgtChoose strict-supersets rule). The answer
// re-enters this effect through ResumeKind "sacrifice" with Ctx.SacPicks
// set, one suspension per Defined$ target (the cursor mirrors effDig's
// per-library asks). An Optional$ ask whose no-host fallback runs takes the
// first Amount$ eligible permanents — the same pick the pre-ask engine made
// — so games that never reach a real player answer replay byte-identically
// up to the pick the answer names.
func effSacrifice(h Host, c *Ctx, sa *cards.SA) {
	// UnlessCost$ is handled by the shared unlessProceed gate in Resolve,
	// exactly as it is for every other API — including the Vexing Devil
	// damage-payment offer (UnlessCost$ DamageYou<N>, UnlessPayer$ Opponent,
	// UnlessSwitched$ True), whose "pay" is taking the damage. When the gate
	// consumed the resolution — an ask was posed (suspended), the answered
	// choice spared the permanent, or every opponent declined the offer —
	// this body does not run at all.
	g := h.Game()
	// SacValid$ narrows WHAT may be sacrificed ("Creature.nonToken",
	// "Artifact"). With no SacValid$ at all the default is "Permanent" (any
	// permanent). The older justification -- that the self-sacrifice and
	// at-end-of-step lines need "Permanent" because the object they sacrifice
	// may be an artifact, a land or a creature -- is empirically false: those
	// lines carry no Defined$ and no ValidTgts$, so Defined() resolves them
	// to the SOURCE object (effects/context.go) and they take the object-target
	// path below.
	//
	// ValidCard$ is the corpus's second narrowing spelling: three Sacrifice
	// SAs carry `ValidCard$ Card.Self` and no SacValid$. Exactly one of them is
	// player-targeted -- Expert-Level Safe's
	// `DB$ Sacrifice | Defined$ You | ValidCard$ Card.Self` -- so before this
	// read its controller handed over whichever permanent sat first in zone
	// order (an artifact, a land, anything) rather than "this artifact". The
	// other two, Departed Deckhand and Dream Strix, carry no Defined$ and no
	// ValidTgts$, so Defined() resolves them to their own source object and
	// they take the object-target path below (where that object already IS the
	// self the spec names). The two spellings never co-occur in the corpus
	// (measured: 3 ValidCard$ lines, 437 SacValid$ lines, 0 carrying both), so
	// applying both as a conjunction reads every line exactly once. Card.Self
	// resolves through SpecContext.Source, so the player-targeted line can only
	// hand over the source itself.
	spec := sa.Params["SacValid"]
	if spec == "" {
		spec = "Permanent"
	}
	validCard := strings.TrimSpace(sa.Params["ValidCard"])
	// RememberSacrificed$ True drives the task's effect-driven sacrifice
	// capture: it makes effSacrifice record the LKI snapshot (power,
	// toughness, mana value) of each object it sacrifices, so a SubAbility$
	// chained after it can resolve Sacrificed$CardPower/CardManaCost/Amount
	// against what THIS ability just sacrificed. Without the flag nothing is
	// remembered -- and nothing is, because the flag is read nowhere else in
	// this package (the sacrifice_audit test only counts its occurrence), so
	// the absence is the conservative same-as-before no-op, not a regression.
	remember := sa.Params["RememberSacrificed"] != ""
	// rememberLKICapture captures the sacrificed object's LKI (before the
	// MoveZone resets its counters) into c.Sacrificed, when the flag asks it
	// to. Idempotent per call site; called exactly once per sacrificed object.
	rememberLKICapture := func(id state.ObjID) {
		if remember {
			c.Sacrificed = append(c.Sacrificed, state.SacrificedInfoOf(g, id))
			// Forge's RememberSacrificed$ also remembers the card, which is
			// what a following ConditionDefined$ Remembered, Remembered$Amount
			// or RememberedCard reads (Braids, Scapeshift, Victimize).
			c.Remembered = append(copyTargets(c.Remembered), state.Target{Obj: id})
			eventRemember(h, c, id)
		}
	}
	// fx42 scoping: capture and clear the answered per-player pick BEFORE the
	// target loop, so a nested sacrifice below this walk poses its own ask.
	// SacTarget identifies the exact target that asked: earlier targets
	// completed before suspension and must be skipped, that target consumes
	// the answer, and later targets pose their own asks (Dig's per-library
	// ask shape).
	sacAns := c.SacPicks
	sacDone := c.SacDone
	sacTarget := c.SacTarget
	c.SacPicks, c.SacDone, c.SacTarget = nil, false, 0
	amount := sacrificeAmount(h, c, sa)
	// An Amount$ of zero has no legal sacrifice and, crucially, no meaningful
	// answer. Do not produce a 0..0 KChoose merely because eligible cards
	// happen to exist (an Optional$ Amount$ X trigger with X=0 has this shape).
	if amount <= 0 {
		return
	}
	optional := sa.Params["Optional"] == "True"
	strict := optional && sa.Params["StrictAmount"] == "True"
	// Optional + StrictAmount is not a 0..Amount range: it is specifically
	// "none, or exactly Amount". The KModes answer is consumed below before a
	// possible exact-batch KChoose; keeping it separate prevents a partial
	// sacrifice from taking the card's "if you do" continuation.
	sacOptional, sacOptionalTarget := c.SacOptional, c.SacOptionalTarget
	c.SacOptional, c.SacOptionalTarget = "", 0
	who := Defined(h, c, sa)
	// ShowSacrificedCards$ True (Demonic Covenant's own sacrifice line): the
	// sacrificed cards are REVEALED publicly — one ids-Note naming everything
	// this call sacrificed, the same payload shape effMill's ShowMilledCards$
	// arm emits. Collected across every path below (the answered batch, the
	// re-entry batch and the plain object path) so one Note covers the call.
	show := strings.EqualFold(strings.TrimSpace(sa.Params["ShowSacrificedCards"]), "True")
	var sacrificed []state.ObjID
	// A Sacrifice that names neither Defined$ nor ValidTgts$ but a SacValid$
	// other than itself is Forge's default Defined$ You: its controller
	// sacrifices a matching permanent (Braids's "you may sacrifice an
	// artifact, creature, ..."). Only a SacValid$ Self/Card.Self line (or no
	// SacValid$ at all) sacrifices the source object itself. Corpus: 66 such
	// lines, which previously sacrificed the source whatever its type.
	if _, targeted := sa.Params["ValidTgts"]; !targeted && strings.TrimSpace(sa.Params["Defined"]) == "" {
		if v := strings.TrimSpace(sa.Params["SacValid"]); v != "" && v != "Self" && v != "Card.Self" {
			who = []state.Target{{Player: c.Controller, IsPlayer: true}}
		}
	}
	for targetIndex, t := range who {
		if sacOptional != "" {
			if targetIndex < sacOptionalTarget {
				continue
			}
			if targetIndex == sacOptionalTarget && sacOptional == "decline" {
				continue
			}
		}
		if sacDone {
			// Re-entry after some target's ask suspended: earlier targets
			// completed on the first pass and must be skipped (re-running
			// them would sacrifice a second batch); the asking target
			// applies its answer; later targets fall through to the normal
			// paths below and pose their own asks (Dig's per-library shape).
			if targetIndex < sacTarget {
				continue
			}
			if targetIndex == sacTarget {
				if t.IsPlayer {
					// Sacrifice exactly the answered cards that still sit on
					// this player's battlefield (a zone check keeps a stray
					// answer from moving an object that left meanwhile), in
					// the player's answer order. One departure snapshot for
					// the whole answered batch (BatchDepartures).
					if len(sacAns) > 0 {
						h.BatchDepartures(sacAns)
						defer h.EndBatchDepartures()
					}
					for _, id := range sacAns {
						if o := g.Obj(id); o == nil || o.Zone != state.ZBattlefield {
							continue
						}
						rememberLKICapture(id)
						sacrificed = append(sacrificed, id)
						h.Emit(events.Sacrifice(id))
					}
				} else if len(sacAns) > 0 {
					// The object-optional ask's sole option was answered
					// "sacrifice it": the object was already zone-checked on
					// the first pass, but re-check here in case it moved.
					if o := g.Obj(t.Obj); o != nil && o.Zone == state.ZBattlefield {
						rememberLKICapture(o.ID)
						sacrificed = append(sacrificed, o.ID)
						h.Emit(events.Sacrifice(o.ID))
					}
				}
				continue
			}
		}
		if t.IsPlayer {
			// Bounds guard: g.Zone indexes g.zones[zoneIndex(z, p)] and
			// zoneIndex has no bounds check, so an out-of-range target-supplied
			// player id would panic with "index out of range" and halt the
			// table. Player targets normally come from askTarget or AliveFrom
			// and are bounded, but the package's idiom (see cardflow.go and
			// count.go) is not to trust a target blindly.
			if int(t.Player) >= len(g.Players) {
				continue
			}
			// The multi-permanent count defaults to `amount` -- 1 for an
			// ordinary Sacrifice line, or the Amount$ the primitive carries.
			// The Annihilator expansion's generated SA carries its own count
			// in its Annihilator$ marker (cards/keywords.go) and overrides it;
			// the two contexts never coincide in the corpus.
			n := amount
			if ann := sa.Params["Annihilator"]; ann != "" {
				if v, err := strconv.Atoi(ann); err == nil && v >= 0 {
					n = int32(v)
				}
			}
			ids := append([]state.ObjID(nil), g.Zone(state.ZBattlefield, t.Player)...)
			eligible := make([]state.ObjID, 0, len(ids))
			sc := c.SpecContext(t.Player)
			for _, id := range ids {
				if h.SacrificeBlocked(id, false) {
					// A CantSacrifice restriction (Call for Aid) or face static:
					// the permanent is not a sacrifice candidate at all — not
					// offered, never taken (Annihilator rides this same pool).
					continue
				}
				// ValidCard$, when present, narrows the same pool: a permanent
				// must match BOTH spellings (they never co-occur, so this is
				// just SacValid$ and ValidCard$ in turn).
				matchesSacValid := MatchesSpecCtx(g, spec, id, sc)
				if ref, ok := sacTargetCardReferent(spec, c); ok {
					// A SacValid$ that names the resolution's card TARGET
					// (TargetedCard.Self) must admit exactly that object, not the
					// resolving source the filter grammar's bare Self reads. The
					// player-targeted pool is already this player's battlefield,
					// so id == ref is also the "belongs to the player asked"
					// check. Unknown referent forms stay with MatchesSpecCtx,
					// which fails closed.
					matchesSacValid = id == ref
				}
				if matchesSacValid &&
					(validCard == "" || MatchesSpecCtx(g, validCard, id, sc)) {
					eligible = append(eligible, id)
				}
			}
			minv, maxv := int32(0), int32(0)
			ask := false
			// n is the deterministic/no-host batch. An optional strict batch
			// with too few eligible permanents cannot be paid partially, so it
			// starts at zero rather than falling through to the old first-N path.
			if optional {
				if strict {
					switch {
					case sacOptional == "sacrifice" && targetIndex == sacOptionalTarget:
						// The player accepted the first yes/no step. If there is a
						// genuine identity choice, ask for EXACTLY Amount; when every
						// eligible permanent is required, there is nothing left to ask.
						if int32(len(eligible)) > amount {
							ask = true
							minv, maxv = amount, amount
						}
					case int32(len(eligible)) >= amount:
						// KChoose can express a range but not the disjoint set
						// {0, Amount}, so ask yes/no first and only then (above)
						// choose the exact batch.
						d := &decision.Decision{Player: t.Player, Kind: decision.KModes,
							Min: 1, Max: 1, Source: c.Source, ResumeKind: "sacrifice_optional",
							ResumeSA: sa, ResumeTarget: targetIndex,
							Prompt: "Sacrifice " + strconv.Itoa(int(amount)) + " permanent(s)?",
							Options: []decision.Option{
								{Index: 0, Kind: "mode", Label: "Sacrifice " + strconv.Itoa(int(amount)) + " permanent(s)", Obj: c.Source, Player: t.Player},
								{Index: 1, Kind: "mode", Label: "Don't sacrifice", Obj: c.Source, Player: t.Player},
							}}
						if Ask(h, d) == AskAsked {
							return
						}
						// R-9 no-host fallback: preserve the old deterministic pick,
						// but only as a complete strict batch.
						h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: t.Player,
							Text: "sacrifices the first matching permanent(s) (no engine host to ask)", Secret: true})
					case int32(len(eligible)) < amount:
						n = 0
					}
				} else if len(eligible) > 0 {
					// A non-strict optional sacrifice permits any number through
					// Amount$, including none.
					ask = true
					maxv = amount
					if maxv > int32(len(eligible)) {
						maxv = int32(len(eligible))
					}
				}
			} else if int32(len(eligible)) > n {
				// Mandatory with a choice: exactly the batch count of the
				// eligible — Amount$, or the Annihilator$ marker's count when
				// the generated expansion carries one.
				ask = true
				minv, maxv = n, n
			}
			// (the remaining mandatory shape — eligible <= amount — sacrifices
			// everything eligible without asking: no choice to record, the
			// effDiscard TgtChoose strict-supersets rule.)
			if ask {
				d := &decision.Decision{Player: t.Player, Kind: decision.KChoose,
					Min:          int(minv),
					Max:          int(maxv),
					Source:       c.Source,
					ResumeKind:   "sacrifice",
					ResumeSA:     sa,
					ResumeTarget: targetIndex,
					Prompt:       sacrificePrompt(optional && !strict, maxv)}
				for _, id := range eligible {
					name := "a permanent"
					if o := g.Obj(id); o != nil && o.Face() != nil {
						name = o.Face().Name
					}
					d.Options = append(d.Options, decision.Option{Index: len(d.Options),
						Kind: "sacrifice", Label: name, Obj: id, Player: t.Player})
				}
				if Ask(h, d) == AskAsked {
					return // resolution suspended; the answer re-enters with Ctx.SacPicks set.
				}
				// Fuzz/no-engine host: the deterministic stand-in (R-9) keeps
				// the pre-ask behaviour — the first Amount$ eligible permanents
				// in zone order, so an Optional$ "may sacrifice" plays "do".
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: t.Player,
					Text: "sacrifices the first matching permanent(s) (no engine host to ask)", Secret: true})
				n = maxv
			}
			// One departure snapshot per emitted batch (BatchDepartures): a
			// sacrifice sweep over a lifelink-granting Equipment and its bearer
			// must not make the bearer's CR 603.10a lifelink LKI depend on
			// battlefield order. Asks suspend before any emission, so every
			// suspend-then-resume path still re-collects its batch here.
			batch := make([]state.ObjID, 0, n)
			for i := int32(0); i < n && int(i) < len(eligible); i++ {
				batch = append(batch, eligible[i])
			}
			if len(batch) > 0 {
				h.BatchDepartures(batch)
				defer h.EndBatchDepartures()
			}
			for _, id := range batch {
				rememberLKICapture(id)
				sacrificed = append(sacrificed, id)
				h.Emit(events.Sacrifice(id))
			}
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		if h.SacrificeBlocked(o.ID, false) {
			// A CantSacrifice restriction (or face static): this specific
			// object cannot be sacrificed at all — neither offered to its
			// Optional$ ask nor emitted. The targeting already picked it; the
			// restriction is what stops the pick.
			continue
		}
		// A specific object target is sacrificed as-is: the choice of which
		// object was already made by the effect's targeting, so SacValid$'
		// "which one may be sacrificed" step does not re-filter a concrete
		// object (and would misfire on the corpus's SacValid$ Self lines,
		// where "Self" is not a type the filter grammar knows).
		//
		// Optional$ True on an object target is a real yes/no ("you may
		// sacrifice this artifact"): a 0..1 ask over the object, answered
		// through the same "sacrifice" resume. A host that cannot ask keeps
		// the mandatory sacrifice (the pre-ask behaviour).
		if optional {
			// A concrete target cannot satisfy a strict batch greater than one:
			// it may decline, but it must not sacrifice this one object as a
			// partial payment.
			if strict && amount != 1 {
				continue
			}
			d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose,
				Min: 0, Max: 1, Source: c.Source,
				ResumeKind: "sacrifice", ResumeSA: sa, ResumeTarget: targetIndex,
				Prompt: sacrificePrompt(true, 1)}
			name := "a permanent"
			if o.Face() != nil {
				name = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: 0,
				Kind: "sacrifice", Label: name, Obj: o.ID, Player: o.Controller})
			if Ask(h, d) == AskAsked {
				return
			}
			// No-host stand-in: the mandatory sacrifice the pre-ask engine
			// made, with the Note that records why the richer path did not run.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: o.Controller,
				Text: "sacrifices the first matching permanent(s) (no engine host to ask)", Secret: true})
		}
		rememberLKICapture(o.ID)
		sacrificed = append(sacrificed, o.ID)
		h.Emit(events.Sacrifice(o.ID))
	}
	if show && len(sacrificed) > 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller, IDs: sacrificed})
	}
}

// ParseDamageUnlessCost reports whether an UnlessCost$ value is the
// damage-payment offer form "DamageYou<N>" and returns N. Recognised: the
// exact spelling (case-insensitive) with a non-negative integer N; anything
// else is not the offer (and falls to the shared gate's pricing).
func ParseDamageUnlessCost(cost string) (int, bool) {
	_, n, ok := strings.Cut(strings.TrimSpace(cost), "DamageYou<")
	if !ok || !strings.HasSuffix(n, ">") {
		return 0, false
	}
	n = strings.TrimSuffix(n, ">")
	v, err := strconv.Atoi(n)
	// N must be a POSITIVE literal: DamageYou<0> (no damage) is not an offer
	// anyone could answer differently, so it fails closed to the ordinary
	// pricing path like every other non-offer spelling.
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// sacrificePrompt renders the player-targeted sacrifice ask's prompt.
func sacrificePrompt(optional bool, n int32) string {
	if optional {
		return "Choose up to " + strconv.Itoa(int(n)) + " permanent(s) to sacrifice, or none"
	}
	return "Choose " + strconv.Itoa(int(n)) + " permanent(s) to sacrifice"
}

// sacrificeAmount resolves Amount$ (default 1). Literals pass through; a
// non-literal resolves through the count evaluator (an SVar name, an inline
// Count$ expression, or {X}). The "X" shape deserves its own arm: on a
// triggered ability Ctx.X is the ability object's own X -- zero, a trigger
// was never paid an X -- so an Amount$ X on a permanent's trigger (Meathook
// Massacre II's "each player sacrifices X creatures") must read the paid X
// off the SOURCE permanent, which CastInfo carried out of the cast onto the
// battlefield object. When that cast X is also zero but an SVar named X
// exists, the evaluator resolves it: Dralnu, Lich Lord's replacement body
// carries SVar:X:ReplaceCount$DamageAmount, naming the replaced event's own
// amount rather than any paid X. An Amount$ that is none of literal, SVar,
// Count$, Sacrificed$ or X is an unknown shape and keeps the
// pre-Amount$-reading behaviour (1) rather than degrading to zero.
func sacrificeAmount(h Host, c *Ctx, sa *cards.SA) int32 {
	raw, ok := sa.Params["Amount"]
	if !ok {
		return 1
	}
	raw = strings.TrimSpace(raw)
	if n, err := strconv.Atoi(raw); err == nil {
		return int32(n)
	}
	if raw == "X" {
		if c.X != 0 {
			return c.X
		}
		if o := h.Game().Obj(c.Source); o != nil && o.X != 0 {
			return o.X
		}
		if c.SVars != nil {
			// Dralnu, Lich Lord: Amount$ X with SVar:X:ReplaceCount$DamageAmount
			// — the damage-replacement body's count is the amount of the event
			// being replaced, which main's replacement machinery carries in
			// Ctx.ReplacementAmount and EvalCount's ReplaceCount$ head reads.
			if body, has := c.SVars["X"]; has {
				if v := EvalCount(h, c, body); v != 0 {
					return v
				}
			}
		}
		return 0
	}
	known := strings.HasPrefix(raw, "Count$") || strings.HasPrefix(raw, "Sacrificed$") ||
		strings.HasPrefix(raw, "TriggerCount$") || strings.HasPrefix(raw, "TriggerCountMax$")
	if !known && c.SVars != nil {
		_, known = c.SVars[raw]
	}
	v := Num(h, c, sa, "Amount", 0)
	if !known && v == 0 {
		return 1 // unknown shape: today's fixed-one behaviour, not a silent zero
	}
	return v
}

// changeZoneChosenTargets serves effChangeZone's object path the targets of a
// ValidTgts$-declared targeting when no ask has offered them yet. The ok
// return is NOT "targets were found" -- it is "use the returned set INSTEAD of
// Defined's own fallthrough": ok=true with a nil set means the ask was posed
// and SUSPENDED the resolution (the caller must return before moving
// anything), and the answered re-entry consumes Ctx.Choice here. Every other
// shape returns false and the caller keeps Defined's own behaviour
// (placement-chosen targets, Defined$-named objects, the source default).
//
// The ask never fires when the resolution already carries targets (the
// placement ask's answered set) or when the SA also carries Defined$ (an
// already-named fetch list is Forge's no-ask shape). Bounds come from
// TargetMin$/TargetMax$ through the ordinary Num grammar, clamped to the
// eligible count; Min == Max == 0 or an empty eligible set is no ask and no
// move. A host that cannot ask takes the deterministic first-max stand-in
// (R-9), which is exactly what botpolicy's clamp fallback answers with.
func changeZoneChosenTargets(h Host, c *Ctx, sa *cards.SA) ([]state.Target, bool) {
	if _, targeted := sa.Params["ValidTgts"]; !targeted ||
		strings.TrimSpace(sa.Params["Defined"]) != "" {
		return nil, false
	}
	if c.SubPreAsk != nil {
		if ts, ok := c.SubPreAsk[sa.Line]; ok {
			return ts, true
		}
	}
	if c.TargetsOffered && (c.OfferedSA == nil || sa.Line == c.OfferedSA.Line) {
		// The announcement/placement ask offered THIS SA's targeting (rules
		// sets the marker exactly for the SA the ask covered, and OfferedSA
		// names it); the chosen-zero election must not be re-asked here. A
		// deeper sub's own targeting was never offered -- the same
		// mvts1 boundary chosenTargetsFor's OfferedSA check draws -- so it
		// falls through to its own ask below.
		return nil, false
	}
	if c.ChoiceDone {
		ans := c.Choice
		c.ChoiceDone, c.Choice = false, nil
		if TargetUniqueRequested(sa) {
			c.TargetsUnique = append(c.TargetsUnique, ans...)
		}
		return ans, true
	}
	if len(c.Targets) > 0 {
		// Inherit ONLY when the targets genuinely belong to THIS SA -- the
		// OfferedSA marker names exactly the SA the placement/announcement ask
		// covered (task spcz1; previously every sub that did not declare
		// TargetUnique$ True inherited, so a targeted root's SubAbility$
		// ChangeZone read the PARENT's targets through Defined's ValidTgts$
		// fallthrough and its own Origin$ filter rejected them into a silent
		// no-op: Cathartic Parting's and Put Away's graveyard "may shuffle"
		// clause never asked). A sub that DOES mean to reuse the parent's
		// target says so with TargetUnique$ True (Withdraw): the shared ask's
		// filter excludes the inherited parent target via TargetsAlreadyChosen,
		// so it asks for ANOTHER target instead of inheriting blindly.
		if c.OfferedSA != nil && sa.Line == c.OfferedSA.Line {
			return nil, false
		}
	}
	// Legality stays referenced to the ability controller; only the
	// decision's Player moves to the TargetingPlayer$ chooser (the same
	// resolver every rules-tier target ask uses).
	candidates := h.LegalTargets(c.Controller, c.Source, sa)
	chooser := h.ChooserFor(c, sa)
	if ch, posed := opponentPick(h, c, sa, chooser); posed {
		// The controller's which-opponent selection ask was posted: the walk
		// is suspended and re-enters this very SA, where the answered
		// selection makes ChooserFor return the chosen seat.
		return nil, true
	} else if !posed {
		chooser = ch
	}
	min := Num(h, c, sa, "TargetMin", 1)
	max := Num(h, c, sa, "TargetMax", 1)
	if max > int32(len(candidates)) {
		max = int32(len(candidates))
	}
	if min > max {
		min = max
	}
	if min < 0 {
		min = 0
	}
	if max <= 0 {
		// Nothing eligible (or an explicitly zero bound): no ask, no move.
		return nil, false
	}
	return poseTargetsAsk(h, c, sa, chooser, candidates, min, max, "choice")
}
