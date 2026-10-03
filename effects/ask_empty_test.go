package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The class test for the empty-answer-only soft-lock (the Squadron Hawk
// fail-to-find live bug): a decision whose only legal answer is the empty
// one (Min 0 with Max 0, or no options at all) is NEVER posted. Every
// asking primitive in this package resolves such a shape silently, through
// the same deterministic path its no-host stand-in already runs, and every
// site asks through the ONE shared helper (effects.Ask) rather than a
// per-site `if` -- so the next asking primitive is covered by construction,
// and rules' Engine.ask boundary guard panics if any site ever bypasses it.
//
// The table drives every asking site reachable with an empty candidate set
// (search with no eligible card and with an empty library, the KArrange
// family with an empty library, Dig with a window that holds nothing
// eligible, and both discard asks with an empty target hand) against an
// ASKABLE host (askHost records and returns true). Each leaf asserts three
// things: no decision was posed, the resolution completed, and -- for the
// sites that record an R-9 note on a genuine no-host run -- that note is
// ABSENT, because skipping an unanswerable ask is the correct resolution,
// not a degradation.

// askEmptyCase is one asking site driven with an empty candidate set.
type askEmptyCase struct {
	name string
	// run resolves the site's ability against the prepared host.
	run func(t *testing.T, h *askHost, c *Ctx)
	// wantMoves is how many library/hand MoveZone events the silent
	// resolution must emit (0 everywhere: nothing moves on an empty
	// candidate set).
	wantMoves int
	// wantShuffle: the search's unconditional shuffle still happens.
	wantShuffle bool
	// wantLibraryOrder: the KArrange stand-in still records the unchanged
	// order (0 cards).
	wantLibraryOrder bool
	// noteFree asserts no "(no engine host to ask)" R-9 note was recorded:
	// the ask was skipped because the decision was unanswerable, not because
	// the host could not ask.
	noteFree bool
}

func (tc askEmptyCase) runCase(t *testing.T) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	c := &Ctx{Source: 0, Controller: 0}
	if c.Source == 0 {
		src := h.g.AddObject(mkCard(t, "Name:Asker\nTypes:Sorcery\nOracle:x\n"), 0)
		src.Zone = state.ZHand
		c.Source = src.ID
	}
	tc.run(t, h, c)

	if h.asked != nil {
		t.Fatalf("%s: posed a decision %+v; an empty-answer-only ask must resolve silently", tc.name, h.asked)
	}
	moves := 0
	shuffles := 0
	orders := 0
	notes := 0
	for _, ev := range h.log {
		switch ev.Kind {
		case events.MoveZone:
			moves++
		case events.Shuffle:
			shuffles++
		case events.LibraryOrder:
			orders++
		case events.Note:
			notes++
		}
	}
	if moves != tc.wantMoves {
		t.Fatalf("%s: %d MoveZone events, want %d: %+v", tc.name, moves, tc.wantMoves, h.log)
	}
	if tc.wantShuffle && shuffles == 0 {
		t.Fatalf("%s: no Shuffle event; the search's unconditional shuffle still applies on a fail-to-find: %+v", tc.name, h.log)
	}
	if !tc.wantShuffle && shuffles != 0 {
		t.Fatalf("%s: %d Shuffle events, want 0: %+v", tc.name, shuffles, h.log)
	}
	if tc.wantLibraryOrder && orders == 0 {
		t.Fatalf("%s: no LibraryOrder event; the arrange stand-in still records the (unchanged) order: %+v", tc.name, h.log)
	}
	if tc.noteFree && notes != 0 {
		t.Fatalf("%s: %d Note event(s) recorded; a skipped empty ask is silent, not an R-9 no-host degradation: %+v", tc.name, notes, h.log)
	}
}

func TestAskingSitesResolveAnEmptyCandidateSetSilently(t *testing.T) {
	cases := []askEmptyCase{
		{
			// The live bug's exact shape: Squadron Hawk's accepted ETB
			// trigger searches for cards named Squadron Hawk with none in the
			// library. The stated-quality filter keeps Min 0 (CR 701.23b),
			// max clamps to 0 eligible, and the old code posted a Min 0 /
			// Max 0 KChoose with no options. Now: no ask, the shuffle still
			// happens, nothing is found.
			name:        "search stated-quality, no eligible card",
			wantShuffle: true,
			noteFree:    true,
			run: func(t *testing.T, h *askHost, c *Ctx) {
				fillLibrary(h.g, 0, mkCard(t, "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n"), 3)
				s := sa(t, "SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Creature")
				Resolve(h, c, s)
			},
		},
		{
			// The same shape with an entirely EMPTY library: the search still
			// shuffles (CR 701.23c) and finds nothing.
			name:        "search, empty library",
			wantShuffle: true,
			noteFree:    true,
			run: func(t *testing.T, h *askHost, c *Ctx) {
				s := sa(t, "SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Card")
				Resolve(h, c, s)
			},
		},
		{
			// Rearranging the top of a library that holds nothing: k is 0,
			// Min == Max == 0 with no options -- the exact wedge shape.
			name:             "rearrange, empty library",
			wantLibraryOrder: true,
			run: func(t *testing.T, h *askHost, c *Ctx) {
				s := sa(t, "SP$ RearrangeTopOfLibrary | Defined$ You | NumCards$ 3")
				Resolve(h, c, s)
			},
		},
		{
			// Scrying an empty library: Min 0 / Max 0 with no options.
			name:             "scry, empty library",
			wantLibraryOrder: true,
			run: func(t *testing.T, h *askHost, c *Ctx) {
				s := sa(t, "SP$ Scry | Defined$ You | ScryNum$ 2")
				Resolve(h, c, s)
			},
		},
		{
			// Surveiling an empty library: the same shared KArrange body.
			name:             "surveil, empty library",
			wantLibraryOrder: true,
			run: func(t *testing.T, h *askHost, c *Ctx) {
				s := sa(t, "SP$ Surveil | Defined$ You | Amount$ 2")
				Resolve(h, c, s)
			},
		},
		{
			// Dig with a window that holds nothing eligible: dig1's own gate
			// already declines to ask (eligible <= changeNum), and the empty
			// guard would catch it even if the gate ever moved. SkipReorder$
			// True holds the default bottom remainder in place so this leaf
			// keeps testing the TAKE gate's silence -- the ordered-bottom ask
			// a live remainder now poses is pinned in dig_ask_test.go and
			// rules/dig_bottom_arrange_test.go.
			name: "dig, no eligible card in the window",
			run: func(t *testing.T, h *askHost, c *Ctx) {
				fillLibrary(h.g, 0, mkCard(t, "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n"), 2)
				s := sa(t, "SP$ Dig | DigNum$ 3 | ChangeNum$ 1 | DestinationZone$ Graveyard | ChangeValid$ Creature | SkipReorder$ True")
				Resolve(h, c, s)
			},
		},
		{
			// Dig with an empty library: the window is empty, the no-choice
			// take takes nothing.
			name: "dig, empty library",
			run: func(t *testing.T, h *askHost, c *Ctx) {
				s := sa(t, "SP$ Dig | DigNum$ 3 | ChangeNum$ 1 | DestinationZone$ Graveyard")
				Resolve(h, c, s)
			},
		},
		{
			// ChooseCard's filter can leave it no candidate. Its optional
			// bounds clamp to Min == Max == 0, so it must record the empty
			// choice rather than post an invisible KChoose.
			name: "ChooseCard, no matching battlefield card",
			run: func(t *testing.T, h *askHost, c *Ctx) {
				s := sa(t, "SP$ ChooseCard | Choices$ Creature | ChoiceZone$ Battlefield")
				Resolve(h, c, s)
			},
		},
		{
			// ChoosePlayer is normally required, but an unsupported qualifier
			// fails closed. Its candidate clamp produces the same zero-option
			// decision shape; it resolves as an empty recorded choice instead.
			name: "ChoosePlayer, no matching player",
			run: func(t *testing.T, h *askHost, c *Ctx) {
				s := sa(t, "SP$ ChoosePlayer | Choices$ Player.NotARealQualifier")
				Resolve(h, c, s)
			},
		},
		{
			// A target-changing effect can find a spell with an existing target
			// but no legal replacement. It must leave that target alone rather
			// than post its clamped 0..0 replacement picker.
			name: "ChangeTargets, no legal replacement",
			run: func(t *testing.T, h *askHost, c *Ctx) {
				target := h.g.AddObject(mkCard(t, "Name:Target spell\nTypes:Sorcery\nOracle:x\n"), 0)
				target.Zone = state.ZStack // fixture setup; effects never mutate game state directly.
				target.Ability = &cards.SA{}
				target.Targets = []state.Target{{Player: 1, IsPlayer: true}}
				c.Targets = []state.Target{{Obj: target.ID}}
				s := sa(t, "SP$ ChangeTargets | Defined$ Targeted")
				Resolve(h, c, s)
				if got := h.g.Obj(target.ID).Targets; len(got) != 1 || got[0].Player != 1 {
					t.Fatalf("replacement-free ChangeTargets changed targets to %+v", got)
				}
			},
		},
		{
			// A selectorless Origin$ Hand ChangeZone with a literal
			// ChangeNum$ 0 over a hand holding eligible cards: more eligible
			// cards than ChangeNum, so the pick would be a Min == Max == 0
			// KChoose -- the empty-answer-only shape. It moves nothing and
			// records no R-9 note.
			name:     "hand move ChangeNum$ 0, eligible cards in hand",
			noteFree: true,
			run: func(t *testing.T, h *askHost, c *Ctx) {
				land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
				ids := []state.ObjID{h.g.AddObject(land, 0).ID, h.g.AddObject(land, 0).ID}
				h.g.SetZone(state.ZHand, 0, ids) // fixture setup, as handAskFixture does.
				for _, id := range ids {
					h.g.Obj(id).Zone = state.ZHand
				}
				s := sa(t, "DB$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | ChangeNum$ 0")
				Resolve(h, c, s)
			},
		},
		{
			// The caster-chooses discard against a hand with nothing
			// DiscardValid$ allows: nothing is eligible, nothing is asked,
			// nothing moves (Thoughtseize against an empty/filtered hand).
			name:     "discard RevealYouChoose, empty target hand",
			noteFree: true,
			run: func(t *testing.T, h *askHost, c *Ctx) {
				c.Targets = []state.Target{{Player: 1, IsPlayer: true}}
				c.TargetsOffered = true
				s := sa(t, "SP$ Discard | ValidTgts$ Player | Mode$ RevealYouChoose | DiscardValid$ Creature | NumCards$ 1")
				Resolve(h, c, s)
			},
		},
		{
			// The discarding-player-chooses discard against an empty hand:
			// exactly the same contract on the TgtChoose arm.
			name:     "discard TgtChoose, empty target hand",
			noteFree: true,
			run: func(t *testing.T, h *askHost, c *Ctx) {
				c.Targets = []state.Target{{Player: 1, IsPlayer: true}}
				c.TargetsOffered = true
				s := sa(t, "SP$ Discard | ValidTgts$ Player | Mode$ TgtChoose | DiscardValid$ Card | NumCards$ 1")
				Resolve(h, c, s)
			},
		},
	}
	for _, tc := range cases {
		tc.runCase(t)
	}
}

// TestAskingSitesStillAskWhenACandidateExists is the class test's negative
// control: the same sites with a NON-empty candidate set must still pose
// their real decisions. Guards the guard against over-suppressing -- the
// failure mode where "resolve silently" quietly eats legitimate asks.
func TestAskingSitesStillAskWhenACandidateExists(t *testing.T) {
	t.Run("search with an eligible card asks", func(t *testing.T) {
		h := &askHost{}
		h.g = state.NewGame(names(2))
		src := h.g.AddObject(mkCard(t, "Name:Asker\nTypes:Sorcery\nOracle:x\n"), 0)
		fillLibrary(h.g, 0, mkCard(t, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n"), 2)
		Resolve(h, &Ctx{Source: src.ID, Controller: 0},
			sa(t, "SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Creature"))
		if h.asked == nil || string(h.asked.Kind) != "choose" || len(h.asked.Options) != 2 {
			t.Fatalf("search with eligible cards posed %+v, want a 2-option choose", h.asked)
		}
	})
	t.Run("scry with a card on top asks", func(t *testing.T) {
		h := &askHost{}
		h.g = state.NewGame(names(2))
		src := h.g.AddObject(mkCard(t, "Name:Asker\nTypes:Sorcery\nOracle:x\n"), 0)
		fillLibrary(h.g, 0, mkCard(t, "Name:Island\nTypes:Basic Land Island\nOracle:x\n"), 2)
		Resolve(h, &Ctx{Source: src.ID, Controller: 0},
			sa(t, "SP$ Scry | Defined$ You | ScryNum$ 2"))
		if h.asked == nil || string(h.asked.Kind) != "arrange" || len(h.asked.Options) != 2 {
			t.Fatalf("scry with cards posed %+v, want a 2-option arrange", h.asked)
		}
	})
}

// TestDefinedHiddenOriginObjectsMoveDirectly covers the structural Defined$
// dispatch. Library is exceptional because it normally enters the search
// path; when Defined$ resolves objects it is the fetch list, never a fresh
// whole-library choice. Hand and Graveyard use the ordinary object mover, and
// share the same identity/origin contract.
func TestDefinedHiddenOriginObjectsMoveDirectly(t *testing.T) {
	cases := []struct {
		name, origin, destination, params string
		from, to                          state.Zone
	}{
		// ChangeType$ cannot re-filter an already named hidden fetch list.
		{"library remembered fetch list", "Library", "Hand", " | ChangeType$ Creature", state.ZLibrary, state.ZHand},
		{"hand remembered objects", "Hand", "Exile", "", state.ZHand, state.ZExile},
		{"graveyard remembered objects", "Graveyard", "Battlefield", "", state.ZGraveyard, state.ZBattlefield},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &askHost{}
			h.g = state.NewGame(names(2))
			src := h.g.AddObject(mkCard(t, "Name:Asker\nTypes:Sorcery\nOracle:x\n"), 0)
			fillLibrary(h.g, 0, mkCard(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"), 3)
			ids := append([]state.ObjID(nil), h.g.Zone(state.ZLibrary, 0)[:2]...)
			for _, id := range ids {
				if tc.from != state.ZLibrary {
					h.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: tc.from})
				}
			}
			h.log = nil // setup is not resolution evidence
			Resolve(h, &Ctx{Source: src.ID, Controller: 0,
				Remembered: []state.Target{{Obj: ids[0]}, {Obj: ids[1]}}},
				sa(t, "DB$ ChangeZone | Origin$ "+tc.origin+" | Destination$ "+tc.destination+" | Defined$ Remembered"+tc.params))
			if h.asked != nil {
				t.Fatalf("posed %+v; Defined$ objects must not become a fresh choice", h.asked)
			}
			for _, id := range ids {
				if got := h.g.Obj(id).Zone; got != tc.to {
					t.Fatalf("object %d in %s, want %s", id, got, tc.to)
				}
			}
		})
	}
	t.Run("Defined$ You still searches", func(t *testing.T) {
		h := &askHost{}
		h.g = state.NewGame(names(2))
		src := h.g.AddObject(mkCard(t, "Name:Asker\nTypes:Sorcery\nOracle:x\n"), 0)
		fillLibrary(h.g, 0, mkCard(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"), 3)
		Resolve(h, &Ctx{Source: src.ID, Controller: 0},
			sa(t, "DB$ ChangeZone | Origin$ Library | Destination$ Hand | Defined$ You"))
		if h.asked == nil || len(h.asked.Options) != 3 {
			t.Fatalf("Defined$ You search posed %+v, want a 3-option search", h.asked)
		}
	})
}

// TestDefinedLibraryPlayerSelectorsStillSearch proves that the hidden-library
// dispatcher derives an owner's role from resolved targets rather than the
// selector spelling. Each selector deliberately resolves only player 0 while
// player 0's library has cards: it must retain effSearchLibrary's ordinary
// owner path, not consume the ChangeZone as an empty direct fetch list.
func TestDefinedLibraryPlayerSelectorsStillSearch(t *testing.T) {
	cases := []struct {
		name, defined string
		bind          func(*Ctx)
	}{
		{
			name: "remembered player", defined: "Remembered",
			bind: func(c *Ctx) { c.Remembered = []state.Target{{Player: 0, IsPlayer: true}} },
		},
		{
			name: "targeted player", defined: "Targeted",
			bind: func(c *Ctx) { c.Targets = []state.Target{{Player: 0, IsPlayer: true}} },
		},
		{
			name: "chosen player", defined: "ChosenPlayer",
			bind: func(c *Ctx) {
				c.Chosen = []state.Target{{Player: 0, IsPlayer: true}}
				c.ChosenValid = true
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &askHost{}
			h.g = state.NewGame(names(2))
			src := h.g.AddObject(mkCard(t, "Name:Asker\nTypes:Sorcery\nOracle:x\n"), 1)
			ids := fillLibrary(h.g, 0, mkCard(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"), 3)
			ctx := &Ctx{Source: src.ID, Controller: 1}
			tc.bind(ctx)

			Resolve(h, ctx, sa(t, "DB$ ChangeZone | Defined$ "+tc.defined+" | Origin$ Library | Destination$ Hand"))
			d := h.asked
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" || len(d.Options) != len(ids) {
				t.Fatalf("%s posed %+v, want player 0's three-card library search", tc.defined, d)
			}
			for _, o := range d.Options {
				if o.Player != 0 {
					t.Fatalf("%s option owner = %d, want searched player 0: %+v", tc.defined, o.Player, d.Options)
				}
			}
			for _, id := range ids {
				if got := h.g.Obj(id).Zone; got != state.ZLibrary {
					t.Fatalf("%s moved %d to %s before its search answer", tc.defined, id, got)
				}
			}
		})
	}
}

// TestDefinedLibraryObjectSelectorsMoveDirectly covers every resolved
// object-valued selector in the exact-Library audit. A fresh library search
// would ask this askable host; each selector instead moves only its established
// fetch-list member and shuffles the source library once. In particular,
// ChosenCard is the list recorded by an earlier ChooseCard answer, not a
// request to search the whole library.
func TestDefinedLibraryObjectSelectorsMoveDirectly(t *testing.T) {
	cases := []struct {
		name, defined string
		want          int
		bind          func(*Ctx, state.ObjID)
	}{
		{
			name: "remembered", defined: "Remembered", want: 1,
			bind: func(c *Ctx, id state.ObjID) { c.Remembered = []state.Target{{Obj: id}} },
		},
		{
			name: "chosen card", defined: "ChosenCard", want: 1,
			bind: func(c *Ctx, id state.ObjID) {
				// This is choiceRecord's post-answer binding. The selected object
				// remains in the library until this ChangeZone consumes it.
				c.Chosen = []state.Target{{Obj: id}}
				c.ChosenValid = true
			},
		},
		{name: "top", defined: "TopOfLibrary", want: 0, bind: func(*Ctx, state.ObjID) {}},
		{name: "bottom", defined: "BottomOfLibrary", want: 2, bind: func(*Ctx, state.ObjID) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &askHost{}
			h.g = state.NewGame(names(2))
			src := h.g.AddObject(mkCard(t, "Name:Asker\nTypes:Sorcery\nOracle:x\n"), 0)
			ids := fillLibrary(h.g, 0, mkCard(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"), 3)
			ctx := &Ctx{Source: src.ID, Controller: 0}
			tc.bind(ctx, ids[tc.want])
			// ChangeType$ deliberately cannot re-filter an already named fetch
			// list: each Forest still moves even though it is not a Creature.
			Resolve(h, ctx, sa(t, "DB$ ChangeZone | Defined$ "+tc.defined+" | Origin$ Library | Destination$ Hand | ChangeType$ Creature"))
			if h.asked != nil {
				t.Fatalf("posed %+v; %s must be a direct fetch", h.asked, tc.defined)
			}
			if got := h.g.Obj(ids[tc.want]).Zone; got != state.ZHand {
				t.Fatalf("%s object in %s, want hand", tc.defined, got)
			}
			var moves, shuffles int
			for _, ev := range h.log {
				if ev.Kind == events.MoveZone && ev.Obj == ids[tc.want] {
					moves++
				}
				if ev.Kind == events.Shuffle && ev.Player == 0 {
					shuffles++
				}
			}
			if moves != 1 || shuffles != 1 {
				t.Fatalf("%s events moved=%d shuffled=%d, want one direct move and one shuffle: %v", tc.defined, moves, shuffles, h.log)
			}
		})
	}
}

// TestImprintedDefinedLibraryFetchFailsClosed pins Dichotomancy's real
// compiled continuation. Defined$ Imprinted became a KNOWN selector when the
// untap/mana wave persisted imprint context (events.Imprint +
// Obj.Imprinted, read by Defined.Imprinted), so the old fail-closed note no
// longer fires; the fetch now resolves the imprint set — empty on a source
// with no imprint — moves nothing, shuffles nothing, and asks nothing. The
// DBChangeZone's SubAbility$ DBShuffle is now a REAL api:Shuffle (registered
// by main's shuffle primitive): it resolves its Defined$ ParentTarget, which
// no trigger context binds in this synthetic resolution, so it shuffles no
// library and the whole resolution is a silent no-op.
func TestImprintedDefinedLibraryFetchFailsClosed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Dichotomancy")
	if !ok || len(card.Faces) == 0 {
		t.Fatal("Dichotomancy is absent from the corpus")
	}
	fetch := cards.ResolveSVar(card.Faces[0].SVars, "DBChangeZone")
	if fetch == nil || fetch.Params["Defined"] != "Imprinted" || fetch.Params["Origin"] != "Library" {
		t.Fatalf("Dichotomancy DBChangeZone = %+v, want Defined$ Imprinted from Library", fetch)
	}
	h := &askHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(card, 0)
	other := h.g.AddObject(mkCard(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"), 0)
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{src.ID, other.ID})
	Resolve(h, &Ctx{Source: src.ID, Controller: 0}, fetch)
	if h.asked != nil {
		t.Fatalf("Imprinted Defined$ posed %+v, want fail-closed no-op", h.asked)
	}
	if got := h.g.Zone(state.ZLibrary, 0); len(got) != 2 || got[0] != src.ID || got[1] != other.ID {
		t.Fatalf("Imprinted Defined$ changed library to %v, want [%d %d]", got, src.ID, other.ID)
	}
	if got := h.g.Obj(src.ID).Zone; got != state.ZLibrary {
		t.Fatalf("Imprinted Defined$ moved source to %s, want library", got)
	}
	// The one event is the compile-time unread-parameter Note for the
	// script's Searched$ (no ChangeZone reader consumes it); nothing moves.
	if len(h.log) != 1 || h.log[0].Kind != events.Note || h.log[0].Text != "ChangeZone ignores unread parameter(s) Searched$" {
		t.Fatalf("Imprinted Defined$ events = %v, want only the Searched$ unread Note (no move, and DBShuffle's ParentTarget binds no player here)", h.log)
	}
	for _, ev := range h.log {
		if ev.Kind == events.MoveZone || ev.Kind == events.Shuffle {
			t.Fatalf("Imprinted Defined$ emitted %v, want no move or shuffle", ev)
		}
	}
}
