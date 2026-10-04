package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Play", effPlay) }

// effPlay implements a "play a card from a zone without paying its mana
// cost" effect (Forge's Play API; CR 601.2/117.3a's "play" verb covers
// casting a spell or playing a land from a non-hand zone). The candidates are
// gathered the same way the rest of the engine gathers a Defined$/filter
// population:
//
//   - Defined$ (ExiledWith, Remembered, Targeted, ...) names the card(s)
//     directly and is resolved through context.go's Defined.
//   - Valid$ / ValidZone$ name a filter and a zone to scan (e.g. Spinerock
//     Knoll's Valid$ Card.ExiledWithSource + ValidZone$ Exile).
//   - ValidTgts$ means the ability already targeted a card at placement, so
//     the recorded target is the population.
//
// Whichever population results, the effect poses a KModes "play" choice over
// the candidate cards (or a yes/no when exactly one is offered), and the
// answered choice is applied by rules' playAnswerSettle, which begins the
// cast of the chosen card from its current zone and records it in Ctx.Play.
// The
// WithoutManaCost$ semantics (cast the card for free) are applied by the cast
// flow, not here, because mana is paid in rules where the cost grammar lives
// (rules/play_tape.go's playAnswerApply; WithoutManaCost$ False and an absent
// param both pay the printed cost, Only a literal True casts free).
//
// The remaining rider parameters:
//
//   - Controller$ names WHO the ask is posed to and whose cast the answer
//     begins (Etali's Controller$ You -- the corpus's dominant shape --
//     and an absent param keep the resolving controller; Word of Command's
//     TargetedPlayer, Wild Evocation's TriggeredPlayer and Spell Queller's
//     RememberedOwner route the play to the named seat). An unresolvable
//     value is a fail-closed no-op under one loud Note, never a silent
//     reroute to the resolving controller.
//   - ShowCards$ (Sunbird's Invocation) is the play's public reveal rider,
//     emitted by rules' answer settle before the cast moves the card out of its
//     hidden zone (see there).
//   - ForgetPlayed$ True (task param:api:Play.ForgetPlayed, read on the
//     PlayDone pass below) drops each actually-begun card from the
//     remembered set so a chained "if you don't play it" arm only sees the
//     unplayed remainder.
func playValidReadsOtherHand(valid string) bool {
	for _, token := range strings.FieldsFunc(valid, func(r rune) bool {
		return r == '.' || r == '+' || r == ','
	}) {
		switch playValidReadsOtherHandCodes.Code(string(strings.ToLower(strings.TrimSpace(token)))) {
		case playValidReadsOtherHandPlayerRef:
			return true
		}
	}
	return false
}

func effPlay(h Host, c *Ctx, sa *cards.SA) {
	if c.PlayDone {
		// The answer has been applied -- INCLUDING a decline (an Optional$
		// Play answered with the empty choice): rules' playAnswerSettle has
		// consumed it (it began the cast of c.Play, or nothing for a
		// decline), so there is nothing left for this effect to do. Clearing
		// Play keeps the answer scoped to this one Play: a nested Play
		// reached below this one in the same walk must pose its own ask
		// instead of inheriting the answered card.
		//
		// ForgetPlayed$ True (task param:api:Play.ForgetPlayed): a card the
		// Play actually BEGAN to play has been cast/put onto the stack by the
		// answer settle and must leave the remembered set now -- the chained
		// "if you don't play it" arm (Vaan, Street Thief's ConditionDefined$
		// Remembered Treasure gate) reads both remembered halves, and with
		// the played card still remembered it fired even though the cast was
		// taken. The decline leaves ctx.Play at 0 (the answer settle only sets
		// it for a begun card), so the guard keeps the decline path untouched
		// and the Treasure is created exactly when nothing was played. The
		// forget shares ForgetChanged$'s body (context.go): a ctx filter AND
		// the "forget-remembered" Choose event on the source, replay-safe
		// with no new event kind. An Amount$ Play that begins SEVERAL cards
		// still only forgets the first (ctx.Play holds one card) -- measured
		// corpus-unreachable: every ForgetPlayed$ carrier plays one card.
		if id := c.Play; id != 0 && strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKForgetPlayed)), "True") {
			forgetRememberedOne(h, c, id)
		}
		c.PlayDone = false
		c.Play = 0
		return
	}
	var candidates []state.ObjID
	g := h.Game()

	// A targeted Play (ValidTgts$ on the same SA, e.g. Conduit of Worlds)
	// plays the card it targeted at placement.
	if TargetsOf(sa).Has(TgtValidPresent) {
		for _, t := range c.Targets {
			candidates = append(candidates, t.Obj)
		}
	} else if defined := DefinedRefOf(sa); defined.Set() {
		// Population by Defined$ (ExiledWith / Remembered / ...).
		for _, t := range DefinedSpec(h, c, defined.Raw) {
			candidates = append(candidates, t.Obj)
		}
		// The trigger-capture exclusion (task castprov2, Amped Raptor's
		// gated DigUntil → DB$ Play chain): Forge's triggering objects never
		// sit in the shared remembered list — they are "triggering objects",
		// a separate channel — so a Play over a REMEMBERED population reads
		// what this resolution chain itself remembered, never what the
		// triggering event captured (Ctx.Captured is exactly that part of
		// Remembered; the same exclusion immediate.go and the
		// RememberChain$ False arm apply). Without it a gated-OFF DigUntil's
		// play offered the triggering permanent itself, and a gated-in one
		// offered it BESIDE the found card. Measured: zero corpus files
		// combine a trigger/replacement RememberObjects$ capture with a
		// DB$ Play | Defined$ Remembered, so no carrier loses a legitimate
		// candidate to this read.
		base := strings.Split(defined.Text, ".")[0]
		// Cipher's encoded card is deliberately captured by its damage trigger:
		// unlike DigUntil's captured event roles, it IS the Play population.
		if (base == "Remembered" || base == "RememberedLKI" || base == "RememberedCard" || base == "DirectRemembered") &&
			!strings.EqualFold(sa.ParamStr(cards.PKCipherCopy), "True") &&
			!strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKCopyCard)), "True") {
			var kept []state.ObjID
			for _, id := range candidates {
				captured := false
				for _, t := range c.Captured {
					if t.Obj == id {
						captured = true
						break
					}
				}
				if !captured {
					kept = append(kept, id)
				}
			}
			candidates = kept
		}
	} else {
		// Population by Valid$ + ValidZone$.
		valid := strings.TrimSpace(sa.ParamStr(cards.PKValid))
		statedValid := valid != ""
		if valid == "" {
			valid = "Card"
		}
		var zones []state.Zone
		if z := strings.TrimSpace(sa.ParamStr(cards.PKValidZone)); z != "" {
			for part := range strings.SplitSeq(z, ",") {
				if zn, ok := ZoneFromString(strings.TrimSpace(part)); ok {
					zones = append(zones, zn)
				}
			}
			if len(zones) == 0 {
				// A PRESENT but unparseable ValidZone$ stays fail-closed.
				return
			}
		} else if statedValid {
			// Forge's default is the resolving controller's hand. Two filters
			// explicitly bind a revealed non-controller population: remembered
			// cards (My Wish Is Your Command) and the targeted player's cards
			// (Reversal of Fortune). Only those shapes may widen the hand walk;
			// every other Valid$ filter remains private to its controller.
			zones = []state.Zone{state.ZHand}
		} else {
			// NO population param at all: no ValidTgts$, no Defined$, no
			// Valid$ and no ValidZone$. The hand default belongs only to a
			// STATED population; applying it here would turn 13 raw corpus
			// lines whose bodies this effect does not implement
			// (CopyFromChosenName$/AnySupportedCard$/self-referent shapes --
			// syrix_carrier_of_the_flame, thunderblade_charge,
			// tibalt_the_chaotic, jhoira_of_the_ghitu_avatar, ...) from a
			// silent no-op into an offer of the controller's WHOLE hand.
			// They stay fail-closed until their own shapes are implemented.
			return
		}
		sc := c.SpecContext(c.Controller)
		for _, zn := range zones {
			// Exile and graveyard are public zones keyed by the card's OWNER;
			// a Play can legitimately read a card another seat's slice holds
			// (Rashmi and Ragavan's remembered exile of an OPPONENT's card),
			// so a public zone is scanned across every alive seat in seat
			// order and the spec's own ownership predicates decide -- the same
			// all-seats walk mayPlaySpellIds uses. A private zone (hand,
			// library) normally scans only the resolving controller's slice;
			// the hand widens only for the two explicit cross-seat filter
			// predicates above, after the chain has revealed that hand.
			seats := []state.PlayerID{c.Controller}
			if zn == state.ZHand && playValidReadsOtherHand(valid) {
				seats = nil
				for _, q := range g.AliveFrom(0) {
					seats = append(seats, q)
				}
			}
			if zn == state.ZExile || zn == state.ZGraveyard {
				seats = nil
				for _, q := range g.AliveFrom(0) {
					seats = append(seats, q)
				}
			}
			for _, q := range seats {
				for _, id := range g.Zone(zn, q) {
					if MatchesSpecCtx(g, valid, id, sc) {
						candidates = append(candidates, id)
					}
				}
			}
		}
	}

	// Dedupe while preserving order.
	seen := map[state.ObjID]bool{}
	var uniq []state.ObjID
	for _, id := range candidates {
		if id != 0 && !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	// ValidSA$ narrows the population to the card SHAPES the play may play
	// ("Spell" = a nonland card, "Instant,Sorcery", "Spell.cmcLE4", ...). It
	// is OR over comma tokens and AND over the + parts of each token; a
	// predicate the reader does not know fails that token closed, so an
	// unknown shape never widens the offer. A non-literal cmc bound RHS (the
	// cmcLTX/cmcLEX/cmcEQX spellings, 30 raw corpus lines) resolves through
	// the context's numeric-RHS resolver -- the same one the general filter
	// grammar's numericPred uses -- so Rashmi and Ragavan's
	// `Spell.cmcLTX` reads the resolution's SVar:X (Count$Valid
	// Artifact.YouCtrl) instead of failing every candidate closed.
	if spec := strings.TrimSpace(sa.ParamStr(cards.PKValidSA)); spec != "" {
		resolve := func(name string) (int32, bool) { return c.resolveNumericRHS(name) }
		var kept []state.ObjID
		for _, id := range uniq {
			if o := g.Obj(id); o != nil && o.Face() != nil && validSAOK(o.Face(), spec, resolve) {
				kept = append(kept, id)
			}
		}
		uniq = kept
	}
	candidates = uniq

	// WithTotalCMC$ is the cumulative mana-value budget over the played
	// cards (Invoke Calamity, Rod of Absorption, Primeval Spawn: "you may
	// cast up to N spells with total mana value M or less"), the exact
	// parameter effDig reads on its own window. A card whose own mana value
	// exceeds the budget can never be played, and the running sum of the
	// plays must not exceed it either; the mechanics mirror effDig's
	// (affordable filter, Decision.MaxSum + Option.Value on the wire). The
	// R-9 no-host stand-in below plays one card, so the affordable filter
	// alone bounds it. Absent the param the budget is 0 and every read is a
	// no-op, so a non-budget Play emits byte-identically. Present but
	// unresolvable degrades to budget 0 -- Num's documented convention.
	budget, hasBudget := NumResolved(h, c, sa, "WithTotalCMC", 0)
	if budget < 0 {
		budget = 0
	}
	if hasBudget {
		kept := make([]state.ObjID, 0, len(candidates))
		for _, id := range candidates {
			if manaValueOf(g, id) <= int(budget) {
				kept = append(kept, id)
			}
		}
		candidates = kept
	}

	// Controller$ (task param:api:Play.Controller): WHO the play ask is
	// posed to and whose cast the answer begins. The value resolves
	// through the ONE shared defined-player machinery (knownDefinedTargets
	// -> definedSpec), so every spelling the corpus writes on a Play --
	// You (125 raw lines, the dominant shape), Targeted/
	// TargetedController/TargetedPlayer (Word of Command), TriggeredPlayer
	// (Wild Evocation), TriggeredCardController (Possibility Storm),
	// RememberedController/RememberedOwner (Guff, Spell Queller),
	// ChosenPlayer (Allure of the Unknown) -- binds exactly the way the
	// rest of the engine binds it, and an unknown spelling is a fail-closed
	// no-op under one loud Note rather than a silent reroute to the
	// resolving controller: "who may play" has no safe default when the
	// named seat cannot be bound. The answer's own Option.Player carries the
	// resolved seat to the answer settle. An Amount$ Play offered to a player
	// takes that seat's first listed candidate only (measured
	// corpus-unreachable: no carrier combines Amount$ with a non-You
	// Controller$).
	playCtl := c.Controller
	if ctl := strings.TrimSpace(sa.ParamStr(cards.PKController)); ctl != "" {
		_, ok := knownDefinedTargets(h, c, ctl)
		if !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "Play cannot resolve its Controller$ (" + ctl + "); the play does not happen"})
			return
		}
		// knownDefinedTargets above preserves the fail-closed selector gate;
		// the shared player walk applies plain-Remembered semantics.
		ps := definedPlayerIDs(h, c, ctl)
		if len(ps) == 0 {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "Play cannot resolve its Controller$ (" + ctl + "); the play does not happen"})
			return
		}
		playCtl = ps[0]
	}

	// The CR 601.3 election filter (task play-prohibited-election): a
	// candidate the CantBeCast statics prohibit for the player WHO WILL CAST
	// (playCtl, which the Controller$ read above resolved -- Word of Command's
	// TargetedPlayer casts it, so its restrictions, not the resolving
	// controller's, decide) must not be OFFERED. The query is an optional Host
	// capability implemented by rules.Engine (castRestricted is rules-side:
	// effects cannot import rules); a host without it keeps the pre-existing
	// behaviour and the beginPlay legality recheck stays the sole gate.
	// An all-prohibited population then takes the ordinary empty-options
	// path: one loud Note, the resolution continues, the found card is
	// never begun (CR 601.3) and the effect's own tail (cascade bottoms it)
	// proceeds as it did on a decline.
	if ch, ok := h.(castProhibitedHost); ok {
		kept := make([]state.ObjID, 0, len(candidates))
		for _, id := range candidates {
			if !ch.CastProhibited(playCtl, id) {
				kept = append(kept, id)
			}
		}
		candidates = kept
	}

	// Optional$ True makes the whole ask declinable (Min 0: the empty answer
	// is a decline). Amount$ sizes the ask: the default (and an unparseable
	// value, which stays conservative) is one card; a literal N offers up to
	// N; "All" offers every candidate (measured corpus: 1 x56, All x68,
	// 2 x4, 3 x5, X/ChandraX x3 -- the X shapes fall back to one card rather
	// than risk an over-wide offer).
	min := 1
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKOptional)), "True") {
		min = 0
	}
	max := 1
	switch amt := strings.TrimSpace(sa.ParamStr(cards.PKAmount)); playAmountCodes.Code(amt) {
	case playAmountOne:
	case playAmountAll:
		max = len(candidates)
	default:
		if n, err := strconv.Atoi(amt); err == nil && n > 1 {
			max = n
		}
	}
	if max > len(candidates) {
		max = len(candidates)
	}
	if max < min {
		max = min
	}

	maxSum := 0
	if hasBudget {
		maxSum = int(budget)
	}
	d := &decision.Decision{Player: playCtl, Kind: decision.KModes,
		Min: min, Max: max, MaxSum: maxSum, Source: c.Source, ResumeKind: "play",
		ResumeSA: sa, Prompt: "Play a card from this zone",
	}
	for _, id := range candidates {
		label := "Play it"
		if o := g.Obj(id); o != nil && o.Face() != nil {
			label = "Play " + o.Face().Name
		}
		optValue := 0
		if hasBudget {
			// Only a budget Play carries a Value: Option.Value is omitempty, so
			// a non-budget Play's option list serialises byte-identically.
			optValue = manaValueOf(g, id)
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "mode",
			Label: label, Obj: id, Player: playCtl, Value: optValue})
	}
	if len(d.Options) == 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "Play found no card to play"})
		return
	}
	if _, ok := AskTape(h, d); ok {
		// Answered in place: the host's answer record began the plays
		// (rules' playAnswerSettle) and set c.PlayDone, so the PlayDone pass
		// finishes the effect.
		effPlay(h, c, sa)
		return
	}
	// Fuzz/no-engine host: play the first candidate deterministically (R-9).
	// PlayDone marks the answer consumed so the field is not left half-set.
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
		Text: "Play chose the first candidate (no engine host to ask)"})
	c.Play = candidates[0]
	c.PlayDone = true
}

// validSAOK reports whether the face passes a ValidSA$ spec: OR over the
// comma tokens, AND over the + parts of one token, and each part may be a
// dotted chain ("Spell.Instant") whose segments all have to hold. The
// vocabulary is the one the corpus' 253 ValidSA$ Play lines actually use:
// Spell (a nonland card), SpellAbility (no card-shape meaning on its own),
// Instant/Sorcery/Creature types, their non forms, the cmcLE/cmcLT/cmcEQ
// comparisons against a literal or a single-letter non-literal (X, Y --
// resolved through the context's numeric-RHS resolver, the SVar table and
// the paid {X}; an unresolvable one fails closed), and the pass-through markers MayPlaySource
// and YouOwn (provenance and ownership are already enforced by the
// population path). Any other segment fails its part closed.
func validSAOK(f *cards.Face, spec string, resolve func(string) (int32, bool)) bool {
	for tok := range strings.SplitSeq(spec, ",") {
		all := true
		sawPart := false
		for part := range strings.SplitSeq(tok, "+") {
			partOK := true
			for seg := range strings.SplitSeq(part, ".") {
				seg = strings.TrimSpace(seg)
				switch {
				case seg == "":
				case seg == "Spell":
					partOK = partOK && !f.IsLand()
				case seg == "SpellAbility", seg == "MayPlaySource", seg == "YouOwn":
				case seg == "Instant":
					partOK = partOK && f.IsInstant()
				case seg == "Sorcery":
					partOK = partOK && f.IsSorcery()
				case seg == "Creature":
					partOK = partOK && f.IsCreature()
				case CreatureTypeWords(seg):
					isSubtype := false
					for _, typ := range f.Types {
						if strings.EqualFold(typ, seg) {
							isSubtype = true
							break
						}
					}
					partOK = partOK && f.IsCreature() && isSubtype
				case seg == "nonCreature":
					partOK = partOK && !f.IsCreature()
				case seg == "nonLand":
					partOK = partOK && !f.IsLand()
				case strings.HasPrefix(seg, "cmcLE"), strings.HasPrefix(seg, "cmcLT"),
					strings.HasPrefix(seg, "cmcEQ"):
					n := -1
					num := seg[5:]
					if len(num) == 1 && num[0] >= 'A' && num[0] <= 'Z' {
						// X / Y: the context's numeric RHS (the resolution's SVar
						// table, the paid {X}, a published roll). Unresolvable
						// stays fail closed, the shape-never-matches contract.
						if resolve != nil {
							if v, ok := resolve(num); ok {
								n = int(v)
							}
						}
					} else if v, err := strconv.Atoi(num); err == nil {
						n = v
					}
					if n < 0 {
						partOK = false // X or unresolvable: fail closed
						break
					}
					switch {
					case strings.HasPrefix(seg, "cmcLE"):
						partOK = partOK && f.Cmc() <= int32(n)
					case strings.HasPrefix(seg, "cmcLT"):
						partOK = partOK && f.Cmc() < int32(n)
					default:
						partOK = partOK && f.Cmc() == int32(n)
					}
				default:
					partOK = false // unknown segment: fail closed
				}
			}
			sawPart = true
			all = all && partOK
		}
		if sawPart && all {
			return true
		}
	}
	return false
}

// ZoneFromString maps a Forge zone name to a state.Zone. Only the zones a
// Play effect actually scans are spelled out; ok is false (and z is zero,
// state.ZLibrary) for an unrecognised name so the caller scans nothing rather
// than scanning every zone.
func ZoneFromString(s string) (state.Zone, bool) {
	switch zoneFromStringCodes.Code(string(s)) {
	case zoneFromStringExile:
		return state.ZExile, true
	case zoneFromStringGraveyard:
		return state.ZGraveyard, true
	case zoneFromStringHand:
		return state.ZHand, true
	case zoneFromStringLibrary:
		return state.ZLibrary, true
	case zoneFromStringBattlefield:
		return state.ZBattlefield, true
	case zoneFromStringCommand:
		return state.ZCommand, true
	}
	return 0, false
}

type playValidReadsOtherHandCode uint16

const (
	playValidReadsOtherHandPlayerRef playValidReadsOtherHandCode = iota + 1
)

var playValidReadsOtherHandCodes = state.NewStrCodes(
	state.StrEntry[playValidReadsOtherHandCode]{Key: "isremembered", Val: playValidReadsOtherHandPlayerRef},
	state.StrEntry[playValidReadsOtherHandCode]{Key: "targetedplayerctrl", Val: playValidReadsOtherHandPlayerRef},
)

type zoneFromStringCode uint16

const (
	zoneFromStringExile zoneFromStringCode = iota + 1
	zoneFromStringGraveyard
	zoneFromStringHand
	zoneFromStringLibrary
	zoneFromStringBattlefield
	zoneFromStringCommand
)

var zoneFromStringCodes = state.NewStrCodes(
	state.StrEntry[zoneFromStringCode]{Key: "Exile", Val: zoneFromStringExile},
	state.StrEntry[zoneFromStringCode]{Key: "Graveyard", Val: zoneFromStringGraveyard},
	state.StrEntry[zoneFromStringCode]{Key: "Hand", Val: zoneFromStringHand},
	state.StrEntry[zoneFromStringCode]{Key: "Library", Val: zoneFromStringLibrary},
	state.StrEntry[zoneFromStringCode]{Key: "Battlefield", Val: zoneFromStringBattlefield},
	state.StrEntry[zoneFromStringCode]{Key: "Command", Val: zoneFromStringCommand},
)

type playAmountCode uint16

const (
	playAmountOne playAmountCode = iota + 1
	playAmountAll
)

var playAmountCodes = state.NewStrCodes(
	state.StrEntry[playAmountCode]{Key: "", Val: playAmountOne},
	state.StrEntry[playAmountCode]{Key: "1", Val: playAmountOne},
	state.StrEntry[playAmountCode]{Key: "All", Val: playAmountAll},
)
