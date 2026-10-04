package cards

// mayask.go is the text half of the resolution kernel's ask-free predicate
// (lasagna spec §7, W3 step 0; spike S3b candidate 1): a conservative
// judgement, from an ability's immutable text alone, of whether resolving it
// can pose a decision. rules' object half (rules/resolve_mayask.go) adds what
// the stack object knows and memoises the per-ability answer on its facts
// record. It is a PERFORMANCE HINT: the kernel catches every miss at its one
// ask choke point and falls back, so "true" is always safe and a wrong
// "false" costs a fallback, never an event.
//
// Every parameter in the ParamKey vocabulary is read through it. The
// presence-only checks in mayAskRawDeny name parameters outside the
// vocabulary (W4 owns it); each moves to mayAskDenyKeys when its key is
// added. They are
// presence tests, not reads that honour the parameter, so they are kept out
// of the rules parameter census on purpose: the predicate supports nothing.

import "strings"

// FaceOwnsSA reports whether sa is one of f's own ability or trigger bodies,
// so f is the face its Defined$ Self names and f.SVars its SVar table.
func FaceOwnsSA(f *Face, sa *SA) bool {
	for _, a := range f.Abilities {
		if a == sa {
			return true
		}
	}
	for i := range f.Triggers {
		if f.Triggers[i].Effect == sa {
			return true
		}
	}
	return false
}

// TriggerLineMayAsk reports whether a trigger line itself asks at
// resolution: an optional trigger ("you may"), or one with an unless or
// additional cost window.
func TriggerLineMayAsk(t Trigger) bool {
	return t.HasParam(PKOptionalDecider) || t.HasParam(PKOptional) ||
		t.HasParam(PKUnlessCost) || t.HasParam(PKCost)
}

// entryAskKeywords are keyword prefixes whose expansion asks as the
// permanent enters (an as-enters choice, a "may" or a pick of objects).
var entryAskKeywords = [...]string{
	"ETBReplacement", "Unleash", "Amplify", "Devour", "Bloodthirst", "Tribute",
	"Sunburst", "Graft", "Modular", "Riot", "Fabricate", "Backup", "Squad",
	"Offspring", "Read ahead", "Ravenous", "Champion", "Soulbond", "Mutate",
	"Bestow",
}

// FaceEntryMayAsk reports whether a permanent face's own text can ask as it
// enters (an as-enters choice, a "may enter as a copy", an entry replacement
// with a choice). Conservative: any R: line that is not a plain moved-body
// replacement the resolving-body allowlist accepts, and any entry keyword.
func FaceEntryMayAsk(f *Face) bool {
	for i := range f.Repls {
		r := &f.Repls[i]
		if r.Event == "Counter" {
			continue // a spell's countering, never part of its entry
		}
		if r.Event != "Moved" || r.With == nil {
			return true
		}
		if r.HasParam(PKOptional) || r.HasParam(PKOptionalDecider) {
			return true
		}
		// The replacement body (enters tapped, enters with counters) is held
		// to the same allowlist as a resolving body.
		if SAChainMayAsk(r.With, f.SVars, nil, false) {
			return true
		}
	}
	for _, kw := range f.Keywords {
		for _, p := range entryAskKeywords {
			if strings.HasPrefix(kw, p) {
				return true
			}
		}
	}
	return false
}

// FaceAttachMayAsk reports whether a face asks as it becomes attached: an
// R:Event$ Attached replacement (its body is an election).
func FaceAttachMayAsk(f *Face) bool {
	for i := range f.Repls {
		if f.Repls[i].Event == "Attached" {
			return true
		}
	}
	return false
}

// mayAskDenyKeys are parameters that pose a decision on any API:
// the optional, unless and chooser riders, divided or announced amounts, a
// counter-kind or "up to" pick.
var mayAskDenyKeys = [...]ParamKey{
	PKOptionalDecider, PKOptional, PKUnlessCost, PKUnlessSwitched,
	PKDividedAsYouChoose, PKChoiceZone, PKChooser, PKChoiceTitle, PKAnyNumber,
	PKAnnounce, PKChooseOrder, PKMode, PKChangeType, PKChangeNum,
	PKTargetingPlayer, PKTargetsWithDefinedController, PKAmount, PKImprint,
	PKStatic, PKUpTo,
	PKUnlessPayer, PKXChoice, PKTgtPrompt2, PKSorcerySpeed2, PKChoiceAmount, PKHidden, PKRememberChosen, PKChoose, PKDefinedPlayerChooses, PKChoiceNum, PKKWChoice, PKBolster, PKSupport, PKChooseCounter, PKCounterTypeChoice, PKUpto, PKPlayerChoices, PKAlternativeDecider, PKShuffleNonMandatory, PKUntapType, PKChooseDifferent, PKCounterTypePerDefined, PKPromptToSkipOptionalAbility, PKOptionalAbilityPrompt,
}

var mayAskDenyMask = ParamMaskOf(mayAskDenyKeys[:]...)

// saDenied reports whether s carries a deny-listed parameter.
func saDenied(s *SA) bool {
	if s.ps.bound(s.Params) {
		return s.MayHaveAnyParam(mayAskDenyMask)
	}
	for _, k := range mayAskDenyKeys {
		if s.HasParam(k) {
			return true
		}
	}
	return false
}

// askFreeAPI reports whether api's primitive poses no decision unless a
// denied parameter is present. Every API not listed is "may ask".
func askFreeAPI(api string) bool {
	_, ok := AskFreeAPIEvents(api)
	return ok
}

// ReplEventMask is a set of ReplEvents: bit k is ReplEvent(k).
type ReplEventMask uint32

// Has reports whether ev is in m.
func (m ReplEventMask) Has(ev ReplEvent) bool { return m&(1<<ev) != 0 }

func replMask(evs ...ReplEvent) ReplEventMask {
	var m ReplEventMask
	for _, ev := range evs {
		m |= 1 << ev
	}
	return m
}

// askFreeAPIEntry is one allowlisted API and the replaceable events its
// primitive -- and the moves it makes -- can propose while it resolves: the
// board gates (rules' replEventGates) an otherwise ask-free chain opens. A
// replacement on such an event can ask as it applies (an Optional$
// election, or two of them competing for one event: the CR 616.1 order
// choice), which text alone cannot see.
type askFreeAPIEntry struct {
	api    string
	events ReplEventMask
}

// askFreeAPITable is the allowlist, sorted by api (AskFreeAPIEvents binary
// searches it). An API with no events still names its decision: nothing it
// does is a replaceable event an R: line can name.
var askFreeAPITable = [...]askFreeAPIEntry{
	{"Animate", 0},
	{"Attach", replMask(ReplAttached)},
	{"ChangeZone", replMask(ReplMoved)},
	{"Charm", 0}, // its Choices$ bodies are walked themselves
	{"Cleanup", 0},
	{"CopyPermanent", replMask(ReplCreateToken, ReplMoved)},
	{"Counter", replMask(ReplCounter, ReplMoved)},
	// Damage to a player is life loss; lifelink gains life; infect, wither
	// and toxic place counters.
	{"DamageAll", replMask(ReplDamageDone, ReplLifeReduced, ReplGainLife, ReplAddCounter)},
	{"DealDamage", replMask(ReplDamageDone, ReplLifeReduced, ReplGainLife, ReplAddCounter)},
	{"Debuff", 0},
	{"Destroy", replMask(ReplMoved)},
	{"DestroyAll", replMask(ReplMoved)},
	// A draw is a Draw event (DrawCards is its alias: rules' replEventBit
	// shares the bit) and a library-to-hand move.
	{"Draw", replMask(ReplDraw, ReplMoved)},
	{"Effect", 0},
	{"Fight", replMask(ReplDamageDone, ReplLifeReduced, ReplGainLife, ReplAddCounter)},
	{"GainLife", replMask(ReplGainLife)},
	{"ImmediateTrigger", 0},
	{"LoseLife", replMask(ReplLifeReduced)},
	{"Mill", replMask(ReplMoved)},
	{"MultiplyCounter", replMask(ReplAddCounter)},
	{"Pump", 0},
	{"PumpAll", 0},
	{"PutCounter", replMask(ReplAddCounter)},
	{"PutCounterAll", replMask(ReplAddCounter)},
	{"Regenerate", 0},
	{"StoreSVar", 0},
	{"Tap", 0}, // no Tap event in the R: vocabulary
	{"TapAll", 0},
	{"Token", replMask(ReplCreateToken, ReplMoved)},
	{"UntapAll", replMask(ReplUntap)},
}

// AskFreeAPIEvents reports whether api is allowlisted (its primitive poses
// no decision unless a denied parameter is present) and the replaceable
// events it can propose.
func AskFreeAPIEvents(api string) (ReplEventMask, bool) {
	lo, hi := 0, len(askFreeAPITable)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if askFreeAPITable[m].api < api {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo < len(askFreeAPITable) && askFreeAPITable[lo].api == api {
		return askFreeAPITable[lo].events, true
	}
	return 0, false
}

// AskFreeAPIGates is the union of every allowlisted API's events: the
// replaceable events an ask-free chain can reach at all.
func AskFreeAPIGates() ReplEventMask {
	var m ReplEventMask
	for _, e := range askFreeAPITable {
		m |= e.events
	}
	return m
}

// AskFreeAPINames lists the allowlist, sorted.
func AskFreeAPINames() []string {
	out := make([]string, len(askFreeAPITable))
	for i, e := range askFreeAPITable {
		out[i] = e.api
	}
	return out
}

// maxMayAskDepth bounds the Charm Choices$ recursion.
const maxMayAskDepth = 6

// SAChainMayAsk walks sa and its SubAbility$ chain (and a Charm's Choices$
// bodies, read from svars) against the allowlist. root marks sa as the
// resolving ability itself (its Cost$ is the cost already paid, and its own
// target set was chosen on the stack). self is the face Defined$ Self names,
// or nil when it is not known. It runs once per configured ability (rules
// memoises the answer); a runtime-built ability is judged each time.
func SAChainMayAsk(sa *SA, svars map[string]string, self *Face, root bool) bool {
	return saChainMayAsk(sa, svars, self, root, 0)
}

func saChainMayAsk(sa *SA, svars map[string]string, self *Face, root bool, depth int) bool {
	if depth > maxMayAskDepth {
		return true
	}
	// A spell's whole chain is targeted at cast (CR 601.2c, the cast-time
	// sub-target pre-ask); an ability's sub-targets past its root are asked
	// mid-resolution (the "tgts" ask).
	spell := root && sa.Kind == "SP"
	for s, first := sa, root; s != nil; s, first = s.Sub, false {
		if !askFreeAPI(s.API) || saDenied(s) {
			return true
		}
		// Cost$ on an activated/spell root is the cost already paid; on a
		// DB body it is a "you may pay" window.
		if s.HasParam(PKCost) && !(first && (s.Kind == "AB" || s.Kind == "SP")) {
			return true
		}
		choices, hasChoices := s.Param(PKChoices)
		if hasChoices && s.API != "Charm" {
			return true
		}
		tgts := s.ParamStr(PKValidTgts)
		switch s.API {
		case "ChangeZone":
			if changeZoneMayAsk(s, self) {
				return true
			}
			if !first && tgts != "" {
				return true // a sub's own target set, asked mid-resolution
			}
		case "Attach":
			if tgts == "" && s.ParamStr(PKDefined) == "" && !(first && s.Kind == "SP") {
				return true // "attach to a permanent of your choice"
			}
			// The attaching object's own "as this becomes attached" choice
			// (Psychic Paper, Sanctuary Blade, Pick-Axe: an R:Event$ Attached
			// replacement) asks as the Attach applies. Only the source
			// attaching itself has a known face.
			if obj := s.ParamStr(PKObject); (obj != "" && obj != "Self") || self == nil || FaceAttachMayAsk(self) {
				return true
			}
		case "PutCounter":
			if strings.Contains(s.ParamStr(PKCounterType), ",") {
				return true // a counter-kind pick
			}
		case "Charm":
			if !first || choices == "" || depth > 0 {
				return true // a Charm reached mid-chain picks its modes here
			}
			// A root Charm's mode was chosen on the stack (CR 601.2b at cast,
			// CR 603.3c at a trigger's placement) and, when exactly one mode
			// is chosen, so were that mode body's own targets (the cast's
			// askCharmModeTargets, the placement's mode-target ask): the body
			// is judged as a root. With more than one mode a later mode's
			// targets may still be asked, so its bodies are judged as subs.
			single := !s.HasParam(PKCharmNum) && !s.HasParam(PKMinCharmNum)
			for _, n := range SplitModeNames(choices) {
				body := ResolveSVar(svars, n)
				if body == nil || saChainMayAsk(body, svars, self, single, depth+1) {
					return true
				}
			}
		case "CopyPermanent":
			if copyEntryMayAsk(s, self) {
				return true
			}
		case "Token":
			if strings.Contains(s.ParamStr(PKTokenScript), ",") {
				return true // a token-kind pick
			}
		}
		if !first && !spell && tgts != "" {
			return true // a sub's own target set, asked mid-resolution
		}
		// A sub-target chained to its parent's target (ParentTarget) is asked
		// mid-resolution (the c21c390de chain); Defined$ ParentTarget reuses
		// the parent's target and never asks.
		if strings.Contains(tgts, "ParentTarget") {
			return true
		}
		// A choice-shaped Defined$ (ChosenCard, "Choose...") is decided
		// earlier or asks here; be conservative.
		if d := s.ParamStr(PKDefined); strings.Contains(d, "Choose") || strings.Contains(d, "Chosen") {
			return true
		}
	}
	return false
}

// changeZoneMayAsk: a ChangeZone asks when it searches a hidden zone, picks
// "up to"/a number, or lets the chooser pick from a public zone without a
// target or Defined$; and a move onto the battlefield asks whatever the
// moved card's own entry asks.
func changeZoneMayAsk(s *SA, self *Face) bool {
	origin, defined := s.ParamStr(PKOrigin), s.ParamStr(PKDefined)
	if strings.Contains(origin, ",") {
		return true
	}
	if defined == "" && s.ParamStr(PKValidTgts) == "" {
		switch origin {
		case "Library", "Hand", "Sideboard", "":
			return true // a hidden-zone search or pick
		}
		defined = "Self" // a public origin with no selector moves the source
	}
	switch s.ParamStr(PKDestination) {
	case "Battlefield":
		// The moved card's own as-enters choice is board-dependent (which
		// card is reanimated or blinked); exempt only the source returning
		// itself, whose face is known.
		return defined != "Self" || self == nil || FaceEntryMayAsk(self) || faceEnchants(self)
	case "Library":
		return s.ParamStr(PKLibraryPosition) == "" // a top/bottom order may ask
	}
	return false
}

// copyEntryMayAsk: a CopyPermanent's token enters as a copy of a board
// object, and the copy asks whatever the copied face's entry asks (CR 303.4f:
// a copied Aura chooses what it enchants; an as-enters choice; a Clone's
// "enter as a copy"). Which object is copied is board-dependent, so only a
// copy of the source itself, whose face is known, can be exempt.
func copyEntryMayAsk(s *SA, self *Face) bool {
	if s.ParamStr(PKDefined) != "Self" || s.ParamStr(PKValidTgts) != "" {
		return true
	}
	return self == nil || FaceEntryMayAsk(self) || faceEnchants(self)
}

// SAChainBoardGates reports which replaceable events sa's SubAbility$ chain
// (and a Charm's Choices$ bodies) can propose through its allowlisted APIs:
// the board gates rules reads off the replacement sources. A chain outside
// the allowlist is judged "may ask" by SAChainMayAsk before its gates matter.
func SAChainBoardGates(sa *SA, svars map[string]string) ReplEventMask {
	return saChainBoardGates(sa, svars, 0)
}

// allReplEvents is every event bit, the depth-exhausted answer.
const allReplEvents = ReplEventMask(1<<ReplEventCount-1) &^ 1

func saChainBoardGates(sa *SA, svars map[string]string, depth int) ReplEventMask {
	if depth > maxMayAskDepth {
		return allReplEvents
	}
	var g ReplEventMask
	for s := sa; s != nil; s = s.Sub {
		ev, ok := AskFreeAPIEvents(s.API)
		if !ok {
			return allReplEvents
		}
		g |= ev
		if s.API == "Charm" {
			choices, _ := s.Param(PKChoices)
			for _, n := range strings.Split(choices, ",") {
				if body := ResolveSVar(svars, strings.TrimSpace(n)); body != nil {
					g |= saChainBoardGates(body, svars, depth+1)
				}
			}
		}
	}
	return g
}

// ReplMayElect reports whether a replacement line's own text elects as it
// applies: Optional$ / OptionalDecider$, or a CreateToken body other than a
// plain ReplaceToken (a chosen-copy election: Esix, Moonlit Meditation's
// ValidChoices$ / TokenScript$ Chosen). The board gate of the rules half;
// a presence test, never a read that honours the parameter.
func ReplMayElect(r *Repl) bool {
	if r.HasParam(PKOptional) || r.HasParam(PKOptionalDecider) {
		return true
	}
	if r.Event != "CreateToken" {
		return false
	}
	return r.With == nil || r.With.API != "ReplaceToken" || r.With.HasParam(PKValidChoices) ||
		strings.EqualFold(strings.TrimSpace(r.With.ParamStr(PKTokenScript)), "Chosen")
}

// ReplParamsMayElect is ReplMayElect's Optional$ test over an Effect-created
// replacement's raw parameter map.
func ReplParamsMayElect(params map[string]string) bool {
	_, opt := params["Optional"]
	_, dec := params["OptionalDecider"]
	return opt || dec
}

// faceEnchants reports whether f has an Enchant keyword: an Aura put onto the
// battlefield without being cast chooses what it enchants as it enters.
func faceEnchants(f *Face) bool {
	for _, kw := range f.Keywords {
		if strings.HasPrefix(kw, "Enchant") {
			return true
		}
	}
	return false
}

// SAChainTokenEntryMayAsk reports whether a Token body in sa's SubAbility$
// chain (or a Charm's Choices$ bodies) mints a face that asks as it enters:
// an Aura token that names no bearer (AttachedTo$) chooses what it enchants
// (CR 303.4f), and a token face's own entry text (FaceEntryMayAsk: Devour,
// an entry replacement). The token faces are the match's, so the text half
// cannot see them; rules folds this into its cached judgement. A script the
// lookup does not know is "may ask".
func SAChainTokenEntryMayAsk(sa *SA, svars map[string]string, tokens map[string]*Card) bool {
	return saChainTokenEntryMayAsk(sa, svars, tokens, 0)
}

func saChainTokenEntryMayAsk(sa *SA, svars map[string]string, tokens map[string]*Card, depth int) bool {
	if depth > maxMayAskDepth {
		return true
	}
	for s := sa; s != nil; s = s.Sub {
		switch s.API {
		case "Token":
			attached := s.HasParam(PKAttachedTo)
			for _, n := range strings.Split(s.ParamStr(PKTokenScript), ",") {
				if n = strings.TrimSpace(n); n == "" {
					continue
				}
				c := tokens[n]
				if c == nil || len(c.Faces) == 0 {
					return true
				}
				if f := c.Faces[0]; (!attached && faceEnchants(f)) || FaceEntryMayAsk(f) {
					return true
				}
			}
		case "Charm":
			choices, _ := s.Param(PKChoices)
			for _, n := range SplitModeNames(choices) {
				if body := ResolveSVar(svars, n); body != nil && saChainTokenEntryMayAsk(body, svars, tokens, depth+1) {
					return true
				}
			}
		}
	}
	return false
}
