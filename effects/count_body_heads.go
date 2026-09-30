package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// evalCountBodyObjHeads evaluates the object-side prefix heads
// (DifferentCounterKinds_, CardCounters., Kicked., PromisedGift.,
// Foretold., NotedNumber, UrzaLands., ThisTurnEntered_).
func evalCountBodyObjHeads(h Host, c *Ctx, g *state.Game, head, arg string, depth int) (int32, bool, bool) {
	// DifferentCounterKinds_<spec> counts distinct real counter kinds over
	// matching battlefield objects. These are the three corpus selectors;
	// other spellings are unreadable, not an evaluated zero.
	if spec, ok := strings.CutPrefix(head, "DifferentCounterKinds_"); ok {
		if arg != "" || (spec != "Card.Self" && spec != "Creature.YouCtrl" && spec != "Permanent.YouCtrl") {
			return 0, false, true
		}
		kinds := make(map[string]bool)
		for i := range g.Objs {
			o := &g.Objs[i]
			if o.Zone != state.ZBattlefield || !matchesZoneSpecCtx(g, spec, o.ID, c.SpecContext(c.Controller), state.ZBattlefield) {
				continue
			}
			for _, counter := range o.Counters {
				if counter.N > 0 && !state.InternalCounterMarker(counter.Kind) {
					kinds[counter.Kind] = true
				}
			}
		}
		return int32(len(kinds)), true, true
	}

	// CardCounters.<KIND> counts a counter kind on the source; ALL is the
	// sum over every kind (Forge's CardCounters.ALL wildcard -- Denry Klin's
	// intervening-if gate, Kyler's and Warden of the Inner Sky's X), which a
	// literal Counter("ALL") lookup can never answer because no object ever
	// carries a counter KIND named ALL.
	if kind, ok := strings.CutPrefix(head, "CardCounters."); ok {
		if o := g.Obj(c.Source); o != nil {
			if strings.EqualFold(kind, "ALL") {
				return sumCounters(o.Counters), true, true
			}
			return o.Counter(kind), true, true
		}
		return 0, true, true
	}
	// Kicked.<yes>.<no> is <yes> when the source was kicked, else <no>.
	// A pending cast's announcement ask reads it BEFORE payment stamps the
	// stack object, so Ctx.PendingKicked (rules' targetBoundCtx binding) is
	// ORed with the object's FlagKicked; at resolution no pending cast exists
	// and the object read is authoritative.
	if rest, ok := strings.CutPrefix(head, "Kicked."); ok {
		yes, no := splitDot(rest)
		if c.PendingKicked {
			return yes, true, true
		}
		if o := g.Obj(c.Source); o != nil && o.CastFlags&state.FlagKicked != 0 {
			return yes, true, true
		}
		return no, true, true
	}
	// PromisedGift.<yes>.<no> is <yes> when the source's cast promised an
	// opponent a gift (CR 702.168), else <no> -- Forge's
	// Count$PromisedGift.2.1 family (Wear Down's destroy-two, Long River's
	// Pull's X/Y, Valley Rally's first strike). The read is Object.PromisedGift,
	// the SAME one home the PromisedGift filter predicate reads (folded by
	// events.GiftPromise), so the matcher and the count can never disagree. A
	// missing source, a card never cast, and a stack copy (whose fresh object
	// carries no promise) all read the <no> branch, the modelled-head
	// convention. A malformed body with a missing branch fails closed.
	if rest, ok := strings.CutPrefix(head, "PromisedGift."); ok {
		yes, no, found := strings.Cut(rest, ".")
		if !found || strings.TrimSpace(yes) == "" || strings.TrimSpace(no) == "" {
			return 0, false, true
		}
		promised := false
		if c.PromisedGiftOverride != nil {
			promised = *c.PromisedGiftOverride
		} else if o := g.Obj(c.Source); o != nil {
			promised = o.CastFlags&state.FlagPromisedGift != 0
		}
		tok := no
		if promised {
			tok = yes
		}
		return evalCountOperand(h, c, tok, depth), true, true
	}
	// Foretold.<ifTrue>.<ifFalse> is <ifTrue> when the resolving source was
	// cast foretold (CR 702.126a -- the pay-time FlagForetold provenance,
	// the same read Kicked makes), else <ifFalse>. The operands resolve
	// through the same operand machinery evalCompare's branches use (a
	// literal, or an SVar name resolved recursively -- Starnheim Unleashed's
	// Count$Foretold.X.1 reads the announced X through the face's SVar
	// table), with the same depth discipline. A carrier missing a branch is
	// a corpus bug: fail closed (0, false) rather than answer a half body.
	if rest, ok := strings.CutPrefix(head, "Foretold."); ok {
		yes, no, found := strings.Cut(rest, ".")
		if !found || strings.TrimSpace(yes) == "" || strings.TrimSpace(no) == "" {
			return 0, false, true
		}
		foretold := false
		if o := g.Obj(c.Source); o != nil {
			foretold = o.CastFlags&state.FlagForetold != 0
		}
		if foretold {
			return evalCountOperand(h, c, yes, depth), true, true
		}
		return evalCountOperand(h, c, no, depth), true, true
	}
	// NotedNumber is the number a trigger's Execute$ body last noted onto
	// the source card (DB$ Pump | NoteNumber$ <expr> -- Lupine Harbingers'
	// exile trigger noting Count$YourTurns). Read off the card the ETB
	// replacement resolves over (c.Source), the same object events.NotedNumber
	// wrote; a card with no note reads 0.
	if head == "NotedNumber" {
		if o := g.Obj(c.Source); o != nil {
			return o.NotedNumber, true, true
		}
		return 0, true, true
	}
	// UrzaLands.<assembled>.<not assembled> is <assembled> when the controller
	// controls at least one of each Urza land subtype on the battlefield
	// (Urza's Mine, Urza's Tower, Urza's Power-Plant), else <not assembled>.
	// The subtypes are matched on the Types line, where Power-Plant is
	// hyphenated -- the card Name's "Urza's Power Plant" is a different
	// string and matching it would be exactly the defect this head fixes.
	if rest, ok := strings.CutPrefix(head, "UrzaLands."); ok {
		assembled, notAssembled := splitDot(rest)
		if controlsAllUrzaLands(g, c.Controller) {
			return assembled, true, true
		}
		return notAssembled, true, true
	}

	// Count$ThisTurnEntered_<Dest>[_from_<Origin>]_<Valid> counts the cards
	// ADDED to zone <Dest> this turn (optionally only those that came from
	// <Origin>) matching <Valid> -- Forge's CardUtil.getThisTurnEntered over
	// the per-zone getCardsAddedThisTurn lists. The list is state.Entered,
	// appended once per move by events.Move and cleared at TurnChange, so a
	// replay folds the identical count. A card listed twice (it entered the
	// zone twice) counts twice, exactly like Forge's per-add list; a card
	// that has since moved on is still listed and its validity is evaluated
	// against the object wherever it now lives -- the same live-card read
	// Forge's getValidCards applies to the zone list. Gravelighter's "draw a
	// card if a creature died this turn" is the corpus carrier.
	if rest, ok := strings.CutPrefix(head, "ThisTurnEntered"); ok && strings.HasPrefix(rest, "_") {
		if arg != "" {
			rest += " " + arg
		}
		av, aok := evalThisTurnEntered(g, c, rest[1:])
		return av, aok, true
	}
	return 0, false, false
}

// evalCountBodyDotted evaluates Forge's generic dotted yes/no branch heads
// (Count$<Predicate>.<yes>.<no>): the fuzz-cov3 branch predicates, the
// wasCastFrom* provenance bits, Morbid/Monarch/Blessing and the devotion
// spellings.
func evalCountBodyDotted(h Host, c *Ctx, g *state.Game, head, arg string, depth int) (int32, bool, bool) {
	// Count$<Predicate>.<yes>.<no> — Forge's yes/no branch heads: the value
	// is the first number when the predicate holds, the second when it does
	// not (Count$Morbid.1.0 ×33 and Count$Monarch.1.0 ×10 are the corpus's
	// dominant spellings). wasCastFromGraveyard is modelled below — the
	// resolving source's graveyard-origin cast bits (the Increasing cycle's
	// Count$wasCastFromGraveyard.10.5, 11 corpus lines); the remaining
	// exotic predicates — Delirium, Void, Adamant_<n>.<colour> —
	// stay unmodelled and degrade to zero (Blessing is read below off the
	// CR 702.131 latch). Morbid is
	// CR 702.53's "a creature died this turn": a creature entered a graveyard
	// FROM THE BATTLEFIELD this turn, folded off the same state.Entered list
	// ThisTurnEntered_ reads (a battlefield→graveyard MoveZone is exactly a
	// death, sacrifice included), so a replay derives the identical answer.
	// Monarch is the resolving controller's current designation (the same
	// state g.IsMonarch answers for a CheckDefinedPlayer$ .isMonarch spec).
	if dot := strings.IndexByte(head, '.'); dot > 0 {
		if holds, known := cov3BranchHolds(h, c, head[:dot], depth); known {
			// The fuzz-cov3 branch predicates (Delirium, Metalcraft,
			// Hellbent, FatefulHour, Landfall, Void): branch tokens through
			// countBranchOperand, the Threshold sibling's read.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			return countBranchOperand(h, c, holds, yesTok, noTok, depth), true, true
		}
		switch head[:dot] {
		case "wasCastFromGraveyard":
			// The resolving source was CAST FROM A GRAVEYARD (CR 601.2b's
			// alternative-cost provenance): any graveyard-origin cast bit —
			// FlagFlashback, FlagHarmonize or FlagEscaped — holds it. This is
			// the same bit test the Card.wasCastFromGraveyard filter predicate
			// and its compiled twin share (effects/filter.go,
			// effects/compiled_predicate.go); a nil/missing source reads
			// false, and so does a stack copy (IsCopy — a copy was never cast,
			// even though StackCopy preserves the original's flags). The
			// branch tokens resolve through resolveCountOperand, not splitDot:
			// the_final_days' YES branch is the SVar X
			// (Count$wasCastFromGraveyard.X.2, X = Count$ValidGraveyard
			// Creature.YouCtrl), the Compare head's evalCountOperand recursion
			// precedent; an unresolvable token degrades to 0, never wedges.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			holds := state.ObjectWasCastFromGraveyard(g.Obj(c.Source))
			if holds {
				y, ok := resolveCountOperand(h, c, yesTok, depth)
				if !ok {
					y = 0
				}
				return y, true, true
			}
			n, ok := resolveCountOperand(h, c, noTok, depth)
			if !ok {
				n = 0
			}
			return n, true, true
		case "wasCastFromYourHandByYou":
			// The resolving source was cast from ITS OWN CONTROLLER's hand by
			// that controller (the Myojin cycle's etbCounter CheckSVar$ gate:
			// "enters with a divinity counter on it if you cast it from your
			// hand", 12 corpus carriers). An ordinary hand-origin cast carries
			// no CastFlags bit — the flags mark alternative costs and origins
			// only — so the provenance is the object's latest PutOnStack
			// (Host.WasCastFromHandByYou's log scan, replay-derivable like
			// CastThisTurn); a copy was never cast, and a card never put on
			// the stack (cheated into play) reads false, the same guards the
			// wasCastFromGraveyard case takes. The branch tokens resolve
			// through resolveCountOperand, the same machinery.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			holds := false
			if o := g.Obj(c.Source); o != nil && !o.IsCopy {
				if h != nil {
					holds = h.WasCastFromHandByYou(c.Source, o.Controller)
				}
			}
			if holds {
				y, ok := resolveCountOperand(h, c, yesTok, depth)
				if !ok {
					y = 0
				}
				return y, true, true
			}
			n2, ok := resolveCountOperand(h, c, noTok, depth)
			if !ok {
				n2 = 0
			}
			return n2, true, true
		case "wasCastFromYourHand":
			// The BARE (no "ByYou") hand-provenance branch head (task
			// castprov3, see_the_truth's SVar:X:Count$wasCastFromYourHand.1.3 —
			// "put one of those cards into your hand ... If this spell was cast
			// from anywhere other than your hand, put each of those cards into
			// your hand instead"): the resolving source's latest cast came from
			// a hand — ANY caster's hand, the player scoping the ByYou twin
			// carries being absent here. The same guards the ByYou case takes:
			// the provenance is the object's latest PutOnStack
			// (Host.WasCastFromHand's log scan, replay-derivable), a copy was
			// never cast, a card never put on the stack (cheated into play)
			// reads false. Branch tokens through resolveCountOperand, the same
			// machinery.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			holds := false
			if o := g.Obj(c.Source); o != nil && !o.IsCopy {
				holds = h.WasCastFromHand(c.Source)
			}
			if holds {
				y, ok := resolveCountOperand(h, c, yesTok, depth)
				if !ok {
					y = 0
				}
				return y, true, true
			}
			n3, ok := resolveCountOperand(h, c, noTok, depth)
			if !ok {
				n3 = 0
			}
			return n3, true, true
		case "wasCastFromExile":
			// The resolving source was CAST FROM EXILE (task wascastfrom; the
			// delayed_blast_fireball `Count$wasCastFromExile.5.2`,
			// lifestreams_blessing `.2.0` and the ultimate_magic `.1.0`
			// carriers): the CR 601.2b provenance of an exile-origin cast —
			// foretell, warp, may-play — which carries no CastFlags bit (the
			// flags mark alternative costs and origins only), so the read is
			// the object's latest PutOnStack (Host.WasCastFromExile's log
			// scan, replay-derivable), the same discipline the hand branch
			// heads take: a copy was never cast, and a card never put on the
			// stack (cheated into play) reads false. Branch tokens resolve
			// through resolveCountOperand, the same machinery.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			holds := false
			if o := g.Obj(c.Source); o != nil && !o.IsCopy {
				if h != nil {
					holds = h.WasCastFromExile(c.Source)
				}
			}
			if holds {
				y, ok := resolveCountOperand(h, c, yesTok, depth)
				if !ok {
					y = 0
				}
				return y, true, true
			}
			nE, ok := resolveCountOperand(h, c, noTok, depth)
			if !ok {
				nE = 0
			}
			return nE, true, true
		case "IfCastInOwnMainPhase", "InOwnMainPhase":
			// CR "if you cast this spell during your main phase": the
			// yes/no branch head Forge's AbilityUtils reads as
			// Count$IfCastInOwnMainPhase.<numMain>.<numNotMain> (7 corpus
			// carriers: Return to Dust's TargetMax$ X, Might of Old
			// Krosa's NumAtt$/NumDef$, Haunting Hymn's and Careful
			// Consideration's NumCards$, Sulfurous Blast's and Summary
			// Judgment's NumDmg$). The reading is LIVE, matching
			// Forge's game.getPhaseHandler(): the current step must be a
			// main phase and the active player the resolving controller
			// -- NOT a stamp of the cast's phase, which would diverge
			// from Forge (a spell cast in a main phase but resolved in
			// another reads the resolution phase). The two spellings
			// differ only in the third conjunct: IfCastInOwnMainPhase
			// additionally requires the source to have been CAST (Forge
			// c.wasCast(), the Host.WasCast read -- the pending CR 601.2c
			// announcement ask counts, since Forge sets castFrom before
			// setupTargets), while bare InOwnMainPhase (Dose of Dawnglow's
			// Count$InOwnMainPhase.0.1 blight gate) skips that conjunct.
			// Branch tokens resolve through resolveCountOperand, the
			// sibling cases' machinery; a missing/invalid token degrades
			// to 0, never wedges.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			holds := g.Step.IsMain() && g.Active == c.Controller
			if holds && head[:dot] == "IfCastInOwnMainPhase" {
				// A copy was never cast (the sibling provenance cases' IsCopy
				// guard); an absent source reads false too. The rules-side
				// WasCast applies the same IsCopy guard, but the count head is
				// reachable with a synthetic Host, so the guard lives here.
				holds = false
				if o := g.Obj(c.Source); o != nil && !o.IsCopy {
					holds = h != nil && h.WasCast(c.Source)
				}
			}
			if holds {
				y, ok := resolveCountOperand(h, c, yesTok, depth)
				if !ok {
					y = 0
				}
				return y, true, true
			}
			n, ok := resolveCountOperand(h, c, noTok, depth)
			if !ok {
				n = 0
			}
			return n, true, true
		case "Morbid", "Monarch":
			y, n := splitDot(head[dot+1:])
			holds := false
			if head[:dot] == "Monarch" {
				holds = g.IsMonarch(c.Controller)
			} else {
				for _, en := range g.Entered {
					if en.To != state.ZGraveyard || en.From != state.ZBattlefield {
						continue
					}
					if o := g.Obj(en.Obj); o != nil && hasType(o, "Creature") {
						holds = true
						break
					}
				}
			}
			if holds {
				return y, true, true
			}
			return n, true, true
		case "Revolt":
			// CR 702.38's branch head (the corpus's two carriers: Lifecraft
			// Cavalry's SVar:Revolt:Count$Revolt.1.0 etbCounter gate and
			// Fatal Push's Count$Revolt.4.2 destroy bound): <yes> when a
			// permanent the resolving CONTROLLER controlled left the
			// battlefield this turn, else <no> -- the same Host predicate
			// the bare Condition$ Revolt gate and the rules-side Revolt$
			// clauses share, so the spellings cannot drift apart. Literal
			// branches, the Morbid/Monarch precedent.
			y, n := splitDot(head[dot+1:])
			if h.RevoltHolds(c.Controller) {
				return y, true, true
			}
			return n, true, true
		case "Blessing":
			// CR 702.131's city's-blessing branch head (10 corpus carriers:
			// Golden Demise's SVar:X:Count$Blessing.1.0 pump fork, Kumena's
			// Awakening's TrigDraw, Expel from Orazca, Anduril/Pride of
			// Conquerors' .2.1, Secrets of the Golden City's .3.2 and the
			// .0.1 "unless you have it" forks). <yes> when the resolving
			// CONTROLLER holds the one-way state.Player.Blessing latch that
			// events.Apply's BlessingChange fold writes (rules/ascend.go
			// grants it), else <no> -- the SAME bit the bare Condition$
			// Blessing gate (effects/conditions.go) and the Activation$
			// Blessing offer gate (rules/legal.go) read, so the three
			// spellings cannot drift apart. Literal-branch read via
			// splitDot, the Revolt/Morbid precedent; an out-of-range
			// controller denies, the fail-closed direction its siblings take.
			y, n := splitDot(head[dot+1:])
			if int(c.Controller) < len(g.Players) && g.Players[c.Controller].Blessing {
				return y, true, true
			}
			return n, true, true
		case "Threshold":
			// CR 702.24's Threshold branch head (7 corpus carriers: Cabal
			// Ritual's Count$Threshold.5.3 mana ritual, Thermal Blast and
			// Swirling Sandstorm's .5.x/.5.0 damage, Far Wanderings' .3.1
			// search, Grizzly Fate's .4.2 tokens, Shower of Coals' and
			// Patriarch's Desire's .4.2): <yes> when the resolving CONTROLLER
			// has Threshold active -- seven or more cards in their graveyard
			// (CR 702.24a's latch-free active-while read, the same seven the
			// Threshold keyword statics share) -- else <no>. The graveyard is
			// a plain zone read (event-backed), so a replay derives the same
			// branch. Branch tokens resolve through resolveCountOperand (the
			// wasCastFromGraveyard precedent -- a literal, an SVar name or an
			// inline expression; an unresolvable token degrades to 0, never
			// wedges), and an out-of-range controller denies, the same
			// fail-closed direction the Blessing sibling takes.
			yesTok, noTok, _ := strings.Cut(head[dot+1:], ".")
			inGrave := false
			if int(c.Controller) >= 0 && int(c.Controller) < len(g.Players) {
				inGrave = len(g.Zone(state.ZGraveyard, c.Controller)) >= 7
			}
			return countBranchOperand(h, c, inGrave, yesTok, noTok, depth), true, true
		case "Devotion":
			// CR 700.5's devotion head (49 corpus carriers: Aspect of Hydra's
			// Count$Devotion.Green pump, Gray Merchant of Asphodel's .Black
			// drain, Nykthos' four .Chosen lines behind a ChooseColor): the
			// number of mana symbols of ONE colour among the mana costs of the
			// permanents the resolving CONTROLLER controls (devotionCount
			// below). <Colour> is a Forge colour word or WUBRG letter
			// (colourLetter); the corpus prints only the five words plus the
			// Chosen spelling, which reads the source object's own recorded
			// colour choice (events.Choose's "color" fold -- effChooseColor's
			// deterministic "W" fallback included) so the count and the ask
			// that binds it can never disagree; an unchosen or unreadable
			// colour fails closed to the unresolvable verdict, never to a
			// fake zero.
			col := colourLetter(head[dot+1:])
			if col == 0 {
				if !strings.EqualFold(head[dot+1:], "Chosen") {
					return 0, false, true
				}
				src := g.Obj(c.Source)
				if src == nil {
					return 0, false, true
				}
				col = colourLetter(src.ChosenColor)
				if col == 0 {
					return 0, false, true
				}
			}
			return devotionCount(g, c.Controller, col), true, true
		case "DevotionDual":
			// CR 700.5's two-colour devotion head (14 corpus carriers: Mogis'
			// Count$DevotionDual.Black.Red drain and the DevotionDual spellings
			// the temples/god cycle print): the SUM of the devotion to both
			// named colours -- each hybrid symbol counts once toward EACH of
			// its colours, so the sum is the oracle's "devotion to <A> and
			// <B>". An unreadable colour fails closed to the unresolvable
			// verdict, the Devotion sibling's read.
			aTok, bTok, found := strings.Cut(head[dot+1:], ".")
			ca, cb := colourLetter(aTok), colourLetter(bTok)
			if !found || ca == 0 || cb == 0 {
				return 0, false, true
			}
			return devotionCount(g, c.Controller, ca) + devotionCount(g, c.Controller, cb), true, true
		}
	}
	return 0, false, false
}
