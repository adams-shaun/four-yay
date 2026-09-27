package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Action coverage -- the completeness counterpart of -decision-stats.
//
// -decision-stats is a FREQUENCY report over the decisions a run happened to
// ask. It cannot answer "was every action an agent COULD take taken at least
// once", because it never states what the untaken actions were. This file is
// the missing report: it accumulates, per run,
//
//   - which decision kinds were ASKED (universe: decision.Kinds, the
//     package's own list, so the two cannot drift);
//   - which (kind, option-kind) shapes were OFFERED and which were CHOSEN --
//     the offered-minus-chosen rows are the "offered but never selected"
//     list, including the cast-option dims decision.Option carries
//     (Mode: kicked/surged/flashback/miracle; AltCostIndex != 0);
//   - which cast SHAPES a paid cast actually carried (plain, {X} paid, or a
//     CastInfo flag such as kicked/flashback) -- the event-log side of the
//     same question;
//   - which deck cards were ever cast or land-played, and which ability
//     slots (card, ability index) were ever activated;
//   - which registered primitives (api:/trig:/stat:/kw:/repl: labels, the
//     same universe cards.Primitives() and effects.Supported() define) were
//     ever EXERCISED at runtime, derived from the finished game's event log.
//
// The primitive walk reads only the log and the end-of-game state, both of
// which the bench already holds, so it needs no engine change: the event log
// is by construction a complete description of the match (events' package
// comment), and every object's ObjID is stable for the whole match, so a
// log event's Obj can be resolved against the final state.
//
// Signal classes, stated honestly because they differ in strength:
//
//   - api:/trig: EXECUTION signals: a CastInfo (a paid cast), an
//     AbilityPush (a paid activation), a TriggerPush/KeywordTriggerPush/
//     DelayedPush (a trigger placed on the stack) or a Resolve all attribute
//     their SA's api chain (Sub links walked) to the run. A trigger that
//     later fizzles on its intervening-if is still counted at placement --
//     a slight overcount, and the only one in this direction.
//   - stat:/kw: PRESENCE signals: a non-token card entering the battlefield
//     marks the labels of the face it entered as. A static being live is
//     passive, so presence is what the log can prove.
//   - repl: has NO runtime signal in the log (a replacement's application is
//     not attributable to its source card by any event field), so repl:
//     labels are reported in the universe but never claimed as exercised;
//     the exercised fraction is computed over the classes that have signals.
//
// SVar-executed sub-abilities reached through a ModeChosen (Charm modes,
// unless-pay answers) are resolved from the source face's SVar table and
// their api chains counted, so a mode-only branch is not invisible.
//
// Known attribution edges, all narrow: a card whose face flipped between an
// ability's push and game end attributes that push to the CURRENT face's
// ability at the same index (no repo-deck card flips mid-match today); a
// token created from a card script is NOT part of the universe (the universe
// is the decks' own cards).

// actionCoverage accumulates the completeness tallies across a whole run.
// Games are played in parallel, so every mutating method takes the mutex;
// write() sorts every list it prints, so the report is deterministic
// regardless of goroutine interleaving. Like decisionStats it is pure
// observation -- it reads the Decision/Intent pair and, at game end, the
// engine's log and state -- and never calls into a policy, the engine's
// advance or any rng, so enabling it cannot perturb what the bots decide.
type actionCoverage struct {
	mu    sync.Mutex
	games int64

	// Decision-side tallies.
	asked   map[string]int64 // decision kind -> asks
	offered map[string]int64 // "kind/optKind" -> options offered
	chosen  map[string]int64 // "kind/optKind" -> options chosen
	// cast-option dims the wire itself distinguishes (Option.Mode /
	// Option.AltCostIndex), keyed by mode and "own"/"alt".
	castModeOffered map[string]int64
	castModeChosen  map[string]int64
	altCostOffered  map[string]int64
	altCostChosen   map[string]int64

	// Event-log tallies.
	castShapes   map[string]int64 // "plain", "x", or a CastInfo flag name
	cardCasts    map[string]int64 // card name -> paid casts + land plays
	abilityUses  map[string]int64 // "card|idx" -> activations
	primExercsed map[string]int64 // primitive label -> exercise events

	// Universes, accumulated once per game from the game's own decks.
	primUniverse    map[string]bool
	cardAbilityIdx  map[string][]int // card name -> non-mana ability indexes over faces (sorted)
	cardManaN       map[string]int   // card name -> mana-ability count over faces
	cardsInUniverse map[string]bool
}

func newActionCoverage() *actionCoverage {
	return &actionCoverage{
		asked:           map[string]int64{},
		offered:         map[string]int64{},
		chosen:          map[string]int64{},
		castModeOffered: map[string]int64{},
		castModeChosen:  map[string]int64{},
		altCostOffered:  map[string]int64{},
		altCostChosen:   map[string]int64{},
		castShapes:      map[string]int64{},
		cardCasts:       map[string]int64{},
		abilityUses:     map[string]int64{},
		primExercsed:    map[string]int64{},
		primUniverse:    map[string]bool{},
		cardAbilityIdx:  map[string][]int{},
		cardManaN:       map[string]int{},
		cardsInUniverse: map[string]bool{},
	}
}

func (c *actionCoverage) game() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.games++
	c.mu.Unlock()
}

// rowKey is the internal (kind, option-kind) join. "\x00" cannot appear in
// either part, so the split at report time is exact.
func rowKey(kind, optKind string) string { return kind + "\x00" + optKind }

func splitRowKey(k string) (string, string) {
	i := strings.IndexByte(k, 0)
	if i < 0 {
		return k, ""
	}
	return k[:i], k[i+1:]
}

// record observes one decision the engine asked and the answer a seat
// returned. Same placement contract as decisionStats.record: after Decide,
// before Submit.
func (c *actionCoverage) record(d *decision.Decision, in decision.Intent) {
	if c == nil {
		return
	}
	kind := string(d.Kind)
	chosenSet := make(map[int]bool, len(in.Choices))
	for _, idx := range in.Choices {
		chosenSet[idx] = true
	}
	c.mu.Lock()
	c.asked[kind]++
	for _, opt := range d.Options {
		c.offered[rowKey(kind, opt.Kind)]++
		if kind == string(decision.KPriority) && opt.Kind == "cast" {
			c.castModeOffered[opt.Mode]++
			if opt.AltCostIndex != 0 {
				c.altCostOffered["alt"]++
			} else {
				c.altCostOffered["own"]++
			}
		}
		if chosenSet[opt.Index] {
			c.chosen[rowKey(kind, opt.Kind)]++
			if kind == string(decision.KPriority) && opt.Kind == "cast" {
				c.castModeChosen[opt.Mode]++
				if opt.AltCostIndex != 0 {
					c.altCostChosen["alt"]++
				} else {
					c.altCostChosen["own"]++
				}
			}
		}
	}
	c.mu.Unlock()
}

// exerciseAPIChain marks an executing SA's whole Sub chain as exercised.
// Self-locking: walkGame calls it outside its own per-case locks, and every
// mutating method of the collector takes the mutex itself so a caller cannot
// forget (the concurrent-map-write crash the 210-pair matrix run caught).
func (c *actionCoverage) exerciseAPIChain(sa *cards.SA) {
	if c == nil {
		return
	}
	c.mu.Lock()
	for s := sa; s != nil; s = s.Sub {
		c.primExercsed["api:"+s.API]++
	}
	c.mu.Unlock()
}

// primitive marks one primitive label exercised. Self-locking, like
// exerciseAPIChain.
func (c *actionCoverage) primitive(label string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.primExercsed[label]++
	c.mu.Unlock()
}

// cardName is a card's display name: the first face's Name, the same
// identity view/describe.go and the rest of the tree use.
func cardName(c *cards.Card) string {
	if c == nil || len(c.Faces) == 0 {
		return ""
	}
	return c.Faces[0].Name
}

// registerCard adds a deck card to the universes (dedup by name; two decks
// sharing a card register it once).
func (c *actionCoverage) registerCard(card *cards.Card) {
	if c == nil || card == nil {
		return
	}
	name := cardName(card)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cardsInUniverse[name] {
		return
	}
	c.cardsInUniverse[name] = true
	for _, p := range card.Primitives() {
		c.primUniverse[p] = true
	}
	nonMana := map[int]bool{}
	mana := 0
	for _, f := range card.Faces {
		for i, ab := range f.Abilities {
			if ab != nil && ab.API == "Mana" {
				mana++
			} else {
				nonMana[i] = true
			}
		}
	}
	idxs := make([]int, 0, len(nonMana))
	for i := range nonMana {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	c.cardAbilityIdx[name] = idxs
	c.cardManaN[name] = mana
}

// walkGame replays the finished game's event log against the final state to
// attribute runtime exercise. It runs once per game, after the game loop, on
// the goroutine that played the game.
func (c *actionCoverage) walkGame(cfg rules.Config, e *rules.Engine) {
	if c == nil {
		return
	}
	for _, deck := range cfg.Decks {
		for _, card := range deck {
			c.registerCard(card)
		}
	}
	resolveSVar := func(o *state.Object, name string) *cards.SA {
		if o == nil || name == "" {
			return nil
		}
		if f := o.Face(); f != nil {
			if sa := cards.ResolveSVar(f.SVars, name); sa != nil {
				return sa
			}
		}
		if src := e.G.Obj(o.Source); src != nil {
			if f := src.Face(); f != nil {
				return cards.ResolveSVar(f.SVars, name)
			}
		}
		return nil
	}
	// A PAID cast is attributed from its PutOnStack: CastInfo is emitted only
	// when a cast carried an {X} or a mode flag (rules/cast.go payCast), so a
	// plain cast has NO CastInfo and counting CastInfos would undercount
	// casts by every plain spell. A PutOnStack that is later reversed (CR
	// 733.1's abort path, a stack->origin MoveZone with Text "reversed") is
	// dropped; a CastInfo pairs with the most recent unpaired PutOnStack of
	// the same object (it is emitted by the same payCast that committed the
	// cast, before the next push of that object can happen).
	type pendingCast struct {
		obj    state.ObjID
		paired bool // a CastInfo attached its shape
		x      bool
		flags  []string
	}
	var pending []*pendingCast        // in log order; only the tail per obj is live
	lastBattlefield := state.ObjID(0) // the MoveZone->battlefield a LandPlayed follows
	for _, ev := range e.L.Events {
		switch ev.Kind {
		case events.PutOnStack:
			if ev.To != state.ZStack {
				break
			}
			o := e.G.Obj(ev.Obj)
			if o == nil || o.Card == nil {
				break
			}
			pending = append(pending, &pendingCast{obj: ev.Obj})
		case events.MoveZone:
			if ev.To == state.ZBattlefield {
				lastBattlefield = ev.Obj
				o := e.G.Obj(ev.Obj)
				if o != nil && o.Card != nil && !o.IsToken {
					// Presence signal: the entered face's statics and keywords
					// were live from this moment.
					if f := o.Face(); f != nil {
						for _, s := range f.Statics {
							c.primitive("stat:" + s.Mode)
						}
						for _, k := range f.Keywords {
							c.primitive("kw:" + cards.KeywordHead(k))
						}
					}
				}
				break
			}
			if ev.Text == "reversed" && ev.From == state.ZStack {
				// The abort path reverses the most recent push of THIS object
				// (CR 733.1 is a strict stack discipline), so dropping the
				// last matching pending cast is exact.
				for i := len(pending) - 1; i >= 0; i-- {
					if pending[i].obj == ev.Obj {
						pending = append(pending[:i], pending[i+1:]...)
						break
					}
				}
			}
		case events.LandPlayed:
			// Both land-play paths emit MoveZone(->battlefield) immediately
			// before LandPlayed (rules/cast.go payCast's land arm and
			// handlePriority's no-choice play_land path), and LandPlayed
			// itself carries only Player -- so the attribute comes from the
			// remembered preceding battlefield move.
			if o := e.G.Obj(lastBattlefield); o != nil && o.Card != nil {
				c.mu.Lock()
				c.cardCasts[cardName(o.Card)]++
				c.mu.Unlock()
			}
		case events.CastInfo:
			// Attach the X / flag shape to the most recent unpaired pending
			// cast of this object. CastInfo on an ability object (Card nil)
			// is commitCast's ability arm -- the {X} an activation paid with
			// -- and is skipped here; the activation itself is attributed by
			// its AbilityPush.
			o := e.G.Obj(ev.Obj)
			if o == nil || o.Card == nil {
				break
			}
			for i := len(pending) - 1; i >= 0; i-- {
				if !pending[i].paired && pending[i].obj == ev.Obj {
					pending[i].paired = true
					if ev.Amount != 0 {
						pending[i].x = true
					}
					for _, part := range strings.Split(ev.Counter, ",") {
						if part = strings.TrimSpace(part); part != "" {
							pending[i].flags = append(pending[i].flags, part)
						}
					}
					break
				}
			}
		case events.ManaAdd:
			// A POSITIVE ManaAdd is mana produced -- by some mana ability. The
			// event does not name WHICH ability produced (Event.Obj was
			// deliberately kept out of ManaAdd -- rules/mana_activation.go), so
			// the claim is label-level: api:Mana was exercised, not that a
			// specific slot fired. Negative ManaAdds are payment spends.
			if ev.Amount > 0 {
				c.primitive("api:Mana")
			}
		case events.AbilityPush:
			// A PAID activation: Obj is the source permanent, Amount the
			// face Abilities index the push used. Mana abilities never reach
			// here (CR 605.3a: they do not use the stack), so mana slots are
			// counted in the universe but reported as having no per-slot
			// signal.
			src := e.G.Obj(ev.Obj)
			if src == nil || src.Card == nil {
				break
			}
			f := src.Face()
			if f == nil || ev.Amount < 0 || int(ev.Amount) >= len(f.Abilities) {
				break
			}
			ab := f.Abilities[ev.Amount]
			if ab != nil && ab.API != "Mana" {
				c.mu.Lock()
				c.abilityUses[fmt.Sprintf("%s|%d", cardName(src.Card), ev.Amount)]++
				c.mu.Unlock()
			}
			c.exerciseAPIChain(ab)
		case events.TriggerPush:
			src := e.G.Obj(ev.Obj)
			if src == nil {
				break
			}
			f := src.Face()
			if f == nil || ev.Amount < 0 || int(ev.Amount) >= len(f.Triggers) {
				break
			}
			c.primitive("trig:" + f.Triggers[ev.Amount].Mode)
			c.exerciseAPIChain(f.Triggers[ev.Amount].Effect)
		case events.KeywordTriggerPush:
			src := e.G.Obj(ev.Obj)
			if src == nil {
				break
			}
			if sa := resolveSVar(src, ev.Counter); sa != nil {
				c.exerciseAPIChain(sa)
			}
		case events.DelayedPush:
			src := e.G.Obj(ev.Obj)
			if src == nil {
				break
			}
			if sa := resolveSVar(src, ev.Counter); sa != nil {
				c.exerciseAPIChain(sa)
			}
		case events.ModeChosen:
			// A modal pick's answer: the chosen SVar bodies execute through
			// the mode walk, so resolve each label against the source face's
			// SVar table. Labels that do not resolve (a plain-word label)
			// attribute nothing -- recorded only as a modes-chosen ask.
			o := e.G.Obj(ev.Obj)
			if o == nil || ev.Text == "" {
				break
			}
			for _, name := range strings.Split(ev.Text, ",") {
				if sa := resolveSVar(o, strings.TrimSpace(name)); sa != nil {
					c.exerciseAPIChain(sa)
				}
			}
		case events.Resolve:
			// The stack object actually resolved. For an ability object
			// (Card == nil) the Ability pointer is the SA; for a spell the
			// face's SpellAbility is. Resolution re-marks the same labels
			// placement marked -- a resolved-only view would be stricter,
			// but a countered spell's targets still exercised the ask path,
			// so placement-or-resolution is the reported signal.
			o := e.G.Obj(ev.Obj)
			if o == nil {
				break
			}
			if o.Ability != nil {
				c.exerciseAPIChain(o.Ability)
			} else if f := o.Face(); f != nil {
				c.exerciseAPIChain(f.SpellAbility())
			}
		}
	}
	// Fold the surviving (unreversed) pushes into the cast tallies.
	c.mu.Lock()
	for _, pc := range pending {
		shape := "plain"
		if pc.x {
			c.castShapes["x"]++
			shape = "x"
		}
		for _, f := range pc.flags {
			c.castShapes[f]++
			shape = f
		}
		c.castShapes[shape]++
	}
	c.mu.Unlock()
}

// kindRows is the static (kind -> sub-kind) row universe for the never-asked
// report, seeded from the option vocabularies the engine and decision
// package define. The choose row's list is the engine's greppable option
// literals; kinds built by fmt (convoke_<n>, pay_<n>) cannot be seeded
// statically -- the offered-side tallies always cover them, and the report
// says so, so a missing seed can only ever make the never-asked list SHORTER,
// never show a false never-asked row for a shape the run did exercise.
var kindRows = map[string][]string{
	// station (Station! actions) and unlock (Room doors) were measured into
	// this seed from a 1050-game -pairs all run over the full repo deck set;
	// the guard test below covers every kind, so a new option kind a real
	// run offers fails the test until it is seeded here.
	string(decision.KPriority):        {"activate", "play_land", "cast", "ability", "pass", "concede", "station", "unlock", "turn_face_up", "specialize"},
	string(decision.KTarget):          {"player", "permanent", "spell", "ability", "trigger"},
	string(decision.KAttackers):       {"attacker"},
	string(decision.KBlockers):        {"block"},
	string(decision.KMulligan):        {"keep", "mulligan", "bottom"},
	string(decision.KModes):           {"mode"},
	string(decision.KTriggerOrder):    {"trigger"},
	string(decision.KTriggerOptional): {"yes", "no"},
	string(decision.KCommanderZone):   {"command_zone", "leave"},
	string(decision.KChoose): {
		"x", "exile", "sacrifice", "discard", "search", "dig", "name", "type",
		"number", "yes", "no", "card", "choice", "player", "untap", "imprint",
		"roll", "mode", "modes", "bottom", "hand_move", "reveal_optional",
		"defined_library_optional", "unless_pay", "ward_mana", "ward_discard",
		"ward_alt", "tapcost", "revealcost", "returncost", "exilecost",
		"altaddcost", "beholdcost", "blightcost", "skip_replacement",
		"division", "trigger_cost_pay", "trigger_cost_decline", "harmonize",
		"granted", "station", "forage_food", "forage_exile", "madness",
		"madness_exile", "madness_graveyard", "opening_yes", "opening_no",
		"opening_exile", "suspend_cast_yes", "suspend_cast_no",
		"cumulative_pay", "cumulative_sac", "cumulative_action_life",
		"cumulative_action_grave", "mana", "pay_life",
		"gift_decline", "gift_promise",
	},
	string(decision.KReplacement): {"replacement", "mana", "apply", "decline", "skip_replacement"},
	string(decision.KArrange):     {"bottom", "graveyard", "exile", "hand", "dig_bottom"},
}

// coverageRow is one (kind, option-kind) tally for the report.
type coverageRow struct {
	kind, optKind string
	offered       int64
	chosen        int64
}

// write prints the completeness report. Every list is sorted; the report is
// byte-deterministic for identical runs.
func (c *actionCoverage) write(out io.Writer) {
	if c == nil {
		return
	}
	c.mu.Lock()
	games := c.games
	asked := make(map[string]int64, len(c.asked))
	for k, v := range c.asked {
		asked[k] = v
	}
	rows := make([]coverageRow, 0, len(c.offered))
	for k, off := range c.offered {
		kind, opt := splitRowKey(k)
		rows = append(rows, coverageRow{kind: kind, optKind: opt, offered: off, chosen: c.chosen[k]})
	}
	castShapes := make(map[string]int64, len(c.castShapes))
	for k, v := range c.castShapes {
		castShapes[k] = v
	}
	abilityUses := make(map[string]int64, len(c.abilityUses))
	for k, v := range c.abilityUses {
		abilityUses[k] = v
	}
	primExercised := make(map[string]int64, len(c.primExercsed))
	for k, v := range c.primExercsed {
		primExercised[k] = v
	}
	cardCasts := make(map[string]int64, len(c.cardCasts))
	for k, v := range c.cardCasts {
		cardCasts[k] = v
	}
	primUniverse := make(map[string]bool, len(c.primUniverse))
	for k := range c.primUniverse {
		primUniverse[k] = true
	}
	cardAbilityIdx := make(map[string][]int, len(c.cardAbilityIdx))
	for k, v := range c.cardAbilityIdx {
		cardAbilityIdx[k] = v
	}
	cardManaN := make(map[string]int, len(c.cardManaN))
	for k, v := range c.cardManaN {
		cardManaN[k] = v
	}
	cardsInUniverse := make(map[string]bool, len(c.cardsInUniverse))
	for k := range c.cardsInUniverse {
		cardsInUniverse[k] = true
	}
	castModeOff := make(map[string]int64, len(c.castModeOffered))
	for k, v := range c.castModeOffered {
		castModeOff[k] = v
	}
	castModeCh := make(map[string]int64, len(c.castModeChosen))
	for k, v := range c.castModeChosen {
		castModeCh[k] = v
	}
	altOff, altCh := c.altCostOffered, c.altCostChosen
	c.mu.Unlock()
	if games == 0 {
		return
	}

	fmt.Fprintf(out, "\naction coverage (%d games):\n", games)

	// 1. Decision kinds never asked (universe: decision.Kinds).
	var askedKinds, neverKinds []string
	for _, k := range decision.Kinds {
		if asked[string(k)] > 0 {
			askedKinds = append(askedKinds, string(k))
		} else {
			neverKinds = append(neverKinds, string(k))
		}
	}
	fmt.Fprintf(out, "decision kinds asked: %d/%d (%s)\n", len(askedKinds), len(decision.Kinds), strings.Join(askedKinds, ","))
	if len(neverKinds) > 0 {
		fmt.Fprintf(out, "  never asked: %s\n", strings.Join(neverKinds, ","))
	}

	// 2. (kind, option-kind) rows never asked, from the static row universe:
	// a row never OFFERED is never asked. Per kind, the sub-kinds that never
	// appeared.
	var neverRows []string
	for _, k := range decision.Kinds {
		var missing []string
		for _, opt := range kindRows[string(k)] {
			if c.offered[rowKey(string(k), opt)] == 0 {
				missing = append(missing, opt)
			}
		}
		if asked[string(k)] == 0 && len(missing) == len(kindRows[string(k)]) {
			continue // the whole kind never asked; already reported above
		}
		if len(missing) > 0 {
			neverRows = append(neverRows, string(k)+": "+strings.Join(missing, ","))
		}
	}
	if len(neverRows) > 0 {
		fmt.Fprintf(out, "  option rows never offered: %s\n", strings.Join(neverRows, "; "))
	}
	fmt.Fprintf(out, "  (the choose row's universe is a static seed; fmt-built option kinds such as convoke_<n> are covered by the offered-side rows below, never by this list)\n")

	// 3. Offered but never chosen.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].kind != rows[j].kind {
			return rows[i].kind < rows[j].kind
		}
		return rows[i].optKind < rows[j].optKind
	})
	var neverChosen []string
	for _, r := range rows {
		if r.chosen == 0 {
			neverChosen = append(neverChosen, fmt.Sprintf("%s/%s (offered %d)", r.kind, r.optKind, r.offered))
		}
	}
	if len(neverChosen) > 0 {
		fmt.Fprintf(out, "  offered but never chosen: %s\n", strings.Join(neverChosen, ", "))
	}

	// 3b. Cast-option dims (Mode / AltCostIndex) offered vs chosen.
	var modeParts []string
	for _, m := range castModeUniverse {
		off, ch := castModeOff[m], castModeCh[m]
		if off == 0 {
			modeParts = append(modeParts, fmt.Sprintf("%s: never offered", m))
		} else if ch == 0 {
			modeParts = append(modeParts, fmt.Sprintf("%s: offered %d, never chosen", m, off))
		} else {
			modeParts = append(modeParts, fmt.Sprintf("%s: chosen %d", m, ch))
		}
	}
	fmt.Fprintf(out, "  cast modes: %s\n", strings.Join(modeParts, ", "))
	fmt.Fprintf(out, "  alt-cost cast options: own offered %d chosen %d; alternative offered %d chosen %d\n",
		altOff["own"], altCh["own"], altOff["alt"], altCh["alt"])

	// 4. Cast shapes from the event log.
	var shapes []string
	for _, s := range sortedCastShapes(castShapes) {
		shapes = append(shapes, fmt.Sprintf("%s %d", s, castShapes[s]))
	}
	fmt.Fprintf(out, "  cast shapes (paid casts): %s\n", strings.Join(shapes, ", "))

	// 5. Cards never cast or played; ability slots never activated.
	var neverCast []string
	for name := range cardsInUniverse {
		if cardCasts[name] == 0 {
			neverCast = append(neverCast, name)
		}
	}
	sort.Strings(neverCast)
	total := 0
	var neverSlots []string
	for _, name := range sortedCardNames(cardManaN) {
		total += len(cardAbilityIdx[name])
	}
	usedByCard := map[string]map[int]bool{}
	for k := range abilityUses {
		name, idx := splitAbilityKey(k)
		if usedByCard[name] == nil {
			usedByCard[name] = map[int]bool{}
		}
		usedByCard[name][idx] = true
	}
	var activated, manaSlots int
	for _, name := range sortedCardNames(cardManaN) {
		manaSlots += cardManaN[name]
		for _, i := range cardAbilityIdx[name] {
			if usedByCard[name][i] {
				activated++
			} else {
				neverSlots = append(neverSlots, fmt.Sprintf("%s#%d", name, i))
			}
		}
	}
	fmt.Fprintf(out, "  cards cast or played: %d/%d; never: %s\n", len(cardCasts), len(cardsInUniverse), orNone(neverCast))
	fmt.Fprintf(out, "  non-mana ability slots activated: %d/%d; never: %s\n", activated, total, orNone(neverSlots))
	fmt.Fprintf(out, "  mana ability slots: %d (no per-slot runtime signal -- a ManaAdd event does not name the producing ability; api:Mana is claimed label-level)\n", manaSlots)

	// 6. Primitive exercise, split by signal class.
	supported := effects.Supported()
	classes := []string{"api:", "trig:", "stat:", "kw:", "repl:"}
	type classSum struct{ uni, sup, ex, exSup int }
	sums := map[string]*classSum{}
	for _, cl := range classes {
		sums[cl] = &classSum{}
	}
	var neverExercised []string
	for p := range primUniverse {
		cl := ""
		for _, cand := range classes {
			if strings.HasPrefix(p, cand) {
				cl = cand
				break
			}
		}
		if cl == "" {
			continue
		}
		s := sums[cl]
		s.uni++
		if supported[p] {
			s.sup++
		}
		if primExercised[p] > 0 {
			s.ex++
			if supported[p] {
				s.exSup++
			}
		} else if cl != "repl:" {
			neverExercised = append(neverExercised, p)
		}
	}
	sort.Strings(neverExercised)
	parts := make([]string, 0, len(classes))
	for _, cl := range classes {
		s := sums[cl]
		parts = append(parts, fmt.Sprintf("%s* exercised %d/%d (supported %d/%d)", strings.TrimSuffix(cl, ":"), s.ex, s.uni, s.exSup, s.sup))
	}
	fmt.Fprintf(out, "  primitives: %s\n", strings.Join(parts, ", "))
	fmt.Fprintf(out, "  never-exercised (excl. repl:, which has no runtime signal): %s\n", orNone(neverExercised))
}

// castModeUniverse is the static cast-mode vocabulary decision.Option.Mode
// documents, plus the empty ordinary-cost mode.
var castModeUniverse = []string{"", "kicked", "surged", "flashback", "miracle"}

// sortedCastShapes renders the observed cast shapes in a stable order: the
// fixed plain/x first, then observed flag names alphabetically.
func sortedCastShapes(m map[string]int64) []string {
	var fixed, rest []string
	for k := range m {
		if k == "plain" || k == "x" {
			fixed = append(fixed, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.Strings(fixed)
	sort.Strings(rest)
	return append(fixed, rest...)
}

// sortedCardNames returns card names with a nonzero ability count, sorted.
func sortedCardNames(m map[string]int) []string {
	names := make([]string, 0, len(m))
	for n, v := range m {
		if v > 0 {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// splitAbilityKey splits "card|idx".
func splitAbilityKey(k string) (string, int) {
	i := strings.LastIndexByte(k, '|')
	if i < 0 {
		return k, 0
	}
	n := 0
	for _, d := range k[i+1:] {
		n = n*10 + int(d-'0')
	}
	return k[:i], n
}

// orNone renders an empty list.
func orNone(list []string) string {
	if len(list) == 0 {
		return "(none)"
	}
	return strings.Join(list, ", ")
}
