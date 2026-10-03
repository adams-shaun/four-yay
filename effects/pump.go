package effects

import (
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effPump is api:Pump's resolution. Every parameter it reads comes from the
// compiled PumpParams (pump_params.go), the one reader of a Pump ability's
// parameters; internal/codeshape's pumpParamLeaks ratchet holds this file to
// no parameter read at all. The shared per-object registration
// (registerPumpEffects, combatfx.go) takes the compiled PumpGrant.
func effPump(h Host, c *Ctx, sa *cards.SA) {
	p := PumpOf(sa)
	noteUnreadParams(h, c, "Pump", p.Unread)
	// NoteNumber$ (Lupine Harbingers' exile trigger: "note the number of
	// turns you've begun"): the body does not pump at all -- it notes the
	// evaluated number onto its source CARD through the events.NotedNumber
	// marker, which Count$NotedNumber reads at the later ETB (the corpus's
	// one carrier is exactly that shape: the exile trigger notes
	// Count$YourTurns, the ETB's SVar reads SVar$X/Minus.Y where Y is
	// Count$NotedNumber). Terminal: a NoteNumber body never also pumps, and
	// the read comes before any registration so a note is never a
	// half-applied pump.
	if p.HasNoteNumber {
		n := numText(h, c, p.NoteNumber, 0)
		if c.Source != 0 {
			h.Emit(events.Event{Kind: events.NoteNumber, Obj: c.Source, Amount: n})
		}
		return
	}

	pumpNotes(h, c, sa, p)

	// Secondary$ True (Amonkhet Raceway's max-speed AddAbility$ grant marks
	// the granted pump with it): Forge CardFactoryUtil sets the key on
	// machine-derived abilities, and Card.java's ability-text renderer skips
	// secondary spell abilities -- a presentation and deck-tooling filter,
	// never a rules tail. The engine delivers a granted pump through the
	// AddAbility static grant structurally (the static is the grantor; the
	// SA is not a printed line), so the recognition has no behavioural half
	// here; compilePump's read keeps the parameter census honest.
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
	if p.HasKWChoice {
		if c.Modes != nil {
			// fx42 scoping: consume the answer once; a nested KWChoice pump
			// reached below poses its own ask.
			chosenKW = c.Modes
			c.Modes = nil
		} else {
			choices := p.KWChoice
			d := &decision.Decision{Player: c.Controller, Kind: decision.KModes,
				Min: 1, Max: 1, Source: c.Source,
				ResumeKind: "modes", ResumeSA: sa,
				Prompt: "Choose a keyword"}
			for i, name := range choices {
				d.Options = append(d.Options, decision.Option{
					Index: i, Kind: "mode", Label: name, Obj: c.Source, Player: c.Controller})
			}
			if ans, ok := AskTape(h, d); ok {
				// The resolution kernel's answer in hand (its Record wrote
				// the ModeChosen a KModes answer records): the chosen
				// keywords by option index into KWChoice$, exactly the
				// names the "modes" arm binds into Ctx.Modes. The legacy
				// re-entry re-runs this body from its first line, so the
				// emissions before the ask are repeated first, exactly as
				// it repeats them.
				noteUnreadParams(h, c, "Pump", p.Unread)
				pumpNotes(h, c, sa, p)
				chosenKW = make([]string, 0, len(ans))
				for _, o := range ans {
					if o.Index >= 0 && o.Index < len(choices) {
						chosenKW = append(chosenKW, choices[o.Index])
					}
				}
			} else {
				_ = Ask(h, d)

				// No engine host (R-9): the deterministic first candidate, with
				// the Note that records why the richer path did not run.
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "chose its first keyword (no engine host to ask)"})
				chosenKW = choices[:1]
			}

		}
	}
	zone := p.PumpZone
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
			if !p.PumpZoneOK {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
					Text: "PumpZone$ " + zone + " is not a zone list this engine can ask; the pump is skipped"})
				continue
			}
			if !p.PumpZoneAll && !slices.Contains(p.PumpZones, o.Zone) {
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
		if p.RememberTargets {
			c.Remembered = append(c.Remembered, t)
			eventRemember(h, c, t.Obj)
		}
		// RememberPumped$ True remembers the objects this Pump actually
		// affects, rather than merely the chosen targets: an off-zone target
		// skipped above is not added to either remembered set. The ctx half is
		// available to chained sub-abilities; eventRemember persists the
		// source's list for later Card.IsRemembered filters and replay.
		if p.RememberPumped {
			c.Remembered = append(c.Remembered, t)
			eventRemember(h, c, t.Obj)
		}
		// Resolve amounts PER OBJECT: Double reads this object's own
		// layer-derived power/toughness, so a multi-object Defined$ must not
		// apply one creature's stat to every creature.
		att := numForObjectText(h, c, p.NumAtt, true, 0, o.ID)
		def := numForObjectText(h, c, p.NumDef, false, 0, o.ID)
		registerPumpEffects(h, c, o.ID, att, def, false, false, &p.Grant, zone, chosenKW)
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
	if spec := p.ForgetImprinted; spec != "" {
		var ids []state.ObjID
		for _, t := range DefinedSpec(h, c, spec) {
			if !t.IsPlayer {
				ids = append(ids, t.Obj)
			}
		}
		if len(ids) > 0 && c.Source != 0 {
			h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: ids, Text: "forget"})
		}
	}
}

// pumpNotes is effPump's ClearNotedCardsFor$ and NoteCards$ notation, run
// before any KWChoice$ ask (and so repeated by that ask's re-entry).
func pumpNotes(h Host, c *Ctx, sa *cards.SA, p *PumpParams) {
	// ClearNotedCardsFor$ clears the requested player labels from its Defined$
	// player set. The event fold keeps a later resolution and a replay from
	// retaining a previous choice (Master of Ceremonies changes these labels
	// every upkeep).
	for _, label := range p.ClearNotedCardsFor {
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
	if label := p.NoteCardsFor; label != "" {
		switch pumpNotes6381Codes.Code(string(p.NoteCards)) {
		case pumpNotes6381Self:
			spec := p.Defined
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
		case pumpNotes6381Remembered:
			for _, t := range resolvedRemembered(h, c) {
				if t.IsPlayer || t.Obj == 0 {
					continue
				}
				h.Emit(events.Event{Kind: events.CardNoted, Obj: t.Obj, Text: label})
			}
		case pumpNotes6381TriggeredSource:
			if c.TriggerSource != 0 {
				h.Emit(events.Event{Kind: events.CardNoted, Obj: c.TriggerSource, Text: label})
			}
		default:
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "unimplemented NoteCards$ " + p.NoteCards})
		}
	}
}

const (
	pumpNotes6381Self            uint16 = 1 // "Self"
	pumpNotes6381Remembered      uint16 = 2 // "Remembered"
	pumpNotes6381TriggeredSource uint16 = 3 // "TriggeredSource"
)

var pumpNotes6381Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Self", Val: pumpNotes6381Self},
	state.StrEntry[uint16]{Key: "Remembered", Val: pumpNotes6381Remembered},
	state.StrEntry[uint16]{Key: "TriggeredSource", Val: pumpNotes6381TriggeredSource},
)
