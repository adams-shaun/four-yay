package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// ---------------------------------------------------------------------------
// §4.4 Section-coverage guard
// ---------------------------------------------------------------------------

// TestQuietCoversWalkSections parses the two entry points that enumerate the
// walk's sections (legalActionsWalkWithWindow and battlefieldWalk) and
// asserts every section call in them is named in quietCoveredSections. A walk
// section added later therefore cannot silently bypass the proof: the call
// shows up and the test fails until the table accounts for it.
func TestQuietCoversWalkSections(t *testing.T) {
	// Collect the calls the two functions make on the walk object (w.X()) or
	// free functions that take w as their first argument (X(w)). Those are
	// the section entry points; e.*/tail.*/rec.* are engine helpers, not
	// sections a blocker must cover.
	collect := func(file, fn string) map[string]bool {
		t.Helper()
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		calls := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if !ok || fd.Name == nil || fd.Name.Name != fn {
				return true
			}
			ast.Inspect(fd, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fun := ce.Fun.(type) {
				case *ast.SelectorExpr:
					if id, ok := fun.X.(*ast.Ident); ok && id.Name == "w" {
						calls[fun.Sel.Name] = true
					}
				case *ast.Ident:
					// A free section function X(w).
					if len(ce.Args) >= 1 {
						if id, ok := ce.Args[0].(*ast.Ident); ok && id.Name == "w" {
							calls[fun.Name] = true
						}
					}
				}
				return true
			})
			return false
		})
		return calls
	}

	calls := collect("legal.go", "legalActionsWalkWithWindow")
	for name := range collect("legal_walk_battlefield.go", "battlefieldWalk") {
		calls[name] = true
	}
	if len(calls) == 0 {
		t.Fatal("section guard precondition: no walk-section calls found (the parser or the function names drifted)")
	}
	for name := range calls {
		if !quietCoveredSections[name] {
			t.Errorf("walk section %q is not in quietCoveredSections (rules/quiet_proof.go); a new walk section must be covered by the quiet proof or explicitly exempted in the table", name)
		}
	}
	// The walk's own named section methods must all be present, so the table
	// cannot silently drop one the parser no longer sees.
	for _, name := range []string{
		"handWalk", "mayPlayLandWalk", "mayhemLandWalk", "mayPlaySpellWalk",
		"plotZoneWalk", "commandZoneWalk", "graveyardCastsWalk", "exileCastsWalk",
		"battlefieldWalk",
	} {
		if !calls[name] {
			t.Errorf("section guard precondition: %q not found in the walk entry points; the guard is not seeing the walk", name)
		}
	}
}

// ---------------------------------------------------------------------------
// §2.4 cost classifier field guard
// ---------------------------------------------------------------------------

// TestQuietCostClassifierCoversCostFields reflects over cost.Cost and fails
// when it gains a field the classifier does not name. A new cost component
// therefore cannot silently read as free (which would make the proof unsound,
// since a cost the classifier reads as {0} makes an ability look affordable).
func TestQuietCostClassifierCoversCostFields(t *testing.T) {
	typ := reflect.TypeOf(Cost{})
	var missing []string
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if !quietCostFieldNames[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("quietCostFloor does not classify cost.Cost field(s) %s; add each to quietCostFieldNames and to quietCostFloor's nonMana test (an unclassified field must mean nonMana)", strings.Join(missing, ", "))
	}
	// The reverse direction: a name in the allowlist that no longer exists is
	// dead bookkeeping and hides the drift above.
	for name := range quietCostFieldNames {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("quietCostFieldNames names %q, which cost.Cost no longer has", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Fixture helpers: the §2.1 contract
// ---------------------------------------------------------------------------

// quietContractOpts returns the walk's options and asserts the seed of the
// contract: when seatQuiet is true, the walk offered nothing but activate,
// pass and concede. It also returns whether the walk offered bare mana.
func quietContract(t *testing.T, e *Engine, p state.PlayerID) (opts []decision.Option, quiet, bareMana bool) {
	t.Helper()
	opts = e.legalActions(p)
	quiet = e.seatQuiet(p)
	bareMana = false
	for i := range opts {
		switch opts[i].Kind {
		case "activate":
			bareMana = true
		case "pass", "concede":
		default:
			if quiet {
				t.Fatalf("quiet proof contract violated for seat %d (turn %d step %v): proof quiet but walk offered %s %q obj %d (blocker %s)",
					p, e.G.Turn, e.G.Step, opts[i].Kind, opts[i].Label, opts[i].Obj, quietBlockerNames[e.quietBlocker(p)])
			}
		}
	}
	return opts, quiet, bareMana
}

// ---------------------------------------------------------------------------
// §6 Q1 item 7: one row per blocker. Each row asserts the proof is true in a
// quiet fixture and false once the one ingredient is added.
// ---------------------------------------------------------------------------

func TestQuietProofFixtures(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	type tc struct {
		name    string
		build   func(t *testing.T) (*Engine, state.PlayerID)
		blocker quietBlockerID
		// nonOpen drives the fixture past Main1 to Begin-Combat before the
		// probe, so the timing class under test is instant speed rather than
		// sorcery timing. A row that relies on instantSpeed MUST set this, or
		// it proves nothing about the non-sorcery branch.
		nonOpen bool
	}
	rows := []tc{
		{
			name: "plains only is quiet (bare mana in the walk)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				return quietBase(t, reg), 0
			},
			blocker: qbNone,
		},
		{
			name: "affordable instant in hand blocks",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				e := quietBaseWith(t, reg, []*cards.Card{lookup(t, reg, "Lightning Bolt")})
				addHand(t, e, 0, lookup(t, reg, "Lightning Bolt"))
				return e, 0
			},
			blocker: qbHandSpell,
		},
		{
			// The spec's "instant-speed castable at a non-sorcery window": a
			// {U} creature with printed Flash, against one untapped land, at
			// Begin-Combat. A creature is not an instant, so the ONLY reason
			// the walk offers it is instantSpeed (the Flash branch); a proof
			// that read sorcery timing alone would wrongly call this quiet.
			name: "flash creature affordable at a non-sorcery window blocks via instantSpeed",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Brinebarrow Intruder")
				e := quietBaseLandWith(t, reg, "Island", []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandSpell,
			nonOpen: true,
		},
		{
			// Counter-row for the Flash row: a plain {U} SORCERY at the same
			// non-sorcery window is NOT castable, so the proof stays quiet.
			// It proves the non-open window is real (the timing class
			// actually changed) rather than a Main1 probe mislabelled.
			name: "plain sorcery at a non-sorcery window is quiet",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Divination")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbNone,
			nonOpen: true,
		},
		{
			name: "flashback card in graveyard blocks",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				e := quietBaseWith(t, reg, []*cards.Card{lookup(t, reg, "Think Twice")})
				addZone(t, e, 0, lookup(t, reg, "Think Twice"), state.ZGraveyard)
				return e, 0
			},
			blocker: qbGraveRoute,
		},
		{
			name: "equipment with Equip {1} on the battlefield blocks",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				e := quietBaseWith(t, reg, []*cards.Card{lookup(t, reg, "Bonesplitter")})
				addZone(t, e, 0, lookup(t, reg, "Bonesplitter"), state.ZBattlefield)
				return e, 0
			},
			blocker: qbBattlefieldAbility,
		},
		{
			name: "command-zone object blocks",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				e := quietBaseWith(t, reg, []*cards.Card{lookup(t, reg, "Grizzly Bears")})
				addZone(t, e, 0, lookup(t, reg, "Grizzly Bears"), state.ZCommand)
				return e, 0
			},
			blocker: qbCommand,
		},
		{
			name: "plotted card in exile blocks",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				e := quietBaseWith(t, reg, []*cards.Card{lookup(t, reg, "Grizzly Bears")})
				id := addZone(t, e, 0, lookup(t, reg, "Grizzly Bears"), state.ZExile)
				e.G.Obj(id).PlottedTurn = e.G.Turn
				return e, 0
			},
			blocker: qbExileRoute,
		},
		{
			name: "self-carried may-play static in hand blocks (Omniscience)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Omniscience")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandSpell,
		},
		{
			name: "self-carried collect-evidence may-play static in hand blocks (Conspiracy Unraveler)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Conspiracy Unraveler")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandSpell,
		},
		{
			name: "spirit guide in hand is quiet (hand mana ability is the mana section)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Elvish Spirit Guide")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbNone,
		},
		{
			name: "cycling card in hand blocks through a granted ability",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Titanoth Rex")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandAbility,
		},
		{
			name: "foretell card in hand blocks (keyword action)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Behold the Multiverse")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandLand,
		},
		{
			name: "battlefield ability with a Sac cost blocks (nonMana)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Viscera Seer")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addZone(t, e, 0, c, state.ZBattlefield)
				return e, 0
			},
			blocker: qbBattlefieldAbility,
		},
		{
			name: "adjustLandPlays keeps a land blocker after the first land",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				exp := lookup(t, reg, "Exploration")
				forest := lookup(t, reg, "Forest")
				e := quietBaseWith(t, reg, []*cards.Card{exp, forest})
				addZone(t, e, 0, exp, state.ZBattlefield)
				addHand(t, e, 0, forest)
				e.G.Players[0].LandsPlayed = 1
				return e, 0
			},
			blocker: qbHandLand,
		},
		{
			name: "may-play grant from a battlefield static blocks (board flag)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				// Grizzly Bears costs more than the seat's one untapped Plains,
				// so the affordable-hand-spell blocker does not preempt the
				// board flag the row is about: only the Omniscience may-play
				// grant opens it.
				om := lookup(t, reg, "Omniscience")
				bears := lookup(t, reg, "Grizzly Bears")
				e := quietBaseWith(t, reg, []*cards.Card{om, bears})
				addZone(t, e, 0, om, state.ZBattlefield)
				addHand(t, e, 0, bears)
				return e, 0
			},
			blocker: qbBoardMayPlay,
		},
		{
			// Kicker is an optional ADDITIONAL cost (the offer path adds it to
			// the printed cost, cost.Plus), so a kicker card whose printed
			// cost is not affordable is quiet: no kicker variant can be
			// cheaper than the plain cast. A classifier that marks Kicker
			// castOpen blocks this window and the row fails.
			name: "kicker card unaffordable at the printed cost is quiet (Aether Figment)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Aether Figment")
				e := quietBaseLandWith(t, reg, "Island", []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbNone,
		},
		{
			// Counter-row: the same kicker card with two Islands — the plain
			// {1}{U} cast is affordable and blocks through the printed floor,
			// so the row above is quiet because of affordability, not because
			// the fixture's board cannot offer anything at all.
			name: "kicker card affordable at the printed cost blocks (Aether Figment)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Aether Figment")
				extra := lookup(t, reg, "Island")
				e := quietBaseLandWith(t, reg, "Island", []*cards.Card{c, extra})
				addZone(t, e, 0, extra, state.ZBattlefield)
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandSpell,
		},
		{
			// §2.5's self-ReduceCost pip floor: Ghalta's only cost static is a
			// self-scoped generic-only reduction (ValidCard$ Card.Self, no
			// Color$), so the face is priced at its coloured pip count ({G}{G}
			// = 2), not castOpen. One Plains cannot pay two pips, so the
			// window is quiet; a classifier that leaves a self ReduceCost
			// castOpen blocks it and the row fails.
			name: "self generic-only ReduceCost is priced at its pip floor (Ghalta)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Ghalta, Primal Hunger")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbNone,
		},
		{
			// Counter-row: the pip floor still governs affordability — with
			// two untapped Forests the two pips are covered and the hand
			// spell blocks.
			name: "self ReduceCost floor blocks once the pips are affordable (Ghalta)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Ghalta, Primal Hunger")
				extra := lookup(t, reg, "Forest")
				e := quietBaseLandWith(t, reg, "Forest", []*cards.Card{c, extra})
				addZone(t, e, 0, extra, state.ZBattlefield)
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandSpell,
		},
		{
			// Fail-closed side of the refinement: Knight of the Stampede's
			// ReduceCost is scoped to DINOSAUR spells (ValidCard$ Dinosaur),
			// not the card itself, so while it sits in hand other hand cards
			// can become cheaper and the face stays castOpen. A refinement
			// that mis-scoped it as self would still block here ({3}{G} has
			// one pip, one Plains covers it), but one that DROPPED the
			// castOpen for a non-self static would price the face at its
			// {3}{G} printed floor, call this window quiet and fail the row.
			name: "non-self ReduceCost static stays castOpen (Knight of the Stampede)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Knight of the Stampede")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandSpell,
		},
		{
			// Fail-closed side, colour reduction: Khalni Hydra's self
			// ReduceCost names Color$ G, so it takes green pips and the face
			// cannot be priced at a pip floor. With one Plains the eight pips
			// are unaffordable, so a classifier that wrongly applied the
			// floor would call this quiet and fail the row.
			name: "self ReduceCost with Color$ stays castOpen (Khalni Hydra)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Khalni Hydra")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				addHand(t, e, 0, c)
				return e, 0
			},
			blocker: qbHandSpell,
		},
		{
			// The walk's ability loop skips a stack object with no face (an
			// activated-ability object minted by AbilityPush carries no card),
			// so the proof must skip it too instead of failing closed on it.
			// Llanowar Elves is the source: its only ability is a mana
			// ability, which the ability summary excludes, so the board is
			// otherwise quiet and the row fails if the proof blocks on the
			// face-less stack object.
			name: "ability object on the stack is quiet (the walk skips it)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Llanowar Elves")
				e := quietBaseWith(t, reg, []*cards.Card{c})
				id := addZone(t, e, 0, c, state.ZBattlefield)
				e.emit(events.Event{Kind: events.AbilityPush, Obj: id, Player: 0, Amount: 0})
				if len(e.G.Stack) != 1 {
					t.Fatal("stack-object precondition: no ability object on the stack")
				}
				if o := e.G.Obj(e.G.Stack[0]); o == nil || o.Card != nil || o.Face() != nil {
					t.Fatalf("stack-object precondition: object is not a face-less ability object: %+v", o)
				}
				return e, 0
			},
			blocker: qbNone,
		},
		{
			// The granted-AddKeyword$-Affinity shape: a live grant that MINTS
			// a bound ReduceCost static (affinityGrantCostStatics) prices hand
			// cards the per-face castOpen classifier never reads -- a granted
			// static has no printed face, and its ValidCard$ Card.Self names
			// the BOUND HOST, so the selfOnly bit the printed-static scan
			// trusts says nothing about it. Mycosynth Golem's grant (Affected$
			// Artifact.Creature+wasCastByYou | AddKeyword$ Affinity:Artifact)
			// plus a second artifact on the battlefield takes {1} off the
			// Arcbound Worker in hand: free at an empty pool. The land drop is
			// closed and the base Plains is returned to the library, because
			// an open land drop (qbHandLand) or one generic unit in the pool
			// (the printed floor {1}) would mask the hole with an earlier
			// blocker -- the exact shape that hid this class from the corpus
			// sweep, which never seats lands.
			name: "granted affinity cost static blocks (Mycosynth Golem)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				golem := lookup(t, reg, "Mycosynth Golem")
				walker := lookup(t, reg, "Phyrexian Walker")
				worker := lookup(t, reg, "Arcbound Worker")
				e := quietBaseWith(t, reg, []*cards.Card{golem, walker, worker})
				for _, id := range append([]state.ObjID{}, e.G.Zone(state.ZBattlefield, 0)...) {
					if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Plains" {
						e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZLibrary})
					}
				}
				addZone(t, e, 0, golem, state.ZBattlefield)
				addZone(t, e, 0, walker, state.ZBattlefield)
				addHand(t, e, 0, worker)
				e.G.Players[0].LandsPlayed = 1
				if e.G.Players[0].Pool.Total() != 0 {
					t.Fatal("affinity-grant precondition: the pool is not empty")
				}
				return e, 0
			},
			blocker: qbBoardCostGrant,
		},
		{
			// The r6 verify mismatch class 1: the hand walk offers a Sneak card's
			// cast ABOVE the sorcery gate, in the caster's own declare-blockers
			// step (legal_walk_hand.go's sneakTimingOK arm). A proof that gated
			// hand casts on the printed timing alone called this window quiet
			// while the walk offered the cast -- the exact panic
			// TestSetAudit_tmt_OrokuSaki_SneakIsCastableDeclareBlockers drove.
			// No attacker is seated, so the walk's offerCastable Return census
			// refuses the cast; the blocker still must fire (the proof shows
			// none CAN be offered, and here it cannot know that).
			name: "sneak card in hand at the caster's declare-blockers blocks (keyword action)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				c := lookup(t, reg, "Oroku Saki, Shredder Rising")
				if !c.Faces[0].HasKeyword("Sneak") {
					t.Fatal("precondition: Oroku Saki does not print Sneak in the corpus")
				}
				e, _ := ninjutsuDeck(t, 9411, c)
				id := searchMoveByName(t, e, "Oroku Saki, Shredder Rising", state.ZHand)
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
					t.Fatalf("precondition: Oroku Saki not in hand: %+v", o)
				}
				_ = attackWithBear(t, e)
				if e.G.Active != 0 || e.G.Step != state.StepDeclareBlockers {
					t.Fatalf("precondition: not at seat 0's declare-blockers step (turn %d step %v)", e.G.Turn, e.G.Step)
				}
				return e, 0
			},
			blocker: qbHandLand,
		},
		{
			// The r6 verify mismatch class 2: Heirloom Epic's TapCreaturesForMana
			// substitution pays most of the printed {4} with creature taps
			// (legal_walk_battlefield.go's offer composes the credit through
			// offerCastableUsing), so the printed floorTap of 4 mis-called the
			// ability unaffordable at a 1-mana ceiling and the proof called the
			// window quiet while the walk offered it. The three Bears are the
			// substitution the ability is payable with.
			name: "battlefield ability priced with creature taps blocks (TapCreaturesForMana nonMana)",
			build: func(t *testing.T) (*Engine, state.PlayerID) {
				epic := lookup(t, reg, "Heirloom Epic")
				bears := []*cards.Card{
					lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Grizzly Bears"),
					lookup(t, reg, "Grizzly Bears"),
				}
				e := quietBaseWith(t, reg, append([]*cards.Card{epic}, bears...))
				if o := e.G.Obj(e.G.Zone(state.ZBattlefield, 0)[0]); o == nil || o.Face() == nil || o.Face().Name != "Plains" {
					t.Fatal("precondition: the base fixture's untapped Plains is missing")
				}
				addZone(t, e, 0, epic, state.ZBattlefield)
				for _, b := range bears {
					id := addZone(t, e, 0, b, state.ZBattlefield)
					if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
						t.Fatalf("precondition: a Bear did not reach the battlefield: %+v", o)
					}
				}
				return e, 0
			},
			blocker: qbBattlefieldAbility,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			e, p := row.build(t)
			if row.nonOpen {
				driveToStep(t, e, e.G.Turn, 0, state.StepBeginCombat)
				if e.G.Step.IsMain() {
					t.Fatal("nonOpen precondition: still at a main step; the row is not testing instant speed")
				}
			}
			opts, quiet, _ := quietContract(t, e, p)
			if got := e.quietBlocker(p); got != row.blocker {
				t.Fatalf("quietBlocker = %s, want %s\nturn %d step %v options: %v",
					quietBlockerNames[got], quietBlockerNames[row.blocker], e.G.Turn, e.G.Step, optKinds(opts))
			}
			if row.blocker == qbNone && !quiet {
				t.Fatalf("fixture is meant to be quiet but the proof blocked")
			}
			if row.blocker != qbNone && quiet {
				t.Fatalf("fixture is meant to block with %s but the proof was quiet", quietBlockerNames[row.blocker])
			}
		})
	}
}

// TestQuietVerifyGrantAffinityBoard runs the granted-affinity hole with the
// verify arm live. The rules test binary runs derivedMemoVerify
// (derivedmemo_verify_test.go's init), so every priorityOptions call
// cross-checks the proof against the walk and panics when a proof that says
// quiet coexists with a non-mana offer. The board is the live grant
// (Mycosynth Golem) plus a second artifact, the discounted spell in hand and
// the land drop closed: the walk offers the cast for free, so a proof that
// called this window quiet panicked here -- and did, before the
// qbBoardCostGrant blocker existed.
func TestQuietVerifyGrantAffinityBoard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	golem := lookup(t, reg, "Mycosynth Golem")
	walker := lookup(t, reg, "Phyrexian Walker")
	worker := lookup(t, reg, "Arcbound Worker")
	e := quietBaseWith(t, reg, []*cards.Card{golem, walker, worker})
	for _, id := range append([]state.ObjID{}, e.G.Zone(state.ZBattlefield, 0)...) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Plains" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZLibrary})
		}
	}
	addZone(t, e, 0, golem, state.ZBattlefield)
	addZone(t, e, 0, walker, state.ZBattlefield)
	gear := addHand(t, e, 0, worker)
	e.G.Players[0].LandsPlayed = 1
	if o := e.G.Obj(gear); o == nil || o.Zone != state.ZHand {
		t.Fatal("precondition: the worker is not in hand")
	}
	for _, name := range []string{"Mycosynth Golem", "Phyrexian Walker"} {
		found := false
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("precondition: %s is not on the battlefield", name)
		}
	}
	if got := e.quietBlocker(0); got != qbBoardCostGrant {
		t.Fatalf("precondition: quietBlocker = %s, want %s (the live affinity grant must block)",
			quietBlockerNames[got], quietBlockerNames[qbBoardCostGrant])
	}
	e.priorityRound()
	// The discount is real: the walk offers the {1} spell for free. This is
	// the assertion that keeps the fixture row honest -- if the grant minted
	// nothing, both this test and the row would pass vacuously.
	if opt := castOptionFor(t, e, gear); opt.Kind != "cast" {
		t.Fatalf("the walk did not offer the granted-affinity cast: %+v", opt)
	}
}

// TestQuietVerifySneakAndHeirloomWindows runs the two r6 verify-mismatch
// windows with the verify arm live, the shape TestQuietVerifyGrantAffinityBoard
// established. Both boards are the ones whose walks OFFERED a non-mana option
// while the proof called the window quiet (the daemon gate's two panics); each
// sub-test first asserts the proof now blocks, then asserts the offer is real,
// so a proof that became quiet again for the wrong reason fails loudly here
// and -- in this binary's verify mode -- panics inside priorityOptions.
func TestQuietVerifySneakAndHeirloomWindows(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	t.Run("sneak at declare-blockers", func(t *testing.T) {
		saki := lookup(t, reg, "Oroku Saki, Shredder Rising")
		if !saki.Faces[0].HasKeyword("Sneak") {
			t.Fatal("precondition: Oroku Saki does not print Sneak in the corpus")
		}
		e, _ := ninjutsuDeck(t, 9411, saki)
		id := searchMoveByName(t, e, "Oroku Saki, Shredder Rising", state.ZHand)
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
			t.Fatalf("precondition: Oroku Saki not in hand: %+v", o)
		}
		_ = attackWithBear(t, e)
		fundPool(t, e, "CB")
		if e.G.Active != 0 || e.G.Step != state.StepDeclareBlockers {
			t.Fatalf("precondition: not at seat 0's declare-blockers step (turn %d step %v)", e.G.Turn, e.G.Step)
		}
		if got := e.quietBlocker(0); got != qbHandLand {
			t.Fatalf("precondition: quietBlocker = %s, want %s (the sneak window must block)",
				quietBlockerNames[got], quietBlockerNames[qbHandLand])
		}
		// The window is real: the walk offers the sneak cast for {1}{B} plus
		// the unblocked attacker.
		if opt := castByName(t, e, 0, "Oroku Saki, Shredder Rising"); opt == nil || opt.Mode != "sneak" {
			t.Fatalf("the walk did not offer the sneak cast: %+v", opt)
		}
	})
	t.Run("heirloom tap-creatures substitution", func(t *testing.T) {
		epic := lookup(t, reg, "Heirloom Epic")
		hasTapCreatures := false
		for _, ab := range epic.Faces[0].Abilities {
			if ab != nil && strings.TrimSpace(ab.ParamStr(cards.PKTapCreaturesForMana)) != "" {
				hasTapCreatures = true
			}
		}
		if !hasTapCreatures {
			t.Fatal("precondition: Heirloom Epic does not print TapCreaturesForMana")
		}
		bears := []*cards.Card{
			lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Grizzly Bears"),
			lookup(t, reg, "Grizzly Bears"),
		}
		e := quietBaseWith(t, reg, append([]*cards.Card{epic}, bears...))
		epicID := addZone(t, e, 0, epic, state.ZBattlefield)
		for _, b := range bears {
			addZone(t, e, 0, b, state.ZBattlefield)
		}
		// The offer prices the floating pool only (manaFeasiblePoolP), so the
		// one colourless unit is what remains after three creature taps cover
		// the rest of the {4}.
		fundPool(t, e, "C")
		if got := e.quietBlocker(0); got != qbBattlefieldAbility {
			t.Fatalf("precondition: quietBlocker = %s, want %s (the tap-creatures substitution must block)",
				quietBlockerNames[got], quietBlockerNames[qbBattlefieldAbility])
		}
		e.priorityRound()
		// The substitution is real: the walk offers the draw ability, payable
		// with the one floating mana plus three creature taps.
		found := false
		for _, o := range e.legalActions(0) {
			if o.Kind == "ability" && o.Obj == epicID && strings.Contains(o.Label, "Draw a card") {
				found = true
			}
		}
		if !found {
			t.Fatalf("the walk did not offer Heirloom Epic's ability: %+v", e.legalActions(0))
		}
	})
}

// quietBase is the proof's quiet fixture: seat 0 has one untapped Plains on
// the battlefield and an empty hand, so no hand land, no cast and no ability
// is open. The walk still offers the Plains' bare mana tap.
func quietBase(t *testing.T, reg *cards.Registry) *Engine {
	t.Helper()
	return quietBaseWith(t, reg, nil)
}

// quietBaseWith is quietBase with the named extra cards seated in seat 0's
// deck (so addZone/addHand can pull one out). Seat 0's battlefield land is a
// Plains.
func quietBaseWith(t *testing.T, reg *cards.Registry, extras []*cards.Card) *Engine {
	t.Helper()
	return quietBaseLandWith(t, reg, "Plains", extras)
}

// quietBaseLandWith is quietBaseWith with the chosen untapped battlefield
// basic, so a fixture whose card needs a coloured source (a blue Flash
// creature, say) can actually be offered by the walk and not only by the
// proof's colour-blind mana ceiling.
func quietBaseLandWith(t *testing.T, reg *cards.Registry, land string, extras []*cards.Card) *Engine {
	t.Helper()
	mountain := lookup(t, reg, "Mountain")
	basic := lookup(t, reg, land)
	deck0 := append([]*cards.Card{basic}, extras...)
	for len(deck0) < 40 {
		deck0 = append(deck0, mountain)
	}
	deck1 := make([]*cards.Card, 40)
	for i := range deck1 {
		deck1[i] = mountain
	}
	e := New(Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{deck0, deck1}})
	e.Advance()
	toMain1(t, e)
	// Empty seat 0's hand, then seat the untapped battlefield land.
	for _, id := range append([]state.ObjID{}, e.G.Zone(state.ZHand, 0)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
	}
	pid := pullByName(t, e, 0, land)
	e.emit(events.Event{Kind: events.MoveZone, Obj: pid, From: state.ZLibrary, To: state.ZBattlefield})
	if o := e.G.Obj(pid); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("quietBase precondition: %s %d is not an untapped battlefield land: %+v", land, pid, o)
	}
	return e
}

// pullByName finds seat p's object for a face name in hand then library.
func pullByName(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				return id
			}
		}
	}
	t.Fatalf("fixture precondition: %q not found in seat %d's hand or library", name, p)
	return 0
}

// addHand seats card in seat p's hand, pulled from the deck.
func addHand(t *testing.T, e *Engine, p state.PlayerID, card *cards.Card) state.ObjID {
	t.Helper()
	id := pullByName(t, e, p, card.Faces[0].Name)
	from := e.G.Obj(id).Zone
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZHand})
	return id
}

// addZone seats card in seat p's zone, pulled from the deck.
func addZone(t *testing.T, e *Engine, p state.PlayerID, card *cards.Card, to state.Zone) state.ObjID {
	t.Helper()
	id := pullByName(t, e, p, card.Faces[0].Name)
	from := e.G.Obj(id).Zone
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: to})
	if o := e.G.Obj(id); o == nil || o.Zone != to {
		t.Fatalf("fixture precondition: %q did not land in %v: %+v", card.Faces[0].Name, to, o)
	}
	return id
}

func optKinds(opts []decision.Option) []string {
	out := make([]string, 0, len(opts))
	for i := range opts {
		out = append(out, opts[i].Kind+":"+opts[i].Label)
	}
	return out
}

// ---------------------------------------------------------------------------
// §4.2 item 5: corpus sweep
// ---------------------------------------------------------------------------

// TestQuietProofCorpusSweep builds one battlefield, hand and graveyard object
// per corpus card face that has facts, and asserts the §2.1 contract for
// every seat and both timing classes: when seatQuiet is true the full walk
// offered no non-mana option. It is the soundness gate for the blocker table.
//
// It is sharded by card index so no shard exceeds the 2 GB / 1 min budget;
// run every shard with -run TestQuietProofCorpusSweep, or one shard with
// -run 'TestQuietProofCorpusSweep$/^3-of-8$'. The shards run in parallel
// (t.Parallel): the whole sweep is ~2x one shard's wall under the 2-vCPU cap
// instead of 8x, so the parent test itself stays inside the per-test budget
// that the serial run exceeded on a loaded box (74 s, round t3).
func TestQuietProofCorpusSweep(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	n := reg.Len()
	if n == 0 {
		t.Fatal("corpus sweep precondition: the registry is empty")
	}
	const shards = 8
	for shard := 0; shard < shards; shard++ {
		t.Run(shardName(shard), func(t *testing.T) {
			t.Parallel()
			sweepShard(t, reg, n, shard, shards)
		})
	}
}

func shardName(shard int) string {
	return string(rune('0'+shard)) + "-of-8"
}

// sweepShard visits every shards'th card by index, places it on the
// battlefield, in hand and in the graveyard in turn, and asserts the §2.1
// contract on both seats at both a sorcery-open and a non-open window. The
// own seat is probed at seat 0's turn-1 Main1 (sorcery-open) and then again
// at Begin-Combat, the same turn with no sorcery timing (so the
// instantSpeed fact and the !sorceryOpen branch of abQuietBlocked are
// actually exercised for the card's own seat); each window is probed for
// seat 0 and seat 1.
func sweepShard(t *testing.T, reg *cards.Registry, n, shard, shards int) {
	t.Helper()
	mountain, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus sweep precondition: Mountain missing")
	}
	for idx := shard; idx < n; idx += shards {
		c := reg.Card(idx)
		if c == nil || len(c.Faces) == 0 {
			continue
		}
		name := c.Faces[0].Name
		e := newSweepEngine(t, reg, c, mountain)
		if e == nil {
			continue
		}
		// Sorcery-open window: seat 0's turn-1 Main1.
		sweepPositions(t, e, name)
		// Non-open window for the same board: Begin-Combat, no sorcery
		// timing. A tolerant driver answers any incidental ask (an ETB type
		// choice, a discard) so the probe still lands on a real non-main
		// step; the contract is asserted against whatever board results. A
		// card whose own effect ends the game has no non-open window to
		// probe, so it is skipped rather than failed.
		if quietDriveToNonMain(t, e) {
			sweepPositions(t, e, name)
		}
	}
}

// sweepPositions moves the card under test through the battlefield, hand and
// graveyard positions and checks the contract for both seats at each.
func sweepPositions(t *testing.T, e *Engine, name string) {
	t.Helper()
	for _, zone := range []state.Zone{state.ZBattlefield, state.ZHand, state.ZGraveyard} {
		id := moveByNameQuiet(t, e, 0, name, zone)
		if id == 0 {
			continue
		}
		for p := state.PlayerID(0); p < 2; p++ {
			quietSweepCheck(t, e, p, name, zone)
		}
		moveQuiet(e, id, zone, state.ZLibrary)
	}
}

// quietDriveToNonMain drives the active seat from its Main1 to Begin-Combat
// (a real non-sorcery window for the current active player), answering any
// incidental decision. It is tolerant where driveToStep fatals because a
// corpus card's ETB can pose a type choice or a discard mid-drive; answering
// it naively keeps the window reachable without changing the probe's
// meaning. It returns false when the game ended or otherwise left the main
// step for a step the probe cannot use, so the caller skips the pass.
func quietDriveToNonMain(t *testing.T, e *Engine) bool {
	t.Helper()
	step := e.G.Step
	for i := 0; i < 4000; i++ {
		if e.G.Step != step {
			if e.G.Over || e.G.Step.IsMain() {
				return false
			}
			return true
		}
		if e.G.Over {
			return false
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatal("no decision pending while driving to a non-main step")
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		case decision.KTriggerOrder:
			picks := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				picks = append(picks, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		default:
			// An incidental ask (a type/name choice, a discard, a scry):
			// answer the minimum number of options so the drive completes. It
			// happens before the step boundary the probe reads, so the board
			// it leaves is the board both the proof and the walk see.
			if len(d.Options) == 0 {
				t.Fatalf("incidental decision %s with no options: %+v", d.Kind, d)
			}
			k := d.Min
			if k < 1 {
				k = 1
			}
			if k > len(d.Options) {
				k = len(d.Options)
			}
			picks := make([]int, k)
			for j := 0; j < k; j++ {
				picks[j] = d.Options[j].Index
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		}
	}
	t.Fatal("did not reach a non-main step within the pass budget")
	return false
}

// moveByNameQuiet is moveByName without the fatal: a card that is neither in
// hand nor library (a token-id card) is skipped by the sweep.
func moveByNameQuiet(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				return id
			}
		}
	}
	return 0
}

// moveQuiet moves id between two zones without a lookup, for the sweep's
// reset between positions.
func moveQuiet(e *Engine, id state.ObjID, from, to state.Zone) {
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: to})
}

// newSweepEngine is a two-seat game whose seat-0 deck is c plus Mountains and
// whose seat-1 deck is Mountains, driven to seat 0's turn-1 Main1. It returns
// nil for a card the engine refuses to seat (a token or a malformed script).
func newSweepEngine(t *testing.T, reg *cards.Registry, c, mountain *cards.Card) (e *Engine) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			e = nil
		}
	}()
	fill := func(n int) []*cards.Card {
		out := make([]*cards.Card, n)
		for i := range out {
			out[i] = mountain
		}
		return out
	}
	e = New(Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{append([]*cards.Card{c}, fill(39)...), fill(40)}})
	e.Advance()
	toMain1(t, e)
	// Empty both hands so the only card under test is the one the shard
	// places; a hand full of filler lands would block on the hand-land
	// blocker and the proof would never be quiet.
	for p := state.PlayerID(0); p < 2; p++ {
		for _, id := range append([]state.ObjID{}, e.G.Zone(state.ZHand, p)...) {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
		}
	}
	return e
}

// quietSweepCheck asserts the contract for one (card, zone, seat) probe.
func quietSweepCheck(t *testing.T, e *Engine, p state.PlayerID, name string, zone state.Zone) {
	t.Helper()
	quiet := e.seatQuiet(p)
	opts := e.legalActions(p)
	if !quiet {
		return
	}
	for i := range opts {
		switch opts[i].Kind {
		case "activate", "pass", "concede":
		default:
			blk := e.quietBlocker(p)
			t.Fatalf("quiet proof unsound: %q in %s, seat %d (turn %d step %v stack %d): proof quiet (blocker %s) but walk offered %s %q obj %d",
				name, zone, p, e.G.Turn, e.G.Step, len(e.G.Stack), quietBlockerNames[blk], opts[i].Kind, opts[i].Label, opts[i].Obj)
		}
	}
}
