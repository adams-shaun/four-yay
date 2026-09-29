package effects

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// splitTrimList splits a comma-separated parameter value into trimmed,
// non-empty entries — the shared shape of KWChoice$'s candidate list.
func splitTrimList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func init() {
	Register("Tap", effTap)
	Register("TapAll", effTapAll)
	Register("UntapAll", effUntapAll)
	Register("Pump", effPump)
	Register("PumpAll", effPumpAll)
	Register("Animate", effAnimate)
	Register("AnimateAll", effAnimateAll)
	Register("Protection", effProtection)
	Register("RemoveFromCombat", effRemoveFromCombat)
}

// effRemoveFromCombat is Forge's RemoveFromCombatEffect (28 raw corpus lines:
// Reconnaissance's {0}, Hollowhenge Spirit's ETB, the Gustcloak cycle,
// Illusionist's Gambit): CR 506.4's "a spell or ability causes it to be
// removed from combat". Each resolved object that is on the battlefield gets
// one events.EndCombatReset{Obj: id} -- the exact event regeneration uses,
// whose nonzero-Obj case clears IsAttacking/BlockedBy and leaves zero
// tombstones in attackers' blocker lists (CR 509.1h: the attacker stays
// blocked) -- and nothing else: the target stays tapped, and untapping is the
// cards' own chained SubAbility (Reconnaissance's DBUntap). The target set is
// the ordinary Defined walk, which already covers every census form --
// Defined$ Self (11), Targeted (5), Enchanted (2), Remembered (1),
// TriggeredAttackerLKICopy (5), TriggeredBlockerLKICopy (1) -- and the
// source/ValidTgts/Valid fallbacks. RememberRemovedFromCombat$ True
// (Illusionist's Gambit) remembers each removed object in both halves -- the
// resolution's ctx set, so the chained `DB$ Untap | Defined$ Remembered`
// untaps exactly the removed set, and the source's event-backed list, so
// Card.IsRemembered reads it later. UnblockCreaturesBlockedOnlyBy$ (4 corpus
// lines) is NOT read: making the attackers a removed blocker was blocking
// become unblocked needs an operation no event currently expresses.
func effRemoveFromCombat(h Host, c *Ctx, sa *cards.SA) {
	remember := strings.EqualFold(strings.TrimSpace(sa.Params["RememberRemovedFromCombat"]), "True")
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		if o := h.Game().Obj(t.Obj); o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		h.Emit(events.Event{Kind: events.EndCombatReset, Obj: t.Obj})
		if remember {
			c.Remembered = append(c.Remembered, state.Target{Obj: t.Obj})
			eventRemember(h, c, t.Obj)
		}
	}
}

// effTapAll is Forge's TapAllEffect (78 raw corpus lines, 75 files): the
// battlefield walk is the DEFINED players' when the script names one (or
// targets), every living seat's otherwise; ValidCards$ filters it (default
// "Permanent"). RememberTapped$ is Forge's clear-then-add contract, exactly
// like SacrificeAll's RememberSacrificed$: the resolution's Remembered set
// is REPLACED by the cards this primitive tapped (Forge clears the host
// card's list before computing the victim list, then adds one entry per
// card in it -- tapped or not, every listed card is remembered).
// TapperController$ hands the tap provenance to each card's own controller
// (Forge's per-card tapper), the resolving controller otherwise.
func effTapAll(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	spec := sa.Params["ValidCards"]
	if spec == "" {
		spec = "Permanent"
	}
	remember := strings.EqualFold(sa.Params["RememberTapped"], "True")
	if remember {
		c.Remembered = nil
		clearEventRemembered(h, c)
	}
	tapper := c.Controller
	perCardTapper := strings.TrimSpace(sa.Params["TapperController"]) != ""
	players := allPlayersFor(h, c, sa)
	for _, p := range players {
		ids := append([]state.ObjID(nil), g.Zone(state.ZBattlefield, p)...)
		for _, id := range ids {
			if !MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				continue
			}
			if remember {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
			o := g.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield || o.Tapped {
				continue
			}
			tap := tapper
			if perCardTapper {
				tap = o.Controller
			}
			h.EmitTap(id, tap, false)
		}
	}
}

// effUntapAll is Forge's UntapAllEffect (126 raw corpus lines, 125 files):
// the same battlefield walk as effTapAll, filtered by ValidCards$ (Forge's
// default is no filter at all -- the whole battlefield -- so "Permanent" is
// the equivalent default here). RememberUntapped$ remembers ONLY the cards
// that actually untapped (Forge adds inside the untapped branch), and the
// resolution's Remembered set is extended, not replaced (UntapAll has no
// clear-remembered step). ControllerUntaps$ hands the per-card controller
// the untap provenance, the resolving controller otherwise.
func effUntapAll(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	spec := sa.Params["ValidCards"]
	if spec == "" {
		spec = "Permanent"
	}
	remember := strings.EqualFold(sa.Params["RememberUntapped"], "True")
	untapper := c.Controller
	perCardUntapper := strings.TrimSpace(sa.Params["ControllerUntaps"]) != ""
	players := allPlayersFor(h, c, sa)
	for _, p := range players {
		ids := append([]state.ObjID(nil), g.Zone(state.ZBattlefield, p)...)
		for _, id := range ids {
			if !MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				continue
			}
			o := g.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield || !o.Tapped {
				continue
			}
			untap := untapper
			if perCardUntapper {
				untap = o.Controller
			}
			h.Emit(events.Event{Kind: events.Untap, Obj: id, Player: untap})
			if remember {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
		}
	}
}

// allPlayersFor scopes an All primitive to its Defined$ players or (when it
// has targets but no explicit Defined$) its chosen player targets.  Forge's
// TargetRestrictions supplies ValidTgts$ as the latter form (Mana Short and
// Early Harvest); falling back to every battlefield is only correct when the
// SA has neither selector.
func allPlayersFor(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	g := h.Game()
	if strings.TrimSpace(sa.Params["Defined"]) == "" {
		if _, targeted := sa.Params["ValidTgts"]; !targeted {
			return g.AliveFrom(0)
		}
	}
	seen := map[state.PlayerID]bool{}
	var out []state.PlayerID
	for _, t := range Defined(h, c, sa) {
		p := PlayerOf(h, c, t)
		if int(p) < len(g.Players) && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// effTap taps each Defined$ permanent. The tapper is the resolving ability's
// controller unless Tapper$ names a player (Forge TapEffect). ETB$ True is the
// "enters tapped" replacement body (804 corpus files, every ETBTapped land):
// the permanent is given its entry state and does not become tapped (CR
// 603.2e), so EmitTap marks it and no Taps trigger runs.
func effTap(h Host, c *Ctx, sa *cards.SA) {
	tapper := c.Controller
	if spec := strings.TrimSpace(sa.Params["Tapper"]); spec != "" {
		if ps := definedPlayerIDs(h, c, spec); len(ps) > 0 {
			tapper = ps[0]
		}
	}
	entering := strings.EqualFold(sa.Params["ETB"], "True")
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield || o.Tapped {
			continue
		}
		h.EmitTap(o.ID, tapper, entering)
	}
}

// effPump, effPumpAll, effAnimate and effProtection are all continuous
// effects: they build a state.ContinuousEffect and hand it to the Host,
// which is rules.Engine.AddContinuous, so the CR 613 layer system Task 19
// built actually has a caller (Task 19c). Every one of them is a temporary
// grant from a resolving spell or ability, never a permanent's own printed
// static, so they always set UntilEOT: true -- the layer system's existing
// EndOfTurnCleanup already drops those on schedule, and Engine.active()
// never checks the source's battlefield presence for an UntilEOT effect
// (Giant Growth outlives the instant that cast it), so nothing else needs
// to change for these to expire correctly.
//
// Each one scopes its effect to exactly the object it targets with
// Affects: "Card.Self" and Source: <that object's ID> -- not the resolving
// ability's own source -- reusing the same Self-predicate pattern Task 19's
// own lord-effect tests already established (layers_test.go), rather than
// inventing a new filter form.
func effPump(h Host, c *Ctx, sa *cards.SA) {
	// NoteNumber$ (Lupine Harbingers' exile trigger: "note the number of
	// turns you've begun"): the body does not pump at all -- it notes the
	// evaluated number onto its source CARD through the events.NotedNumber
	// marker, which Count$NotedNumber reads at the later ETB (the corpus's
	// one carrier is exactly that shape: the exile trigger notes
	// Count$YourTurns, the ETB's SVar reads SVar$X/Minus.Y where Y is
	// Count$NotedNumber). Terminal: a NoteNumber body never also pumps, and
	// the read comes before any registration so a note is never a
	// half-applied pump.
	if note := strings.TrimSpace(sa.Params["NoteNumber"]); note != "" {
		n := Num(h, c, sa, "NoteNumber", 0)
		if c.Source != 0 {
			h.Emit(events.Event{Kind: events.NoteNumber, Obj: c.Source, Amount: n})
		}
		return
	}

	// ClearNotedCardsFor$ clears the requested player labels from its Defined$
	// player set. The event fold keeps a later resolution and a replay from
	// retaining a previous choice (Master of Ceremonies changes these labels
	// every upkeep).
	for _, label := range splitTrimList(sa.Params["ClearNotedCardsFor"]) {
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer && playerHasNote(h.Game(), t.Player, label) {
				h.Emit(events.Event{Kind: events.PlayerNoteCleared, Player: t.Player, Text: label})
			}
		}
	}

	// NoteCards$ <defined> + NoteCardsFor$ <label> (Forge's NoteCardsEffect):
	// the body records a notation that a later resolution reads back through
	// the shared filters. The player half (NoteCards$ Self, state.Player.Notes
	// and the `Player.NotedFor<label>` qualifier) is unchanged: corpus
	// carriers are Seize the Spotlight's fame/fortune branches, Master of
	// Ceremonies' money/friends/secrets, Wheel of Potential, Borderland
	// Explorer. The noted SEAT is the resolution's Defined set (a remembered
	// chooser, `Defined$ Player`, or `Defined$ Player.!IsRemembered`); Forge's
	// NoteCardsEffect notes the CURRENT player when Defined$ is absent, which
	// here is the resolving controller. The CARD half is the other corpus
	// family: `NoteCards$ Remembered` (Volatile Chimera, Arcane Savant, Caller
	// of the Untamed) notes the resolution's Remembered cards and
	// `NoteCards$ TriggeredSource` (Maelstrom Archangel Avatar) notes the
	// triggering source, both onto the noted CARD through events.CardNoted,
	// for the later `Card.NotedFor<label>` reads at ChooseCard's Choices$, DB$
	// Play's Valid$ and RepeatEach's RepeatCards$. CopyPermanent's
	// RevealFromExile cost is an evidenced corpus shape but remains unsupported.
	// The note lands through its own event so a
	// log-only replay rebuilds state.Object.Notes exactly; the pump body then
	// runs unchanged (a `Defined$ Remembered` chooser is a player entry,
	// skipped by the object walk below). Any other NoteCards$ form stays
	// loud-unimplemented (transcript note, no state write).
	if label := strings.TrimSpace(sa.Params["NoteCardsFor"]); label != "" {
		switch strings.TrimSpace(sa.Params["NoteCards"]) {
		case "Self":
			spec := strings.TrimSpace(sa.Params["Defined"])
			noted := false
			for _, t := range Defined(h, c, sa) {
				if !t.IsPlayer {
					continue
				}
				h.Emit(events.Event{Kind: events.PlayerNoted, Player: t.Player, Text: label})
				noted = true
			}
			if !noted && spec == "" {
				h.Emit(events.Event{Kind: events.PlayerNoted, Player: c.Controller, Text: label})
			}
		case "Remembered":
			for _, t := range resolvedRemembered(h, c) {
				if t.IsPlayer || t.Obj == 0 {
					continue
				}
				h.Emit(events.Event{Kind: events.CardNoted, Obj: t.Obj, Text: label})
			}
		case "TriggeredSource":
			if c.TriggerSource != 0 {
				h.Emit(events.Event{Kind: events.CardNoted, Obj: c.TriggerSource, Text: label})
			}
		default:
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "unimplemented NoteCards$ " + strings.TrimSpace(sa.Params["NoteCards"])})
		}
	}

	// Secondary$ True (Amonkhet Raceway's max-speed AddAbility$ grant marks
	// the granted pump with it): Forge CardFactoryUtil sets the key on
	// machine-derived abilities, and Card.java's ability-text renderer skips
	// secondary spell abilities -- a presentation and deck-tooling filter,
	// never a rules tail. The engine delivers a granted pump through the
	// AddAbility static grant structurally (the static is the grantor; the
	// SA is not a printed line), so the recognition has no behavioural half
	// here; the read keeps the parameter census honest.
	_ = sa.Params["Secondary"]
	// KWChoice$ (30 corpus files: Angelic Skirmisher's "choose first strike,
	// vigilance or lifelink" trigger, the equipment/ally "gains your choice
	// of ..." family): the pump's keyword grant is not a fixed list but a
	// player's choice from a fixed candidate list, chosen ONE per execution
	// (every corpus line reads "your choice of X, Y or Z"). The ask is the
	// same mid-resolution KModes vocabulary effCharm uses — ResumeKind
	// "modes" with ResumeSA, the answer re-entering this effect through
	// rules' resumeResolution with Ctx.Modes set to the chosen labels. The
	// ask comes FIRST, before any registration, so a suspension never leaves
	// a half-applied pump behind; on re-entry the whole effect re-runs with
	// the answer in hand (the charm pattern).
	var chosenKW []string
	if kwList := strings.TrimSpace(sa.Params["KWChoice"]); kwList != "" {
		if c.Modes != nil {
			// fx42 scoping: consume the answer once; a nested KWChoice pump
			// reached below poses its own ask.
			chosenKW = c.Modes
			c.Modes = nil
		} else {
			choices := splitTrimList(kwList)
			d := &decision.Decision{Player: c.Controller, Kind: decision.KModes,
				Min: 1, Max: 1, Source: c.Source,
				ResumeKind: "modes", ResumeSA: sa,
				Prompt: "Choose a keyword"}
			for i, name := range choices {
				d.Options = append(d.Options, decision.Option{
					Index: i, Kind: "mode", Label: name, Obj: c.Source, Player: c.Controller})
			}
			if Ask(h, d) == AskAsked {
				return
			}
			// No engine host (R-9): the deterministic first candidate, with
			// the Note that records why the richer path did not run.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "chose its first keyword (no engine host to ask)"})
			chosenKW = choices[:1]
		}
	}
	zone := strings.TrimSpace(sa.Params["PumpZone"])
	var ateotIDs []state.ObjID
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil {
			continue
		}
		// PumpZone$ (Snapcaster Mage's "Flashback until end of turn" grant
		// lives in the GRAVEYARD): the pump applies only while the object is
		// in the named zone(s) — ParseZones accepts a comma list and All —
		// and the registered continuous effect carries the same AffectedZone
		// scope, so Derived grants the keywords exactly there and nowhere
		// else. Without the parameter the historic battlefield-only guard
		// stands. P/T and keyword grants share one zone scope: a PumpZone$
		// pump of a battlefield creature is unchanged behaviour.
		if zone != "" {
			zones, all, ok := ParseZones(zone)
			if !ok {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "PumpZone$ " + zone + " is not a zone list this engine can ask; the pump is skipped"})
				continue
			}
			if !all && !slices.Contains(zones, o.Zone) {
				continue
			}
		} else if o.Zone != state.ZBattlefield {
			continue
		}
		// RememberTargets$ True (Bile Blight): the CHOSEN TARGETS join the
		// ability's Remembered, in both halves -- the ctx list the chained
		// sub-ability reads (DBPumpAll's ValidCards$ Remembered.sameName+
		// Other+Creature) and the source's event-backed persistent list. The
		// object must actually be on the battlefield to be pumped, and only a
		// pumped target is remembered, so the Remembered set names exactly
		// what the spell acted on.
		if strings.EqualFold(strings.TrimSpace(sa.Params["RememberTargets"]), "True") {
			c.Remembered = append(c.Remembered, t)
			eventRemember(h, c, t.Obj)
		}
		// RememberPumped$ True remembers the objects this Pump actually
		// affects, rather than merely the chosen targets: an off-zone target
		// skipped above is not added to either remembered set. The ctx half is
		// available to chained sub-abilities; eventRemember persists the
		// source's list for later Card.IsRemembered filters and replay.
		if strings.EqualFold(strings.TrimSpace(sa.Params["RememberPumped"]), "True") {
			c.Remembered = append(c.Remembered, t)
			eventRemember(h, c, t.Obj)
		}
		// Resolve amounts PER OBJECT: Double reads this object's own
		// layer-derived power/toughness, so a multi-object Defined$ must not
		// apply one creature's stat to every creature.
		att := NumForObject(h, c, sa, "NumAtt", 0, o.ID)
		def := NumForObject(h, c, sa, "NumDef", 0, o.ID)
		registerPumpEffects(h, c, o.ID, att, def, false, false, sa, zone, chosenKW)
		if atEOTInclude(h, c, sa, o.ID) {
			ateotIDs = append(ateotIDs, o.ID)
		}
	}
	scheduleAtEOT(h, c, sa, ateotIDs)
	// ForgetImprinted$ names (in the Defined$ grammar) the imprinted card(s)
	// to forget (Chrome Mox's DBForget: the exiled card left exile): each is
	// removed from the source's persistent Imprinted list. Forge's
	// forgetImprinted on Pump -- the o.Imprinted half is NOT auto-pruned on
	// move (only exiledCards is), so without this read a returning Chrome
	// Mox would read a stale imprint.
	if spec := strings.TrimSpace(sa.Params["ForgetImprinted"]); spec != "" {
		sub := *sa
		sub.Params = map[string]string{"Defined": spec}
		var ids []state.ObjID
		for _, t := range Defined(h, c, &sub) {
			if !t.IsPlayer {
				ids = append(ids, t.Obj)
			}
		}
		if len(ids) > 0 && c.Source != 0 {
			h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: ids, Text: "forget"})
		}
	}
}

// effPumpAll bakes in the affected set at resolution time (CR 611.2c: such an
// effect applies only to the permanents matching the filter when the ability
// resolves, not to ones that start matching later), so it registers one
// continuous effect per matching object -- found by walking AliveFrom(0)'s
// fixed APNAP seat order and each seat's battlefield zone slice in its
// existing registration order, never a map -- rather than one shared
// filter-based effect that Derived would re-evaluate against the battlefield
// forever.
func effPumpAll(h Host, c *Ctx, sa *cards.SA) {
	spec := sa.Params["ValidCards"]
	if spec == "" {
		spec = "Creature"
	}
	// PumpZone$ (PumpAll's graveyard-grant family, e.g. TrigFlashback's
	// "each instant and sorcery card in your graveyard gains flashback"):
	// the walk covers the named zones instead of the battlefield, and the
	// registered effects carry the AffectedZone scope so the grant applies
	// only while the card sits there.
	zone := strings.TrimSpace(sa.Params["PumpZone"])
	g := h.Game()
	var ateotIDs []state.ObjID
	for si, p := range g.AliveFrom(0) {
		if zone != "" {
			zones, all, ok := ParseZones(zone)
			if !ok {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "PumpZone$ " + zone + " is not a zone list this engine can ask; the pump is skipped"})
				break
			}
			if all {
				// "All": every public game zone plus the owner-private ones
				// g.Zone covers; ZCeased has no membership list (see
				// state/ids.go) so it is skipped. ZStack is included: a
				// spell-object pump (the "PumpZone$ Stack" shape) is a real
				// grant.
				zones = []state.Zone{state.ZLibrary, state.ZHand, state.ZBattlefield,
					state.ZGraveyard, state.ZExile, state.ZStack, state.ZCommand}
			}
			for _, z := range zones {
				// g.Zone(ZStack, p) is the SHARED stack, identical for
				// every seat (state/game.go Zone), so only the first alive
				// seat scans it -- otherwise an N-seat table registers the
				// same pump grant N times and schedules N end-of-turn
				// expiries. The convention is rules/statics.go and
				// effects/count.go's Count$ValidStack guard. Every other
				// zone is per-player and keeps its ordered per-seat walk.
				if z == state.ZStack && si > 0 {
					continue
				}
				for _, id := range g.Zone(z, p) {
					if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
						if strings.EqualFold(strings.TrimSpace(sa.Params["RememberPumped"]), "True") {
							c.Remembered = append(c.Remembered, state.Target{Obj: id})
							eventRemember(h, c, id)
						}
						att := NumForObject(h, c, sa, "NumAtt", 0, id)
						def := NumForObject(h, c, sa, "NumDef", 0, id)
						registerPumpEffects(h, c, id, att, def, false, false, sa, zone, nil)
						ateotIDs = append(ateotIDs, id)
					}
				}
			}
			continue
		}
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				if strings.EqualFold(strings.TrimSpace(sa.Params["RememberPumped"]), "True") {
					c.Remembered = append(c.Remembered, state.Target{Obj: id})
					eventRemember(h, c, id)
				}
				att := NumForObject(h, c, sa, "NumAtt", 0, id)
				def := NumForObject(h, c, sa, "NumDef", 0, id)
				registerPumpEffects(h, c, id, att, def, false, false, sa, "", nil)
				ateotIDs = append(ateotIDs, id)
			}
		}
	}
	scheduleAtEOT(h, c, sa, ateotIDs)
}

// durationTiming maps a Pump/PumpAll Duration$ value to its expiry: an
// explicitly indefinite shape ("Permanent", CR 611.2a) becomes a Permanent
// effect that outlives its source and every cleanup; "UntilEndOfCombat"
// (CR 511.2) becomes a combat-phase-scoped effect, dropped when the
// end-of-combat step ends; every other value ("", "UntilEndOfTurn", ...)
// keeps the historic UntilEOT default, dropped by end-of-turn cleanup.
// The one-sentence rule: a resolution-created one-shot pump honours the
// duration its script declared instead of being forced to end of turn.
func durationTiming(dur string) (permanent bool, untilEOT bool) {
	switch strings.ToLower(strings.TrimSpace(dur)) {
	case "permanent":
		return true, false
	case "untilendofcombat":
		return false, false
	default:
		return false, true
	}
}

// registerPumpEffects is Pump and PumpAll's sole per-object registration
// path. It also owns their sole KW$ read, so neither effect can reimplement
// Forge's keyword-list grammar: both always use cards.SplitKeywordList. It
// creates a layer-7c modification for a nonzero stat change and a separate
// layer-6 grant for any keywords, since Derived applies each layer
// independently. Skipping a zero/empty half avoids polluting
// Engine.continuous with an effect that would never do anything. zone is the
// caller's PumpZone$ value ("" for the default battlefield-only scope) and
// chosenKW the answered KWChoice$ candidates — extra keyword grants riding
// the same layer-6 registration.
//
// LeaveBattlefield$ (Moira and Teshar, Dreams of the Dead on the DB$ Pump
// site) is read here so effPump and effPumpAll cannot drift: the promise
// registers through the shared registerLeaveExile helper and the rider's
// move-driven lifetime rides BOTH halves below -- the one per-object
// structural home, exactly as the Animate site registers through
// registerAnimateEffects.
// pumpStatAmount keeps Double as an operation until layer 7c, where the
// recipient's current characteristic at this effect's timestamp is available.
func pumpStatAmount(h Host, c *Ctx, sa *cards.SA, key string) (int32, bool) {
	if strings.EqualFold(strings.TrimSpace(sa.Params[key]), "Double") {
		return 0, true
	}
	return Num(h, c, sa, key, 0), false
}

func registerPumpEffects(h Host, c *Ctx, id state.ObjID, att, def int32, doublePower, doubleToughness bool, sa *cards.SA, zone string, chosenKW []string) {
	kws := cards.SplitKeywordList(sa.Params["KW"])
	kws = append(kws, chosenKW...)
	// Suspend is unusual among keyword grants: its target is commonly an
	// exiled card, and the later upkeep/cast/filter machinery needs a replayed
	// provenance bit rather than only a transient layer effect. The event is
	// emitted only for the actual Exile scope; ordinary battlefield keyword
	// pumps must not make a card suspendable.
	grantSuspend := false
	for _, kw := range kws {
		if strings.EqualFold(cards.KeywordHead(kw), "Suspend") {
			grantSuspend = true
			break
		}
	}
	if strings.EqualFold(strings.TrimSpace(zone), "Exile") && grantSuspend {
		if o := h.Game().Obj(id); o != nil && o.Zone == state.ZExile && !o.SuspendGranted {
			h.Emit(events.Event{Kind: events.AlterAttribute, Obj: id, Text: "Suspend", Amount: 1})
		}
	}
	permanent, untilEOT := durationTiming(sa.Params["Duration"])
	// The move-driven lifetime of a Duration$ Permanent pump: when the pumped
	// object leaves the zone it was pumped in, the grant ends (CR 400.7 -- it
	// is a new object on return), via the same ExileOnMoved$/Remembered pair
	// effectMoveSweep reads. Non-permanent durations keep their existing
	// cleanup/combat lifetimes and take no sweep.
	//
	// A LeaveBattlefield$ Exile rider overrides with its own pair
	// (leaveExileLifetime): the grant ends exactly on the object's
	// battlefield departure -- the departure the rider's own promise
	// rewrites -- so a `Duration$ Permanent` grant cannot re-apply to the
	// card when it later re-enters (CR 400.7).
	var exileOn string
	var remembered []state.ObjID
	if lr, lw := leaveExileLifetime(id, sa.Params["LeaveBattlefield"]); lw != "" {
		exileOn, remembered = lw, lr
	} else if permanent {
		if o := h.Game().Obj(id); o != nil {
			if w := ZoneWord(o.Zone); w != "" {
				exileOn = w
				remembered = []state.ObjID{id}
			}
		}
	}
	if att != 0 || def != 0 || doublePower || doubleToughness {
		h.AddContinuous(state.ContinuousEffect{
			Source: id, Affects: "Card.Self", Controller: c.Controller,
			Layer: state.LPT, Sub: state.SubModify,
			AddPower: att, AddToughness: def,
			DoublePower: doublePower, DoubleToughness: doubleToughness,
			Duration: sa.Params["Duration"], Permanent: permanent, UntilEOT: untilEOT,
			ExileOnMoved: exileOn, Remembered: remembered,
			AffectedZone: zone,
		})
	}
	if len(kws) > 0 {
		h.AddContinuous(state.ContinuousEffect{
			Source: id, Affects: "Card.Self", Controller: c.Controller,
			Layer: state.LAbilities, AddKeywords: kws,
			Duration: sa.Params["Duration"], Permanent: permanent, UntilEOT: untilEOT,
			ExileOnMoved: exileOn, Remembered: remembered,
			AffectedZone: zone,
		})
	}
	// The LeaveBattlefield$ Exile promise (Moira and Teshar, Dreams of the
	// Dead; effects/leavebattlefield.go): it rides the pumped object for the
	// granting body's own duration, one registration per object -- the same
	// helper the Animate site uses, so an unsupported rider value takes that
	// helper's loud-Note behaviour.
	registerLeaveExile(h, c, id, sa.Params["LeaveBattlefield"], sa.Params["Duration"], permanent)
}

// effAnimate does not require the target to already be on the battlefield --
// Forge's own Animate targets a creature card in a graveyard as often as a
// permanent -- so unlike Pump it has no zone guard beyond "the object still
// exists". Setting base P/T is layer 7b (SubSet), which is why it uses
// SetPower/SetToughness/HasSet rather than Pump's AddPower/AddToughness: a
// later set must overwrite an earlier one regardless of timestamp order
// (TestSetBeforeModifyRegardlessOfTimestamp already locks that in), where an
// Add would incorrectly stack.
//
// The P/T effect is only registered when Power$ or Toughness$ is actually
// present: roughly two thirds of the corpus's own DB$ Animate calls (e.g.
// Kitesail Larcenist, Kami of Industry) use it purely to grant a type or
// keyword change and never mention Power$/Toughness$ at all. Num's zero
// default would otherwise turn every one of those into "becomes a 0/0",
// silently killing the very creature the card was granting an ability to.
func effAnimate(h Host, c *Ctx, sa *cards.SA) {
	ag := parseAnimateGrant(h, c, sa)
	emitAnimateColorsNotes(h, c, ag, "Animate")
	emitAnimateTriggersNotes(h, c, ag, "Animate")
	// RememberAnimated$ True (Rise and Shine): every permanent this Animate
	// affected joins the ability's Remembered, both halves -- the ctx list
	// the chained SubAbility reads (DBPutCounter's Defined$ Remembered) and
	// the source's event-backed persistent list -- the same two-half
	// discipline effPumpAll's RememberTargets$ applies (eventRemember
	// self-gates on a source-less ctx).
	rememberAnimated := strings.EqualFold(strings.TrimSpace(sa.Params["RememberAnimated"]), "True")
	var ateotIDs []state.ObjID
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil {
			continue
		}
		if rememberAnimated {
			c.Remembered = append(c.Remembered, t)
			eventRemember(h, c, o.ID)
		}
		registerAnimateEffects(h, c, o.ID, ag)
		if atEOTInclude(h, c, sa, o.ID) {
			ateotIDs = append(ateotIDs, o.ID)
		}
	}
	scheduleAtEOT(h, c, sa, ateotIDs)
}

// animateGrant is the per-object payload Animate and AnimateAll share: their
// Forge bodies read the identical parameter set and differ only in the
// affected-set selector (Defined$/chosen targets vs the ValidCards$ sweep),
// so one parser and one per-object registration path serve both and the two
// primitives can never drift.
type animateGrant struct {
	pw, tf              int32
	hasPower, hasTough  bool
	types               []string
	removeCreatureTypes bool
	removeTypes         []string
	allCreatureTypes    bool
	removeCardTypes     bool
	// name is the Name$ rider: the animated object's derived NAME for the
	// animation's own lifetime (CR 613.1d, layer 3). It is registered as a
	// real layer-3 SetName ContinuousEffect -- the same characteristic the
	// printed SetName$ static registers and rules' layer walk folds into
	// Derived.Name -- never a display-only alias, so every reader of the
	// derived name (view, name filters through the rename table, a copy
	// effect that reads the animated object's derived name) sees it. Empty
	// when the SA carries no Name$.
	//
	// CR 706.2: a rename is NOT a copiable value, so a copy effect that
	// copies this object copies its PRINTED face's name, exactly as the
	// printed SetName$ static behaves. What "derived" buys here is that the
	// rename is the same characteristic every layer-3 reader consults, not
	// that it transfers through a copy.
	name            string
	colorsRaw       string
	colors          []string
	overwriteColors bool
	colorsGrant     bool
	kws             []string
	// hiddenKws is the HiddenKeywords$ read: Forge's "derived keyword text"
	// grant (Opportunistic Dragon's `CARDNAME can't attack or block.`,
	// Elemental Uprising's `CARDNAME must be blocked if able.`). Forge keeps
	// the value out of the card's displayed keyword line, but it is a real
	// layer-6 keyword grant, so it joins ag.kws in the SAME AddKeywords list
	// and the SAME ContinuousEffect as Keywords$/RemoveAllAbilities$ (see
	// registerAnimateEffects). Kept as its own field so AnimateAll can leave
	// it unread, exactly as the two layer-6 siblings above.
	hiddenKws []string
	abilities []string
	duration  string
	permanent bool
	// perpetual is Duration$ Perpetual (the Alchemy "perpetually" family:
	// Chronicler of Worship's "It perpetually gains ...", Absorb Energy's
	// "... perpetually gain ..."): the animation lasts for the rest of the
	// game and follows the object through every zone change, so it is
	// registered Permanent with NO move-driven sweep (a Duration$ Permanent
	// animation instead ends when its object leaves the zone it was granted
	// in). See registerAnimateEffects.
	perpetual bool
	zone      string
	// endOnLeave ends the grant the moment the animated object leaves the
	// battlefield, regardless of Duration$. registerAnimateEffects expresses
	// it through the existing move-driven lifetime (ExileOnMoved$ + the
	// object's own id in Remembered, effectMoveSweep's end-the-effect
	// clause), so a Duration$ Permanent animation that must NOT survive a
	// zone round-trip -- api:Earthbend's "becomes a 0/0 creature ... When it
	// dies or is exiled, return it to the battlefield tapped"; the returned
	// land is a plain land -- is built on the same path as Stalking
	// Stones's genuinely forever grant, never a second animator.
	endOnLeave bool
	// triggers is the Triggers$ grant: each named SVar body parsed into the
	// same cards.Trigger shape a printed T: line would have; the animated
	// object gains it for the animation's own lifetime.
	triggers []cards.Trigger
	// triggerGrantor is the ANIMATING source (Ctx.Source): the T:-shaped
	// body lives on its face's SVar table, which for the cross-object shape
	// (Dragon Cursed Halls animating a target creature) is not the animated
	// object's own table.
	triggerGrantor state.ObjID
	// triggersUnread holds the Triggers$ names whose body the parser refused
	// (missing SVar, or no Mode$ — an ability body, not a trigger); one loud
	// note each, emitted by emitAnimateTriggersNotes.
	triggersUnread []string
	// leaveExile is the raw LeaveBattlefield$ value (Whip of Erebos's
	// "If it would leave the battlefield, exile it instead of putting it
	// anywhere else"): only "Exile" is implemented, per object in
	// registerAnimateEffects (effects/leavebattlefield.go).
	leaveExile string
	// svars names the sVars$ SVars the animated object gains for the
	// animation's own lifetime (WhipMustAttack, KheruMustAttack,
	// MustBeBlocked, ...), resolved from THIS face's table at grant time.
	svars []string
	// staticAbilities names SVar Mode$ bodies the animated object gains for
	// the animation's own lifetime (for example Stilt-Man's CantSacrifice).
	staticAbilities []string
	// removeKeywords is the RemoveKeywords$ read (state.ContinuousEffect
	// .RemoveKeywords, the CopyPermanent site's mechanism): keyword entries
	// matched by head (cards.KeywordHead) that the animated object LOSES at
	// layer 6 BEFORE this same grant's Keywords$ apply -- Animate Dead's
	// "it loses 'enchant creature card in a graveyard'". AnimateAll keeps
	// the parameter unread: effAnimateAll clears the field and
	// animateAllUnreadNote still names it.
	removeKeywords []string
	// removeAbilities is the RemoveAllAbilities$ read (state.ContinuousEffect
	// .RemoveAbilities, the same layer-6 strip Humility's static and
	// CopyPermanent use): when set the animation clears the object's printed
	// (and any earlier-granted) keywords at layer 6 before its own Keywords$
	// apply -- the control-theft rider on Opportunistic Dragon ("gain control
	// of that permanent, it loses all abilities"). AnimateAll keeps the
	// parameter unread: effAnimateAll clears the field and
	// animateAllUnreadNote still names it.
	removeAbilities bool
	// replacements names the Replacements$ SVars the animated object gains
	// for the animation's own lifetime: each is an R:-shaped body on THIS
	// face's table (Spirit-Sister's Call's ReplaceLeaves: "If this permanent
	// would leave the battlefield, exile it instead"). registerAnimateEffects
	// resolves each into an Effect-created replacement (effects'
	// registerAnimateReplacements). AnimateAll keeps the parameter unread:
	// it clears the field, and animateAllUnreadNote names it.
	replacements []string
}

// parseAnimateGrant reads the shared Animate/AnimateAll parameter set. See
// effAnimate's doc for the layer assignment (base P/T = 7b SubSet, types = 4,
// colours = 5, keywords/abilities = 6) and for the no-P/T guard; Name$ is the
// layer-3 rename.
func parseAnimateGrant(h Host, c *Ctx, sa *cards.SA) animateGrant {
	ag := animateGrant{
		duration:  sa.Params["Duration"],
		colorsRaw: strings.TrimSpace(sa.Params["Colors"]),
		zone:      strings.TrimSpace(sa.Params["Zone"]),
	}
	// Name$ is a literal replacement name (The Curse of Fenric's "named
	// Fenric", Awakening of Vitu-Ghazi's "named Vitu-Ghazi"); a value the
	// corpus writes as a chooser token is not a rename this parser can
	// resolve, so it fails closed to no rename rather than overwriting the
	// object's name with a literal token word. Trimmed, and compared
	// case-insensitively against the two chooser spellings so a stray
	// "ChosenName" cannot leak onto the board as a name.
	if raw := strings.TrimSpace(sa.Params["Name"]); raw != "" &&
		!strings.EqualFold(raw, "ChosenName") && !strings.EqualFold(raw, "Chosen") {
		ag.name = raw
	}
	_, ag.hasPower = sa.Params["Power"]
	_, ag.hasTough = sa.Params["Toughness"]
	ag.pw = Num(h, c, sa, "Power", 0)
	ag.tf = Num(h, c, sa, "Toughness", 0)
	ag.types = strings.Fields(strings.ReplaceAll(sa.Params["Types"], ",", " "))
	// Colors$ names the colour set the animated object carries; with
	// OverwriteColors$ True it REPLACES the object's colours (the manland
	// family -- Celestial Colonnade's "white and blue" -- where the land's
	// printed colourlessness must not survive), without it the colours are
	// ADDED. Both are layer-5 grants, normalised to WUBRG letters here so
	// "All" (every colour) and "Colorless" (an overwrite to the empty set)
	// never leak their words downstream. A value colorLetters cannot fully
	// parse (the corpus's "ChosenColor" family, which asks its controller for
	// a colour) fails closed: colorsGrant is false, the grant is NOT
	// registered and a Note says so, so the object keeps its printed colours
	// instead of the parse's empty prefix being overwritten over them. For
	// the same reason "Colorless" without OverwriteColors$ -- an add of the
	// empty set, a no-op whose corpus lines (raging_spirit) intend "becomes
	// colourless" -- is noted and skipped rather than silently registering a
	// dead effect.
	colors, colorsOK := colorLetters(sa.Params["Colors"])
	ag.colors = colors
	ag.overwriteColors = ag.colorsRaw != "" && strings.EqualFold(strings.TrimSpace(sa.Params["OverwriteColors"]), "True")
	ag.colorsGrant = ag.colorsRaw != "" && colorsOK && (len(colors) > 0 || ag.overwriteColors)
	// Keywords$ is a "&"-separated keyword list (Celestial Colonnade's
	// "Flying & Vigilance"), the same grammar Pump's KW$ uses.
	ag.kws = cards.SplitKeywordList(sa.Params["Keywords"])
	// HiddenKeywords$ is the SAME derived-keyword grammar (see
	// animateGrant.hiddenKws): a HiddenKeywords$ value is one keyword line
	// Forge simply does not print on the card, and rules' derived-keyword
	// readers (hasCantBlockKeyword/hasCantAttackKeyword/
	// hasMustBeBlockedKeyword) consult the derived list alike.
	ag.hiddenKws = cards.SplitKeywordList(sa.Params["HiddenKeywords"])
	// RemoveKeywords$ (see animateGrant.removeKeywords): split with the same
	// grammar, applied at layer 6 BEFORE this effect's own AddKeywords
	// (rules' LAbilities walk), so one DB$ Animate both strips the old
	// enchant and grants the new one in the same pass.
	ag.removeKeywords = cards.SplitKeywordList(sa.Params["RemoveKeywords"])
	// RemoveCreatureTypes$ True strips the object's creature-type subtypes
	// (Mishra's Factory's land base carries none, but an animated creature or
	// planeswalker face does) before this animation's own Types$ apply.
	ag.removeCreatureTypes = strings.EqualFold(strings.TrimSpace(sa.Params["RemoveCreatureTypes"]), "True")
	// AddAllCreatureTypes$ True (Mutavault's "all creature types"): the
	// same LType emission rides the flag, never a materialised type list --
	// rules' typeCharacteristics appends the CreatureTypeWords vocabulary
	// for affected objects (see state.ContinuousEffect.AddAllCreatureTypes).
	ag.allCreatureTypes = strings.EqualFold(strings.TrimSpace(sa.Params["AddAllCreatureTypes"]), "True")
	// RemoveTypes$ names card types, supertypes or subtypes to strip before
	// this animation's Types$ apply (Weeping Angel removes Creature).
	for part := range strings.SplitSeq(sa.Params["RemoveTypes"], ",") {
		ag.removeTypes = append(ag.removeTypes, strings.Fields(part)...)
	}
	// RemoveCardTypes$ True (state.ContinuousEffect.RemoveCardTypes, the
	// Darksteel Mutation strip) keeps only the object's supertypes in the
	// layer-4 walk -- one line on the shared path, so both primitives read it.
	ag.removeCardTypes = strings.EqualFold(strings.TrimSpace(sa.Params["RemoveCardTypes"]), "True")
	// Abilities$ names the SVar bodies (comma-separated, on THIS face's table)
	// the animated object gains -- Urza's Saga's chapters ("CARDNAME gains
	// '{T}: Add {C}'.") are the corpus's flagship shape. The grant is a
	// layer-6 ability grant (CR 613.1f): rules' grantedAbilities resolves the
	// names back through the SOURCE face's SVar table, so the name travels,
	// never a parsed copy. Duration$ Permanent makes the grant last while the
	// object is on the battlefield (the source-presence lifetime, which is
	// also what the object's own text obeys); any other Duration -- the
	// corpus's animate-a-land-for-a-turn lines -- keeps the ordinary
	// until-end-of-turn lifetime.
	for nm := range strings.SplitSeq(sa.Params["Abilities"], ",") {
		if nm = strings.TrimSpace(nm); nm != "" {
			ag.abilities = append(ag.abilities, nm)
		}
	}
	// Triggers$ names (comma-separated) SVars on THIS face's table whose
	// bodies are T:-shaped triggers the animated object gains for the
	// animation's own lifetime (Raging Ravine's "Whenever this creature
	// attacks, put a +1/+1 counter on it"). cards.ParseTriggerLine gives the
	// body the same shape a printed T: line would have; rules' granted-trigger
	// walk (checkGrantedStaticTriggers, the AddTrigger$ static-grant
	// precedent) matches it like any other trigger and links its Execute$
	// from the ANIMATING face's own SVar table (triggerGrantor -- the table
	// events.Apply's GrantTriggerPush resolves from, so the live queue and a
	// replayed one mint the same stack object). A name whose body is missing
	// or carries no Mode$ fails closed under one loud note per name
	// (triggersUnread), never a silently inert half.
	ag.triggerGrantor = svarTableOwner(h, c)
	for nm := range strings.SplitSeq(sa.Params["Triggers"], ",") {
		if nm = strings.TrimSpace(nm); nm == "" {
			continue
		}
		t, ok := cards.ParseTriggerLine(c.SVars[nm])
		if !ok {
			ag.triggersUnread = append(ag.triggersUnread, nm)
			continue
		}
		ag.triggers = append(ag.triggers, t)
	}
	ag.permanent = strings.EqualFold(strings.TrimSpace(sa.Params["Duration"]), "Permanent")
	ag.perpetual = isPerpetualDuration(sa.Params["Duration"])
	// RemoveAllAbilities$ True (state.ContinuousEffect.RemoveAbilities): the
	// layer-6 ability strip the static Humility carries, delivered here by
	// the Animate-param path. See animateGrant.removeAbilities.
	ag.removeAbilities = strings.EqualFold(strings.TrimSpace(sa.Params["RemoveAllAbilities"]), "True")
	ag.leaveExile = strings.TrimSpace(sa.Params["LeaveBattlefield"])
	// Replacements$ names (comma-separated) SVars on THIS face's table whose
	// bodies are R:-shaped replacements the animated object gains for the
	// animation's own lifetime (Spirit-Sister's Call's ReplaceLeaves). Each
	// is resolved at registration time under this face's SVar context, so the
	// named body -- not a parsed copy -- travels (effects'
	// registerAnimateReplacements). AnimateAll clears the field.
	for nm := range strings.SplitSeq(sa.Params["Replacements"], ",") {
		if nm = strings.TrimSpace(nm); nm != "" {
			ag.replacements = append(ag.replacements, nm)
		}
	}
	for nm := range strings.SplitSeq(sa.Params["sVars"], ",") {
		if nm = strings.TrimSpace(nm); nm != "" {
			ag.svars = append(ag.svars, nm)
		}
	}
	// Forge uses the lower-case spelling on Animate bodies. Accept the
	// canonical spelling too so parser-produced and hand-authored SAs agree.
	for _, raw := range []string{sa.Params["staticAbilities"], sa.Params["StaticAbilities"]} {
		for _, nm := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t' || r == '\n'
		}) {
			if nm != "" {
				ag.staticAbilities = append(ag.staticAbilities, nm)
			}
		}
	}
	return ag
}

// animateHostScoped reports a Duration$ value whose grant lasts only while a
// HOST permanent remains on the battlefield, so the host presence cannot come
// from the ordinary source-leaves check (the effect's Source is the ANIMATED
// object, Affects Card.Self); registerAnimateEffects anchors it through
// ContinuousEffect.DurationSource, the Exchange of Words mechanism rules'
// continuousLive already honours. The host is the ANIMATING source for both
// spellings:
//
//   - UntilHostLeavesPlay -- the host the card text names (Opportunistic
//     Dragon's "For as long as CARDNAME remains on the battlefield, gain
//     control of that permanent, it loses all abilities").
//   - AsLongAsInPlay -- Skilled Animator's "for as long as CARDNAME remains on
//     the battlefield": the animating PERMANENT is the host, not the animated
//     target. Reading the target's own presence instead would leave the 5/5
//     alive after the Animator is gone (the defect this predicate exists to
//     prevent).
func animateHostScoped(dur string) bool {
	switch strings.ToLower(strings.TrimSpace(dur)) {
	case "untilhostleavesplay", "aslongasinplay":
		return true
	}
	return false
}

// animateUntilEOT is registerAnimateEffects' one UntilEOT decision, shared by
// every half of one animation so the halves can never disagree about the
// lifetime. A host-scoped duration is NOT UntilEOT (it ends on the host's
// departure instead); a Duration$ Permanent is not; a next-turn duration gets
// its own turn boundary from AddContinuous. Every other value -- the absent
// Duration$ and UntilEndOfTurn -- keeps the historic end-of-turn cleanup.
func animateUntilEOT(dur string) bool {
	if animateHostScoped(dur) || isPerpetualDuration(dur) {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(dur), "Permanent") && !IsNextTurnDuration(dur)
}

// isPerpetualDuration reports Duration$ Perpetual: the effect lasts for the
// rest of the game, across every zone change of the affected object.
func isPerpetualDuration(dur string) bool {
	return strings.EqualFold(strings.TrimSpace(dur), "Perpetual")
}

// emitAnimateTriggersNotes is the shared Triggers$ fail-closed surface: one
// loud note per named body the parser refused.
func emitAnimateTriggersNotes(h Host, c *Ctx, ag animateGrant, api string) {
	for _, nm := range ag.triggersUnread {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: api + " Triggers$ " + nm + " is not a trigger body this engine can read; ignored"})
	}
}

// emitAnimateColorsNotes is the shared Colors$ fail-closed surface: the two
// loud notes effAnimate has always emitted, one per unimplementable shape.
func emitAnimateColorsNotes(h Host, c *Ctx, ag animateGrant, api string) {
	if ag.colorsRaw != "" && ag.colorsGrant {
		return
	}
	if ag.colorsRaw == "" {
		return
	}
	_, colorsOK := colorLetters(ag.colorsRaw)
	if !colorsOK {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: api + " Colors$ " + ag.colorsRaw + " is not implemented; colours unchanged"})
		return
	}
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
		Text: api + " Colors$ Colorless without OverwriteColors$ is not implemented; colours unchanged"})
}

// registerAnimateEffects is Animate and AnimateAll's sole per-object
// registration path: id is the animated object itself (Source = affected
// object, Controller = the granting controller, Affects Card.Self, the same
// one-shot-sweep shape registerPumpEffects uses). Skipping a half the SA
// never named avoids polluting Engine.continuous with an effect that would
// never do anything.
func registerAnimateEffects(h Host, c *Ctx, id state.ObjID, ag animateGrant) {
	// Every half of one animation shares ONE lifetime decision: a Duration$
	// UntilHostLeavesPlay grant is anchored to the ANIMATING source (the host)
	// through DurationSource, while the ordinary Duration$ values keep the
	// until-end-of-turn / Permanent classes. Hoisting it keeps the halves from
	// disagreeing (see animateUntilEOT).
	untilEOT := animateUntilEOT(ag.duration)
	// Duration$ Perpetual registers every half Permanent, like Duration$
	// Permanent, but WITHOUT the zone-scoped sweep below: a perpetual grant
	// follows its object through hand, stack, battlefield and graveyard
	// (Chronicler of Worship's discount must still apply when the card is
	// cast -- the cost is determined with the spell on the stack).
	permanent := ag.permanent || ag.perpetual
	var durSource state.ObjID
	if animateHostScoped(ag.duration) {
		durSource = c.Source
	}
	// The move-driven lifetime (ag.endOnLeave): the animated object's own id
	// rides Remembered and ExileOnMoved$ names the battlefield, so
	// effectMoveSweep ends EVERY half of the grant on the departure Move --
	// a returned object is a plain permanent again, not a re-activated
	// animation. The LeaveBattlefield$ promise family (Whip of Erebos,
	// Kheru Lich Lord, Gruesome Encore, Storm Herald) takes the same
	// lifetime: its whole animation -- haste, everything -- is the rider
	// sentence's own scope, so the animated object's departure ends every
	// half of it too, and a re-entered card is a plain permanent again.
	var exileOn, exileAlso string
	var remembered []state.ObjID
	if ag.endOnLeave || strings.EqualFold(ag.leaveExile, "Exile") {
		exileOn = "Battlefield"
		remembered = []state.ObjID{id}
	} else if ag.permanent || animateHostScoped(ag.duration) {
		// Duration$ Permanent without a rider ends when its animated object
		// leaves the zone it was granted in. A host-scoped grant ends on EITHER
		// the target's departure (CR 400.7) or the host's departure, so neither
		// object can return and reactivate the old grant. The sweep zone is the
		// target's current zone: Animate can target cards outside the battlefield.
		if o := h.Game().Obj(id); o != nil {
			if w := ZoneWord(o.Zone); w != "" {
				exileOn = w
				remembered = []state.ObjID{id}
			}
		}
	}
	if animateHostScoped(ag.duration) && c.Source != id {
		remembered = append(remembered, c.Source)
		if exileOn != "Battlefield" {
			exileAlso = "Battlefield"
		}
	}
	if ag.hasPower || ag.hasTough {
		h.AddContinuous(state.ContinuousEffect{
			Source: id, Affects: "Card.Self", Controller: c.Controller,
			Layer: state.LPT, Sub: state.SubSet,
			SetPower: ag.pw, SetToughness: ag.tf, HasSet: true,
			// The P/T grant lives as long as the type grant: a
			// Duration$ Permanent animation is WHOLLY permanent
			// (Stalking Stones's 3/3 lasts indefinitely), never
			// half-permanent — types kept while an UntilEOT P/T set
			// strips them to an untransformed-basis 0/0 the CR 704.5f
			// SBA destroys.
			Duration: ag.duration, Permanent: permanent, UntilEOT: untilEOT, DurationSource: durSource,
			ExileOnMoved: exileOn, ExileOnMovedAlso: exileAlso, Remembered: remembered,
			AffectedZone: ag.zone,
		})
	}
	if len(ag.types) > 0 || ag.removeCreatureTypes || len(ag.removeTypes) > 0 || ag.allCreatureTypes || ag.removeCardTypes {
		h.AddContinuous(state.ContinuousEffect{
			Source: id, Affects: "Card.Self", Controller: c.Controller,
			Layer: state.LType, AddTypes: ag.types,
			RemoveCreatureTypes: ag.removeCreatureTypes, RemoveTypes: ag.removeTypes,
			AddAllCreatureTypes: ag.allCreatureTypes, RemoveCardTypes: ag.removeCardTypes,
			Duration: ag.duration, Permanent: permanent, UntilEOT: untilEOT, DurationSource: durSource,
			ExileOnMoved: exileOn, ExileOnMovedAlso: exileAlso, Remembered: remembered,
			AffectedZone: ag.zone,
		})
	}
	if ag.name != "" {
		// Name$ (CR 613.1d, layer 3): the rename is the SAME characteristic
		// a printed SetName$ static registers, so Derived.Name and every
		// reader built on it (the resolving filter table, the view) answer
		// the new name. Layer 3 precedes the type/colour/ability/P-T halves
		// below in CR 613 order, and the walk keeps timestamp order inside
		// layer 3, so two competing renames resolve the way their grants
		// were registered.
		h.AddContinuous(state.ContinuousEffect{
			Source: id, Affects: "Card.Self", Controller: c.Controller,
			Layer: state.LText, SetName: ag.name,
			Duration: ag.duration, Permanent: permanent, UntilEOT: untilEOT, DurationSource: durSource,
			ExileOnMoved: exileOn, ExileOnMovedAlso: exileAlso, Remembered: remembered,
			AffectedZone: ag.zone,
		})
	}
	if ag.colorsGrant {
		h.AddContinuous(state.ContinuousEffect{
			Source: id, Affects: "Card.Self", Controller: c.Controller,
			Layer: state.LColor, AddColors: ag.colors, OverwriteColors: ag.overwriteColors,
			Duration: ag.duration, Permanent: permanent, UntilEOT: untilEOT, DurationSource: durSource,
			ExileOnMoved: exileOn, ExileOnMovedAlso: exileAlso, Remembered: remembered,
			AffectedZone: ag.zone,
		})
	}
	// Keywords$ and HiddenKeywords$ are ONE keyword grant: both are layer-6
	// derived keywords, and the brief's "do not let ability removal erase its
	// own simultaneous grant" needs HiddenKeywords$ to ride the SAME
	// AddKeywords list as Keywords$, AFTER RemoveAbilities clears. Concatenate
	// only when HiddenKeywords$ is present so an ordinary animation's list is
	// the parser's own slice, byte-identical to before.
	kws := ag.kws
	if len(ag.hiddenKws) > 0 {
		kws = append(append([]string(nil), ag.kws...), ag.hiddenKws...)
	}
	if ag.removeAbilities || len(kws) > 0 || len(ag.removeKeywords) > 0 {
		// CR 613.1f: RemoveAllAbilities$, RemoveKeywords$ and the keyword grant
		// of ONE animation are a single simultaneous layer-6 modification, so
		// they register as ONE LAbilities effect. The layer walk's in-effect
		// order (clear on RemoveAbilities, then the named removals, then
		// AddKeywords) is the card text's own; splitting them into separate
		// AddContinuous calls would stamp each its own ClockTick and let the
		// removal run at a LATER timestamp than a grant it must precede (the
		// defect registerStaticEffectGrant fixed for the identical static body).
		h.AddContinuous(state.ContinuousEffect{
			Source: id, Affects: "Card.Self", Controller: c.Controller,
			Layer: state.LAbilities, RemoveAbilities: ag.removeAbilities,
			AddKeywords: kws, RemoveKeywords: ag.removeKeywords,
			Duration: ag.duration, Permanent: permanent, UntilEOT: untilEOT, DurationSource: durSource,
			ExileOnMoved: exileOn, ExileOnMovedAlso: exileAlso, Remembered: remembered,
			AffectedZone: ag.zone,
		})
	}
	if len(ag.abilities) > 0 {
		h.AddContinuous(state.ContinuousEffect{
			Source: id, Affects: "Card.Self", Controller: c.Controller,
			Layer: state.LAbilities, AddAbilities: ag.abilities,
			SVars: c.SVars, AbilityGrantor: svarTableOwner(h, c),
			Duration: ag.duration, Permanent: permanent, UntilEOT: untilEOT, DurationSource: durSource,
			ExileOnMoved: exileOn, ExileOnMovedAlso: exileAlso, Remembered: remembered,
			AffectedZone: ag.zone,
		})
	}
	// The Triggers$ grant: one continuous effect per named trigger, Source =
	// the animated object itself (the trigger fires as ITS trigger; Affects
	// Card.Self names it at the granted walk's match site), the body's
	// Execute$ resolved from the ANIMATING face's table (TriggerGrantor --
	// the self-animation shape degenerates to the animated object). The
	// lifetime is the animation's, so the trigger leaves with it: UntilEOT
	// at cleanup, Duration$ Permanent forever, and endOnLeave ends the grant
	// on the animated object's departure like every other half. A fresh
	// Trigger copy per object so no two grants share a pointer.
	for i := range ag.triggers {
		t := ag.triggers[i]
		h.AddContinuous(state.ContinuousEffect{
			Source: id, Affects: "Card.Self", Controller: c.Controller,
			Layer:          state.LAbilities,
			AddTrigger:     &t,
			TriggerGrantor: ag.triggerGrantor,
			Duration:       ag.duration, Permanent: permanent, UntilEOT: untilEOT, DurationSource: durSource,
			ExileOnMoved: exileOn, ExileOnMovedAlso: exileAlso, Remembered: remembered,
			AffectedZone: ag.zone,
		})
	}
	// The LeaveBattlefield$ promise and the sVars$ grant (Whip of Erebos,
	// Kheru Lich Lord, Gruesome Encore, Storm Herald): both ride the
	// animation's own lifetime, one registration per animated object -- see
	// effects/leavebattlefield.go for the shape each takes.
	registerLeaveExile(h, c, id, ag.leaveExile, ag.duration, permanent)
	registerSVarGrants(h, c, id, ag.svars, ag.duration, permanent, untilEOT, durSource, exileOn, exileAlso, remembered)
	registerAnimateStaticAbilities(h, c, id, ag.staticAbilities, ag.duration, permanent, untilEOT, durSource, exileOn, exileAlso, remembered)
	// The Replacements$ grant: one Effect-created replacement per named SVar
	// body, riding the animation's own lifetime (ExileOnMoved$/Remembered),
	// exactly like the LeaveBattlefield$ promise above.
	registerAnimateReplacements(h, c, id, ag.replacements, ag.duration, permanent, untilEOT, durSource, exileOn, exileAlso, remembered)
}

// animateAllUnreadNote names, in ONE loud note, every parameter the SA carries
// that neither Animate nor AnimateAll reads (RemoveKeywords$/RemoveAllAbilities$/
// staticAbilities$/Triggers$/Replacements$/CantHaveKeyword$/RemoveLandTypes$ --
// the shared pre-existing Animate gaps, so the gap stays visible instead of
// silently doing nothing), then the supported parameters still apply.
func animateAllUnreadNote(h Host, c *Ctx, sa *cards.SA) {
	var unread []string
	for _, key := range []struct{ name, val string }{
		{"RemoveKeywords$", sa.Params["RemoveKeywords"]},
		{"RemoveAllAbilities$", sa.Params["RemoveAllAbilities"]},
		{"HiddenKeywords$", sa.Params["HiddenKeywords"]},
		{"Replacements$", sa.Params["Replacements"]},
		{"CantHaveKeyword$", sa.Params["CantHaveKeyword"]},
		{"RemoveLandTypes$", sa.Params["RemoveLandTypes"]},
	} {
		if strings.TrimSpace(key.val) != "" {
			unread = append(unread, key.name)
		}
	}
	if len(unread) > 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "AnimateAll " + strings.Join(unread, "/") + " not implemented; ignored"})
	}
}

// effAnimateAll is Animate's ValidCards$ sweep: the identical per-object
// registration, but the affected set is baked at resolution time by walking
// AliveFrom(0)'s fixed APNAP seat order and each seat's zone slice in its
// existing registration order, never a map (the effPumpAll pattern, CR 611.2c:
// such an effect applies only to the objects matching the filter when the
// ability resolves), filtered with MatchesSpecCtx against ValidCards$ (default
// "Creature") rather than taken from Defined$/chosen targets. Zone$ widens
// the walk the way PumpAll's PumpZone$ does, and the registered effects carry
// the AffectedZone scope so a non-battlefield grant applies only while the
// card sits there.
func effAnimateAll(h Host, c *Ctx, sa *cards.SA) {
	ag := parseAnimateGrant(h, c, sa)
	// AnimateAll's RemoveKeywords$ and RemoveAllAbilities$ stay unread (out of
	// scope for the tickets that read them on Animate): clear what the shared
	// parser read so the sweep below cannot apply them behind
	// animateAllUnreadNote's "not implemented; ignored" note.
	ag.removeKeywords = nil
	ag.removeAbilities = false
	ag.replacements = nil
	// AnimateAll's HiddenKeywords$ stays unread too (the corpus carries no
	// AnimateAll HiddenKeywords$ line, and this ticket scopes the derived
	// restriction to Animate): clear what the shared parser read so the
	// sweep below cannot apply it behind animateAllUnreadNote's note.
	ag.hiddenKws = nil
	// AnimateAll's Name$ stays unread too: the rename is Animate-scoped, so
	// clear what the shared parser read, exactly as the two parameters above.
	ag.name = ""
	emitAnimateColorsNotes(h, c, ag, "AnimateAll")
	emitAnimateTriggersNotes(h, c, ag, "AnimateAll")
	animateAllUnreadNote(h, c, sa)
	var ateotIDs []state.ObjID
	spec := sa.Params["ValidCards"]
	if spec == "" {
		spec = "Creature"
	}
	g := h.Game()
	for si, p := range g.AliveFrom(0) {
		if ag.zone != "" {
			zones, all, ok := ParseZones(ag.zone)
			if !ok {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "AnimateAll Zone$ " + ag.zone + " is not a zone list this engine can ask; the animation is skipped"})
				break
			}
			if all {
				// "All": every public game zone plus the owner-private ones
				// g.Zone covers; ZCeased has no membership list (see
				// state/ids.go) so it is skipped. ZStack is included: an
				// object on the stack is a real animation target.
				zones = []state.Zone{state.ZLibrary, state.ZHand, state.ZBattlefield,
					state.ZGraveyard, state.ZExile, state.ZStack, state.ZCommand}
			}
			for _, z := range zones {
				// The shared stack is scanned once, under the first alive
				// seat (state/game.go Zone; the effects/count.go and
				// rules/statics.go convention). Without the guard an N-seat
				// table registers one Animate grant per seat for the same
				// stack object.
				if z == state.ZStack && si > 0 {
					continue
				}
				for _, id := range g.Zone(z, p) {
					if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
						registerAnimateEffects(h, c, id, ag)
						ateotIDs = append(ateotIDs, id)
					}
				}
			}
			continue
		}
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				registerAnimateEffects(h, c, id, ag)
				ateotIDs = append(ateotIDs, id)
			}
		}
	}
	scheduleAtEOT(h, c, sa, ateotIDs)
}

func effProtection(h Host, c *Ctx, sa *cards.SA) {
	gains := resolveGains(sa.Params["Gains"], sa.Params["Choices"], h.Game().Obj(c.Source))
	if gains == "" {
		return
	}
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		h.AddContinuous(state.ContinuousEffect{
			Source: o.ID, Affects: "Card.Self", Controller: c.Controller,
			Layer: state.LAbilities, AddKeywords: []string{"Protection from " + gains},
			UntilEOT: true,
		})
	}
}

// resolveGains turns Protection's Gains$ parameter into a concrete quality
// string ("red", "artifacts", ...). ChosenColor reads the resolving source's
// event-folded answer and fails closed when it is absent or unreadable. Gains$
// Choice (Mother of Runes: "Gains$ Choice | Choices$ AnyColor") still has no
// chooser in this build, so it keeps the deterministic AnyColor/first-choice
// fallback used by effCharm and effVote.
func resolveGains(gains, choices string, source *state.Object) string {
	if strings.EqualFold(gains, "ChosenColor") {
		if source == nil {
			return ""
		}
		switch colourLetter(source.ChosenColor) {
		case 'W':
			return "white"
		case 'U':
			return "blue"
		case 'B':
			return "black"
		case 'R':
			return "red"
		case 'G':
			return "green"
		default:
			return ""
		}
	}
	if !strings.EqualFold(gains, "Choice") {
		return gains
	}
	if strings.EqualFold(choices, "AnyColor") {
		return "white"
	}
	return strings.TrimSpace(strings.SplitN(choices, ",", 2)[0])
}

// svarTableOwner names the object whose face carries c.SVars -- the table a
// by-name grant (Animate's Abilities$/Triggers$) resolves its bodies from,
// and so the grantor GrantAbilityPush/GrantTriggerPush must re-resolve them
// against. For an ordinary resolution that is the source. For a
// HAS-ALL-ABILITIES-OF wrapper (Manascape Refractor activating Spawning
// Pool's Animate) the body and its SVar table belong to the FOREIGN card the
// wrapper was minted from (state.Object.GainedFrom), not the recipient that
// is c.Source: naming the recipient registered a grant the offer loop could
// read (it reads ce.SVars) but the activation could never resolve, so the
// offered ability silently did nothing and a bot re-chose it forever.
func svarTableOwner(h Host, c *Ctx) state.ObjID {
	if c.ResolvingObj != 0 {
		if w := h.Game().Obj(c.ResolvingObj); w != nil && w.GainedFrom != 0 && w.GainedFace != nil {
			if f := h.Game().Obj(w.GainedFrom); f != nil && f.Face() != nil {
				return w.GainedFrom
			}
		}
	}
	return c.Source
}
