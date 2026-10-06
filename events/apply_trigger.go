// Event folds for trigger and ability pushes onto pending queues.
//
// Split out of events/apply.go: code moved verbatim, no behaviour
// change. Each fold function is the body of the matching case in
// Apply (g, e) switch; see apply.go for the dispatch.
package events

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// foldTriggerPush folds Kind TriggerPush into state.
func foldTriggerPush(g *state.Game, e *Event) {
	// Ruling T20-a: the ability object is minted here, inside Apply, so
	// a log-only replay creates the exact same object a live game did --
	// not via a direct, unlogged AddObject call from rules.Engine. e.Obj
	// names the permanent whose trigger fired; a permanent that no
	// longer exists, or an out-of-range trigger index (stale data, a
	// tampered log), degrades to a no-op rather than panicking.
	//
	// Ruling T20-e: Player must be checked too, same as every sibling
	// case in this switch that indexes a Player-keyed slice (LifeChange,
	// TurnChange, Priority, ManaAdd, ManaClear). This case skipped it,
	// and an out-of-range Player flows straight into g.AddObject below,
	// then into Move's zoneOwner/SetZone/zoneIndex path, which indexes
	// g.zones (sized numZones*len(g.Players) at NewGame) with no bounds
	// check of its own -- panic: index out of range. Unreachable via
	// ordinary self-play (Player is always sourced from a real object's
	// controller) but directly reachable replaying an external,
	// corrupted, or tampered log -- exactly the case this event exists
	// to support.
	if !validPlayer(g, e.Player) {
		return
	}
	src := g.Obj(e.Obj)
	if src == nil {
		return
	}
	f := src.Face()
	if f == nil || e.Amount < -1 || int(e.Amount) >= len(f.Triggers) {
		return
	}
	o := g.AddObject(nil, e.Player)
	// Move first (it resets Remembered, among other stack-only fields,
	// for anything other than a battlefield destination -- see Move's
	// own default case below), then set what the ability actually
	// carries. Setting these before Move would have them wiped by that
	// same reset; ordering them after is what makes Remembered actually
	// survive onto the stack (Ruling T20-c).
	Move(g, o.ID, state.ZLibrary, state.ZStack)
	if e.Amount == -1 {
		// A layer-granted Dethrone has no printed Trigger index. The
		// matcher already established its condition; this logged sentinel
		// carries the fixed keyword body through replay.
		o.Ability = &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{
			"Defined": "Self", "CounterType": "P1P1", "CounterNum": "1",
		}}
	} else {
		o.Ability = f.Triggers[e.Amount].Effect
	}
	o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
	o.Source = e.Obj
	// FL-41: an id in IDs is either a real object (the ordinary case)
	// or a player reference (state.PlayerRef, rules.pushTrigger) --
	// triggerRemembered's DeclareAttackers case appends the defending
	// player to Remembered, and IDs has no field of its own for a bare
	// PlayerID, so that entry travels here encoded rather than as a bare
	// ObjID that would decode as "object 0": playersOf (effects/context.go)
	// filters that entry out, so Defined$ TriggeredDefendingPlayer would
	// resolve to nothing and the effect silently no-op.
	o.Remembered = rememberedFrom(e.IDs)
}

// foldKeywordTriggerPush folds Kind KeywordTriggerPush into state.
func foldKeywordTriggerPush(g *state.Game, e *Event) {
	if !validPlayer(g, e.Player) {
		return
	}
	src := g.Obj(e.Obj)
	if src == nil || src.Face() == nil {
		return
	}
	sa := cards.ResolveSVar(src.Face().SVars, e.Counter)
	conspire := false
	cipher := false
	casualty := false
	demonstrate := false
	flanking := false
	gift := e.Counter == "GiftAbility"
	melee := e.Counter == "__kwMeleeGranted"
	if sa == nil {
		// A granted Melee instance has no printed SVar. Rebuild its
		// pump from the logged marker; IDs holds one player ref per
		// opponent attacked in the triggering declaration.
		if e.Counter == "__kwMeleeGranted" {
			sa = &cards.SA{Kind: "DB", API: "Pump", Params: map[string]string{
				"Defined": "Self", "NumAtt": cards.MeleePumpCount, "NumDef": cards.MeleePumpCount}}
		}
		// A granted ward (rules.pushTrigger's __kwWard: payload) has no
		// SVar to resolve: the ability is rebuilt structurally from the
		// payload -- the same DB$ Ward | UnlessCost$ <cost> a printed
		// K:Ward's compiled trigger carries -- so the live game and the
		// replay mint identical objects from the event text alone.
		if rest, ok := strings.CutPrefix(e.Counter, "__kwWard:"); ok {
			sa = &cards.SA{Kind: "DB", API: "Ward",
				Params: map[string]string{"UnlessCost": rest, "TriggerDescription": "Ward"}}
		}
		// A granted afflict (rules.pushTrigger's __kwAfflict: payload) has
		// no SVar either: rebuilt structurally into the same
		// DB$ LoseLife | Defined$ TriggeredDefendingPlayer body the printed
		// K:Afflict expansion carries, so live game and replay mint
		// identical objects from the event text alone.
		if rest, ok := strings.CutPrefix(e.Counter, "__kwAfflict:"); ok {
			sa = &cards.SA{Kind: "DB", API: "LoseLife",
				Params: map[string]string{"Defined": "TriggeredDefendingPlayer", "LifeAmount": rest,
					"TriggerDescription": "Afflict"}}
		}
		// A granted Conspire (rules.pushTrigger's __kwConspire: payload)
		// has no SVar either: rebuilt structurally into the same
		// DB$ CopySpellAbility body the printed K:Conspire expansion
		// carries, so the live game and the replay mint identical
		// objects from the event text alone. The triggering spell rides
		// Remembered (IDs) -- Defined$ TriggeredSpellAbility reads it
		// there, exactly as the printed expansion's own TriggerPush
		// entries carry it. The trailing colon (the Ward/Afflict shape)
		// keeps the payload distinct from the "__kwConspire" SVar a
		// printed bare K:Conspire line mints.
		if _, ok := strings.CutPrefix(e.Counter, "__kwConspire:"); ok {
			sa = &cards.SA{Kind: "DB", API: "CopySpellAbility",
				Params: map[string]string{"Defined": "TriggeredSpellAbility", "Amount": "Count$Conspired",
					"MayChooseTarget": "True"}}
			conspire = ok
		}
		// A CASUALTY cast trigger (rules.pushTrigger's __kwCasualty:
		// payload) has no SVar either: rebuilt structurally into the same
		// DB$ CopySpellAbility | Defined$ TriggeredSpellAbility body the
		// printed K:Casualty expansion cannot carry (the keyword is read
		// directly by the cast flow, not expanded). The triggering spell
		// rides IDs as Remembered. The payload after the colon is the
		// StackCopyCounter grammar: the Casualty:X carrier's script riders
		// (Ob Nixilis, the Adversary) -- the copy isn't legendary and its
		// starting loyalty is the casualty amount -- which the live game
		// and the replay mint identically from the event text alone. The
		// trailing colon keeps the payload from aliasing the "__kwCasualty"
		// SVar a printed bare K:Casualty line mints.
		if rest, ok := strings.CutPrefix(e.Counter, "__kwCasualty:"); ok {
			sa = &cards.SA{Kind: "DB", API: "CopySpellAbility",
				Params: map[string]string{"Defined": "TriggeredSpellAbility", "MayChooseTarget": "True"}}
			cc := ParseStackCopyCounter(rest)
			if cc.NonLegendary {
				sa.Params["NonLegendary"] = "True"
			}
			if cc.HasLoyalty {
				sa.Params["SetLoyalty"] = strconv.Itoa(int(cc.Loyalty))
			}
			casualty = ok
		}
		// A CIPHER combat-damage trigger (rules.pushTrigger's __kwCipher:
		// payload) has no SVar either -- the association that grants it is
		// state.Object.EncodedCards -- so it is rebuilt structurally into
		// the same body the printed expansion cannot carry: a free cast of
		// a COPY of the encoded card (CR 702.99b). The encoded card rides
		// IDs as Remembered, which Defined$ Remembered resolves; CopyCard$
		// True makes that a copy on the stack while the exiled original
		// stays put, and Optional$ True makes the cast the "may". The
		// trailing colon (the Ward/Afflict/Conspire shape) keeps the
		// payload distinct from the "__kwCipher" SVar a printed bare
		// K:Cipher line mints.
		if _, ok := strings.CutPrefix(e.Counter, "__kwCipher:"); ok {
			sa = &cards.SA{Kind: "DB", API: "Play",
				Params: map[string]string{"Defined": "Remembered", "ValidZone": "Exile",
					"ValidSA": "Spell", "CopyCard": "True", "CipherCopy": "True", "WithoutManaCost": "True",
					"Optional": "True", "TriggerDescription": "Cipher"}}
			cipher = true
		}
		// A granted Demonstrate (rules.pushTrigger's __kwDemonstrate:
		// payload) has no SVar either: rebuilt structurally into the same
		// DB$ Demonstrate body the printed K:Demonstrate expansion
		// carries (cards/kw_demonstrate.go), so the live game and the
		// replay mint identical objects from the event text alone. The
		// may-copy election and the opponent choice are the body's own
		// asks (effects/demonstrate.go); the triggering spell rides
		// Remembered (IDs) -- Defined$ TriggeredSpellAbility reads it
		// there, exactly as the printed expansion's own TriggerPush
		// entries carry it. The trailing colon (the Conspire shape)
		// keeps the payload distinct from the "__kwDemonstrate" SVar a
		// printed bare K:Demonstrate line mints.
		if _, ok := strings.CutPrefix(e.Counter, "__kwDemonstrate:"); ok {
			sa = &cards.SA{Kind: "DB", API: "Demonstrate",
				Params: map[string]string{"Defined": "TriggeredSpellAbility"}}
			demonstrate = ok
		}
		// A cascade trigger (rules.pushTrigger's __kwCascade: payload) has
		// no SVar either: rebuilt structurally into the DB$ Cascade body
		// both a printed K:Cascade line and every layer-6 AddKeyword$
		// Cascade grant share, so the live game and the replay mint
		// identical objects from the event text alone. The trigger's
		// Source (the cast spell) is what the effect reads its mana value
		// off at resolution (CR 702.85a's "costs less" comparison). The
		// trailing colon keeps the payload from aliasing a printed bare
		// K:Cascade line's "__kwCascade" SVar.
		if _, ok := strings.CutPrefix(e.Counter, "__kwCascade:"); ok {
			sa = &cards.SA{Kind: "DB", API: "Cascade",
				Params: map[string]string{"TriggerDescription": "Cascade"}}
		}
		// A granted flanking (rules.pushTrigger's __kwFlanking: payload) has
		// no SVar either: rebuilt structurally into the same
		// DB$ Pump | Defined$ TriggeredBlockerLKICopy | NumAtt$ -1 | NumDef$
		// -1 body the printed K:Flanking expansion carries
		// (cards/kw_flanking.go), so the live game and the replay mint
		// identical objects from the event text alone. The blocked creature
		// rides Remembered (IDs), exactly as the printed expansion's own
		// TriggerPush entries carry it. The trailing colon keeps the payload
		// from aliasing the "__kwFlanking" SVar a printed bare K:Flanking
		// line mints.
		if _, ok := strings.CutPrefix(e.Counter, "__kwFlanking:"); ok {
			sa = &cards.SA{Kind: "DB", API: "Pump",
				Params: map[string]string{"Defined": "TriggeredBlockerLKICopy", "NumAtt": "-1", "NumDef": "-1"}}
			flanking = ok
		}
		// A granted Firebending (rules.pushTrigger's
		// __kwFirebendingGranted:<N> payload) has no SVar either: rebuilt
		// structurally into the same DB$ Mana | Produced$ R | Amount$ <N>
		// | PersistentUntilEndOfCombat$ True body the printed K:Firebending
		// expansion carries (cards/kw_firebending.go), so the live game and
		// the replay mint identical objects from the event text alone.
		// The trigger has no target roles; the mana goes to the trigger's
		// controller (the KeywordTriggerPush's Player). The "Granted"
		// suffix keeps the payload from aliasing the "__kwFirebending:<N>"
		// SVar a printed K:Firebending line mints (the Exploit/Offspring
		// rule).
		if rest, ok := strings.CutPrefix(e.Counter, "__kwFirebendingGranted:"); ok {
			sa = &cards.SA{Kind: "DB", API: "Mana", Params: map[string]string{
				"Produced": "R", "Amount": rest, "PersistentUntilEndOfCombat": "True",
			}}
		}
		// A granted cumulative upkeep (rules.pushTrigger's
		// __kwCumulativeUpkeepGranted:<cost> payload) has no SVar either:
		// rebuilt structurally into the same DB$ CumulativeUpkeep |
		// Cost$ <cost> ability the printed K:Cumulative upkeep expansion
		// carries (cards/kw_cumulativeupkeep.go), so the live game and the
		// replay mint identical objects from the event text alone. The
		// "Granted" suffix and the trailing colon keep the payload from
		// aliasing the "__kw<keyword-line>" SVar a printed bare
		// K:Cumulative upkeep line mints (the Exploit/Offspring rule).
		// The cost rides Params["Cost"], which rules' startCumulativeUpkeep
		// reads at resolution.
		if rest, ok := strings.CutPrefix(e.Counter, "__kwCumulativeUpkeepGranted:"); ok {
			sa = &cards.SA{Kind: "DB", API: "CumulativeUpkeep",
				Params: map[string]string{"Cost": rest, "TriggerDescription": "Cumulative upkeep"}}
		}
		// A granted Exploit (rules.pushTrigger's __kwExploitGranted
		// payload) has no SVar either: rebuilt structurally into the same
		// DB$ Sacrifice | Optional$ True | SacValid$ Creature |
		// RememberSacrificed$ True -> DB$ Exploit chain the printed
		// K:Exploit expansion carries (cards/kw_exploit.go), so the live
		// game and the replay mint identical objects from the event text
		// alone. The Exploit body reads the sacrificed creature off
		// Ctx.Sacrificed, exactly as the printed chain does. The payload
		// is deliberately NOT the bare "__kwExploit": addKeywordTrigger
		// mints a printed bare K:Exploit line's SVar as "__kw"+line =
		// "__kwExploit", so the old spelling aliased a real printed-face
		// SVar. The SVar lookup above wins today, but a future caller
		// that pushed the bare payload for a face defining that SVar
		// would silently take the SVar path; "Granted" cannot collide
		// with any "__kw"+<keyword-line> mint.
		if _, ok := strings.CutPrefix(e.Counter, "__kwExploitGranted"); ok {
			sac := &cards.SA{Kind: "DB", API: "Sacrifice",
				Params: map[string]string{"Defined": "You", "Optional": "True", "SacValid": "Creature",
					"RememberSacrificed": "True"}}
			sac.Sub = &cards.SA{Kind: "DB", API: "Exploit",
				Params: map[string]string{"TriggerDescription": "Exploit"}}
			sa = sac
		}
		// A granted Offspring (rules.pushTrigger's __kwOffspringGranted
		// payload) has no SVar either: rebuilt structurally into the same
		// DB$ CopyPermanent | Defined$ Self | NumCopies$ Count$OffspringPaid
		// | SetPower$ 1 | SetToughness$ 1 body the printed K:Offspring
		// expansion carries (cards/kw_offspring.go), so the live game and
		// the replay mint identical objects from the event text alone. The
		// "Granted" suffix keeps the payload from aliasing a printed bare
		// K:Offspring line's "__kwOffspring" SVar (the Exploit comment's
		// rule).
		if _, ok := strings.CutPrefix(e.Counter, "__kwOffspringGranted"); ok {
			sa = &cards.SA{Kind: "DB", API: "CopyPermanent",
				Params: map[string]string{"Defined": "Self", "NumCopies": "Count$OffspringPaid",
					"SetPower": "1", "SetToughness": "1"}}
		}
		// A granted Mentor (rules.pushTrigger's __kwMentorGranted payload)
		// has no SVar either: rebuilt structurally into the same
		// DB$ PutCounter | ValidTgts$ Creature.attacking | Mentor$ True
		// targeted body the printed K:Mentor expansion carries
		// (cards/kw_mentor.go), so the live game and the replay mint
		// identical objects from the event text alone. The "Granted"
		// suffix keeps the payload from aliasing the "__kwMentor" SVar a
		// printed bare K:Mentor line mints (the Exploit/Offspring rule).
		// The Mentor$ marker rides the params, so rules' mentorAdmits reads
		// it at both the target offer and the CR 608.2b recheck either way.
		if _, ok := strings.CutPrefix(e.Counter, "__kwMentorGranted"); ok {
			sa = &cards.SA{Kind: "DB", API: "PutCounter",
				Params: map[string]string{"ValidTgts": "Creature.attacking",
					"TgtPrompt": "Select target attacking creature with lesser power",
					"Mentor":    "True", "CounterType": "P1P1", "CounterNum": "1"}}
		}
	}
	// The Gift trigger's payload (rules.pushTrigger's GiftAbility) DOES
	// resolve a real card SVar, but its promise referent must not read
	// the live source: the ability resolves independently of its source
	// (CR 112.7a), and events.Move clears the source's CastFlags bit and
	// GiftPromisedTo the moment the permanent leaves the battlefield --
	// the response window the trigger's own respondability creates. The
	// promised receiver rides the payload as Remembered (IDs), and the
	// body's Defined$/TokenOwner$ Promised referents are rewritten to
	// PromisedSnapshot -- effects' read of that snapshot -- so the live
	// game and a replay mint identical objects from the event text alone.
	// The rewrite CLONES the resolved SA: the SVar table is the card's
	// shared compiled data and Apply must never mutate it.
	if gift && sa != nil {
		clone := *sa
		clone.Params = make(map[string]string, len(sa.Params))
		for k, v := range sa.Params {
			if (k == "Defined" || k == "TokenOwner") && v == "Promised" {
				v = "PromisedSnapshot"
			}
			clone.Params[k] = v
		}
		sa = &clone
	}
	if sa == nil {
		return
	}
	incarnation := src.Incarnation
	o := g.AddObject(nil, e.Player)
	Move(g, o.ID, state.ZLibrary, state.ZStack)
	o.Ability = sa
	o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
	o.Source = e.Obj
	// The incarnation stamp arms resolveTop's source-incarnation gate,
	// which drops a keyword trigger whose effect names the SOURCE
	// PERMANENT (Evoke's "sacrifice it") once that permanent leaves and
	// returns as a new object (CR 400.7). It is armed ONLY for a trigger
	// that demonstrably acts on its source (keywordTriggerBindsSource): an
	// ability resolves independently of its source (CR 112.7a), so the
	// default is NO stamp. A granted Ward or Afflict, for example, acts on
	// the targeting spell / captured defender and must still resolve when
	// the granting permanent is removed in response to its own trigger
	// -- the response window that trigger's respondability creates. A
	// promised permanent's gift (CR 702.168c) is likewise independent: its
	// body acts on the promised player (snapshotted into Remembered), so
	// it must still deliver after the source is removed. Leaving the stamp
	// at its zero value makes the gate skip such a trigger exactly as it
	// does for an ordinary matched ETB trigger pushed by TriggerPush.
	if !gift && keywordTriggerBindsSource(e.Counter, sa) {
		o.SourceIncarnation = incarnation
	}
	if conspire || casualty || demonstrate || flanking || melee || cipher || gift {
		o.Remembered = rememberedFrom(e.IDs)
	}
}

// foldKeywordAbilityPush folds Kind KeywordAbilityPush into state.
func foldKeywordAbilityPush(g *state.Game, e *Event) {
	// A keyword-GRANTED activated ability (CR 613.1f): the mint mirrors
	// AbilityPush (Ruling T20-a) so a log-only replay creates the same
	// object a live game did, but the body is not a face index -- it is
	// SYNTHESIZED from the derived keyword line Counter carries
	// ("Cycling:1 U", "TypeCycling:Sliver:3", "Saddle:2", "Crew:1"),
	// exactly the synthesis the offer loop and pcAbility re-derive, so live
	// game and replay mint the identical ability. Obj is the activating
	// card and the minted object's Source (`Defined$ Self`/`CARDNAME`
	// names it); no registration is consumed, a grant lives exactly as
	// long as its granting static. A line no synthesizer can model, an
	// invalid
	// controller or a missing source mints nothing (the totality stance
	// every case here takes).
	if !validPlayer(g, e.Player) {
		return
	}
	src := g.Obj(e.Obj)
	if src == nil {
		return
	}
	sa := cards.GrantedKeywordAbility(e.Counter)
	if sa == nil {
		return
	}
	// AbilityPush's per-source activation census, same condition: only a
	// battlefield activation counts (a cycling activation is from the
	// hand, so this is the mirror rather than a live increment).
	if src.Zone == state.ZBattlefield {
		src.ActivatedThisTurn++
	}
	o := g.AddObject(nil, e.Player)
	Move(g, o.ID, state.ZLibrary, state.ZStack)
	o.Ability = sa
	o.StackKind, o.StackKindKnown = state.StackKindActivated, true
	o.Source = e.Obj
	o.Remembered = rememberedFrom(e.IDs)
}

// foldAbilityPush folds Kind AbilityPush into state.
func foldAbilityPush(g *state.Game, e *Event) {
	// Mirrors TriggerPush above (Ruling T20-a): the ability object is
	// minted here, inside Apply, so a log-only replay creates the same
	// object a live game did.
	if !validPlayer(g, e.Player) {
		return
	}
	src := g.Obj(e.Obj)
	if src == nil {
		return
	}
	// Amount is a FLAT pile-ability index: the top face's Abilities in
	// order, then each card merged beneath it (CR 702.140d). A plain
	// permanent's flat index is exactly its old top-face index, so no
	// existing event changes meaning; a mutated pile's under-card ability
	// decodes against the same folded MergedCards a replay rebuilt before
	// this push. Resolving through the pile is what makes an under-card
	// activation mint the under-card's SA rather than the top face's.
	pa, ok := src.PileAbilityAt(int(e.Amount))
	if !ok {
		return
	}
	// The per-source activation census (state/object.go's
	// ActivatedThisTurn): one AbilityPush per non-mana activation, folded
	// onto the source the same way the other per-turn object facts are.
	// Mutated BEFORE AddObject, per StackCopy's discipline below:
	// AddObject appends to g.Objs and may reallocate its backing array,
	// and src is a pointer into it (g.Obj returns &g.Objs[id-1]) -- a
	// src pointer mutated after the mint would write into the orphaned
	// old array whenever the append reallocated, silently dropping the
	// increment. A cloned engine's Objs slice is exactly full (Game.Clone
	// copies into len-sized storage), so its very next mint always
	// reallocates: the snapshot-derived view path lost every activation
	// the mint coincided with, while the from-genesis replay kept them
	// (the parked-overshoot view test's Shepherd of Rot divergence).
	if src.Zone == state.ZBattlefield {
		src.ActivatedThisTurn++
	}
	o := g.AddObject(nil, e.Player)
	Move(g, o.ID, state.ZLibrary, state.ZStack)
	o.Ability = pa.SA
	o.StackKind, o.StackKindKnown = state.StackKindActivated, true
	o.Source = e.Obj
	// Same PlayerRef decode as TriggerPush above (FL-41): an activated
	// ability can remember a player the same way a trigger can, so the
	// two mint paths stay symmetric through rememberedFrom.
	o.Remembered = rememberedFrom(e.IDs)
}

// foldMergedTriggerPush folds Kind MergedTriggerPush into state.
func foldMergedTriggerPush(g *state.Game, e *Event) {
	// CR 702.140d: a mutated pile's under-card trigger fired and its
	// ability object is minted here, inside Apply, so a log-only replay
	// creates the same object a live game did (the Ruling T20-a/DelayedPush
	// precedent). Unlike DelayedPush no registration is consumed -- the
	// grant-trigger shape -- and unlike both by-name siblings the ability
	// is minted from the UNDER-CARD's own COMPILED trigger, named by
	// e.Amount (the packed pair: its pile index in MergedCards, and that
	// face's Triggers index), exactly as TriggerPush mints from the top
	// face's. Not a by-name SVar walk, for two reasons: the pile's top
	// face may define the same Execute$ name with a different body
	// (Cubwarden's two Cats must not become Everquill Phoenix's Feather),
	// and cards.ResolveSVar parses a fresh *SA whose pointer matches no
	// compiled cards.Trigger -- which is how every consumer that recovers
	// a resolving ability's owning trigger (findTriggerForAbilityFace:
	// the OptionalDecider$ gate, the intervening-if recheck, the
	// ResolvedLimit$ count, the label, the merged-face SVar table)
	// identifies it. e.Counter carries the Execute$ name as the log's
	// readable provenance and is checked against the trigger line here,
	// so a truncated or tampered log mints nothing rather than the wrong
	// ability.
	if !validPlayer(g, e.Player) {
		return
	}
	src := g.Obj(e.Obj)
	if src == nil {
		return
	}
	mergedIdx, trigIdx, ok := MergedTriggerIndexes(e.Amount)
	if !ok {
		return
	}
	f := src.MergedFaceAt(mergedIdx)
	if f == nil || trigIdx >= len(f.Triggers) {
		return
	}
	tr := f.Triggers[trigIdx]
	if tr.Effect == nil || tr.Params["Execute"] != e.Counter {
		return
	}
	sa := tr.Effect
	o := g.AddObject(nil, e.Player)
	Move(g, o.ID, state.ZLibrary, state.ZStack)
	o.Ability = sa
	o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
	o.Source = e.Obj
	o.Remembered = rememberedFrom(e.IDs)
}
