package botpolicy

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// refBoard is the Board as it was before its four keyed fields became
// IDTables: the same facts in Go maps. refBoardFromGame is the reference
// TestBoardTablesMatchMapReference holds the table-backed fill to over
// whole bot-played games.
type refBoard struct {
	OwnDeck                   *deck.Manifest
	IsMain, FirstMain, MyTurn bool
	Creatures                 map[state.ObjID]Creature
	Life                      map[state.PlayerID]int32
	Cards                     map[state.ObjID]Card
	Commanders                map[state.ObjID]Commander
	Stack                     []StackEntry
	Pool, PoolRestricted      state.Mana
	Step                      state.Step
	LibrarySize, HandSize     int32
}

// refBoardFromGame is the pre-table BoardFromGameInto, verbatim but for
// fresh maps in place of cleared ones.
func refBoardFromGame(g *state.Game, ch Chars, me state.PlayerID) refBoard {
	b := &refBoard{}
	if sc, ok := ch.(derivedReadScoper); ok {
		sc.BeginDerivedReads()
		defer sc.EndDerivedReads()
	}
	combined, hasCombined := ch.(combinedChars)
	b.Creatures = make(map[state.ObjID]Creature)
	b.Life = make(map[state.PlayerID]int32)
	b.Cards = make(map[state.ObjID]Card)
	b.Commanders = make(map[state.ObjID]Commander)
	// The public stack census (C8's facts): the stack's own bottom-to-top
	// order, truncated in place so the reused Board's slice never carries a
	// stale entry from the previous refill (the same clear-the-buckets
	// discipline the maps above get). IsSpell is o.Ability == nil — exactly
	// the test the view-shaped half mirrors as StackView.Kind == "spell"
	// (view/view.go's stackViews projects an ability object as
	// "trigger"/"ability" and a card object as "spell"), so the two halves
	// agree entry for entry, order included, on every intent of a whole
	// game (seat/integration_test.go's parity tests).
	b.Stack = b.Stack[:0]
	for _, id := range g.Stack {
		o := g.Obj(id)
		if o == nil {
			continue
		}
		var cmc int32
		var manaCost string
		if f := o.Face(); f != nil {
			cmc = cmcOfFace(f)
			// Only a spell carries a printed payment; an ability object is
			// Face-less anyway, and the view half's StackView.Card is nil for
			// a "trigger"/"ability", so guarding on IsSpell keeps the halves
			// in step even if a face ever rides an ability stack object.
			if o.Ability == nil {
				manaCost = f.ManaCost
			}
		}
		b.Stack = append(b.Stack, StackEntry{ID: id, Controller: o.Controller, IsSpell: o.Ability == nil, CMC: cmc, ManaCost: manaCost})
	}
	// The manifest is read, never written, by everything a Board reaches,
	// so the engine's shared read-only pointer (rules.Engine.OwnDeckShared)
	// serves it without the per-decision copy OwnDeck makes; a Chars that
	// only offers OwnDeck still gets its copy.
	if shared, ok := ch.(interface {
		OwnDeckShared(state.PlayerID) *deck.Manifest
	}); ok {
		b.OwnDeck = shared.OwnDeckShared(me)
	} else if manifests, ok := ch.(interface {
		OwnDeck(state.PlayerID) *deck.Manifest
	}); ok {
		b.OwnDeck = manifests.OwnDeck(me)
	} else {
		b.OwnDeck = nil
	}
	b.IsMain = g.Step.IsMain()
	// The cast scorer's two board-half features (cast.go): FirstMain is the
	// FIRST main phase (the Precombat feature) and MyTurn whether the
	// deciding seat is the active player (the InstantOnOwnTurn feature's
	// "own main phase" half -- a main phase can belong to another seat, so
	// IsMain alone cannot say it). Same function of the same engine state
	// the view half reads off the projected View (v.Phase == "main1",
	// v.Active == v.Viewer), so the halves agree wherever they agree on
	// IsMain itself.
	b.FirstMain = g.Step == state.StepMain1
	b.MyTurn = g.Active == me
	// The exact engine step (the cast scorer's timing features): the game
	// half reads g.Step directly; the view half parses the projected
	// View.Step string (view/view.go sets it to g.Step.String()) back
	// through state.ParseStep, so both halves name the same step.
	b.Step = g.Step
	b.Pool = g.Players[me].Pool
	b.PoolRestricted = RestrictedPool(g.Players[me].RestrictedMana)
	b.LibrarySize = int32(len(g.Zone(state.ZLibrary, me)))
	b.HandSize = int32(len(g.Zone(state.ZHand, me)))
	for i := range g.Players {
		p := &g.Players[i]
		b.Life[p.ID] = p.Life
		for _, id := range g.Zone(state.ZBattlefield, p.ID) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil || o.Ephemeral() || !o.Face().IsCreature() {
				continue
			}
			var power, toughness int32
			var keywords []string
			if hasCombined {
				power, toughness, keywords = combined.Characteristics(id)
				keywords = append([]string(nil), keywords...)
			} else {
				power = ch.Power(id)
				toughness = ch.Toughness(id)
				keywords = append([]string(nil), ch.Keywords(id)...)
			}
			b.Creatures[id] = Creature{
				Power:      power,
				Toughness:  toughness,
				Damage:     o.Damage,
				Keywords:   keywords,
				Tapped:     o.Tapped,
				Controller: o.Controller,
			}
		}
	}
	// The commander bookkeeping: every commander in the match, in the
	// dense order rules.New assigns at genesis (player order, then each
	// player's CmdCasts-parallel Commanders order) — the same index every
	// player's CmdDamage slice is keyed by, transposed here to the
	// per-commander, per-damaged-player shape the clock rules read
	// (closesClock). InCommandZone is zone-LIST membership, the exact
	// mirror of the view half's p.Command membership, so a cast commander
	// (moved out of the zone list) reads false on both halves.
	dense := 0
	for i := range g.Players {
		p := &g.Players[i]
		for k, id := range p.Commanders {
			var casts int32
			if k < len(p.CmdCasts) {
				casts = p.CmdCasts[k]
			}
			cmdr := Commander{Casts: casts}
			for _, zid := range g.Zone(state.ZCommand, p.ID) {
				if zid == id {
					cmdr.InCommandZone = true
					break
				}
			}
			for q := range g.Players {
				// Guarded to totality: a game whose CmdDamage was never
				// sized (a non-Commander game, or a hand-built state) reads
				// nothing here, never a panic.
				if dense < len(g.Players[q].CmdDamage) && g.Players[q].CmdDamage[dense] != 0 {
					if cmdr.Damage == nil {
						cmdr.Damage = make(map[state.PlayerID]int32, len(g.Players))
					}
					cmdr.Damage[g.Players[q].ID] = g.Players[q].CmdDamage[dense]
				}
			}
			b.Commanders[id] = cmdr
			dense++
		}
	}
	// The casting Card census: every object in the deciding seat's own hand,
	// graveyard, battlefield and command zone — exactly the zones
	// BoardFromView fills from the viewer's own Hand/Graveyard/Battlefield/
	// Command CardViews. Reading the face's Types and ManaCost and the
	// engine's derived Power here, and the CardView's matching fields on the
	// view side, fills the same fact with the same function (CmcOf,
	// hasTypeWord), so a card ranks identically on both halves — including
	// a commander sitting in the command zone, which is why the casting
	// rule can read its power and mana value like any other castable. The
	// zone walk also fills the two tap-gate facts (tap.go, T1): ManaCost is
	// the printed cost the gate re-parses for coloured pips, and Castable
	// is the zone membership that says whether a card is worth mana at all
	// — true for a hand card (the engine offers its cast as soon as the
	// pool pays the cost), true for the command zone (a commander the CR
	// 903.8 tax prices), true for a graveyard card with the Flashback
	// keyword (derived, mirroring rules/legal.go's own flashback gate read
	// off the same Derived keyword list the View projects), and false for
	// the battlefield, whose permanents are already cast.
	for _, z := range [...]state.Zone{state.ZHand, state.ZGraveyard, state.ZBattlefield, state.ZCommand} {
		for _, id := range g.Zone(z, me) {
			o := g.Obj(id)
			if o == nil || o.Ephemeral() {
				continue
			}
			f := o.Face()
			if f == nil {
				continue
			}
			var power, toughness int32
			var castable, instantSpeed bool
			if hasCombined {
				var keywords []string
				power, toughness, keywords = combined.Characteristics(id)
				castable = z == state.ZHand || z == state.ZCommand || (z == state.ZGraveyard && hasFlashback(keywords))
				instantSpeed = f.TypeLineHas("Instant", twInstant) || hasFlash(keywords)
			} else {
				power = ch.Power(id)
				toughness = ch.Toughness(id)
				castable = z == state.ZHand || z == state.ZCommand || (z == state.ZGraveyard && hasFlashback(ch.Keywords(id)))
				instantSpeed = f.TypeLineHas("Instant", twInstant) || hasFlash(ch.Keywords(id))
			}
			b.Cards[id] = Card{
				PrintedName:   f.Name,
				Creature:      f.IsCreature(),
				Power:         power,
				Toughness:     toughness,
				CMC:           cmcOfFace(f),
				Basic:         f.TypeLineHas("Basic", twBasic),
				AttachedTo:    o.AttachedTo,
				Activated:     o.ActivatedThisTurn,
				ManaCost:      f.ManaCost,
				Castable:      castable,
				OnBattlefield: z == state.ZBattlefield,
				Tapped:        o.Tapped,
				Sick:          o.SummonSick,
				Produces:      f.ManaProduction(),
				InstantSpeed:  instantSpeed,
				Counter:       f.SpellAbility() != nil && f.SpellAbility().API == "Counter",
			}
		}
	}
	// The public battlefield census (vote_card1): every OTHER player's
	// battlefield permanents — public information under CR 400.2, and
	// exactly the objects a ballot offers a voter (VoteCard$'s Council's
	// Judgment filter names only permanents the caster does not control) —
	// filled with the same WORTH facts the viewer's own battlefield walk
	// fills above (Creature/Power/Toughness/CMC/Basic/ManaCost, what
	// cardWorth prices), so a card ballot's bot policy prices an offered
	// opponent permanent instead of reading the zero Card{} it got when
	// b.Cards held the deciding seat's own zones alone.
	//
	// The seat-relative facts stay ZERO on a foreign entry: OnBattlefield,
	// Produces, Tapped, Castable, InstantSpeed and AttachedTo are the
	// deciding seat's OWN-board facts (cast.go's land-drop greedy and
	// reserve hold the invariant that every OnBattlefield/Castable entry is
	// a source the seat itself controls — producibleMana and
	// availableColours would otherwise count an opponent's lands as the
	// seat's own mana), and a foreign permanent must never inflate them.
	// Activated is the public census A5 reads, filled for foreign sources
	// too (an "any player may activate" ability, Lethal Vapors).
	for i := range g.Players {
		p := &g.Players[i]
		if p.ID == me {
			continue
		}
		for _, id := range g.Zone(state.ZBattlefield, p.ID) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil || o.Ephemeral() {
				continue
			}
			if o.FaceDown {
				// CR 708.5 redaction parity: another seat's facedown
				// permanent projects as a stripped CardView (ID, Controller,
				// Owner, nothing printed), so the view half's fillZone lands
				// a zero-fact entry for it and this half writes the same
				// entry, never a printed-face fact the voter cannot see.
				b.Cards[id] = Card{}
				continue
			}
			var power, toughness int32
			if cr, seen := b.Creatures[id]; seen {
				// The public creature census above (the ZBattlefield pass that
				// walks every seat) already queried this object's combined
				// characteristics; reuse them rather than querying the same
				// object twice (TestBoardFromGameUsesCombinedCharacteristicsOncePerObject
				// pins one combined query per projected object).
				power, toughness = cr.Power, cr.Toughness
			} else if hasCombined {
				power, toughness, _ = combined.Characteristics(id)
			} else {
				power = ch.Power(id)
				toughness = ch.Toughness(id)
			}
			f := o.Face()
			b.Cards[id] = Card{
				PrintedName: f.Name,
				Creature:    f.IsCreature(),
				Power:       power,
				CMC:         cmcOfFace(f),
				Basic:       f.TypeLineHas("Basic", twBasic),
				ManaCost:    f.ManaCost,
				Toughness:   toughness,
				Activated:   o.ActivatedThisTurn,
			}
		}
	}
	return *b
}

// tableMatchesMap reports the first difference between a table and the
// reference map it must equal entry for entry (order-insensitively, as map
// equality is), or "".
func tableMatchesMap[K TableKey, V any](name string, t IDTable[K, V], m map[K]V) string {
	if t.Len() != len(m) {
		return fmt.Sprintf("%s: %d entries, reference %d", name, t.Len(), len(m))
	}
	seen := 0
	for k, v := range t.All() {
		want, ok := m[k]
		if !ok {
			return fmt.Sprintf("%s: key %v absent from the reference", name, k)
		}
		if !reflect.DeepEqual(v, want) {
			return fmt.Sprintf("%s[%v] = %+v, reference %+v", name, k, v, want)
		}
		if got, ok := t.Lookup(k); !ok || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(t.Get(k), want) {
			return fmt.Sprintf("%s.Lookup(%v) disagrees with its own iteration", name, k)
		}
		seen++
	}
	for k := range m {
		if !t.Has(k) {
			return fmt.Sprintf("%s: reference key %v missing", name, k)
		}
	}
	if seen != len(m) {
		return fmt.Sprintf("%s: iterated %d entries, reference %d", name, seen, len(m))
	}
	return ""
}

// boardMatchesRef is the first difference between a table-backed Board and
// the map reference, or "".
func boardMatchesRef(b Board, r refBoard) string {
	for _, d := range []string{
		tableMatchesMap("Creatures", b.Creatures, r.Creatures),
		tableMatchesMap("Life", b.Life, r.Life),
		tableMatchesMap("Cards", b.Cards, r.Cards),
		tableMatchesMap("Commanders", b.Commanders, r.Commanders),
	} {
		if d != "" {
			return d
		}
	}
	switch {
	case b.OwnDeck != r.OwnDeck:
		return "OwnDeck"
	case b.IsMain != r.IsMain || b.FirstMain != r.FirstMain || b.MyTurn != r.MyTurn:
		return "IsMain/FirstMain/MyTurn"
	case !reflect.DeepEqual(b.Stack, r.Stack) && (len(b.Stack) != 0 || len(r.Stack) != 0):
		return fmt.Sprintf("Stack %+v, reference %+v", b.Stack, r.Stack)
	case b.Pool != r.Pool || b.PoolRestricted != r.PoolRestricted:
		return "Pool"
	case b.Step != r.Step:
		return "Step"
	case b.LibrarySize != r.LibrarySize || b.HandSize != r.HandSize:
		return "LibrarySize/HandSize"
	}
	return ""
}

// TestBoardTablesMatchMapReference plays whole bot games -- the 12 Legacy
// repo decks paired at 2 seats and grouped at 4, and two Commander
// pairings -- and at every decision holds three Boards to the map-backed
// reference fill: the deciding seat's one reused Board refilled in place
// (every Reset, stale index slot, regrown table and keyword arena across a
// whole game), one Board shared by every seat (refills alternating the
// deciding seat), and a fresh BoardFromGame. The bot answers off the reused
// Board, so the games are the ones the production loop plays.
func TestBoardTablesMatchMapReference(t *testing.T) {
	if testing.Short() {
		t.Skip("plays whole games")
	}
	reg := testutil.CorpusRegistry(t)
	type game struct {
		label string
		cfg   rules.Config
	}
	var games []game
	legacy := testutil.LegacyDeckNames()
	for i := 0; i+1 < len(legacy); i += 2 {
		a, b := legacy[i], legacy[i+1]
		games = append(games, game{"legacy " + a + "," + b, rules.Config{Seed: uint64(10 + i), Names: []string{a, b},
			Decks: [][]*cards.Card{testutil.RepoDeck(t, reg, a), testutil.RepoDeck(t, reg, b)}, Tokens: reg.Tokens}})
	}
	for i := 0; i+4 <= len(legacy); i += 4 {
		names := legacy[i : i+4]
		decks := make([][]*cards.Card, len(names))
		for k, n := range names {
			decks[k] = testutil.RepoDeck(t, reg, n)
		}
		games = append(games, game{fmt.Sprintf("legacy 4-seat %v", names), rules.Config{Seed: uint64(100 + i), Names: names, Decks: decks, Tokens: reg.Tokens}})
	}
	for i, pair := range [][2]string{{"foundations-reign-of-dragons", "foundations-wretched-ranks"}, {"foundations-calling-all-angels", "foundations-tramplesaurus-rex"}} {
		a, b := pair[0], pair[1]
		games = append(games, game{"commander " + a + "," + b, rules.Config{Seed: uint64(1000 + i), Names: []string{a, b},
			Decks:  [][]*cards.Card{testutil.RepoDeck(t, reg, a), testutil.RepoDeck(t, reg, b)},
			Tokens: reg.Tokens, Format: rules.FormatCommander, StartingLife: 40,
			Commanders: [][]int{testutil.RepoDeckFile(t, a).CommanderIndices(), testutil.RepoDeckFile(t, b).CommanderIndices()}}})
	}
	totals := map[string]int{}
	for _, gm := range games {
		e := rules.New(gm.cfg)
		e.Advance()
		perSeat := make([]Board, len(gm.cfg.Names))
		for i := range perSeat {
			perSeat[i] = NewBoard(len(gm.cfg.Names))
		}
		var shared Board // the zero Board: its tables grow on first use
		r := rng(gm.cfg.Seed)
		n := 0
		for !e.G.Over && e.Pending() != nil && n < 20000 {
			d := e.Pending()
			ref := refBoardFromGame(e.G, e, d.Player)
			reused := BoardFromGameInto(e.G, e, d.Player, &perSeat[d.Player])
			for _, c := range []struct {
				name string
				b    Board
			}{
				{"reused", reused},
				{"shared", BoardFromGameInto(e.G, e, d.Player, &shared)},
				{"fresh", BoardFromGame(e.G, e, d.Player)},
			} {
				if diff := boardMatchesRef(c.b, ref); diff != "" {
					t.Fatalf("%s: decision %d (seat %d, %s, step %v): %s board differs from the map reference: %s", gm.label, n, d.Player, d.Kind, e.G.Step, c.name, diff)
				}
			}
			totals["creatures"] += len(ref.Creatures)
			totals["cards"] += len(ref.Cards)
			totals["commanders"] += len(ref.Commanders)
			for _, c := range ref.Creatures {
				totals["keywords"] += len(c.Keywords)
			}
			if err := e.Submit(Decide(reused, d, r)); err != nil {
				t.Fatalf("%s: decision %d: submit: %v", gm.label, n, err)
			}
			e.Advance()
			n++
		}
		totals["decisions"] += n
	}
	// Non-vacuity: the games populated every table, commanders and keyword
	// lists included.
	for _, k := range []string{"decisions", "creatures", "cards", "commanders", "keywords"} {
		if totals[k] == 0 {
			t.Fatalf("no %s were compared: the equivalence check is vacuous (%v)", k, totals)
		}
	}
	t.Logf("compared %v", totals)
}
