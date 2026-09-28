package v1agent

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// TacticalOptions configures Tactical.
type TacticalOptions struct {
	// Trace, when set, receives one line per decision: the board summary,
	// every candidate's score and the pick (debugging aid).
	Trace io.Writer
}

// Tactical is the board-reading v1 policy: it scores every candidate
// against the Board (kernel observation when present) and the card
// knowledge (cardfacts.json from gorge IR, kernelcards.json, hints.go),
// plans each combat declaration once per decision group, and answers the
// scan substeps from the plan.
type Tactical struct {
	opts       TacticalOptions
	seat       string
	deck       string
	deckList   map[string]int
	attackGrp  int64
	attackPlan map[uint32]bool
	blockGrp   int64
	blockPlan  map[uint32]uint32 // blocker -> attacker
	// In-game opponent model (no state crosses games): combats where the
	// opponent could have blocked one of our attackers, and how many of
	// those it did block in.
	blockChances, blocksSeen int
	lastBlockObs             int
	// and turns where it could have attacked, and how many it did attack in
	attackChances, attacksSeen int
	lastAttackObs              int
	// gen selects hint-free card knowledge (derive.go, generic.go) over
	// hints.go and the named branches.
	gen      bool
	style    deckStyle
	lastMode map[string]int // generic: the mode last chosen, by source name
	// seen holds our own cards observed so far, by arena id: the deck
	// style falls back to them when the catalog does not know our deck.
	seen map[uint32]string
}

// NewTactical builds the policy.
func NewTactical(opts TacticalOptions) *Tactical {
	return &Tactical{opts: opts, attackGrp: -1, blockGrp: -1}
}

// GameStart implements Policy.
func (t *Tactical) GameStart(g *GameStart) {
	t.seat = g.Seat
	if i := seatIndex(g.Seat); i < len(g.CatalogIDs) {
		t.deck = g.CatalogIDs[i]
	}
	t.deckList = deckCounts(t.deck)
	t.style = styleOf(t.deckList)
	t.lastMode = map[string]int{}
	t.seen = map[uint32]string{}
	t.attackGrp, t.blockGrp = -1, -1
	t.blockChances, t.blocksSeen, t.lastBlockObs = 0, 0, -1
	t.attackChances, t.attacksSeen, t.lastAttackObs = 0, 0, -1
}

// observeAttacks records, once per opponent turn (seen from its second
// main phase or end step, where this turn's combat is still on the
// board), whether it attacked when it had an untapped, unsick creature.
func (t *Tactical) observeAttacks(b *Board) {
	if b.Thin || b.MyTurn || (b.Phase != "main2" && b.Phase != "end") || t.lastAttackObs == b.Turn {
		return
	}
	t.lastAttackObs = b.Turn
	if b.Combat.AttackersDeclared && len(b.Combat.Attackers) > 0 {
		t.attackChances++
		t.attacksSeen++
		return
	}
	for _, c := range Creatures(b.Theirs) {
		if !c.Tapped && !c.SummoningSick && c.Power() > 0 && !Kw(c).Defender {
			t.attackChances++
			return
		}
	}
}

// oppNeverAttacks: at least two turns with an attacker and no attack.
func (t *Tactical) oppNeverAttacks() bool { return t.attackChances >= 2 && t.attacksSeen == 0 }

// observeBlocks records, once per turn of ours, whether the opponent blocked
// when it could have.
func (t *Tactical) observeBlocks(b *Board) {
	if b.Thin || !b.MyTurn || !b.Combat.BlockersDeclared || t.lastBlockObs == b.Turn || len(b.Combat.Attackers) == 0 {
		return
	}
	t.lastBlockObs = b.Turn
	blocks := b.Combat.Blocks()
	blocked := false
	for _, bs := range blocks {
		if len(bs) > 0 {
			blocked = true
		}
	}
	could := blocked
	for _, a := range b.Combat.Attackers {
		ac := b.Card(a.ArenaID)
		if ac == nil {
			continue
		}
		for _, bl := range Creatures(b.Theirs) {
			if CanBlock(bl, ac) {
				could = true
			}
		}
	}
	if could {
		t.blockChances++
		if blocked {
			t.blocksSeen++
		}
	}
}

// oppNeverBlocks: the opponent has passed up every one of at least two
// chances to block this game.
func (t *Tactical) oppNeverBlocks() bool { return t.blockChances >= 2 && t.blocksSeen == 0 }

// GameOver implements Policy.
func (t *Tactical) GameOver(*Terminal) {}

func (t *Tactical) aggro() bool {
	if t.gen {
		return t.style.aggro
	}
	switch t.deck {
	case "Burn", "Rally", "Affinity", "Elves":
		return true
	}
	return false
}

func (t *Tactical) burnDeck() bool {
	if t.gen {
		return t.style.burn
	}
	return t.deck == "Burn" || t.deck == "Rally"
}

// Choose implements Policy.
func (t *Tactical) Choose(d *Decision) int {
	b := NewBoard(d)
	t.observeBlocks(b)
	t.observeAttacks(b)
	if t.gen && t.deckList == nil {
		t.observeOwnCards(b)
	}
	has := func(kind string) bool {
		for i := range d.Candidates {
			if d.Candidates[i].Kind() == kind {
				return true
			}
		}
		return false
	}
	switch {
	case has("choose_attacker_inclusion"):
		return t.attack(d, b)
	case has("choose_blocker_inclusion"):
		return t.block(d, b)
	}
	best, bestScore := 0, math.Inf(-1)
	scores := make([]float64, len(d.Candidates))
	for i := range d.Candidates {
		s := t.score(d, b, i)
		scores[i] = s
		if s > bestScore {
			best, bestScore = i, s
		}
	}
	if t.gen && d.Candidates[best].Kind() == "choose_spell_mode" && t.lastMode != nil {
		c := &d.Candidates[best]
		t.lastMode[normName(srcName(c))] = int(c.Semantic.Int("mode_index"))
	}
	t.trace(d, b, scores, best)
	return best
}

func (t *Tactical) trace(d *Decision, b *Board, scores []float64, pick int) {
	if t.opts.Trace == nil {
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s #%d T%d %s my=%v life=%d/%d hand=%d | mine:", d.GameID, d.Step, b.Turn, b.Phase, b.MyTurn, b.Life[b.Me], b.Life[b.Opp], b.HandCount[b.Me])
	for _, c := range b.Mine {
		if c.IsCreature() {
			fmt.Fprintf(&sb, " %s %d/%d%s", c.Name, c.Power(), c.Toughness(), tapMark(c))
		}
	}
	sb.WriteString(" | theirs:")
	for _, c := range b.Theirs {
		if c.IsCreature() {
			fmt.Fprintf(&sb, " %s %d/%d%s", c.Name, c.Power(), c.Toughness(), tapMark(c))
		}
	}
	sb.WriteString("\n")
	for i := range d.Candidates {
		mark := "  "
		if i == pick {
			mark = " *"
		}
		s := 0.0
		if scores != nil {
			s = scores[i]
		}
		fmt.Fprintf(&sb, "%s %6.2f %s\n", mark, s, describe(d, b, i))
	}
	io.WriteString(t.opts.Trace, sb.String())
}

func tapMark(c *KCard) string {
	if c.Tapped {
		return "(T)"
	}
	return ""
}

func describe(d *Decision, b *Board, i int) string {
	c := &d.Candidates[i]
	s := c.Kind()
	if n := srcName(c); n != "" {
		s += " " + n
	}
	sem := &c.Semantic
	for _, f := range []string{"attacker", "blocker", "card", "candidate"} {
		if o := sem.Obj(f); o != nil {
			s += " " + f + "=" + o.Name()
		}
	}
	if tr := sem.Target("target"); tr != nil {
		if tr.Player != nil {
			s += " ->" + *tr.Player
		} else if tr.Object != nil {
			s += " ->" + tr.Object.Name() + "@" + tr.Object.ControllerSeat
		}
	}
	for _, f := range []string{"include", "use_cost", "cast_it", "pay", "value", "mode", "choice", "cost_kind", "mana_choice", "option_index", "mode_index"} {
		if raw, ok := sem.Fields[f]; ok {
			s += " " + f + "=" + string(raw)
		}
	}
	return s
}

// ---- helpers over candidates ----

func kact(d *Decision, i int) *KAction {
	if d.Kernel == nil {
		return nil
	}
	return &d.Kernel.Actions[i]
}

func srcName(c *Candidate) string {
	if o := c.Semantic.Source(); o != nil {
		return o.Name()
	}
	return ""
}

func cardName(b *Board, r *KRef) string {
	if r == nil {
		return ""
	}
	if c := b.Card(r.ArenaID); c != nil {
		return c.Name
	}
	for _, h := range b.Hand {
		if h.Stable.ArenaID == r.ArenaID {
			return h.Name
		}
	}
	if k := KernelCardByID(r.CardDBID); k != nil {
		return k.Name
	}
	return ""
}

// burnDamage is the damage a burn spell named n deals now.
func (t *Tactical) burnDamage(b *Board, n string) int {
	if t.gen {
		return t.burnDamageG(b, n)
	}
	h := hintFor(n)
	if n == "Galvanic Blast" {
		arts := 0
		for _, c := range b.Mine {
			if c.Characteristics.Types.Artifact {
				arts++
			}
		}
		if arts >= 3 {
			return 4
		}
		return 2
	}
	if h.dmg > 0 {
		return h.dmg
	}
	if f := FactN(n); f != nil && f.Spell != nil {
		return f.Spell.Damage
	}
	return 0
}

// handBurn totals the burn damage in our hand (a lethal check).
func (t *Tactical) handBurn(b *Board) int {
	if t.gen {
		return t.handBurnG(b)
	}
	n := 0
	for _, h := range b.Hand {
		if hintFor(h.Name).role == RoleBurn {
			n += t.burnDamage(b, h.Name)
		}
	}
	return n
}

func (t *Tactical) faceWeight(b *Board) float64 {
	if t.burnDeck() {
		return 1.3
	}
	if b.Life[b.Opp] <= 8 && t.aggro() {
		return 1.1
	}
	return 0.5
}

// bestBurnTarget returns the best score for dmg damage: a kill of an
// opposing creature, or face.
func (t *Tactical) bestBurnTarget(b *Board, dmg int) (score float64, face bool) {
	opp := b.Life[b.Opp]
	if dmg >= opp {
		return 1000, true
	}
	score, face = float64(dmg)*t.faceWeight(b), true
	for _, c := range Creatures(b.Theirs) {
		if Kw(c).Hexproof || Kw(c).Indestructible || Kw(c).ProtMonocolored {
			continue
		}
		if c.Remaining() <= dmg {
			v := t.cv(c)*1.2 + 0.5
			if v > score {
				score, face = v, false
			}
		}
	}
	return
}

// bestRemovalTarget is the value of the best opposing creature.
func (t *Tactical) bestRemovalTarget(b *Board) float64 {
	best := 0.0
	for _, c := range Creatures(b.Theirs) {
		if Kw(c).Hexproof || Kw(c).ProtMonocolored {
			continue
		}
		if v := t.cv(c); v > best {
			best = v
		}
	}
	return best
}

func (b *Board) oppSpellOnStack() *KStackItem {
	if len(b.Stack) == 0 {
		return nil
	}
	top := &b.Stack[len(b.Stack)-1]
	if top.Controller == b.OppSeat && top.Kind == "spell" && !top.IsCopy {
		return top
	}
	return nil
}

func (b *Board) inMain() bool { return b.Phase == "main1" || b.Phase == "main2" }

func (b *Board) oppEndStep() bool { return !b.MyTurn && b.Phase == "end" }

func (b *Board) fodder() int {
	n := 0
	for _, c := range b.Mine {
		if c.IsToken && (!c.IsCreature() || c.Power() <= 1) {
			n++
		}
	}
	return n
}

func handLands(b *Board) int {
	n := 0
	for _, h := range b.Hand {
		if k := KernelCardByName(h.Name); k != nil && k.IsType("Land") {
			n++
		}
	}
	return n
}

func flooded(b *Board) bool {
	return handLands(b) >= 2 && Lands(b.Mine) >= 4 || b.HandCount[b.Me] >= 7
}

// ---- priority-style scoring ----

func (t *Tactical) score(d *Decision, b *Board, i int) float64 {
	c := &d.Candidates[i]
	sem := &c.Semantic
	switch c.Kind() {
	case "pass":
		return 0
	case "play_land":
		return t.landScore(b, srcName(c))
	case "cast_spell":
		// mtg-kernel's policy schema v5 cannot represent a staged Escape
		// graveyard-exile cost: escaping halts the game (measured, Terror
		// mirrors), and a halted game is unrated
		if src := c.Semantic.Source(); src != nil && src.Zone == "graveyard" && FactN(srcName(c)).HasKeyword("Escape") {
			return -100
		}
		return t.castScore(d, b, i)
	case "activate_mana_ability":
		return -50
	case "activate_ability":
		return t.abilityScore(d, b, i)
	case "plot_spell":
		return -5
	case "choose_target", "choose_effect_target":
		return t.targetScore(d, b, i)
	case "finish_target_selection", "finish_effect_selection":
		return 0.05
	case "discard":
		return -t.keepValue(b, cardRefName(d, b, i, "cards"))
	case "choose_cost_target":
		return t.costTargetScore(d, b, i)
	case "choose_cast_mode":
		if t.gen {
			return t.castModeScoreG(b, srcName(c), sem.Str("mode"))
		}
		return t.castModeScore(b, srcName(c), sem.Str("mode"))
	case "choose_kicker":
		if sem.Bool("pay") {
			if t.gen {
				if profileFor(srcName(c)).kickTeam && len(Creatures(b.Mine)) == 0 {
					return -1
				}
				return 1
			}
			if srcName(c) == "Goblin Bushwhacker" && len(Creatures(b.Mine)) == 0 {
				return -1
			}
			return 1
		}
		return 0
	case "choose_spell_mode":
		if t.gen {
			return t.modeScoreG(b, srcName(c), int(sem.Int("mode_index")))
		}
		return t.modeScore(b, srcName(c), int(sem.Int("mode_index")))
	case "choose_option":
		if t.gen {
			return t.optionScoreG(b, srcName(c), int(sem.Int("option_index")))
		}
		return t.optionScore(b, srcName(c), int(sem.Int("option_index")))
	case "choose_number":
		return float64(sem.Int("value")) // X: as large as allowed
	case "choose_boolean":
		if sem.Bool("value") {
			return 1
		}
		return 0
	case "choose_color":
		return t.colorScore(b, sem.Str("color"))
	case "choose_optional_cost_use":
		if sem.Bool("use_cost") {
			return 1
		}
		return 0
	case "choose_optional_cost_which":
		switch sem.Str("choice") {
		case "discard":
			if flooded(b) || handLands(b) >= 1 {
				return 2
			}
			return 1
		case "sacrifice_land":
			if Lands(b.Mine) >= 6 {
				return 1.5
			}
			return 0.5
		}
		return 0
	case "choose_spell_copy_payment":
		if sem.Bool("pay") {
			return -1
		}
		return 0
	case "choose_spell_copy_retarget":
		if sem.Bool("change_target") {
			return -1
		}
		return 0
	case "choose_madness_cast":
		if sem.Bool("cast_it") {
			return 1
		}
		return 0
	case "order_triggers":
		return -float64(i) * 0.001
	}
	return -float64(i) * 0.001
}

func cardRefName(d *Decision, b *Board, i int, field string) string {
	if ka := kact(d, i); ka != nil {
		switch field {
		case "cards":
			if len(ka.Cards) > 0 {
				return cardName(b, &ka.Cards[0])
			}
		case "candidate":
			return cardName(b, ka.Candidate)
		}
	}
	sem := &d.Candidates[i].Semantic
	if field == "cards" {
		if objs := sem.Objs("cards"); len(objs) > 0 {
			return objs[0].Name()
		}
		return ""
	}
	return sem.Obj(field).Name()
}

func landProduces(name string) string {
	if k := KernelCardByName(name); k != nil {
		s := ""
		for _, p := range k.Produces {
			s += p
		}
		return s
	}
	return ""
}

func (t *Tactical) neededColors(b *Board) map[byte]int {
	have := map[byte]bool{}
	for _, c := range b.Mine {
		if c.IsLand() {
			for _, ch := range []byte(landProduces(c.Name)) {
				have[ch] = true
			}
		}
	}
	need := map[byte]int{}
	for _, h := range b.Hand {
		k := KernelCardByName(h.Name)
		if k == nil || k.IsType("Land") {
			continue
		}
		for _, ch := range []byte(k.CostColors()) {
			if !have[ch] {
				need[ch]++
			}
		}
	}
	return need
}

func (t *Tactical) landScore(b *Board, name string) float64 {
	if !b.MyTurn {
		return 100
	}
	s := 100.0
	k := KernelCardByName(name)
	h := t.kn(name)
	tapped := h.tapped || k.Has("enters_tapped")
	// would an untapped land let us cast something this turn?
	untapped := UntappedLands(b.Mine)
	wantNow := false
	for _, hc := range b.Hand {
		hk := KernelCardByName(hc.Name)
		if hk == nil || hk.IsType("Land") {
			continue
		}
		if hk.MV == untapped+1 {
			wantNow = true
		}
	}
	if tapped {
		if wantNow {
			s -= 3
		} else {
			s += 2
		}
	}
	need := t.neededColors(b)
	for _, ch := range []byte(landProduces(name)) {
		if need[ch] > 0 {
			s += 1 + 0.2*float64(need[ch])
		}
	}
	return s
}

func (t *Tactical) castModeScore(b *Board, name, mode string) float64 {
	if name == "Fireblast" && mode == "alternative" {
		if b.Life[b.Opp] <= 4+t.handBurn(b)-4 || Lands(b.Mine) >= 6 {
			return 2
		}
		return -1
	}
	if mode == "normal" {
		return 1
	}
	return 0.5
}

func (t *Tactical) modeScore(b *Board, name string, idx int) float64 {
	if name == "Thraben Charm" {
		if idx == 0 {
			return 1
		}
		return 0
	}
	return -float64(idx) * 0.01
}

func (t *Tactical) optionScore(b *Board, name string, idx int) float64 {
	if hasXCost(name) {
		return float64(idx) // the kernel poses X as an option index
	}
	if dungeonSource(name) {
		return roomOptionScore(b, idx)
	}
	return -float64(idx) * 0.01
}

// roomOptionScore scores an Undercity next-room choice, options in the
// kernel's printed order (rules knowledge: the dungeon, not a card).
func roomOptionScore(b *Board, idx int) float64 {
	switch b.Room {
	case 0, roomSecretEntrance: // [Forge, Lost Well]
		if len(Creatures(b.Mine)) > 0 {
			return -float64(idx)
		}
		return float64(idx)
	case roomForge: // [Trap, Arena]: 5 life
		return -float64(idx)
	case roomLostWell: // [Arena, Stash]
		return -float64(idx)
	case roomArena: // [Archives, Catacombs]: a 4/1
		return float64(idx)
	}
	return -float64(idx) * 0.01
}

// scrySources pose "choose the cards to put on the bottom" as a
// card_selection over the library top.
var scrySources = map[string]bool{"Faerie Seer": true, "Preordain": true, "Lembas": true}

func hasXCost(name string) bool {
	k := KernelCardByName(name)
	return k != nil && strings.Contains(k.ManaCost, "{X}")
}

// manaSources counts our untapped lands and untapped, unsick mana
// creatures/artifacts.
func manaSources(b *Board) int {
	n := 0
	for _, c := range b.Mine {
		if c.Tapped {
			continue
		}
		if c.IsLand() {
			n++
			continue
		}
		if k := KernelCardByName(c.Name); k != nil && len(k.Produces) > 0 && !(c.IsCreature() && c.SummoningSick) {
			n++
		}
	}
	return n
}

// hiddenZoneValue scores a card offered from a library, hand or exile
// selection (searches, reveals, Mesmeric Fiend).
func (t *Tactical) hiddenZoneValue(b *Board, k *KernelCard) float64 {
	if k == nil {
		return 1
	}
	if k.IsType("Land") {
		if Lands(b.Mine)+handLands(b) < 5 {
			return 4
		}
		return 1
	}
	v := 3 + 0.2*float64(k.MV)
	if k.MV > Lands(b.Mine)+3 {
		v -= 1
	}
	switch t.kn(k.Name).role {
	case RoleBurn, RoleRemoval, RoleCounter:
		v += 1
	}
	return v
}

func (t *Tactical) colorScore(b *Board, color string) float64 {
	letter := map[string]byte{"white": 'W', "blue": 'U', "black": 'B', "red": 'R', "green": 'G'}[color]
	s := 0.0
	for _, h := range b.Hand {
		for _, ch := range []byte(KernelCardByName(h.Name).CostColors()) {
			if ch == letter {
				s++
			}
		}
	}
	// Prismatic Strands: name the color of the attackers.
	for _, a := range b.Combat.Attackers {
		if c := b.Card(a.ArenaID); c != nil && c.Stable.Controller == b.OppSeat {
			if colorBit(letter)&c.Characteristics.ColorMask != 0 {
				s += 3
			}
		}
	}
	return s
}

func colorBit(l byte) uint8 {
	switch l {
	case 'W':
		return 1
	case 'U':
		return 2
	case 'B':
		return 4
	case 'R':
		return 8
	case 'G':
		return 16
	}
	return 0
}

// castScore scores casting the candidate's spell now.
func (t *Tactical) castScore(d *Decision, b *Board, i int) float64 {
	if t.gen {
		return t.castScoreG(d, b, i)
	}
	c := &d.Candidates[i]
	name := srcName(c)
	src := c.Semantic.Source()
	h := hintFor(name)
	k := KernelCardByName(name)
	f := FactN(name)
	instant := k.IsType("Instant") || f.HasKeyword("Flash") || h.flash
	fromGrave := src != nil && src.Zone == "graveyard"
	oppStack := b.oppSpellOnStack()
	sorcerySpeed := b.MyTurn && b.inMain() && len(b.Stack) == 0

	if hasXCost(name) && manaSources(b) < 3 {
		return -5
	}
	switch h.role {
	case RoleNever:
		if name == "Lotus Petal" && sorcerySpeed && Lands(b.Mine) <= 2 {
			for _, hc := range b.Hand {
				if hk := KernelCardByName(hc.Name); hk != nil && !hk.IsType("Land") && hk.MV == manaSources(b)+1 {
					return 6
				}
			}
		}
		return -10
	case RoleCounter:
		if oppStack == nil {
			return -10
		}
		tk := KernelCardByID(oppStack.Source.CardDBID)
		switch name {
		case "Dispel":
			if !tk.IsType("Instant") {
				return -10
			}
		case "Spell Pierce":
			if tk.IsType("Creature") || UntappedLands(b.Theirs) >= 2 {
				return -10
			}
		case "Force Spike":
			if UntappedLands(b.Theirs) >= 1 {
				return -10
			}
		}
		if tk != nil && tk.MV <= 1 && !tk.IsType("Creature") && hintFor(tk.Name).role != RoleBurn && hintFor(tk.Name).role != RoleRemoval {
			return 5
		}
		return 90
	case RoleBurn:
		dmg := t.burnDamage(b, name)
		score, face := t.bestBurnTarget(b, dmg)
		if score >= 1000 {
			return 200
		}
		if name == "Fireblast" && !(b.Life[b.Opp] <= 4+t.handBurn(b)-4 || Lands(b.Mine) >= 6) {
			return -5
		}
		if fromGrave && !(face && dmg >= b.Life[b.Opp]) && score < 3 {
			return -5 // Lava Dart flashback costs a land
		}
		if face {
			// face burn: at the opponent's end step, or main2 of our turn
			// (burn decks), or when the opponent is in reach
			if b.Life[b.Opp] <= t.handBurn(b)+2 {
				return 60 + score
			}
			if !t.burnDeck() {
				return -5
			}
			if b.oppEndStep() || (b.MyTurn && b.Phase == "main2") {
				return 30 + score
			}
			return -5
		}
		return 45 + score*3
	case RoleRemoval:
		v := t.bestRemovalTarget(b)
		if v < 1.5 {
			return -5
		}
		if name == "Snap" && v < 3 {
			return -5
		}
		return 50 + v*3
	case RoleSweeper:
		kills := 0.0
		for _, cr := range Creatures(b.Theirs) {
			if cr.Remaining() <= 1 {
				kills += t.cv(cr)
			}
		}
		for _, cr := range Creatures(b.Mine) {
			if cr.Remaining() <= 1 && name != "End the Festivities" {
				kills -= t.cv(cr)
			}
		}
		if kills >= 2 {
			return 45 + kills*3
		}
		if t.burnDeck() && (b.oppEndStep() || b.Phase == "main2") {
			return 20
		}
		return -5
	case RoleProtect:
		// Prismatic Strands: when the opponent attacks into us.
		if !b.MyTurn && (b.Phase == "declare_blockers" || b.Phase == "declare_attackers") && len(b.Combat.Attackers) >= 2 {
			return 50
		}
		return -5
	case RolePump:
		if b.oppEndStep() {
			return 8
		}
		return -5
	case RoleReanimate:
		best := 0.0
		for _, g := range b.MyGrave {
			if g.IsCreature() {
				if v := float64(g.Power()+g.Toughness()) / 2; v > best {
					best = v
				}
			}
		}
		if name == "Pulse of Murasa" {
			if b.Life[b.Me] <= 8 || (best >= 1 && b.oppEndStep()) {
				return 35 + best
			}
			return -5
		}
		if name == "Dread Return" && t.giantLethal(b) {
			return 170
		}
		if best >= 3 {
			return 55 + best
		}
		return -5
	case RoleRamp:
		if handLands(b) == 0 {
			return 70
		}
		return 10
	}
	if h.sacCost && b.fodder() == 0 {
		return -5
	}
	isCreature := k.IsType("Creature") || f.HasType("creature")
	if name == "Balustrade Spy" && sorcerySpeed && t.spyCombo(b, false) {
		return 180
	}
	if isCreature {
		v := 3.0
		if f != nil && f.HasPT {
			v = 1 + float64(f.Power) + float64(f.Toughness)/2
		}
		v += h.bonus
		if name == "Spellstutter Sprite" {
			if oppStack != nil {
				tk := KernelCardByID(oppStack.Source.CardDBID)
				faeries := 1
				for _, cr := range b.Mine {
					if hasSubtypeFaerie(cr.Name) {
						faeries++
					}
				}
				if tk != nil && tk.MV <= faeries {
					return 95
				}
			}
		}
		if h.flash || (instant && !b.MyTurn) {
			if !b.MyTurn && (b.Phase == "end" || b.Phase == "declare_attackers") {
				return 40 + v
			}
			if b.MyTurn && b.Phase == "main2" {
				return 15 + v
			}
			return -5
		}
		if !sorcerySpeed {
			return -5
		}
		if name == "Guttersnipe" || name == "Priest of Titania" {
			v += 5
		}
		return 40 + v*2
	}
	switch h.role {
	case RoleToken:
		if sorcerySpeed {
			return 48
		}
		return -5
	case RoleArtifact:
		if sorcerySpeed {
			return 42
		}
		return -5
	case RoleDraw:
		if name == "Cleansing Wildfire" && !sorcerySpeed {
			return -5
		}
		if name == "Faithless Looting" && !(flooded(b) || handHas(b, "Fiery Temper")) {
			return -5
		}
		if instant {
			if b.oppEndStep() || (b.MyTurn && b.Phase == "main2") {
				return 30
			}
			return -5
		}
		if sorcerySpeed {
			return 32
		}
		return -5
	}
	// unknown spell: behave like the heuristic, but only at sorcery speed
	// for non-instants
	if sorcerySpeed || instant {
		return 20
	}
	return -5
}

// spyCombo reports whether Balustrade Spy aimed at ourselves wins this
// turn: our library holds no land (so the Spy mills all of it), Lotleth
// Giant and Dread Return end up in the graveyard, Dread Return's flashback
// (sacrifice three creatures) is payable from the board, and the Giant's
// trigger (1 damage per creature card in our graveyard) is lethal.
func (t *Tactical) spyCombo(b *Board, spyOnBoard bool) bool {
	lib := libraryCounts(t.deckList, b)
	if lib == nil || countWhere(lib, isLandCard) > 0 {
		return false
	}
	inGrave := func(name string) bool {
		for _, g := range b.MyGrave {
			if g.Name == name {
				return true
			}
		}
		return false
	}
	if lib["Lotleth Giant"] == 0 && !inGrave("Lotleth Giant") {
		return false
	}
	if lib["Dread Return"] == 0 && !inGrave("Dread Return") {
		return false
	}
	creatures := len(Creatures(b.Mine))
	if !spyOnBoard {
		creatures++ // the Spy itself
	}
	if creatures < 3 {
		return false
	}
	graveCreatures := countWhere(lib, isCreatureCard)
	for _, g := range b.MyGrave {
		if g.IsCreature() {
			graveCreatures++
		}
	}
	// three sacrificed creatures join the graveyard, the Giant leaves it
	dmg := graveCreatures + 3 - 1
	return dmg >= b.Life[b.Opp]
}

// giantLethal: Lotleth Giant is in our graveyard and its trigger (one
// damage per creature card in our graveyard, the Giant itself excluded
// once it returns) kills.
func (t *Tactical) giantLethal(b *Board) bool {
	giant, n := false, 0
	for _, g := range b.MyGrave {
		if g.Name == "Lotleth Giant" {
			giant = true
		}
		if g.IsCreature() {
			n++
		}
	}
	return giant && n-1+3 >= b.Life[b.Opp]
}

func handHas(b *Board, name string) bool {
	for _, h := range b.Hand {
		if h.Name == name {
			return true
		}
	}
	return false
}

func hasSubtypeFaerie(name string) bool {
	switch name {
	case "Faerie Seer", "Spellstutter Sprite", "Faerie Miscreant", "Sneaky Snacker":
		return true
	}
	return false
}

// ---- activated abilities ----

func (t *Tactical) abilityScore(d *Decision, b *Board, i int) float64 {
	if t.gen {
		return t.abilityScoreG(d, b, i)
	}
	return t.abilityScoreFor(d, b, i, hintFor(srcName(&d.Candidates[i])).ab)
}

// abilityScoreFor scores activating the candidate's ability used as u.
func (t *Tactical) abilityScoreFor(d *Decision, b *Board, i int, u abUse) float64 {
	c := &d.Candidates[i]
	src := c.Semantic.Source()
	switch u {
	case abNever:
		return -10
	case abEndStep:
		if b.oppEndStep() {
			return 12
		}
		return -5
	case abLoot:
		if b.oppEndStep() && flooded(b) {
			return 10
		}
		return -5
	case abCycle:
		if src != nil && src.Zone == "hand" && Lands(b.Mine)+handLands(b) < 4 && (b.inMain() || b.oppEndStep()) {
			return 25
		}
		return -5
	case abEquip:
		if b.MyTurn && b.Phase == "main1" && len(b.Stack) == 0 && len(Creatures(b.Mine)) > 0 {
			var ar uint32
			if ka := kact(d, i); ka != nil && ka.Source != nil {
				ar = ka.Source.ArenaID
			}
			for _, cr := range b.Mine {
				for _, at := range cr.Attachments {
					if at == ar {
						return -5
					}
				}
			}
			return 25
		}
		return -5
	case abNinjutsu:
		// One ninjutsu per combat, never returning a creature that entered
		// this turn: chaining a second ninjutsu onto a just-ninjutsued
		// attacker halts mtg-kernel (InvalidEffectContinuation, measured in
		// four Faeries mirrors) and a halted game is unrated.
		if !b.MyTurn || b.Phase != "declare_blockers" {
			return -5
		}
		for _, u := range t.unblockedAttackers(b) {
			if u.EnteredTurn != nil && *u.EnteredTurn == b.Turn {
				return -5
			}
		}
		for _, u := range t.unblockedAttackers(b) {
			if t.kn(u.Name).ab != abNinjutsu {
				return 70
			}
		}
		return -5
	case abCombat:
		if b.MyTurn && b.Phase == "declare_blockers" && len(t.unblockedAttackers(b)) > 0 {
			return 20
		}
		return -5
	case abShaman:
		net := 0.0
		for _, cr := range Creatures(b.Theirs) {
			if !Kw(cr).Flying && cr.Remaining() <= 1 {
				net += t.cv(cr)
			}
		}
		for _, cr := range Creatures(b.Mine) {
			if !Kw(cr).Flying && cr.Remaining() <= 1 {
				net -= t.cv(cr)
			}
		}
		if net >= 2.5 {
			return 30 + net
		}
		return -5
	case abPing:
		if b.Life[b.Opp] <= 1 {
			return 150
		}
		return -5
	case abStun:
		if b.oppEndStep() && t.bestRemovalTarget(b) >= 3 {
			return 12
		}
		return -5
	}
	return -5
}

func (t *Tactical) unblockedAttackers(b *Board) []*KCard {
	blocks := b.Combat.Blocks()
	var out []*KCard
	for _, a := range b.Combat.Attackers {
		c := b.Card(a.ArenaID)
		if c == nil || c.Stable.Controller != b.Seat {
			continue
		}
		if len(blocks[a.ArenaID]) == 0 {
			out = append(out, c)
		}
	}
	return out
}

// ---- targets ----

// polarity: -1 harmful (aim at the opponent), +1 beneficial (aim at us).
func polarity(name string) int {
	h := hintFor(name)
	if h.pol != 0 {
		return h.pol
	}
	switch h.role {
	case RoleBurn, RoleRemoval, RoleCounter, RoleSweeper:
		return -1
	case RolePump, RoleReanimate, RoleProtect:
		return 1
	}
	if f := FactN(name); f != nil {
		harm := f.Spell != nil && f.Spell.Harmful
		for _, e := range f.Triggers {
			harm = harm || e.Harmful
		}
		for _, e := range f.Abilities {
			harm = harm || e.Harmful
		}
		if harm {
			return -1
		}
	}
	return 1
}

// Undercity rooms (mtg-kernel UndercityRoomV1 stable ids) and the target
// polarity of the ones that target: Forge puts counters on a creature,
// Trap makes a player lose 5 life, Arena goads a creature.
const (
	roomSecretEntrance = 1
	roomForge          = 2
	roomLostWell       = 3
	roomTrap           = 4
	roomArena          = 5
)

func dungeonSource(name string) bool { return name == "Avenging Hunter" }

// targetKnow is what target scoring needs to know about the source.
type targetKnow struct {
	pol       int
	dungeon   bool // an initiative source: room-aware polarity
	burn      bool
	selfLand  bool
	scry      bool
	powerOnly bool
	comboSelf func() bool            // aiming at ourselves wins now
	finisher  func(*KernelCard) bool // returning this card wins now
}

func (t *Tactical) targetKnowFor(b *Board, name string) targetKnow {
	if !t.gen {
		h := hintFor(name)
		return targetKnow{
			pol: polarity(name), dungeon: dungeonSource(name), burn: h.role == RoleBurn,
			selfLand: h.selfLand, scry: scrySources[name], powerOnly: name == "Humbling Elder",
			comboSelf: func() bool { return name == "Balustrade Spy" && t.spyCombo(b, true) },
			finisher: func(k *KernelCard) bool {
				return name == "Dread Return" && k.Name == "Lotleth Giant" && t.giantLethal(b)
			},
		}
	}
	p := profileFor(name)
	tk := targetKnow{pol: p.pol, dungeon: p.initiative, burn: p.role == RoleBurn, selfLand: p.selfLand,
		scry: p.scry, powerOnly: p.powerOnly}
	if pl := t.modePlay(name); pl != nil {
		tk.pol, tk.burn = pl.pol, pl.role == RoleBurn
	}
	if p.millTarget && t.graveSynergy() {
		tk.pol = 1 // fuel our own graveyard
	}
	tk.comboSelf = func() bool { return p.millUntil && t.comboG(b, true) }
	tk.finisher = func(k *KernelCard) bool {
		return p.role == RoleReanimate && !p.reanimHand && profileFor(k.Name).gyFinisher && t.finisherLethalG(b, p.flashSacN)
	}
	return tk
}

func (t *Tactical) targetScore(d *Decision, b *Board, i int) float64 {
	c := &d.Candidates[i]
	name := srcName(c)
	tkn := t.targetKnowFor(b, name)
	pol := tkn.pol
	if tkn.dungeon {
		switch b.Room {
		case roomForge:
			pol = 1
		case roomTrap, roomArena:
			pol = -1
		}
	}
	var kt *KTarget
	if ka := kact(d, i); ka != nil {
		kt = ka.Target
	}
	if kt == nil {
		// thin: use the wire reference
		tr := c.Semantic.Target("target")
		if tr == nil {
			return 0
		}
		mine := (tr.Player != nil && *tr.Player == b.Seat) || (tr.Object != nil && tr.Object.ControllerSeat == b.Seat)
		if (pol < 0) == mine {
			return -1
		}
		return 1
	}
	if kt.Kind == "player" && tkn.comboSelf() {
		if kt.Player == b.Seat {
			return 500
		}
		return 0
	}
	if kt.Kind == "player" {
		me := kt.Player == b.Seat
		switch {
		case pol < 0 && me:
			return -100
		case pol < 0:
			if tkn.burn {
				dmg := t.burnDamage(b, name)
				if dmg >= b.Life[b.Opp] {
					return 1000
				}
				return float64(dmg) * t.faceWeight(b)
			}
			return 1
		case me:
			return 1
		default:
			return -1
		}
	}
	if kt.Object == nil {
		return 0
	}
	ref := kt.Object
	// spells on the stack (counters)
	if ref.Zone == "Stack" {
		for _, s := range b.Stack {
			if s.Source.ArenaID == ref.ArenaID {
				v := 1.0
				if k := KernelCardByID(ref.CardDBID); k != nil {
					v = float64(k.MV) + 1
				}
				if s.Controller == b.Seat {
					return -v * float64(-pol)
				}
				return v * float64(-pol)
			}
		}
		return 0
	}
	if ref.Zone == "Library" || ref.Zone == "Hand" || ref.Zone == "Exile" {
		v := t.hiddenZoneValue(b, KernelCardByID(ref.CardDBID))
		if ref.Owner == b.Seat {
			switch {
			case b.Purpose == "library_order" && ref.Zone == "Hand":
				return -v // Brainstorm: put the worst cards back
			case b.Purpose == "card_selection" && tkn.scry:
				return 2 - v // scry: bottom only what we do not want
			}
		}
		if (ref.Owner == b.Seat) == (pol > 0) {
			return v
		}
		return -v
	}
	if ref.Zone == "Graveyard" && ref.Owner == b.Seat {
		if k := KernelCardByID(ref.CardDBID); k != nil && tkn.finisher(k) {
			return 500
		}
	}
	if ref.Zone == "Graveyard" {
		k := KernelCardByID(ref.CardDBID)
		v := 1.0
		if k != nil {
			v = float64(k.MV)
		}
		if ref.Owner == b.Seat {
			return v * float64(pol)
		}
		return -v * float64(pol)
	}
	cd := b.Card(ref.ArenaID)
	if cd == nil {
		return 0
	}
	mine := cd.Stable.Controller == b.Seat
	if tkn.selfLand && cd.IsLand() {
		if mine && Kw(cd).Indestructible {
			return 10
		}
		if mine {
			return -5
		}
		return 2
	}
	if !cd.IsCreature() {
		v := 1.0
		if cd.IsLand() {
			v = 0.8
		}
		if cd.IsToken {
			v = 0.3
		}
		if mine == (pol > 0) {
			return v
		}
		return -v
	}
	v := t.cv(cd)
	if pol < 0 {
		if mine {
			return -v*3 - 5
		}
		if tkn.burn {
			dmg := t.burnDamage(b, name)
			if !Kw(cd).Indestructible && cd.Remaining() <= dmg {
				return v*1.2 + 0.5
			}
			return 0.1 * v
		}
		if tkn.powerOnly {
			return float64(cd.Power()) + 0.1*v
		}
		return v
	}
	if !mine {
		return -v
	}
	// beneficial on our creature: favour attackers that got through
	for _, u := range t.unblockedAttackers(b) {
		if u.Stable.ArenaID == cd.Stable.ArenaID {
			v += 5
		}
	}
	return v
}

// ---- costs and discards ----

// keepValue is how much we want to keep a hand card (discard the lowest).
func (t *Tactical) keepValue(b *Board, name string) float64 {
	k := KernelCardByName(name)
	if k == nil {
		return 2
	}
	lands := Lands(b.Mine)
	if k.IsType("Land") {
		if lands+handLands(b) >= 6 {
			return 0.5
		}
		if lands >= 4 {
			return 2
		}
		return 6
	}
	v := 4.0
	if k.MV > lands+2 {
		v -= 1.5
	}
	switch t.kn(name).role {
	case RoleBurn, RoleRemoval, RoleCounter:
		v += 1
	case RoleNever:
		v -= 2
	}
	if f := FactN(name); f.HasKeyword("Madness") {
		v -= 2
	}
	return v
}

func (t *Tactical) costTargetScore(d *Decision, b *Board, i int) float64 {
	c := &d.Candidates[i]
	kind := c.Semantic.Str("cost_kind")
	var ref *KRef
	if ka := kact(d, i); ka != nil {
		ref = ka.Candidate
	}
	if ref == nil {
		return -float64(i) * 0.001
	}
	name := cardName(b, ref)
	switch kind {
	case "discard_cards":
		return -t.keepValue(b, name)
	case "exile_from_graveyard":
		return -float64(i) * 0.001
	case "return_permanents_to_hand":
		// ninjutsu: return the least valuable unblocked attacker
		if cd := b.Card(ref.ArenaID); cd != nil {
			if cd.IsCreature() {
				return -t.cv(cd)
			}
			if cd.IsLand() {
				return -0.5
			}
		}
		return 0
	}
	// sacrifice / tap: the least valuable permanent
	cd := b.Card(ref.ArenaID)
	if cd == nil {
		return 0
	}
	if cd.IsCreature() {
		return -t.cv(cd)
	}
	if cd.IsToken {
		return 0
	}
	if cd.IsLand() {
		return -2
	}
	return -1.5
}

// ---- combat ----

func (t *Tactical) attack(d *Decision, b *Board) int {
	if d.Kernel == nil {
		// thin: everything attacks (the heuristic's rule)
		return HeuristicPick(d)
	}
	if t.attackPlan == nil || t.attackGrp != d.Group.GroupID {
		t.attackGrp = d.Group.GroupID
		var cands []*KCard
		if s := b.Selection; s != nil {
			for _, r := range append([]KRef{s.Current}, s.Remaining...) {
				if c := b.Card(r.ArenaID); c != nil {
					cands = append(cands, c)
				}
			}
		}
		t.attackPlan = t.planAttack(b, cands)
	}
	for i := range d.Candidates {
		ka := kact(d, i)
		if ka == nil || ka.Attacker == nil {
			continue
		}
		if d.Candidates[i].Semantic.Bool("include") == t.attackPlan[ka.Attacker.ArenaID] {
			t.trace(d, b, nil, i)
			return i
		}
	}
	return 0
}

func (t *Tactical) planAttack(b *Board, cands []*KCard) map[uint32]bool {
	plan := map[uint32]bool{}
	var attackers []*KCard
	for _, a := range cands {
		if a.Power() <= 0 || Kw(a).Defender {
			continue
		}
		attackers = append(attackers, a)
	}
	var blockers []*KCard
	for _, c := range Creatures(b.Theirs) {
		if !c.Tapped && !t.oppNeverBlocks() {
			blockers = append(blockers, c)
		}
	}
	oppLife := b.Life[b.Opp]
	// all-in if the damage that must get through is lethal
	total := 0
	var powers []int
	for _, a := range attackers {
		total += strikeDamage(a)
		powers = append(powers, strikeDamage(a))
	}
	sort.Sort(sort.Reverse(sort.IntSlice(powers)))
	stopped := 0
	for j := 0; j < len(blockers) && j < len(powers); j++ {
		stopped += powers[j]
	}
	reach := 0
	if t.gen {
		reach = t.burnReach(b)
	}
	if total-stopped+reach >= oppLife && len(attackers) > 0 {
		for _, a := range attackers {
			plan[a.Stable.ArenaID] = true
		}
		return plan
	}
	for _, a := range attackers {
		ok, evasive := true, true
		av := t.cv(a)
		for _, bl := range blockers {
			if !CanBlock(bl, a) {
				continue
			}
			if Kw(a).Menace && len(blockers) < 2 {
				continue
			}
			evasive = false
			aDies, bDies := Fight(a, bl)
			switch {
			case aDies && !bDies:
				ok = false
			case aDies && bDies:
				if av > t.cv(bl)+0.5 && !t.aggro() {
					ok = false
				}
			}
		}
		if evasive || ok {
			plan[a.Stable.ArenaID] = true
		}
	}
	// keep enough home to survive the crack-back: their creatures attack
	// next turn and each of our untapped non-attackers blocks the biggest
	// attacker it can.
	myLife := b.Life[b.Me]
	var theirs []*KCard
	for _, c := range Creatures(b.Theirs) {
		if !Kw(c).Defender && c.Power() > 0 {
			theirs = append(theirs, c)
		}
	}
	sort.SliceStable(theirs, func(i, j int) bool { return strikeDamage(theirs[i]) > strikeDamage(theirs[j]) })
	crack := func() int {
		used := map[uint32]bool{}
		dmg := 0
		for _, a := range theirs {
			blocked := false
			for _, c := range Creatures(b.Mine) {
				if used[c.Stable.ArenaID] || (plan[c.Stable.ArenaID] && !Kw(c).Vigilance) {
					continue
				}
				home := *c
				home.Tapped = false
				if CanBlock(&home, a) {
					used[c.Stable.ArenaID], blocked = true, true
					break
				}
			}
			if !blocked {
				dmg += strikeDamage(a)
			}
		}
		return dmg
	}
	ourHit := func() int {
		var ps []int
		for _, a := range attackers {
			if plan[a.Stable.ArenaID] {
				ps = append(ps, strikeDamage(a))
			}
		}
		sort.Sort(sort.Reverse(sort.IntSlice(ps)))
		dmg := 0
		for j, p := range ps {
			if j >= len(blockers) {
				dmg += p
			}
		}
		return dmg
	}
	for !t.oppNeverAttacks() {
		c := crack()
		racing := b.Life[b.Opp]-ourHit() <= ourHit() && ourHit() > 0
		if c < myLife && (racing || 2*c < myLife) {
			break
		}
		// pull back the attacker that blocks best (highest toughness)
		var best *KCard
		for _, a := range attackers {
			if plan[a.Stable.ArenaID] && !Kw(a).Vigilance && (best == nil || a.Toughness() > best.Toughness()) {
				best = a
			}
		}
		if best == nil {
			break
		}
		plan[best.Stable.ArenaID] = false
	}
	return plan
}

var elfNames = map[string]bool{
	"Llanowar Elves": true, "Fyndhorn Elves": true, "Elvish Mystic": true, "Elves of Deep Shadow": true,
	"Priest of Titania": true, "Timberwatch Elf": true, "Quirion Ranger": true, "Wellwisher": true,
	"Masked Vandal": true,
}

// oppPump is the largest +X/+X the opponent can give one attacker from an
// untapped on-board source.
func (t *Tactical) oppPump(b *Board) int {
	if t.gen {
		return t.oppPumpG(b)
	}
	best := 0
	for _, c := range b.Theirs {
		if c.Tapped {
			continue
		}
		x := 0
		switch c.Name {
		case "Timberwatch Elf":
			if c.SummoningSick {
				continue
			}
			for _, e := range Creatures(b.Theirs) {
				if elfNames[e.Name] {
					x++
				}
			}
		case "Basilisk Gate":
			for _, g := range b.Theirs {
				if k := KernelCardByName(g.Name); k != nil && k.Has("gate_land") {
					x++
				}
			}
		}
		if x > best {
			best = x
		}
	}
	return best
}

func (t *Tactical) block(d *Decision, b *Board) int {
	if d.Kernel == nil {
		return HeuristicPick(d)
	}
	if t.blockPlan == nil || t.blockGrp != d.Group.GroupID {
		t.blockGrp = d.Group.GroupID
		t.blockPlan = t.planBlocks(b)
	}
	for i := range d.Candidates {
		ka := kact(d, i)
		if ka == nil || ka.Attacker == nil || ka.Blocker == nil {
			continue
		}
		// arena id 0 is a real object (the first card of seat p0's deck)
		target, planned := t.blockPlan[ka.Blocker.ArenaID]
		want := planned && target == ka.Attacker.ArenaID
		if d.Candidates[i].Semantic.Bool("include") == want {
			t.trace(d, b, nil, i)
			return i
		}
	}
	return 0
}

func (t *Tactical) planBlocks(b *Board) map[uint32]uint32 {
	plan := map[uint32]uint32{}
	var attackers []*KCard
	for _, r := range b.Combat.Attackers {
		if c := b.Card(r.ArenaID); c != nil {
			attackers = append(attackers, c)
		}
	}
	t.sortByValueDesc(attackers)
	var mine []*KCard
	for _, c := range Creatures(b.Mine) {
		if !c.Tapped {
			mine = append(mine, c)
		}
	}
	// Combat tricks the opponent has on board: an untapped Timberwatch Elf
	// or Basilisk Gate pumps one attacker after blocks. Judge blocker
	// survival against the pumped attacker and count the pump once in the
	// damage that gets through.
	pump := t.oppPump(b)
	pumped := func(a *KCard) *KCard {
		if pump == 0 {
			return a
		}
		c := *a
		p := a.Power() + pump
		tt := a.Toughness() + pump
		c.Characteristics.Power, c.Characteristics.Toughness = &p, &tt
		return &c
	}
	used := map[uint32]bool{}
	blocked := map[uint32]bool{}
	assign := func(bl, a *KCard) {
		plan[bl.Stable.ArenaID] = a.Stable.ArenaID
		used[bl.Stable.ArenaID] = true
		blocked[a.Stable.ArenaID] = true
	}
	canBlock := func(bl, a *KCard) bool {
		return !used[bl.Stable.ArenaID] && CanBlock(bl, a) && !Kw(a).Menace
	}
	// 1. blocks that kill and survive (cheapest blocker)
	for _, a := range attackers {
		var pick *KCard
		for _, bl := range mine {
			if !canBlock(bl, a) {
				continue
			}
			blDies, _ := Fight(bl, pumped(a))
			_, aDies := Fight(bl, a)
			if aDies && !blDies && (pick == nil || t.cv(bl) < t.cv(pick)) {
				pick = bl
			}
		}
		if pick != nil {
			assign(pick, a)
		}
	}
	// 1b. double blocks that kill a big attacker for at most one blocker
	for _, a := range attackers {
		if blocked[a.Stable.ArenaID] || Kw(a).FirstStrike || Kw(a).DoubleStrike || Kw(a).Indestructible || t.cv(a) < 4 {
			continue
		}
		pa := pumped(a)
		var bestPair [2]*KCard
		bestGain := 0.5
		for i, x := range mine {
			for _, y := range mine[i+1:] {
				if !canBlock(x, a) || !canBlock(y, a) {
					continue
				}
				if strikeDamage(x)+strikeDamage(y) < a.Remaining() && !Kw(x).Deathtouch && !Kw(y).Deathtouch {
					continue
				}
				// the attacker kills what its power reaches, the most
				// valuable first
				lost := 0.0
				power := strikeDamage(pa)
				pair := []*KCard{x, y}
				sort.SliceStable(pair, func(i, j int) bool { return t.cv(pair[i]) > t.cv(pair[j]) })
				for _, bl := range pair {
					if power >= bl.Remaining() || (Kw(pa).Deathtouch && power > 0) {
						lost += t.cv(bl)
						power -= bl.Remaining()
					}
				}
				if gain := t.cv(a) - lost; gain > bestGain {
					bestGain, bestPair = gain, [2]*KCard{x, y}
				}
			}
		}
		if bestPair[0] != nil {
			assign(bestPair[0], a)
			assign(bestPair[1], a)
		}
	}
	// 2. free blocks (blocker survives)
	for _, a := range attackers {
		if blocked[a.Stable.ArenaID] || Kw(a).Trample {
			continue
		}
		var pick *KCard
		for _, bl := range mine {
			if !canBlock(bl, a) {
				continue
			}
			if blDies, _ := Fight(bl, pumped(a)); !blDies && (pick == nil || t.cv(bl) < t.cv(pick)) {
				pick = bl
			}
		}
		if pick != nil {
			assign(pick, a)
		}
	}
	// 3. even-or-better trades
	for _, a := range attackers {
		if blocked[a.Stable.ArenaID] {
			continue
		}
		var pick *KCard
		for _, bl := range mine {
			if !canBlock(bl, a) {
				continue
			}
			blDies, aDies := Fight(bl, a)
			if blDies && aDies && t.cv(a) >= t.cv(bl)-0.3 && (pick == nil || t.cv(bl) < t.cv(pick)) {
				pick = bl
			}
		}
		if pick != nil {
			assign(pick, a)
		}
	}
	// 4. survive: chump the biggest unblocked attackers
	incoming := func() int {
		n := 0
		for _, a := range attackers {
			if !blocked[a.Stable.ArenaID] {
				n += strikeDamage(a)
			}
		}
		if n > 0 {
			n += pump
		}
		return n
	}
	// 4a. chump a big hit when life gets low
	for {
		life := b.Life[b.Me] - incoming()
		if life > 6 || life <= 0 {
			break
		}
		var target *KCard
		for _, a := range attackers {
			if !blocked[a.Stable.ArenaID] && strikeDamage(a) >= 3 && !Kw(a).Trample && (target == nil || strikeDamage(a) > strikeDamage(target)) {
				target = a
			}
		}
		if target == nil {
			break
		}
		var pick *KCard
		for _, bl := range mine {
			if canBlock(bl, target) && t.cv(bl) <= 4 && (pick == nil || t.cv(bl) < t.cv(pick)) {
				pick = bl
			}
		}
		if pick == nil {
			break
		}
		assign(pick, target)
	}
	for incoming() >= b.Life[b.Me] {
		var target *KCard
		for _, a := range attackers {
			if !blocked[a.Stable.ArenaID] && (target == nil || strikeDamage(a) > strikeDamage(target)) {
				target = a
			}
		}
		if target == nil {
			break
		}
		var pick *KCard
		for _, bl := range mine {
			if canBlock(bl, target) && (pick == nil || t.cv(bl) < t.cv(pick)) {
				pick = bl
			}
		}
		if pick == nil {
			blocked[target.Stable.ArenaID] = true // cannot be stopped; look at the next
			continue
		}
		assign(pick, target)
	}
	return plan
}

// Scores returns Tactical's score of every candidate of a priority-style
// decision (the values Choose maximises; nil for combat scans). It does not
// update the in-game opponent model.
func (t *Tactical) Scores(d *Decision) []float64 {
	for i := range d.Candidates {
		switch d.Candidates[i].Kind() {
		case "choose_attacker_inclusion", "choose_blocker_inclusion":
			return nil
		}
	}
	b := NewBoard(d)
	out := make([]float64, len(d.Candidates))
	for i := range d.Candidates {
		out[i] = t.score(d, b, i)
	}
	return out
}

// AttackPlan returns the attack plan Tactical made for decision group grp
// (attacker arena id -> attacks), nil when it has none for that group.
func (t *Tactical) AttackPlan(grp int64) map[uint32]bool {
	if t.attackGrp != grp {
		return nil
	}
	return t.attackPlan
}

// BlockPlan returns the block plan Tactical made for decision group grp
// (blocker arena id -> attacker arena id), nil when it has none.
func (t *Tactical) BlockPlan(grp int64) map[uint32]uint32 {
	if t.blockGrp != grp {
		return nil
	}
	return t.blockPlan
}
