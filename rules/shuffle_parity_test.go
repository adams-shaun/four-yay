package rules

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
)

// These tests compare ordinary New against the independently owned
// NewHypothetical-without-a-planner path. The two engines hold different RNG
// wrappers, so this catches a ShuffleLibrary route that changes event bytes,
// chain head, RNG consumption, or clone state while preserving a superficial
// final library order.
func TestOrdinaryGenesisShuffleLibraryParity(t *testing.T) {
	t.Parallel()
	normal, hypothetical := ordinaryShuffleParityEngines(t, chanceConfig(t))
	assertOrdinaryShuffleParity(t, "genesis", normal, hypothetical)
	assertOrdinaryShuffleCloneParity(t, "genesis", normal, hypothetical)
}

func TestOrdinaryMulliganShuffleLibraryParity(t *testing.T) {
	t.Parallel()
	cfg := chanceConfig(t)
	cfg.Mulligans = 1
	normal, hypothetical := ordinaryShuffleParityEngines(t, cfg)
	normal.Advance()
	if err := hypothetical.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	submitMulliganParity(t, normal, hypothetical)
	assertOrdinaryShuffleParity(t, "mulligan", normal, hypothetical)
	assertOrdinaryShuffleCloneParity(t, "mulligan", normal, hypothetical)
}

func TestOrdinaryEffectShuffleLibraryParity(t *testing.T) {
	t.Parallel()
	shuffle := card(t, "Name:Ordinary Shuffle\nManaCost:0\nTypes:Sorcery\nA:SP$ Shuffle | Defined$ You\nOracle:Test shuffle.\n")
	normal, hypothetical := ordinaryShuffleParityEngines(t, shuffleRouteConfig(t, shuffle))
	driveCastToShuffleParity(t, normal, hypothetical)
	assertOrdinaryShuffleParity(t, "effect", normal, hypothetical)
	assertOrdinaryShuffleCloneParity(t, "effect", normal, hypothetical)
}

func TestOrdinarySearchShuffleLibraryParity(t *testing.T) {
	t.Parallel()
	search := card(t, "Name:Ordinary Search\nManaCost:0\nTypes:Sorcery\nA:SP$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Card | ChangeNum$ 1\nOracle:Test search.\n")
	normal, hypothetical := ordinaryShuffleParityEngines(t, shuffleRouteConfig(t, search))
	driveCastToShuffleParity(t, normal, hypothetical)
	assertOrdinaryShuffleParity(t, "search", normal, hypothetical)
	assertOrdinaryShuffleCloneParity(t, "search", normal, hypothetical)
}

func ordinaryShuffleParityEngines(t *testing.T, cfg Config) (*Engine, *Engine) {
	t.Helper()
	normal := New(cfg)
	hypothetical, err := NewHypothetical(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertOrdinaryShuffleParity(t, "genesis", normal, hypothetical)
	return normal, hypothetical
}

func shuffleRouteConfig(t *testing.T, spell *cards.Card) Config {
	t.Helper()
	cfg := chanceConfig(t)
	for i := range cfg.Decks[0] {
		cfg.Decks[0][i] = spell
	}
	return cfg
}

func submitMulliganParity(t *testing.T, normal, hypothetical *Engine) {
	t.Helper()
	d := normal.Pending()
	if d == nil || d.Kind != decision.KMulligan {
		t.Fatalf("normal pending = %+v, want mulligan", d)
	}
	choice := -1
	for _, option := range d.Options {
		if option.Kind == "mulligan" {
			choice = option.Index
			break
		}
	}
	if choice < 0 {
		t.Fatalf("mulligan option missing: %+v", d)
	}
	submitShuffleParity(t, normal, hypothetical, []int{choice})
}

// driveCastToShuffleParity takes the first cast option, passes priority until
// it resolves, and answers a search with its first offered card. The choice is
// taken from the normal engine and submitted verbatim to both engines only
// after their pending decisions have been checked equal.
func driveCastToShuffleParity(t *testing.T, normal, hypothetical *Engine) {
	t.Helper()
	normal.Advance()
	if err := hypothetical.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	before := shuffleCount(normal.L.Events, 0)
	cast := false
	for i := 0; i < 100; i++ {
		d := normal.Pending()
		if d == nil {
			t.Fatal("normal engine has no pending decision")
		}
		var choices []int
		switch d.Kind {
		case decision.KPriority:
			for _, option := range d.Options {
				if !cast && option.Kind == "cast" {
					choices = []int{option.Index}
					cast = true
					break
				}
				if option.Kind == "pass" {
					choices = []int{option.Index}
				}
			}
		case decision.KChoose:
			if len(d.Options) > 0 {
				choices = []int{d.Options[0].Index}
			}
		default:
			t.Fatalf("unexpected decision while resolving shuffle route: %+v", d)
		}
		if len(choices) == 0 {
			t.Fatalf("no choice for decision: %+v", d)
		}
		submitShuffleParity(t, normal, hypothetical, choices)
		if cast && shuffleCount(normal.L.Events, 0) > before {
			return
		}
	}
	t.Fatal("spell did not produce its shuffle")
}

func submitShuffleParity(t *testing.T, normal, hypothetical *Engine, choices []int) {
	t.Helper()
	normalDecision, hypotheticalDecision := normal.Pending(), hypothetical.Pending()
	if !reflect.DeepEqual(normalDecision, hypotheticalDecision) {
		t.Fatalf("pending decisions diverged:\nnormal=%+v\nhypothetical=%+v", normalDecision, hypotheticalDecision)
	}
	in := decision.Intent{Seq: normalDecision.Seq, Player: normalDecision.Player, Choices: choices}
	if err := normal.Submit(in); err != nil {
		t.Fatal(err)
	}
	if err := hypothetical.SubmitHypothetical(in); err != nil {
		t.Fatal(err)
	}
	assertOrdinaryShuffleParity(t, "after decision", normal, hypothetical)
}

func assertOrdinaryShuffleParity(t *testing.T, route string, normal, hypothetical *Engine) {
	t.Helper()
	if normal.L.Head() != hypothetical.L.Head() {
		t.Fatalf("%s head normal=%s hypothetical=%s", route, normal.L.Head(), hypothetical.L.Head())
	}
	if normal.RNGDraws() != hypothetical.RNGDraws() {
		t.Fatalf("%s RNG draws normal=%d hypothetical=%d", route, normal.RNGDraws(), hypothetical.RNGDraws())
	}
	if got, want := eventBytes(normal.L.Events), eventBytes(hypothetical.L.Events); !bytes.Equal(got, want) {
		t.Fatalf("%s event bytes diverged\nnormal=%x\nhypothetical=%x", route, got, want)
	}
}

func assertOrdinaryShuffleCloneParity(t *testing.T, route string, normal, hypothetical *Engine) {
	t.Helper()
	normalClone, hypotheticalClone := normal.Clone(), hypothetical.Clone()
	assertOrdinaryShuffleParity(t, route+" clone", normalClone, hypotheticalClone)
	if got, want := eventBytes(normal.L.Events), eventBytes(normalClone.L.Events); !bytes.Equal(got, want) ||
		normal.L.Head() != normalClone.L.Head() || normal.RNGDraws() != normalClone.RNGDraws() {
		t.Fatalf("%s normal clone did not preserve log/head/RNG state", route)
	}
	if got, want := eventBytes(hypothetical.L.Events), eventBytes(hypotheticalClone.L.Events); !bytes.Equal(got, want) ||
		hypothetical.L.Head() != hypotheticalClone.L.Head() || hypothetical.RNGDraws() != hypotheticalClone.RNGDraws() {
		t.Fatalf("%s hypothetical clone did not preserve log/head/RNG state", route)
	}
	for n := 2; n < 10; n++ {
		if normal.Rand(n) != normalClone.Rand(n) || hypothetical.Rand(n) != hypotheticalClone.Rand(n) {
			t.Fatalf("%s clone RNG continuation diverged at bound %d", route, n)
		}
	}
}

func eventBytes(log []events.Event) []byte {
	var out []byte
	for _, event := range log {
		out = append(out, event.Append(nil)...)
	}
	return out
}
