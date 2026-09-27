package rules

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// stagedGrantCreature is an original test-only creature whose Updated
// PutCounter|ETB$ True body is an entry grant, so Hardened Scales and
// Branching Evolution compete non-commutatively on its entry: 1 -> 2 -> 4
// (Scales first) against 1 -> 2 -> 3 (Evolution first).
func stagedGrantCreature(t *testing.T, name, types string) *cards.Card {
	t.Helper()
	return card(t, "Name:"+name+"\nTypes:"+types+"\nPT:1/1\n"+
		"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ AddEntry | ReplacementResult$ Updated | Description$ entry counter\n"+
		"SVar:AddEntry:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\nOracle:x\n")
}

// rememberedChooses counts the Choose "remembered" events that name id (the
// RememberTokens$ rider's event-backed half).
func rememberedChooses(log []events.Event, id state.ObjID) int {
	n := 0
	for _, ev := range log {
		if ev.Kind == events.Choose && ev.Counter == "remembered" && slices.Contains(ev.IDs, id) {
			n++
		}
	}
	return n
}

// TestChosenCopyRidersWaitForItsStagedEntry is the chosen-copy half of the
// one-publication-point rule (rules/token_rest.go publishTokenEntry): a
// resolving DB$ Token (TokenTapped$ True, RememberTokens$ True) whose mint a
// chosen-copy CreateToken replacement rewrites into a copy of a creature
// whose entry grant competes under Hardened Scales and Branching Evolution.
// The copy's CopyToken mints it in the library; its battlefield MoveZone
// stages behind the CR 616.1 order ask. Until that answer the copy has NOT
// entered, so neither TokenTapped$ nor RememberTokens$ may touch it
// (events.Apply accepts a Tap on a library object, so a premature rider is
// silently "successful"); after the atomic entry each lands exactly once,
// and the SubAbility$ runs once, after every mint.
//
// Two copiers: the real Esix, Fractal Bloom (an Optional$ election, so the
// copy mints inside the election's answer), and an original test-only
// mandatory copier whose single candidate needs no election, so the copy
// mints synchronously inside the resolving effect's own EmitTokenCreate --
// the path where a pre-published id reached effToken's riders at once. (Its
// second creation then sees two candidates, the Bear and the first copy, and
// elects.)
func TestChosenCopyRidersWaitForItsStagedEntry(t *testing.T) {
	mandatory := card(t, "Name:Mandatory Copier\nTypes:Enchantment\n"+
		"R:Event$ CreateToken | ActiveZones$ Battlefield | ValidPlayer$ You | Layer$ Copy | ReplaceWith$ DBCopy | Description$ Create copies of the other creature instead.\n"+
		"SVar:DBCopy:DB$ ReplaceToken | Type$ ReplaceToken | ValidChoices$ Creature.Other | TokenScript$ Chosen\nOracle:x\n")
	for _, cp := range []struct {
		name              string
		esix              bool
		elections, copies int
	}{
		{"esix-election", true, 1, 1},
		{"mandatory-copier", false, 1, 2},
	} {
		for _, tc := range []struct {
			name string
			pick int
			want int32
		}{{"scales-first", 0, 4}, {"evolution-first", 1, 3}} {
			t.Run(cp.name+"/"+tc.name, func(t *testing.T) {
				chosenCopyStagedRiders(t, cp.esix, mandatory, cp.elections, cp.copies, tc.pick, tc.want)
			})
		}
	}
}

func chosenCopyStagedRiders(t *testing.T, useEsix bool, mandatory *cards.Card, wantElections, wantCopies, pick0 int, want int32) {
	scales := tokenReplCorpusCard(t, "Hardened Scales")
	evolution := tokenReplCorpusCard(t, "Branching Evolution")
	copier := mandatory
	if useEsix {
		copier = tokenReplCorpusCard(t, "Esix, Fractal Bloom")
	}
	bear := stagedGrantCreature(t, "Staged Copy Bear", "Creature Bear")
	spell := card(t, "Name:Tapped Remembered Tokens\nManaCost:0\nTypes:Sorcery\n"+
		"A:SP$ Token | TokenAmount$ 2 | TokenScript$ c_a_powerstone | TokenTapped$ True | RememberTokens$ True | SubAbility$ Rider | SpellDescription$ x\n"+
		"SVar:Rider:DB$ GainLife | LifeAmount$ 1\nOracle:x\n")
	e, cfg := tokenReplGame(t, 1021, scales, evolution, copier, bear, spell)
	// The Bear enters before the counter modifiers, so its own entry is
	// uncontested; only the copies' entries stage.
	copierID := moveSeededCard(t, e, 0, copier, state.ZBattlefield)
	bearID := moveSeededCard(t, e, 0, bear, state.ZBattlefield)
	for _, c := range []*cards.Card{scales, evolution} {
		if o := e.G.Obj(moveSeededCard(t, e, 0, c, state.ZBattlefield)); o == nil || o.Zone != state.ZBattlefield {
			t.Fatal("precondition: counter modifier absent")
		}
	}
	e.SetCounterAdder(0)
	for _, id := range []state.ObjID{copierID, bearID} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %d not on the battlefield: %+v", id, o)
		}
	}
	spellID := moveSeededCard(t, e, 0, spell, state.ZHand)
	addMana(t, e, 0, "")
	castSpellOption(t, e, "Tapped Remembered Tokens")
	life := e.G.Players[0].Life
	first := e.G.NextID
	elections, orders := 0, 0
	var copies []state.ObjID
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while resolving")
		}
		var pick int
		switch d.Kind {
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				i = 40
				continue
			}
			passPriorityOnce(t, e)
			continue
		case decision.KChoose:
			elections++
			pick = optionForObj(d, bearID)
		case decision.KReplacement:
			orders++
			// The staged copy: the newest object, minted by CopyToken and
			// still in the library.
			copyID := e.G.NextID - 1
			o := e.G.Obj(copyID)
			if o == nil || o.Zone != state.ZLibrary || !o.IsCopy {
				t.Fatalf("precondition: at order ask %d the chosen copy %d = %+v, want a copy still in the library", orders, copyID, o)
			}
			if n := countKind(e.L.Events, events.Tap, copyID); n != 0 {
				t.Fatalf("TokenTapped$ tapped copy %d %d times before its entry-order answer (still in the library)", copyID, n)
			}
			if n := rememberedChooses(e.L.Events, copyID); n != 0 {
				t.Fatalf("RememberTokens$ remembered copy %d %d times before its entry-order answer", copyID, n)
			}
			if got := e.G.Players[0].Life; got != life {
				t.Fatalf("SubAbility$ ran before copy %d entered: life %d, want %d", copyID, got, life)
			}
			copies = append(copies, copyID)
			pick = pick0
		default:
			t.Fatalf("unexpected decision %+v", d)
		}
		if pick < 0 {
			t.Fatalf("decision does not offer the expected option: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
			t.Fatal(err)
		}
	}
	if elections != wantElections || orders != wantCopies || len(copies) != wantCopies {
		t.Fatalf("asked %d elections and %d order asks, want %d and %d", elections, orders, wantElections, wantCopies)
	}
	for _, copyID := range copies {
		o := e.G.Obj(copyID)
		if o == nil || o.Zone != state.ZBattlefield || !o.IsCopy || o.Face().Name != "Staged Copy Bear" {
			t.Fatalf("copy %d = %+v, want the staged Bear copy on the battlefield", copyID, o)
		}
		if got := o.Counter("P1P1"); got != want {
			t.Fatalf("copy %d counters = %d, want %d (the answer decides 1->2->4 vs 1->2->3)", copyID, got, want)
		}
		if !o.Tapped || countKind(e.L.Events, events.Tap, copyID) != 1 {
			t.Fatalf("copy %d tapped=%v with %d Tap events, want exactly one after its entry", copyID, o.Tapped, countKind(e.L.Events, events.Tap, copyID))
		}
		if n := rememberedChooses(e.L.Events, copyID); n != 1 {
			t.Fatalf("copy %d remembered %d times, want exactly once after its entry", copyID, n)
		}
		// The riders follow the atomic entry in the log.
		entryAt, tapAt, memAt := -1, -1, -1
		for i, ev := range e.L.Events {
			switch {
			case ev.Kind == events.MoveZone && ev.Obj == copyID && ev.To == state.ZBattlefield && entryAt < 0:
				entryAt = i
			case ev.Kind == events.Tap && ev.Obj == copyID:
				tapAt = i
			case ev.Kind == events.Choose && ev.Counter == "remembered" && slices.Contains(ev.IDs, copyID):
				memAt = i
			}
		}
		if entryAt < 0 || tapAt < entryAt || memAt < entryAt {
			t.Fatalf("copy %d entry at %d, Tap at %d, remember at %d: every rider must follow the entry", copyID, entryAt, tapAt, memAt)
		}
	}
	stones := 0
	for id := first; id < e.G.NextID; id++ {
		if s := e.G.Obj(id); s != nil && s.Zone == state.ZBattlefield && s.Face().Name == "Powerstone Token" {
			stones++
			if countKind(e.L.Events, events.Tap, id) != 1 || rememberedChooses(e.L.Events, id) != 1 {
				t.Fatalf("powerstone %d took its riders %d/%d times, want once each", id,
					countKind(e.L.Events, events.Tap, id), rememberedChooses(e.L.Events, id))
			}
		}
	}
	if stones != 2-wantCopies {
		t.Fatalf("minted %d Powerstones, want %d", stones, 2-wantCopies)
	}
	if got := e.G.Players[0].Life; got != life+1 {
		t.Fatalf("SubAbility$ life = %d, want %d exactly once", got, life+1)
	}
	if s := e.G.Obj(spellID); s == nil || s.Zone != state.ZGraveyard {
		t.Fatalf("resolved spell = %+v, want it in the graveyard", s)
	}
	replayCheck(t, e, cfg)
}

// stagedProducer is one token-entry producer driven through a parked
// entry-counter stage by TestNoTokenRiderPrecedesItsStagedEntry.
type stagedProducer struct {
	name string
	// setup builds the game (the counter modifiers already in play, the
	// producer ready) and returns the engine, its replay config, the id of a
	// creature an election should pick (0 for none), and the act that starts
	// the resolution.
	setup func(t *testing.T) (*Engine, Config, state.ObjID, func(*Engine))
	// entered counts the producer's tokens (ids >= first) that have entered.
	entered func(e *Engine, first state.ObjID) int
	// riders counts the post-entry riders, markers and continuations the
	// producer has applied so far; each entered token owes perToken of them.
	riders   func(e *Engine, first state.ObjID) int
	perToken int
	tokens   int
}

// countTokensOnBattlefield counts tokens with id >= first named name on the
// battlefield.
func countTokensOnBattlefield(e *Engine, first state.ObjID, name string) int {
	n := 0
	for id := first; id < e.G.NextID; id++ {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Zone == state.ZBattlefield && o.Face() != nil && o.Face().Name == name {
			n++
		}
	}
	return n
}

// countEventsFrom counts log events of kind k whose Obj is >= first (the
// objects this resolution minted) and that pass keep.
func countEventsFrom(e *Engine, first state.ObjID, k events.Kind, keep func(events.Event) bool) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == k && ev.Obj >= first && (keep == nil || keep(ev)) {
			n++
		}
	}
	return n
}

// withStagedModifiers seats Hardened Scales and Branching Evolution (real)
// on seat 0 and makes seat 0 the counter adder: every staged-grant entry
// after this call competes non-commutatively.
func withStagedModifiers(t *testing.T, e *Engine, scales, evolution *cards.Card) {
	t.Helper()
	for _, c := range []*cards.Card{scales, evolution} {
		if o := e.G.Obj(moveSeededCard(t, e, 0, c, state.ZBattlefield)); o == nil || o.Zone != state.ZBattlefield {
			t.Fatal("precondition: counter modifier absent")
		}
	}
	e.SetCounterAdder(0)
}

// TestNoTokenRiderPrecedesItsStagedEntry drives every producer of a token
// battlefield entry through a parked entry-counter stage (the entering
// token's original test-only entry grant under Hardened Scales and Branching
// Evolution) and asserts the one-publication-point rule at every ask: no
// rider, marker or continuation of a token precedes that token's completed
// entry (riders <= perToken x entered), and after the resolution every token
// has entered and taken exactly its riders. Both answer orders, replay.
//
// Producers: DB$ Token's direct mint, its finalized CreateToken plan
// (Parallel Lives), a chosen-copy plan (CopyToken + MoveZone), Encore's
// CardToken copies, Incubate, Amass, Investigate and DB$ CopyPermanent.
func TestNoTokenRiderPrecedesItsStagedEntry(t *testing.T) {
	const grantName = "Staged Grant Token"
	tapped := func(e *Engine, first state.ObjID) int { return countEventsFrom(e, first, events.Tap, nil) }
	positiveCounters := func(e *Engine, first state.ObjID) int {
		// The entry's own notification-only notice is part of the entry, not
		// a rider.
		return countEventsFrom(e, first, events.CounterChange, func(ev events.Event) bool {
			return ev.Amount > 0 && ev.Text != events.EntryCounterNotice
		})
	}
	named := func(name string) func(*Engine, state.ObjID) int {
		return func(e *Engine, first state.ObjID) int { return countTokensOnBattlefield(e, first, name) }
	}
	tokenSpell := func(t *testing.T, name, amount string) *cards.Card {
		return card(t, "Name:"+name+"\nManaCost:0\nTypes:Sorcery\n"+
			"A:SP$ Token | TokenAmount$ "+amount+" | TokenScript$ staged_grant | TokenTapped$ True | SubAbility$ Rider | SpellDescription$ x\n"+
			"SVar:Rider:DB$ GainLife | LifeAmount$ 1\nOracle:x\n")
	}
	// spellGame seats the given cards, overrides the named token scripts with
	// the staged-grant creature, puts the counter modifiers into play after
	// the pre-seeded battlefield cards, and casts spell.
	spellGame := func(t *testing.T, seed uint64, spell *cards.Card, mana string, overrides []string, battlefield, graveyard []*cards.Card, pickName string) (*Engine, Config, state.ObjID, func(*Engine)) {
		scales := tokenReplCorpusCard(t, "Hardened Scales")
		evolution := tokenReplCorpusCard(t, "Branching Evolution")
		seat := append([]*cards.Card{scales, evolution, spell}, battlefield...)
		seat = append(seat, graveyard...)
		e, cfg := tokenReplGame(t, seed, seat...)
		if len(overrides) > 0 {
			cfg.Tokens = maps.Clone(cfg.Tokens)
			for _, key := range overrides {
				cfg.Tokens[key] = stagedGrantCreature(t, grantName, "Artifact Creature Construct Army Incubator Clue")
			}
			e = New(cfg)
			e.Advance()
		}
		var pick state.ObjID
		for _, c := range battlefield {
			id := moveSeededCard(t, e, 0, c, state.ZBattlefield)
			if c.Faces[0].Name == pickName {
				pick = id
			}
		}
		for _, c := range graveyard {
			moveSeededCard(t, e, 0, c, state.ZGraveyard)
		}
		withStagedModifiers(t, e, scales, evolution)
		moveSeededCard(t, e, 0, spell, state.ZHand)
		return e, cfg, pick, func(e *Engine) {
			addMana(t, e, 0, mana)
			castSpellOption(t, e, spell.Faces[0].Name)
		}
	}
	producers := []stagedProducer{
		{
			name: "token-direct",
			setup: func(t *testing.T) (*Engine, Config, state.ObjID, func(*Engine)) {
				return spellGame(t, 1031, tokenSpell(t, "Staged Direct Tokens", "2"), "", []string{"staged_grant"}, nil, nil, "")
			},
			entered: named(grantName), riders: tapped, perToken: 1, tokens: 2,
		},
		{
			name: "token-plan",
			setup: func(t *testing.T) (*Engine, Config, state.ObjID, func(*Engine)) {
				pl := tokenReplCorpusCard(t, "Parallel Lives")
				return spellGame(t, 1033, tokenSpell(t, "Staged Plan Tokens", "1"), "", []string{"staged_grant"},
					[]*cards.Card{pl}, nil, "")
			},
			entered: named(grantName), riders: tapped, perToken: 1, tokens: 2,
		},
		{
			name: "chosen-copy",
			setup: func(t *testing.T) (*Engine, Config, state.ObjID, func(*Engine)) {
				copier := card(t, "Name:Mandatory Copier\nTypes:Enchantment\n"+
					"R:Event$ CreateToken | ActiveZones$ Battlefield | ValidPlayer$ You | Layer$ Copy | ReplaceWith$ DBCopy | Description$ Create copies of the other creature instead.\n"+
					"SVar:DBCopy:DB$ ReplaceToken | Type$ ReplaceToken | ValidChoices$ Creature.Other | TokenScript$ Chosen\nOracle:x\n")
				bear := stagedGrantCreature(t, "Staged Copy Bear", "Creature Bear")
				spell := card(t, "Name:Staged Copied Token\nManaCost:0\nTypes:Sorcery\n"+
					"A:SP$ Token | TokenAmount$ 1 | TokenScript$ c_a_powerstone | TokenTapped$ True | SubAbility$ Rider | SpellDescription$ x\n"+
					"SVar:Rider:DB$ GainLife | LifeAmount$ 1\nOracle:x\n")
				return spellGame(t, 1035, spell, "", nil, []*cards.Card{copier, bear}, nil, "Staged Copy Bear")
			},
			entered: named("Staged Copy Bear"), riders: tapped, perToken: 1, tokens: 1,
		},
		{
			name: "copy-permanent",
			setup: func(t *testing.T) (*Engine, Config, state.ObjID, func(*Engine)) {
				bear := stagedGrantCreature(t, "Staged Copy Bear", "Creature Bear")
				spell := card(t, "Name:Staged Permanent Copies\nManaCost:0\nTypes:Sorcery\n"+
					"A:SP$ CopyPermanent | Defined$ Valid Creature.YouCtrl | NumCopies$ 2 | RememberTokens$ True | SubAbility$ Rider | SpellDescription$ x\n"+
					"SVar:Rider:DB$ GainLife | LifeAmount$ 1\nOracle:x\n")
				return spellGame(t, 1037, spell, "", nil, []*cards.Card{bear}, nil, "")
			},
			entered: named("Staged Copy Bear"),
			riders: func(e *Engine, first state.ObjID) int {
				n := 0
				for id := first; id < e.G.NextID; id++ {
					n += rememberedChooses(e.L.Events, id)
				}
				return n
			},
			perToken: 1, tokens: 2,
		},
		{
			name: "incubate",
			setup: func(t *testing.T) (*Engine, Config, state.ObjID, func(*Engine)) {
				return spellGame(t, 1039, tokenReplCorpusCard(t, "Eyes of Gitaxias"), "UUU",
					[]string{"incubator_c_0_0_a_phyrexian"}, nil, nil, "")
			},
			entered: named(grantName), riders: positiveCounters, perToken: 1, tokens: 1,
		},
		{
			name: "amass",
			setup: func(t *testing.T) (*Engine, Config, state.ObjID, func(*Engine)) {
				fodder := card(t, "Name:Graveyard Sorcery\nManaCost:0\nTypes:Sorcery\nA:SP$ GainLife | LifeAmount$ 1 | SpellDescription$ x\nOracle:x\n")
				return spellGame(t, 1041, tokenReplCorpusCard(t, "Invade the City"), "UUR",
					[]string{"b_0_0_zombie_army", "b_0_0_army"}, nil, []*cards.Card{fodder}, "")
			},
			entered: named(grantName), riders: positiveCounters, perToken: 1, tokens: 1,
		},
		{
			name: "investigate",
			setup: func(t *testing.T) (*Engine, Config, state.ObjID, func(*Engine)) {
				spell := card(t, "Name:Twice Investigate\nManaCost:0\nTypes:Sorcery\n"+
					"A:SP$ Investigate | Num$ 2 | SubAbility$ Rider | SpellDescription$ Investigate twice.\n"+
					"SVar:Rider:DB$ GainLife | LifeAmount$ 1\nOracle:x\n")
				return spellGame(t, 1043, spell, "", []string{"c_a_clue_draw"}, nil, nil, "")
			},
			entered: named(grantName),
			riders: func(e *Engine, first state.ObjID) int {
				n := 0
				for _, ev := range e.L.Events {
					if ev.Kind == events.Investigate {
						n++
					}
				}
				return n
			},
			perToken: 1, tokens: 2,
		},
		{
			name: "encore",
			setup: func(t *testing.T) (*Engine, Config, state.ObjID, func(*Engine)) {
				scales := tokenReplCorpusCard(t, "Hardened Scales")
				evolution := tokenReplCorpusCard(t, "Branching Evolution")
				grace := card(t, "Name:Encore Grace\nManaCost:1\nTypes:Creature Elf\nPT:1/1\nK:Encore:1\n"+
					"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ AddEntry | ReplacementResult$ Updated | Description$ entry counter\n"+
					"SVar:AddEntry:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\nOracle:x\n")
				reg := testutil.CorpusRegistry(t).Tokens
				cfg := seatZeroStart(Config{Seed: 1045, Names: []string{"a", "b", "c"},
					Decks: [][]*cards.Card{
						append([]*cards.Card{scales, evolution, grace}, mountainDeck(t, 37)...),
						mountainDeck(t, 40), mountainDeck(t, 40),
					},
					Tokens: reg})
				e := New(cfg)
				e.Advance()
				withStagedModifiers(t, e, scales, evolution)
				graceID := moveSeededCard(t, e, 0, grace, state.ZGraveyard)
				return e, cfg, 0, func(e *Engine) {
					addMana(t, e, 0, "C")
					submitChoices(t, e, abilityOption(t, e, graceID, 0).Index)
				}
			},
			entered: named("Encore Grace"),
			riders: func(e *Engine, first state.ObjID) int {
				n := 0
				for id := first; id < e.G.NextID; id++ {
					if o := e.G.Obj(id); o != nil && o.IsToken && e.HasKeyword(id, "Haste") {
						n++
					}
				}
				for _, d := range e.G.Delayed {
					for _, id := range targetObjIDs(d.Remembered) {
						if id >= first {
							n++
						}
					}
				}
				return n
			},
			perToken: 2, tokens: 2,
		},
	}
	for _, p := range producers {
		for _, order := range []struct {
			name string
			pick int
		}{{"scales-first", 0}, {"evolution-first", 1}} {
			t.Run(p.name+"/"+order.name, func(t *testing.T) {
				e, cfg, electFor, start := p.setup(t)
				first := e.G.NextID
				start(e)
				staged := 0
				for i := 0; i < 80; i++ {
					d := e.Pending()
					if d == nil {
						t.Fatal("no decision while resolving")
					}
					pick := 0
					switch d.Kind {
					case decision.KPriority:
						if len(e.G.Stack) == 0 {
							i = 80
							continue
						}
						passPriorityOnce(t, e)
						continue
					case decision.KChoose:
						if electFor != 0 {
							if k := optionForObj(d, electFor); k >= 0 {
								pick = k
							}
						}
					case decision.KReplacement:
						if strings.Contains(d.Prompt, "enters with") {
							staged++
						}
						entered, riders := p.entered(e, first), p.riders(e, first)
						if riders > p.perToken*entered {
							t.Fatalf("ask %q: %d riders applied with only %d tokens entered (%d each): a rider preceded its token's entry",
								d.Prompt, riders, entered, p.perToken)
						}
						if order.pick < len(d.Options) {
							pick = order.pick
						}
					default:
						t.Fatalf("unexpected decision %+v", d)
					}
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
						t.Fatal(err)
					}
				}
				if staged == 0 {
					t.Fatal("precondition: no token entry staged behind an entry-counter order ask")
				}
				if len(e.G.Stack) != 0 {
					t.Fatalf("resolution did not finish: stack %v", e.G.Stack)
				}
				entered, riders := p.entered(e, first), p.riders(e, first)
				if entered != p.tokens || riders != p.perToken*p.tokens {
					t.Fatalf("after resolution: %d tokens entered with %d riders, want %d tokens with %d riders",
						entered, riders, p.tokens, p.perToken*p.tokens)
				}
				replayCheck(t, e, cfg)
			})
		}
	}
}
