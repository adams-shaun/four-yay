package v1agent

import "strings"

// The hint-free (sb-generic) halves of Tactical's card-specific decisions.
// Each function here replaces a hints.go lookup or a named branch of
// tactical.go with a rule over derived card knowledge (derive.go); no card
// or deck name appears in this file.

// NewGeneric builds the hint-free tactical policy: the same board reading,
// combat planning and scoring skeleton as Tactical, with every card- and
// deck-name rule replaced by one derived from card data.
func NewGeneric(opts TacticalOptions) *Tactical {
	t := NewTactical(opts)
	t.gen = true
	return t
}

// kn is a card's play knowledge: hand-written (hints.go) for the hinted
// build, derived for the generic one.
func (t *Tactical) kn(name string) hint {
	if t.gen {
		return profileFor(name).hint
	}
	return hintFor(name)
}

// cv is CreatureValue with this build's card knowledge.
func (t *Tactical) cv(c *KCard) float64 {
	if !t.gen {
		return CreatureValue(c)
	}
	if c == nil || !c.IsCreature() {
		return 0
	}
	v := baseCreatureValue(c) + profileFor(c.Name).bonus
	if Kw(c).MinimumBlockers >= 2 {
		v++ // hard to block
	}
	return v
}

func srcZone(src *ObjectRef) string {
	if src == nil {
		return "battlefield"
	}
	switch z := strings.ToLower(src.Zone); z {
	case "hand", "graveyard", "exile":
		return z
	}
	return "battlefield"
}

// ---- deck-level ----

func (t *Tactical) finisherIn(names map[string]int, b *Board) bool {
	for n, c := range names {
		if c > 0 && profileFor(n).gyFinisher {
			return true
		}
	}
	for _, g := range b.MyGrave {
		if profileFor(g.Name).gyFinisher {
			return true
		}
	}
	return false
}

// reanimatorSacN is the smallest creature-sacrifice flashback among the
// graveyard-to-battlefield reanimation spells in our library or
// graveyard (0 when there is none): the spell a self-mill leaves castable.
func (t *Tactical) reanimatorSacN(lib map[string]int, b *Board) int {
	best := 0
	consider := func(n string) {
		p := profileFor(n)
		if p.role == RoleReanimate && !p.reanimHand && p.flashSacN > 0 && (best == 0 || p.flashSacN < best) {
			best = p.flashSacN
		}
	}
	for n, c := range lib {
		if c > 0 {
			consider(n)
		}
	}
	for _, g := range b.MyGrave {
		consider(g.Name)
	}
	return best
}

// comboG is spyCombo recognised from what the cards do: a mill-until-land
// aimed at ourselves with no land left in the library mills it all; a
// reanimation spell with a creature-sacrifice flashback then returns a
// creature whose arrival deals damage per creature card in our graveyard.
func (t *Tactical) comboG(b *Board, enablerOnBoard bool) bool {
	lib := libraryCounts(t.deckList, b)
	if lib == nil || countWhere(lib, isLandCard) > 0 {
		return false
	}
	if !t.finisherIn(lib, b) {
		return false
	}
	sacN := t.reanimatorSacN(lib, b)
	if sacN == 0 {
		return false
	}
	creatures := len(Creatures(b.Mine))
	if !enablerOnBoard {
		creatures++
	}
	if creatures < sacN {
		return false
	}
	graveCreatures := countWhere(lib, isCreatureCard)
	for _, g := range b.MyGrave {
		if g.IsCreature() {
			graveCreatures++
		}
	}
	return graveCreatures+sacN-1 >= b.Life[b.Opp]
}

// finisherLethalG: a graveyard finisher is in our graveyard and returning
// it now kills (sacN more creature cards join the graveyard when the
// reanimation is flashed back).
func (t *Tactical) finisherLethalG(b *Board, sacN int) bool {
	fin, n := false, 0
	for _, g := range b.MyGrave {
		if profileFor(g.Name).gyFinisher {
			fin = true
		}
		if g.IsCreature() {
			n++
		}
	}
	return fin && n-1+sacN >= b.Life[b.Opp]
}

// graveSynergy: our deck gets better with cards in our graveyard.
func (t *Tactical) graveSynergy() bool {
	for n, c := range t.deckList {
		if c > 0 && profileFor(n).gyScaling {
			return true
		}
	}
	return false
}

// ---- spells on the stack ----

// counterFits reports whether a counter effect e may and should target
// the opposing stack item s now. self is the counter's source (it counts
// itself for "number of <subtype> you control" once it resolves).
func (t *Tactical) counterFits(b *Board, e *EffectFact, s *KStackItem, self string) bool {
	tk := KernelCardByID(s.Source.CardDBID)
	if e == nil || tk == nil {
		return false
	}
	f := e.Tgt
	if f != nil {
		ok := len(f.Types) == 0
		for _, ty := range f.Types {
			switch ty {
			case "card", "spell":
				ok = true
			case "instant", "sorcery", "creature", "artifact", "enchantment", "land":
				if tk.IsType(strings.ToUpper(ty[:1]) + ty[1:]) {
					ok = true
				}
			}
		}
		if !ok {
			return false
		}
		for _, ty := range f.Non {
			if len(ty) > 1 && tk.IsType(strings.ToUpper(ty[:1])+ty[1:]) {
				return false
			}
		}
		if len(f.Colors) > 0 && !hasColor(tk, f.Colors) {
			return false
		}
		if f.CMCLEX {
			x, ok := evalAmount(e.TgtX, b, true)
			if !ok {
				return false
			}
			if e.TgtX != nil && e.TgtX.Kind == "count" && matchCard(e.TgtX.Of, selfCard(self), true) {
				x++ // the source joins the count
			}
			if tk.MV > x {
				return false
			}
		}
	}
	if e.Cond == "targeted_color" && e.CondColor != "" && !hasColor(tk, []string{e.CondColor}) {
		return false
	}
	if e.Unless > 0 && UntappedLands(b.Theirs) >= e.Unless {
		return false
	}
	return true
}

func selfCard(name string) *KCard {
	c := &KCard{Name: name}
	if k := KernelCardByName(name); k != nil {
		setTypes(c, k)
	}
	return c
}

var colorLetter = map[string]byte{"white": 'W', "blue": 'U', "black": 'B', "red": 'R', "green": 'G'}

func hasColor(k *KernelCard, colors []string) bool {
	cc := k.CostColors()
	for _, c := range colors {
		if strings.IndexByte(cc, colorLetter[c]) >= 0 {
			return true
		}
	}
	return false
}

// counterWorth scores countering the opposing top spell.
func (t *Tactical) counterWorth(s *KStackItem) float64 {
	tk := KernelCardByID(s.Source.CardDBID)
	if tk != nil && tk.MV <= 1 && !tk.IsType("Creature") {
		r := t.kn(tk.Name).role
		if r != RoleBurn && r != RoleRemoval {
			return 5
		}
	}
	return 90
}

// ---- removal targets ----

// permValue is what removing an opposing permanent is worth.
func (t *Tactical) permValue(c *KCard) float64 {
	if c.IsCreature() {
		return t.cv(c)
	}
	v := 1.5
	if k := KernelCardByName(c.Name); k != nil {
		v += 0.5 * float64(k.MV)
	}
	if c.IsLand() {
		v = 0.8
	}
	if c.IsToken {
		v = 0.3
	}
	return v
}

func untargetable(c *KCard) bool {
	return Kw(c).Hexproof || Kw(c).ProtMonocolored
}

// bestRemovalG is the value of the best opposing permanent pl may remove.
func (t *Tactical) bestRemovalG(b *Board, pl *play) float64 {
	if pl.edict {
		worst := -1.0
		for _, c := range Creatures(b.Theirs) {
			if v := t.cv(c); worst < 0 || v < worst {
				worst = v
			}
		}
		return worst
	}
	best := 0.0
	for _, c := range b.Theirs {
		if untargetable(c) || !matchCard(pl.tgt, c, false) {
			continue
		}
		if pl.tgt == nil && !c.IsCreature() {
			continue
		}
		if pl.eff != nil && pl.eff.Cond == "targeted_color" && pl.eff.CondColor != "" &&
			colorBit(colorLetter[pl.eff.CondColor])&c.Characteristics.ColorMask == 0 {
			continue
		}
		if pl.eff != nil && pl.eff.API == "Destroy" && Kw(c).Indestructible {
			continue
		}
		if v := t.permValue(c); v > best {
			best = v
		}
	}
	return best
}

// bestKillG is the best opposing creature dmg damage kills (value 0 when
// none).
func (t *Tactical) bestKillG(b *Board, dmg int) float64 {
	best := 0.0
	for _, c := range Creatures(b.Theirs) {
		if untargetable(c) || Kw(c).Indestructible || c.Remaining() > dmg {
			continue
		}
		if v := t.cv(c)*1.2 + 0.5; v > best {
			best = v
		}
	}
	return best
}

// ---- casting ----

func (t *Tactical) handBurnG(b *Board) int {
	n := 0
	for _, h := range b.Hand {
		p := profileFor(h.Name)
		for i := range p.plays {
			if p.plays[i].role == RoleBurn && p.plays[i].face {
				n += playDamage(&p.plays[i], b, true)
				break
			}
		}
	}
	return n
}

func handHasMadness(b *Board) bool {
	for _, h := range b.Hand {
		if profileFor(h.Name).madness {
			return true
		}
	}
	return false
}

// burnDamageG is the damage the card's first burn play deals now.
func (t *Tactical) burnDamageG(b *Board, name string) int {
	p := profileFor(name)
	if pl := t.modePlay(name); pl != nil && pl.role == RoleBurn {
		return playDamage(pl, b, true)
	}
	for i := range p.plays {
		if p.plays[i].role == RoleBurn {
			return playDamage(&p.plays[i], b, true)
		}
	}
	return 0
}

// modePlay is the play of the mode we last chose for a modal source.
func (t *Tactical) modePlay(name string) *play {
	p := profileFor(name)
	if len(p.plays) < 2 {
		return nil
	}
	if idx, ok := t.lastMode[normName(name)]; ok && idx >= 0 && idx < len(p.plays) {
		return &p.plays[idx]
	}
	return nil
}

func (t *Tactical) castScoreG(d *Decision, b *Board, i int) float64 {
	c := &d.Candidates[i]
	name := srcName(c)
	src := c.Semantic.Source()
	p := profileFor(name)
	k, f := p.kc, p.fact
	instant := k.IsType("Instant") || f.HasKeyword("Flash")
	fromGrave := src != nil && src.Zone == "graveyard"
	sorcerySpeed := b.MyTurn && b.inMain() && len(b.Stack) == 0

	if hasXCost(name) && manaSources(b) < 3 {
		return -5
	}
	if p.oneShotMana {
		// cast a one-shot mana source only when it lets a spell come down a
		// turn early
		if sorcerySpeed && Lands(b.Mine) <= 2 {
			for _, hc := range b.Hand {
				if hk := KernelCardByName(hc.Name); hk != nil && !hk.IsType("Land") && hk.MV == manaSources(b)+1 {
					return 6
				}
			}
		}
		return -10
	}
	if k.IsType("Creature") || f.HasType("creature") {
		return t.creatureCastG(b, p, name, instant, sorcerySpeed)
	}
	if p.sacCost && b.fodder() == 0 {
		return -5
	}
	if len(p.plays) == 0 {
		switch p.role {
		case RoleNever:
			return -10
		case RoleArtifact:
			if sorcerySpeed {
				return 42
			}
			return -5
		case RoleDraw:
			if sorcerySpeed {
				return 32
			}
			return -5
		}
		if sorcerySpeed || instant {
			return 20
		}
		return -5
	}
	best := -1e9
	for j := range p.plays {
		if s := t.playScore(b, p, &p.plays[j], fromGrave, instant, sorcerySpeed); s > best {
			best = s
		}
	}
	return best
}

func (t *Tactical) creatureCastG(b *Board, p *profile, name string, instant, sorcerySpeed bool) float64 {
	f := p.fact
	v := 3.0
	if f != nil && f.HasPT {
		v = 1 + float64(f.Power) + float64(f.Toughness)/2
	}
	v += p.bonus
	if p.millUntil && sorcerySpeed && t.comboG(b, false) {
		return 180
	}
	if p.counterETB != nil {
		if s := b.oppSpellOnStack(); s != nil && t.counterFits(b, p.counterETB, s, name) {
			return 95
		}
	}
	if p.flash || (instant && !b.MyTurn) {
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
	if p.engine {
		v += 5
	}
	return 40 + v*2
}

// playScore scores casting a spell for one of its plays now.
func (t *Tactical) playScore(b *Board, p *profile, pl *play, fromGrave, instant, sorcerySpeed bool) float64 {
	oppStack := b.oppSpellOnStack()
	switch pl.role {
	case RoleNever:
		return -10
	case RoleCounter:
		if oppStack == nil || !t.counterFits(b, pl.eff, oppStack, p.name) {
			return -10
		}
		return t.counterWorth(oppStack)
	case RoleBurn:
		dmg := playDamage(pl, b, true)
		if dmg <= 0 {
			return -5
		}
		if !pl.face {
			v := t.bestKillG(b, dmg)
			if v < 1.5 {
				return -5
			}
			return 45 + v*3
		}
		score, face := t.bestBurnTarget(b, dmg)
		if score >= 1000 {
			return 200
		}
		if p.altSacLands > 0 && !(b.Life[b.Opp] <= t.handBurnG(b) || Lands(b.Mine) >= 6) {
			return -5
		}
		if fromGrave && !(face && dmg >= b.Life[b.Opp]) && score < 3 {
			return -5
		}
		if face {
			if b.Life[b.Opp] <= t.handBurnG(b)+2 {
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
		v := t.bestRemovalG(b, pl)
		if v < 1.5 {
			return -5
		}
		if pl.bounce && v < 3 {
			return -5
		}
		return 50 + v*3
	case RoleSweeper:
		dmg := playDamage(pl, b, true)
		kills := 0.0
		for _, cr := range Creatures(b.Theirs) {
			if matchCard(pl.eff.Each, cr, false) && cr.Remaining() <= dmg && !Kw(cr).Indestructible {
				kills += t.cv(cr)
			}
		}
		for _, cr := range Creatures(b.Mine) {
			if matchCard(pl.eff.Each, cr, true) && cr.Remaining() <= dmg && !Kw(cr).Indestructible {
				kills -= t.cv(cr)
			}
		}
		if kills >= 2 {
			return 45 + kills*3
		}
		if t.burnDeck() && pl.eff.EachPlayer == "opp" && (b.oppEndStep() || b.Phase == "main2") {
			return 20
		}
		return -5
	case RoleProtect:
		if !b.MyTurn && (b.Phase == "declare_blockers" || b.Phase == "declare_attackers") && len(b.Combat.Attackers) >= 2 {
			return 50
		}
		return -5
	case RolePump:
		return t.pumpScoreG(b, pl, sorcerySpeed)
	case RoleReanimate:
		best := 0.0
		for _, g := range b.MyGrave {
			if g.IsCreature() {
				if v := float64(g.Power()+g.Toughness()) / 2; v > best {
					best = v
				}
			}
		}
		if p.reanimHand {
			if (p.reanimLife && b.Life[b.Me] <= 8) || (best >= 1 && b.oppEndStep()) {
				return 35 + best
			}
			return -5
		}
		sacN := 0
		if fromGrave {
			sacN = p.flashSacN
		}
		if t.finisherLethalG(b, sacN) {
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
	case RoleToken:
		if sorcerySpeed {
			return 48
		}
		if instant && b.oppEndStep() {
			return 40
		}
		return -5
	case RoleDraw:
		if p.looter && !(flooded(b) || handHasMadness(b)) {
			return -5
		}
		if pl.eff != nil && pl.eff.API == "Discard" {
			if sorcerySpeed || (instant && b.oppEndStep()) {
				return 33
			}
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
	case RoleNone:
		if pl.pol < 0 {
			return -5 // graveyard hate: a charm mode, not a reason to cast
		}
	}
	if sorcerySpeed || instant {
		return 20
	}
	return -5
}

// pumpScoreG: a combat trick is cast after blocks when it wins a fight or
// the game; a permanent buff at sorcery speed; a pump with a side effect
// (a clue, a card) at the opponent's end step.
func (t *Tactical) pumpScoreG(b *Board, pl *play, sorcerySpeed bool) float64 {
	e := pl.eff
	if e == nil {
		return -5
	}
	if e.API == "PutCounter" || e.API == "Attach" {
		if sorcerySpeed && len(Creatures(b.Mine)) > 0 {
			return 25
		}
		return -5
	}
	pw := e.PumpPower
	if pw == 0 && e.PumpX != nil {
		pw, _ = evalAmount(e.PumpX, b, true)
	}
	deathtouch := false
	for _, k := range e.Keywords {
		if strings.EqualFold(k, "Deathtouch") {
			deathtouch = true
		}
	}
	if b.Phase == "declare_blockers" && b.Combat.BlockersDeclared {
		blocks := b.Combat.Blocks()
		unblocked := 0
		for _, u := range t.unblockedAttackers(b) {
			unblocked += strikeDamage(u)
		}
		if b.MyTurn && unblocked > 0 && pw > 0 && unblocked+pw >= b.Life[b.Opp] && unblocked < b.Life[b.Opp] {
			return 150
		}
		// a fight of ours the pump turns around
		for _, a := range b.Combat.Attackers {
			ac := b.Card(a.ArenaID)
			if ac == nil {
				continue
			}
			for _, br := range blocks[a.ArenaID] {
				bc := b.Card(br.ArenaID)
				if bc == nil {
					continue
				}
				mine, other := ac, bc
				if ac.Stable.Controller != b.Seat {
					mine, other = bc, ac
				}
				if mine.Stable.Controller != b.Seat {
					continue
				}
				myDies, theyDie := Fight(mine, other)
				pumped := *mine
				pp, pt := mine.Power()+pw, mine.Toughness()+e.PumpToughness
				pumped.Characteristics.Power, pumped.Characteristics.Toughness = &pp, &pt
				if deathtouch {
					pumped.Characteristics.Keywords.Deathtouch = true
				}
				myDies2, theyDie2 := Fight(&pumped, other)
				if (myDies && !myDies2) || (!theyDie && theyDie2) {
					return 40 + t.cv(other)
				}
			}
		}
	}
	if len(e.Chain) > 0 && b.oppEndStep() {
		return 8
	}
	return -5
}

// ---- mode, option and cost choices ----

func (t *Tactical) castModeScoreG(b *Board, name, mode string) float64 {
	p := profileFor(name)
	if mode == "alternative" {
		switch {
		case p.altSacLands > 0:
			if b.Life[b.Opp] <= t.handBurnG(b) || Lands(b.Mine) >= 6 {
				return 2
			}
			return -1
		case p.altFree:
			return 2
		}
	}
	if mode == "normal" {
		return 1
	}
	return 0.5
}

// modeScoreG values a modal spell's mode by what it would do now.
func (t *Tactical) modeScoreG(b *Board, name string, idx int) float64 {
	p := profileFor(name)
	if idx < 0 || idx >= len(p.plays) {
		return -float64(idx) * 0.01
	}
	pl := &p.plays[idx]
	e := pl.eff
	if e != nil && e.Cost != nil {
		// "pay this to get the effect" modes
		switch {
		case e.Cost.Discard > 0:
			if flooded(b) || handLands(b) >= 1 {
				return 2
			}
			return 1
		case e.Cost.Sac != nil:
			if Lands(b.Mine) >= 6 {
				return 1.5
			}
			return 0.5
		}
	}
	switch pl.role {
	case RoleCounter:
		if s := b.oppSpellOnStack(); s != nil && t.counterFits(b, e, s, name) {
			return 10
		}
		return -10
	case RoleBurn:
		dmg := playDamage(pl, b, true)
		if pl.face && dmg >= b.Life[b.Opp] {
			return 20
		}
		if v := t.bestKillG(b, dmg); v > 0 {
			return 3 + v
		}
		if pl.face {
			return 1
		}
		return -1
	case RoleRemoval:
		if v := t.bestRemovalG(b, pl); v > 0 {
			return 3 + v
		}
		return -2
	case RoleDraw, RoleToken:
		return 2
	case RolePump:
		if len(Creatures(b.Mine)) > 0 {
			return 1.5
		}
		return -1
	case RoleNone:
		n := 0
		for _, g := range b.TheirGrave {
			if g.IsCreature() {
				n++
			}
		}
		return 0.2 * float64(n)
	}
	return -float64(idx) * 0.01
}

func (t *Tactical) optionScoreG(b *Board, name string, idx int) float64 {
	if hasXCost(name) {
		return float64(idx)
	}
	if profileFor(name).initiative {
		return roomOptionScore(b, idx)
	}
	return -float64(idx) * 0.01
}

// ---- activated abilities ----

func (t *Tactical) abilityScoreG(d *Decision, b *Board, i int) float64 {
	c := &d.Candidates[i]
	name := srcName(c)
	src := c.Semantic.Source()
	p := profileFor(name)
	u, ok := p.abBy[srcZone(src)]
	if !ok {
		u = p.ab
	}
	if u == abMain2 {
		if b.MyTurn && b.Phase == "main2" && len(b.Stack) == 0 {
			return 12
		}
		return -5
	}
	if u == abStun {
		e := p.abEff[srcZone(src)]
		if e != nil && e.Counter == "m1m1" {
			// -1/-1 counters kill only what they shrink to 0 toughness
			n := e.CounterN
			if n == 0 {
				n = 1
			}
			if (b.oppEndStep() || b.Phase == "declare_blockers") && t.bestKillG(b, n) >= 2.5 {
				return 30
			}
			return -5
		}
		if e != nil && e.API == "Destroy" {
			pl := play{role: RoleRemoval, eff: e, tgt: e.Tgt}
			if b.oppEndStep() && t.bestRemovalG(b, &pl) >= 2.5 {
				return 12
			}
			return -5
		}
	}
	return t.abilityScoreFor(d, b, i, u)
}

// ---- opposing combat tricks ----

// oppPumpG is the largest +X/+X the opponent can give one attacker from
// an untapped activated pump it can pay for.
func (t *Tactical) oppPumpG(b *Board) int {
	best := 0
	for _, c := range b.Theirs {
		if c.Tapped {
			continue
		}
		e := profileFor(c.Name).pumpAb
		if e == nil {
			continue
		}
		cost := e.Cost
		if cost == nil {
			cost = &CostFact{}
		}
		if cost.Tap && c.IsCreature() && c.SummoningSick {
			continue
		}
		mana := UntappedLands(b.Theirs)
		if c.IsLand() && cost.Tap {
			mana--
		}
		if cost.Mana > mana {
			continue
		}
		x := e.PumpPower
		if e.PumpX != nil {
			x, _ = evalAmount(e.PumpX, b, false)
		}
		if x > best {
			best = x
		}
	}
	return best
}

// observeOwnCards records our own cards as the board shows them and, when
// a new one appears, re-reads the deck style from everything seen so far
// (the fallback for a deck the catalog does not list).
func (t *Tactical) observeOwnCards(b *Board) {
	if b.Thin || t.seen == nil {
		return
	}
	grew := false
	add := func(r KRef, name string) {
		if _, ok := t.seen[r.ArenaID]; !ok && name != "" {
			t.seen[r.ArenaID] = name
			grew = true
		}
	}
	for _, h := range b.Hand {
		add(h.Stable, h.Name)
	}
	for _, c := range b.Mine {
		if !c.IsToken && c.Stable.Owner == b.Seat {
			add(c.Stable, c.Name)
		}
	}
	for i := range b.MyGrave {
		add(b.MyGrave[i].Stable, b.MyGrave[i].Name)
	}
	if !grew {
		return
	}
	counts := map[string]int{}
	for _, n := range t.seen {
		counts[normName(n)]++
	}
	t.style = styleOf(counts)
}
