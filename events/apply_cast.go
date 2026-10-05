// Event folds for the stack and casting: CastInfo, Resolve, Priority, Choose answers and SVar storage.
//
// Split out of events/apply.go: code moved verbatim, no behaviour
// change. Each fold function is the body of the matching case in
// Apply (g, e) switch; see apply.go for the dispatch.
package events

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// foldCastInfo folds Kind CastInfo into state.
func foldCastInfo(g *state.Game, e *Event) {
	if o := g.Obj(e.Obj); o != nil {
		o.CastFlags = FlagsFrom(e.Counter)
		// FlagConverged's Amount is the distinct-colour spend count (CR
		// 107.4f converge), never an X value: converge faces carrying their
		// own {X} pip (Skyrider Elf) keep the two on separate pay-time
		// CastInfo events, and the flag routes this Amount into the count
		// field instead of overwriting X.
		// FlagReplicated's Amount is the replicate payment count, never an
		// X value (measured: no K:Replicate carrier's mana value carries
		// {X}), so the flag routes the Amount into the count field instead
		// of overwriting X.
		// FlagMultikicked's Amount is CR 702.43's times-kicked count, never
		// an X value: the count rides its own TRAILING pay-time CastInfo
		// (rules/cast.go's payCast), so a multikicker carrier that pairs
		// {X} with Multikicker (Comet Storm) keeps the two on separate
		// events.
		// FlagManaSpent's Amount is the TOTAL mana actually spent to cast
		// the spell (CR 601.2h), never an X value: the count rides its own
		// TRAILING pay-time CastInfo (rules/cast.go's payCast), so a
		// carrier that pairs {X} with the read (none measured) keeps the
		// two on separate events.
		// Conspire (CR 702.78a) is a BOOL fold, not an amount: it is set
		// whenever the resolved cast's pay-time CastInfo carries
		// FlagConspired, whatever other tags ride the same event. Folded
		// OUTSIDE the exclusive switch below so a later event carrying the
		// flag (each later event accumulates all earlier flags) cannot
		// steal that event's Amount from its own routing case.
		if FlagsFrom(e.Counter)&state.FlagConspired != 0 {
			o.Conspired = true
		}
		// Offspring (CR 702.175a) is a BOOL fold as well: paid at most
		// once, so it is set whenever the pay-time CastInfo carries
		// FlagOffspringPaid, whatever other tags ride the same event
		// (the Conspired pattern).
		if FlagsFrom(e.Counter)&state.FlagOffspringPaid != 0 {
			o.OffspringPaid = true
		}
		// Teamwork (CR 702.194b) is a BOOL fold as well: the optional
		// additional tap cost is paid at most once, so it is set whenever the
		// pay-time CastInfo carries FlagTeamworkPaid, whatever other tags ride
		// the same event (the Conspired pattern).
		if FlagsFrom(e.Counter)&state.FlagTeamworkPaid != 0 {
			o.TeamworkPaid = true
		}
		if FlagsFrom(e.Counter)&state.FlagOptionalCostPaid != 0 {
			o.OptionalCostPaid = true
		}
		// Convoke (CR 702.66, task connive1) is an ID-LIST fold, not an
		// amount: the convoked creatures ride the pay-time CastInfo's IDs
		// whenever the flag is present, whatever other tags ride the same
		// event. Folded OUTSIDE the exclusive switch below (the Conspired
		// pattern) so a later event carrying the flag cannot steal that
		// event's Amount from its own routing case.
		if FlagsFrom(e.Counter)&state.FlagConvoked != 0 {
			o.Convoked = append([]state.ObjID(nil), e.IDs...)
		}
		// AddsCounters$ (Opal Palace and siblings) is a structured-payload
		// fold: the producing ABILITIES whose riders applied to this cast --
		// snapshotted at production, with how many of each one's mana units
		// the payment spent -- ride the pay-time CastInfo's Text payload into
		// Object.ManaAddsCounterGrants, alongside the flag that records the
		// spend. Folded OUTSIDE the exclusive switch below (the Convoked
		// pattern) so the Amount stays for its own consume arm; the
		// entry-counter plan uses the stored rider verbatim, never re-reading
		// the source's face.
		if FlagsFrom(e.Counter)&state.FlagAddsCounters != 0 {
			o.ManaAddsCounterGrants = ManaAddsCounterGrantsFromText(e.Text)
		}
		// The per-colour spend vector (Adamant, CR 702.5) is a
		// structured-payload fold: the six state.Mana slots ride the
		// CastInfo's Text payload into Object.ManaColorSpent, alongside the
		// flag (the ManaAddsCounterGrants pattern). The non-empty Text guard
		// keeps a later event that merely ACCUMULATED the flag -- the
		// Compleated capture rides a trailing CastInfo with an empty Text --
		// from resetting the folded vector to zero. Folded OUTSIDE the
		// exclusive switch below so the Amount stays for its own consume arm.
		if FlagsFrom(e.Counter)&state.FlagManaColorSpent != 0 && e.Text != "" {
			o.ManaColorSpent = ManaColorSpentFromText(e.Text)
		}
		switch {
		// Conspire's Amount is a marker, never data: the bool was folded
		// above, and the flag rides a LOCAL counter at the emission site
		// (rules/cast.go's payCast never ORs FlagConspired into the
		// accumulating flags), so no later CastInfo carries it and this
		// arm's position in the newest-flag-first ordering is
		// order-independent. The arm exists to CONSUME the Amount: without
		// it the event fell through to default and wrote o.X = 1 onto every
		// conspired cast (and StackCopy propagated that onto its copies).
		case FlagsFrom(e.Counter)&state.FlagConspired != 0:
			// bool folded above; the Amount is deliberately unused
		case FlagsFrom(e.Counter)&state.FlagOffspringPaid != 0:
			// bool folded above; the Amount is deliberately unused
		case FlagsFrom(e.Counter)&state.FlagTeamworkPaid != 0:
			// bool folded above; the Amount is deliberately unused
		case FlagsFrom(e.Counter)&state.FlagOptionalCostPaid != 0:
			// bool folded above; the Amount is deliberately unused
		case FlagsFrom(e.Counter)&state.FlagConvoked != 0:
			// the convoked id list was folded above; the Amount is
			// deliberately unused (the Conspired arm's consume shape)
		case FlagsFrom(e.Counter)&state.FlagAddsCounters != 0:
			// the rider-source id list was folded above; the Amount is
			// deliberately unused (the Conspired arm's consume shape)
		case FlagsFrom(e.Counter)&state.FlagManaColorSpent != 0:
			// the per-colour spend vector was folded above; the Amount is
			// deliberately unused (the Conspired arm's consume shape)
		case FlagsFrom(e.Counter)&state.FlagCompleated != 0:
			o.CompleatedLifePaid = e.Amount
		case FlagsFrom(e.Counter)&state.FlagConverged != 0:
			o.ConvergeColours = e.Amount
		case FlagsFrom(e.Counter)&state.FlagReplicated != 0:
			o.ReplicateTimes = e.Amount
		case FlagsFrom(e.Counter)&state.FlagSquadPaid != 0:
			o.SquadPaid = e.Amount
		case FlagsFrom(e.Counter)&state.FlagMultikicked != 0:
			o.TimesKicked = e.Amount
		// One CastInfo per captured total, each LATER event carrying ALL
		// earlier flags (payCast's flags |= accumulation), so this switch
		// checks the NEWEST flag first -- the reverse of the emission
		// order -- or every later event would route into the first tag's
		// field: Artifact, Desert, Cave, Treasure, then Snow, then the
		// total.
		case FlagsFrom(e.Counter)&state.FlagManaArtifactSpent != 0:
			o.ManaArtifactSpent = e.Amount
		case FlagsFrom(e.Counter)&state.FlagManaDesertSpent != 0:
			o.ManaDesertSpent = e.Amount
		case FlagsFrom(e.Counter)&state.FlagManaCaveSpent != 0:
			o.ManaCaveSpent = e.Amount
		case FlagsFrom(e.Counter)&state.FlagManaTreasureSpent != 0:
			o.ManaTreasureSpent = e.Amount
		case FlagsFrom(e.Counter)&state.FlagManaSnowSpent != 0:
			o.ManaSnowSpent = e.Amount
		case FlagsFrom(e.Counter)&state.FlagManaSpent != 0:
			o.ManaSpent = e.Amount
		default:
			o.X = e.Amount
		}
	}
}

// foldResolve folds Kind Resolve into state.
func foldResolve(g *state.Game, e *Event) {
	// The resolving object leaves the stack through its own MoveZone event,
	// so popping here would drop a second object; what the case DOES fold
	// is the per-ability resolution tally Forge's Count$ResolvedThisTurn
	// reads. e.Obj is the ability stack-object wrapper, whose Source (the
	// permanent) and Ability (the root Ability$ body, re-derived from the
	// TriggerPush/AbilityPush event) together identify "this ability". A
	// SPELL resolution carries no Ability and no tally target: every corpus
	// carrier of the head is a triggered or activated ability. Incremented
	// here, from the existing Resolve event, so a log-only replay rebuilds
	// the identical tally with no new Kind or field; TurnChange zeroes it.
	// The increment happens BEFORE the rules side builds the resolving Ctx,
	// so the count the card reads already includes its own resolution --
	// Forge's "if this is the FOURTH time" counts the current one.
	if o := g.Obj(e.Obj); o != nil && o.Ability != nil {
		if g.ResolvedThisTurn == nil {
			g.ResolvedThisTurn = make(map[string]int32)
		}
		g.ResolvedThisTurn[ResolvedAbilityKey(o.Source, o.Ability)]++
	}
}

// foldPriority folds Kind Priority into state.
func foldPriority(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		g.Priority = e.Player
		passes := e.Amount
		if passes < 0 {
			passes = 0
		}
		g.Passes = passes
	}
}

// foldXChange folds Kind XChange into state.
func foldXChange(g *state.Game, e *Event) {
	// A mid-resolution effect rewrote the {X} a stack object was cast or
	// activated with (DB$ ChangeX: Unbound Flourishing's doubling, Glava's
	// "the value of X becomes 5"). Amount is the new value, Obj the stack
	// object -- downstream readers (resolution's ctx.X, the ETB
	// replacement ctx, Count$xPaid) pick it up fresh, so the rewrite is
	// the only write needed.
	if o := g.Obj(e.Obj); o != nil {
		o.X = e.Amount
	}
}

// foldGiftPromise folds Kind GiftPromise into state.
func foldGiftPromise(g *state.Game, e *Event) {
	// CR 702.168: the cast-time gift election. Obj is the spell on the
	// stack, Player the promised opponent (valid only when Amount != 0),
	// Amount 1 for a promise and 0 for a decline. Folded onto the object
	// so the PromisedGift predicate, the Count$PromisedGift head and
	// Defined$ Promised read one home, and preserved across the
	// stack->battlefield move by events.Move (the X/CastFlags window).
	if o := g.Obj(e.Obj); o != nil {
		o.CastFlags &^= state.FlagPromisedGift
		o.GiftPromisedTo = 0
		if e.Amount != 0 {
			o.CastFlags |= state.FlagPromisedGift
			o.GiftPromisedTo = e.Player
		}
	}
}

// foldStoreSVar folds Kind StoreSVar into state.
func foldStoreSVar(g *state.Game, e *Event) {
	// api:StoreSVar wrote one named runtime SVar onto its source (Forge's
	// sa.setSVar: Minion of the Wastes / Phyrexian Processor's
	// `Cost$ Mandatory PayLife<X>` body storing the paid life under
	// LifePaidOnETB). Obj is the object, Text the SVar name and Amount
	// the resolved value; Object.RuntimeSVars overlays the printed face
	// table for the CDA and token reads that consume it. An empty name
	// writes nothing rather than a ghost entry, and events.Move's
	// leave-the-battlefield reset clears the table with the cast-time
	// window. The write is a keyed map insert, so map order never
	// reaches an event.
	if o := g.Obj(e.Obj); o != nil && e.Text != "" {
		if o.RuntimeSVars == nil {
			o.RuntimeSVars = make(map[string]int32)
		}
		o.RuntimeSVars[e.Text] = e.Amount
	}
}

// foldNoteNumber folds Kind NoteNumber into state.
func foldNoteNumber(g *state.Game, e *Event) {
	// A trigger's Execute$ body noted a number onto the CARD (DB$ Pump
	// NoteNumber$ <expr> -- Lupine Harbingers' exile trigger noting
	// Count$YourTurns). Amount is the value, Obj the card; Count$
	// NotedNumber reads it at the later ETB, and events.Move's
	// leave-the-battlefield reset clears it with the X/CastFlags window.
	if o := g.Obj(e.Obj); o != nil {
		o.NotedNumber = e.Amount
	}
}

// foldChoose folds Kind Choose into state.
func foldChoose(g *state.Game, e *Event) {
	if o := g.Obj(e.Obj); o != nil {
		switch e.Counter {
		case "name":
			o.ChosenName = e.Text
		case "type":
			o.ChosenType = e.Text
		case "color":
			o.ChosenColor = e.Text
		case "mode":
			// SetChosenMode$ on an as-enters GenericChoice: the mode
			// persists on the permanent, unlike a stack-only modal answer.
			o.ChosenModes = []string{e.Text}
		case "number":
			o.ChosenNumber = e.Amount
		case "riot":
			o.RiotChoice = e.Text
		case "untap":
			o.UntapChoice = e.Text
		case "unleash":
			o.UnleashChoice = e.Text
		case "clone":
			o.ETBCloneChoiceValid = true
			o.ETBCloneChoice = 0
			if len(e.IDs) > 0 {
				o.ETBCloneChoice = e.IDs[0]
			}
		case state.ModeChoiceCounterPrefix + state.ModeScopeThisTurn,
			state.ModeChoiceCounterPrefix + state.ModeScopeThisGame,
			state.ModeChoiceCounterPrefix + state.ModeScopeYourLastCombat:
			scope := strings.TrimPrefix(e.Counter, state.ModeChoiceCounterPrefix)
			o.ModeChoices = append(o.ModeChoices, state.ModeChoice{
				Mode: e.Text, Scope: scope,
				Turn: g.Turn, Combat: g.CombatsThisTurn,
			})
			if scope == state.ModeScopeYourLastCombat {
				// Resynchronise the object's combat identity to the clock the
				// pick was just stamped from. An object that entered the
				// battlefield (or changed controller) after this combat's
				// BeginCombat rotation was not in that loop, so its
				// CurCombat* is stale or zero; without this, a second ask in
				// the SAME combat would wrongly treat the current combat's
				// pick as an earlier combat's and withhold its mode. Setting
				// it here keeps the identity derived from the same game clock
				// the pick carries, so rotation at the next combat start still
				// retains exactly this pick.
				o.CurCombatTurn, o.CurCombatCombat = g.Turn, g.CombatsThisTurn
			}
		case "protector":
			// CR 310.10: the Siege protector chosen as this Battle
			// entered. Player carries the chosen opponent's seat.
			o.Protector = e.Player
			o.ProtectorValid = true
		case "chosen":
			o.Chosen = rememberedFrom(e.IDs)
		case "remembered":
			o.Remembered = append(o.Remembered, rememberedFrom(e.IDs)...)
		case "sneak-defender":
			// CR 702.190b: the defender a K:Sneak cast captured when its
			// Return cost was paid. It is a DEDICATED channel, never the
			// generic Remembered list, so a stale remembered player on the
			// card cannot masquerade as the sneak defender (rules/sneak.go
			// sneakDefenderFrom). IDs[0] is the defender's PlayerRef;
			// optional IDs[1] is the planeswalker/battle being attacked.
			if len(e.IDs) > 0 {
				if p, ok := e.IDs[0].PlayerRef(); ok {
					o.SneakDefender = p
					o.SneakDefenderObject = 0
					if len(e.IDs) > 1 {
						o.SneakDefenderObject = e.IDs[1]
					}
					o.SneakDefenderValid = true
				}
			}
		case "forget-remembered":
			// ForgetChanged$ True (Forge ChangeZoneEffect's
			// host.removeRemembered on the moved card): the named cards leave
			// the source object's persistent Remembered list. Player entries
			// and ids not named are kept, so a bad or partial payload degrades
			// to a smaller forget, never a wider one.
			drop := make(map[state.ObjID]bool, len(e.IDs))
			for _, id := range e.IDs {
				drop[id] = true
			}
			kept := make([]state.Target, 0, len(o.Remembered))
			for _, t := range o.Remembered {
				if !t.IsPlayer && drop[t.Obj] {
					continue
				}
				kept = append(kept, t)
			}
			o.Remembered = kept
		case "clear-remembered":
			o.Remembered = nil
		case "clear-chosen-card":
			// Forge's Cleanup ClearChosenCard$: the chosen-card half of the
			// object's chosen list goes; chosen players stay.
			kept := o.Chosen[:0]
			for _, t := range o.Chosen {
				if t.IsPlayer {
					kept = append(kept, t)
				}
			}
			o.Chosen = kept
		case "clear-chosen-player":
			// The chosen-PLAYER half goes; chosen cards stay.
			keptCards := o.Chosen[:0]
			for _, t := range o.Chosen {
				if !t.IsPlayer {
					keptCards = append(keptCards, t)
				}
			}
			o.Chosen = keptCards
		case "noted-mana":
			// RememberCostMana$ (Jeweled Amulet: "note the type of mana
			// spent to pay this activation cost"): the payment path's
			// negative ManaAdd events carry the spend, and this marker
			// folds the SAME colours onto the source object so the card's
			// mana ability (Produced$ Special LastNotedType) can read
			// them later. Text is the WUBRG-ordered colour letters the
			// payment spent; an empty Text degrades to a cleared note.
			o.LastNotedMana = e.Text
		}
	}
}
