package builtins

import (
	"math"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// tcre is one battlefield creature as the tactical seat sees it: the
// projected (post-effects) power, toughness and keywords of a public
// permanent, plus the printed profile its name resolves to.
type tcre struct {
	id                                  state.ObjID
	cv                                  *view.CardView
	p                                   *tProfile
	mine                                bool
	pow, tough, dmg                     int32
	flying, reach, deathtouch, lifelink bool
	firstStrike, doubleStrike, trample  bool
	vigilance, menace, hexproof, indest bool
	defender, haste, unblockable        bool
	tapped, sick, attacking             bool
	blocked                             bool
}

func (c *tcre) remTough() int32 { return c.tough - c.dmg }

// canAttack reports whether the creature can attack on its controller's next
// turn (it untaps; summoning sickness wears off).
func (c *tcre) canAttack() bool { return !c.defender && c.pow > 0 }

func newTcre(cv *view.CardView, p *tProfile, mine bool) *tcre {
	c := &tcre{id: cv.ID, cv: cv, p: p, mine: mine, pow: cv.Power, tough: cv.Toughness, dmg: cv.Damage,
		tapped: cv.Tapped, sick: cv.SummonSick, attacking: cv.Attacking, blocked: len(cv.BlockedBy) > 0}
	for _, k := range cv.Keywords {
		switch strings.ToLower(cards.KeywordHead(k)) {
		case "flying":
			c.flying = true
		case "reach":
			c.reach = true
		case "deathtouch":
			c.deathtouch = true
		case "lifelink":
			c.lifelink = true
		case "first strike":
			c.firstStrike = true
		case "double strike":
			c.doubleStrike = true
		case "trample":
			c.trample = true
		case "vigilance":
			c.vigilance = true
		case "menace":
			c.menace = true
		case "hexproof", "shroud":
			c.hexproof = true
		case "indestructible":
			c.indest = true
		case "defender":
			c.defender = true
		case "haste":
			c.haste = true
		case "unblockable":
			c.unblockable = true
		}
	}
	if c.pow < 0 {
		c.pow = 0
	}
	return c
}

// tstate is the seat-visible feature extraction one decision is scored
// against. Everything here comes from the seat's projected View: public
// zones (battlefields, stack, graveyards, life, hand SIZES) and the seat's
// own private zones (hand, pool). The opponent's hand and every library are
// never read.
type tstate struct {
	w      *TacticalWeights
	v      *view.View
	me     state.PlayerID
	opp    state.PlayerID
	meP    *view.PlayerView
	oppP   *view.PlayerView
	objs   map[state.ObjID]*view.CardView // lookup only
	owner  map[state.ObjID]state.PlayerID // lookup only (controller of a battlefield object)
	cre    map[state.ObjID]*tcre          // lookup only
	mine   []*tcre
	theirs []*tcre

	myTurn, main, main1 bool
	step                string
	early               bool
	myLife, oppLife     int32
	producible          int32 // own untapped mana sources + floating pool
	lands, oppLands     int32
	foreign             *view.StackView // the top-most opponent spell on the stack
	stackEmpty          bool
	combat              bool // a combat step
	blocksDone          bool // blockers are declared (declare-blockers or later)

	myDmg, oppDmg     float64 // projected damage per attack
	myClock, oppClock float64 // projected turns to kill (math.Inf when no damage)

	// arch is the Archetype group's classification of the opponent; its zero
	// value means "unsure" and leaves every weight at its default. It is
	// populated only when the Archetype group is on.
	arch tArchState
}

func (t *tactical) newState(v *view.View, me state.PlayerID) *tstate {
	s := &tstate{w: &t.w, v: v, me: me, objs: map[state.ObjID]*view.CardView{},
		owner: map[state.ObjID]state.PlayerID{}, cre: map[state.ObjID]*tcre{}}
	s.myTurn = v.Active == me
	s.step = v.Step
	s.main = v.Phase == "main1" || v.Phase == "main2"
	s.main1 = v.Phase == "main1"
	s.combat = v.Phase == "combat"
	s.blocksDone = v.Step == "declare-blockers" || v.Step == "combat-damage" || v.Step == "end-combat"
	for i := range v.Players {
		p := &v.Players[i]
		if p.ID == me {
			s.meP = p
		} else if s.oppP == nil && !p.Lost {
			s.oppP = p
		}
	}
	if s.meP == nil {
		s.meP = &view.PlayerView{ID: me}
	}
	if s.oppP == nil {
		s.oppP = &view.PlayerView{ID: me ^ 1}
	}
	s.opp = s.oppP.ID
	s.myLife, s.oppLife = s.meP.Life, s.oppP.Life
	// Own turn number: the game's turn counter counts both players' turns.
	s.early = (v.Turn+1)/2 <= int32(t.w.EarlyTurns)
	add := func(zone []view.CardView, ctl state.PlayerID) {
		for i := range zone {
			cv := &zone[i]
			s.objs[cv.ID] = cv
			s.owner[cv.ID] = ctl
		}
	}
	for i := range v.Players {
		p := &v.Players[i]
		add(p.Battlefield, p.ID)
		add(p.Graveyard, p.ID)
		add(p.Exile, p.ID)
		if p.ID == me {
			add(p.Hand, p.ID) // own hand only: the opponent's is never read
		}
	}
	for i := range v.Stack {
		sv := &v.Stack[i]
		if sv.Card != nil {
			s.objs[sv.ID] = sv.Card
			s.owner[sv.ID] = sv.Controller
		}
	}
	s.stackEmpty = len(v.Stack) == 0
	for i := len(v.Stack) - 1; i >= 0; i-- {
		if v.Stack[i].Controller != me && v.Stack[i].Kind == "spell" {
			s.foreign = &v.Stack[i]
			break
		}
	}
	for _, pv := range []*view.PlayerView{s.meP, s.oppP} {
		mine := pv.ID == me
		for i := range pv.Battlefield {
			cv := &pv.Battlefield[i]
			if isLandView(cv) {
				if mine {
					s.lands++
				} else {
					s.oppLands++
				}
			}
			if mine && !cv.Tapped && cv.Produces != nil && !(isCreatureTypes(cv.Types) && cv.SummonSick) {
				s.producible++
			}
			if !isCreatureTypes(cv.Types) {
				continue
			}
			c := newTcre(cv, t.profile(cv), mine)
			s.cre[c.id] = c
			if mine {
				s.mine = append(s.mine, c)
			} else {
				s.theirs = append(s.theirs, c)
			}
		}
	}
	for _, n := range s.meP.Pool {
		s.producible += n
	}
	s.myDmg = attackDamage(s.mine, s.theirs)
	s.oppDmg = attackDamage(s.theirs, s.mine)
	s.myClock = clock(s.oppLife, s.myDmg)
	s.oppClock = clock(s.myLife, s.oppDmg)
	if t.base.Archetype {
		s.arch = t.classify(s.opp)
	}
	return s
}

func isCreatureTypes(types string) bool {
	for t := range strings.FieldsSeq(types) {
		if strings.EqualFold(t, "Creature") {
			return true
		}
	}
	return false
}

func isLandView(cv *view.CardView) bool {
	for t := range strings.FieldsSeq(cv.Types) {
		if strings.EqualFold(t, "Land") {
			return true
		}
	}
	return false
}

func clock(life int32, dmg float64) float64 {
	if life <= 0 {
		return 0
	}
	if dmg <= 0.01 {
		return math.Inf(1)
	}
	return math.Ceil(float64(life) / dmg)
}

// attackDamage projects the damage attackers deal per attack against
// defenders that block optimally-for-life: evasive attackers the defenders
// cannot block connect in full; the defenders' blockers each stop one of the
// biggest remaining ground attackers (a chump), trample carrying the excess
// over the blocker's toughness through.
func attackDamage(atk, def []*tcre) float64 {
	var flyBlock, ground int
	var blockers []*tcre
	for _, d := range def {
		if d.flying || d.reach {
			flyBlock++
		}
		blockers = append(blockers, d)
		ground++
	}
	var total float64
	var grounders []*tcre
	for _, a := range atk {
		if !a.canAttack() {
			continue
		}
		p := float64(a.pow)
		if a.doubleStrike {
			p *= 2
		}
		switch {
		case a.unblockable, a.flying && flyBlock == 0:
			total += p
		default:
			grounders = append(grounders, a)
		}
	}
	// Biggest attackers are blocked first (the defender stops what hurts most).
	sortCre(grounders, func(x, y *tcre) bool { return x.pow > y.pow || (x.pow == y.pow && x.id < y.id) })
	nb := ground
	for _, a := range grounders {
		p := float64(a.pow)
		if a.doubleStrike {
			p *= 2
		}
		need := 1
		if a.menace {
			need = 2
		}
		if nb >= need {
			nb -= need
			if a.trample {
				total += math.Max(0, p-2) // a typical blocker soaks about two
			}
			continue
		}
		total += p
	}
	_ = blockers
	return total
}

// sortCre is an insertion sort (tiny slices, deterministic, no reflection).
func sortCre(cs []*tcre, less func(x, y *tcre) bool) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && less(cs[j], cs[j-1]); j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

// creValue is a creature's worth in card-value points (a vanilla 2/2 is
// about 5; a card in hand is TacticalWeights.Card).
func (s *tstate) creValue(c *tcre) float64 {
	p, t := float64(c.pow), float64(c.remTough())
	if t < 0 {
		t = 0
	}
	v := 1.5*p + 1.0*t
	if c.defender {
		v = 0.5*p + 1.0*t
	}
	if c.flying {
		v += 1.0 * p
	}
	if c.deathtouch {
		v += 2
	}
	if c.firstStrike {
		v += 0.5 * p
	}
	if c.doubleStrike {
		v += 1.0 * p
	}
	if c.lifelink {
		v += 0.5 * p
	}
	if c.trample {
		v += 0.3 * p
	}
	if c.menace {
		v += 0.5 * p
	}
	if c.hexproof {
		v += 1
	}
	if c.indest {
		v += 3
	}
	if c.vigilance {
		v += 0.5
	}
	if c.p != nil {
		if c.p.manaSource {
			v += 2
		}
		if c.p.engine {
			v += 2
		}
	}
	return v
}

// keyPiece is the early-game extra worth of removing an opposing creature:
// a mana source (it accelerates every later turn), an engine (it keeps
// generating value) or an evasive attacker (it cannot be raced by blocks).
func (s *tstate) keyPiece(c *tcre) float64 {
	if !s.w.EarlyGame || c.p == nil {
		return 0
	}
	k := 0.0
	if c.p.manaSource {
		k += s.w.KeyPiece
		if s.early {
			k += s.w.KeyPiece
		}
	}
	if c.p.engine {
		k += s.w.KeyPiece
	}
	if c.flying && c.pow > 0 {
		k += s.w.KeyPiece * (0.5 + s.arch.tempoRisk)
	}
	return k
}

// lifeValue is the worth of `amount` life at total `life` (points per life
// rise as the total falls), plus the Lethal bonus when it kills.
//
// ownLifeValue is the worth of `amount` of OUR life, scaled by the Archetype
// group's ArchLife against a burn opponent (life is a resource they spend,
// so it is worth more). 1.0x when the group is off.
func (s *tstate) ownLifeValue(amount int32) float64 {
	return s.lifeValue(s.myLife, amount) * s.w.ArchLife
}

func (s *tstate) lifeValue(life, amount int32) float64 {
	v := 0.0
	for i := int32(0); i < amount; i++ {
		l := life - i
		switch {
		case l <= 0:
		case l <= 3:
			v += 1.6
		case l <= 6:
			v += 1.0
		case l <= 12:
			v += 0.6
		default:
			v += 0.35
		}
	}
	return v
}

// faceValue scores `dmg` damage to the opponent's face: life value, scaled
// by the race (Race group), plus the lethal and clock-turn bonuses.
func (s *tstate) faceValue(dmg int32) float64 {
	if dmg <= 0 {
		return 0
	}
	v := s.lifeValue(s.oppLife, dmg)
	if dmg >= s.oppLife {
		return v + s.w.Lethal
	}
	if s.w.Race {
		if s.ahead() {
			v *= s.w.FaceAhead
		} else {
			v *= s.w.FaceBehind
		}
		if s.myDmg > 0 && clock(s.oppLife-dmg, s.myDmg) < s.myClock {
			v += s.w.ClockTurn
		}
	}
	return v
}

// ahead reports whether we win the race: our clock is no slower than
// theirs (ties go to whoever attacks next, approximated as us).
func (s *tstate) ahead() bool {
	return s.myClock <= s.oppClock
}

// hand returns the seat's own hand.
func (s *tstate) hand() []view.CardView { return s.meP.Hand }

// countExpr evaluates the few Count$ shapes the benchmark's cards print
// against the seat-visible board: Count$Valid <filter>[/Times.N] (battlefield
// permanents), Count$ValidGraveyard <filter> (our graveyard), and
// Count$Metalcraft.A.B. ok is false for anything else.
func (s *tstate) countExpr(expr string) (int32, bool) {
	expr = strings.TrimSpace(expr)
	mult := int32(1)
	if i := strings.Index(expr, "/Times."); i >= 0 {
		if n, ok := literal(expr[i+len("/Times."):]); ok {
			mult = n
		}
		expr = expr[:i]
	}
	switch {
	case strings.HasPrefix(expr, "Count$Metalcraft."):
		parts := strings.Split(strings.TrimPrefix(expr, "Count$Metalcraft."), ".")
		if len(parts) == 2 {
			a, ok1 := literal(parts[0])
			b, ok2 := literal(parts[1])
			if ok1 && ok2 {
				if s.artifacts() >= 3 {
					return a * mult, true
				}
				return b * mult, true
			}
		}
	case strings.HasPrefix(expr, "Count$ValidGraveyard "):
		f := strings.TrimPrefix(expr, "Count$ValidGraveyard ")
		var n int32
		for i := range s.meP.Graveyard {
			if filterMatch(f, &s.meP.Graveyard[i], s.me, s.me) {
				n++
			}
		}
		return n * mult, true
	case strings.HasPrefix(expr, "Count$Valid "):
		f := strings.TrimPrefix(expr, "Count$Valid ")
		var n int32
		for _, p := range []*view.PlayerView{s.meP, s.oppP} {
			for i := range p.Battlefield {
				if filterMatch(f, &p.Battlefield[i], p.ID, s.me) {
					n++
				}
			}
		}
		return n * mult, true
	}
	return 0, false
}

// filterMatch reads a Forge card filter ("Elf", "Creature.YouCtrl",
// "Gate.YouCtrl", "Creature.YouOwn") against a CardView's type line.
func filterMatch(filter string, cv *view.CardView, ctl, me state.PlayerID) bool {
	for _, alt := range strings.Split(filter, ",") {
		typ, quals, _ := strings.Cut(strings.TrimSpace(alt), ".")
		if typ != "Card" && typ != "Permanent" && !hasWord(cv.Types, typ) {
			continue
		}
		ok := true
		for _, q := range strings.Split(quals, "+") {
			switch q {
			case "YouCtrl", "YouOwn":
				ok = ok && ctl == me
			case "OppCtrl", "OppOwn":
				ok = ok && ctl != me
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func hasWord(types, w string) bool {
	for t := range strings.FieldsSeq(types) {
		if strings.EqualFold(t, w) {
			return true
		}
	}
	return false
}

// canAfford is a conservative check that our untapped mana sources and pool
// can pay a Forge mana cost ("2 B B", "1 U", "RG RG"): one mana per source,
// each coloured pip needing a source (or floating mana) of its colour. A
// pursuit of an unaffordable play would tap sources for nothing.
func (s *tstate) canAfford(cost string, extra int32) bool {
	const syms = "WUBRGC"
	var total int32
	var pips [6]int32
	for _, f := range strings.Fields(strings.NewReplacer("{", " ", "}", " ").Replace(cost)) {
		if n, ok := literal(f); ok {
			total += n
			continue
		}
		switch {
		case f == "X" || f == "no" || f == "cost":
		case len(f) == 1 && strings.Contains(syms, f):
			pips[strings.Index(syms, f)]++
			total++
		default:
			total++ // hybrid / phyrexian: any source
		}
	}
	total += extra
	var have int32
	var colour [6]int32
	for i := range s.meP.Battlefield {
		cv := &s.meP.Battlefield[i]
		if cv.Tapped || cv.Produces == nil || (isCreatureTypes(cv.Types) && cv.SummonSick) {
			continue
		}
		have++
		listed := cv.Produces.Colour != [6]int32{}
		for c := 0; c < 6; c++ {
			if cv.Produces.Colour[c] > 0 || cv.Produces.Any && !listed {
				colour[c]++
			}
		}
	}
	for sym, n := range s.meP.Pool {
		have += n
		if i := strings.Index(syms, sym); i >= 0 && len(sym) == 1 {
			colour[i] += n
		}
	}
	if total > have {
		return false
	}
	for c := 0; c < 6; c++ {
		if pips[c] > colour[c] {
			return false
		}
	}
	return true
}

// handPressure scales mana value up while cards wait in hand for mana.
func (s *tstate) handPressure() float64 {
	return 1 + 0.25*float64(max(0, s.meP.HandSize-4))
}

// deckingRisk reports a library low enough, and no longer than the
// opponent's, that drawing extra cards loses the deck-out race of a stall.
func (s *tstate) deckingRisk() bool {
	return s.meP.LibrarySize < 12 && s.meP.LibrarySize <= s.oppP.LibrarySize+2
}
