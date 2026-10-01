package botpolicy

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// manifestChars is stubChars plus the rules engine's OwnDeck accessor, so
// BoardFromGame fills OwnDeck (and so OwnLibrary) from a hand-built game.
type manifestChars struct {
	stubChars
	m *deck.Manifest
}

func (c manifestChars) OwnDeck(state.PlayerID) *deck.Manifest { return c.m }

func ownLibCard(t *testing.T, src string) *cards.Card {
	t.Helper()
	c, err := cards.ParseBytes("ownlib.txt", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	c.Link()
	return c
}

// ownLibFixture is a hand-built two-seat game exercising every per-card rule
// of the own-library fold at once. Seat 0's list is Alpha x2, Beta x2,
// Forest x3, Twin (a double-faced card) x1 and Mimic x1.
type ownLibFixture struct {
	g       *state.Game
	m       deck.Manifest
	libBeta state.ObjID
}

func newOwnLibFixture(t *testing.T) ownLibFixture {
	t.Helper()
	alpha := ownLibCard(t, "Name:Alpha\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	beta := ownLibCard(t, "Name:Beta\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	forest := ownLibCard(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n")
	mimic := ownLibCard(t, "Name:Mimic\nManaCost:3 U\nTypes:Creature Shapeshifter\nPT:0/0\nOracle:x\n")
	front := ownLibCard(t, "Name:Twin Front\nManaCost:1 G\nTypes:Creature Human\nPT:1/1\nOracle:x\n")
	back := ownLibCard(t, "Name:Twin Back\nTypes:Creature Wolf\nPT:3/3\nOracle:x\n")
	twin := &cards.Card{Faces: []*cards.Face{front.Faces[0], back.Faces[0]}}
	opp := ownLibCard(t, "Name:Beta\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	main := []*cards.Card{alpha, alpha, beta, beta, forest, forest, forest, twin, mimic}
	g := state.NewGame([]string{"me", "opp"})
	add := func(c *cards.Card, owner state.PlayerID) state.ObjID { return g.AddObject(c, owner).ID }
	libAlpha, libBeta, libF1, libF2 := add(alpha, 0), add(beta, 0), add(forest, 0), add(forest, 0)
	handForest := add(forest, 0)
	twinID := add(twin, 0)
	mimicID := add(mimic, 0)
	gyBeta := add(beta, 0)
	exAlpha := add(alpha, 0)
	tokenAlpha := add(alpha, 0)
	stolen := add(opp, 1)
	put := func(z state.Zone, p state.PlayerID, ids ...state.ObjID) {
		g.SetZone(z, p, ids)
		for _, id := range ids {
			g.Obj(id).Zone = z
		}
	}
	put(state.ZLibrary, 0, libAlpha, libBeta, libF1, libF2)
	put(state.ZHand, 0, handForest)
	// Twin transformed (its back face up), a token Alpha of ours, and a Beta
	// stolen FROM the opponent: only Twin is one of our deck's cards.
	put(state.ZBattlefield, 0, twinID, tokenAlpha, stolen)
	g.Obj(twinID).SetFaceIdx(1)
	g.Obj(tokenAlpha).IsToken = true
	g.Obj(stolen).Controller = 0
	// Our Mimic, stolen by the opponent and copying Alpha: it must count as
	// the Mimic it is, never as a second Alpha.
	put(state.ZBattlefield, 1, mimicID)
	g.Obj(mimicID).Controller = 1
	g.Obj(mimicID).SetCopyFace(alpha.Faces[0])
	put(state.ZGraveyard, 0, gyBeta)
	put(state.ZExile, 0, exAlpha)
	return ownLibFixture{g: g, m: deck.NewManifest("me", "", main, nil, nil), libBeta: libBeta}
}

// folds returns seat 0's composition folded by both halves, requiring them
// to agree.
func (f ownLibFixture) folds(t *testing.T) deck.LibraryComposition {
	t.Helper()
	fromGame := BoardFromGame(f.g, manifestChars{m: &f.m}, 0).OwnLibrary
	v := view.Project(f.g, nil, 0, nil)
	v.OwnDeck = &f.m
	var fromView deck.LibraryComposition
	view.OwnLibrary(v, 0, &fromView)
	if !fromGame.Equal(fromView) {
		t.Fatalf("halves diverged: game %+v, view %+v", fromGame, fromView)
	}
	return fromGame
}

func (f ownLibFixture) counts(t *testing.T, lc deck.LibraryComposition) map[string]int32 {
	t.Helper()
	out := map[string]int32{}
	for i, c := range lc.Counts {
		out[lc.Row(i).Name] = c
	}
	return out
}

// TestOwnLibraryFoldRules pins every per-card rule on a hand-built board
// (both halves): printed front-face identity (a transformed card, a copy),
// ownership (a stolen card each way), tokens ignored, a face-down exile
// counted when the seat may look at it and Unknown when it may not.
func TestOwnLibraryFoldRules(t *testing.T) {
	f := newOwnLibFixture(t)
	got := f.folds(t)
	want := map[string]int32{"Alpha": 1, "Beta": 1, "Forest": 2, "Mimic": 0, "Twin Front": 0}
	if !got.Known || !maps(f.counts(t, got), want) || got.Size != 4 || got.Lands != 2 || got.Nonlands != 2 {
		t.Fatalf("fold = %+v (%v), want Known %v, 2 lands of 4", got, f.counts(t, got), want)
	}

	// Without the printed identity the copy is counted as an Alpha and the
	// transformed Twin not at all: the fold no longer matches the library.
	// That is why CardName exists.
	v := view.Project(f.g, nil, 0, nil)
	v.OwnDeck = &f.m
	for i := range v.Players {
		for j := range v.Players[i].Battlefield {
			v.Players[i].Battlefield[j].CardName = ""
		}
	}
	var nameOnly deck.LibraryComposition
	view.OwnLibrary(v, 0, &nameOnly)
	if nameOnly.Known && slices.Equal(nameOnly.Counts, got.Counts) {
		t.Fatal("the fixture does not exercise printed identity: a name-only fold agrees")
	}

	// A Hideaway-style face-down exile we may look at is still counted.
	f.g.SetZone(state.ZLibrary, 0, slices.DeleteFunc(slices.Clone(f.g.Zone(state.ZLibrary, 0)), func(id state.ObjID) bool { return id == f.libBeta }))
	f.g.SetZone(state.ZExile, 0, append(slices.Clone(f.g.Zone(state.ZExile, 0)), f.libBeta))
	o := f.g.Obj(f.libBeta)
	o.Zone, o.FaceDown = state.ZExile, true
	if got := f.folds(t); !got.Known || f.counts(t, got)["Beta"] != 0 || got.Size != 3 {
		t.Fatalf("own-looker face-down exile: %+v", got)
	}
	// The same card under an opponent's look-only permission is not ours to
	// read: Unknown, with no counts.
	o = f.g.Obj(f.libBeta)
	o.HasMayLook, o.MayLookPlayer = true, 1
	if got := f.folds(t); got.Known || got.Unknown != deck.UnknownHiddenOwnCard || len(got.Counts) != 0 {
		t.Fatalf("opponent-looker face-down exile: %+v", got)
	}
}

func maps(a, b map[string]int32) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestOwnLibraryRefillDoesNotAllocate: a Board refilled per decision reuses
// OwnLibrary.Counts' backing array.
func TestOwnLibraryRefillDoesNotAllocate(t *testing.T) {
	f := newOwnLibFixture(t)
	var lc deck.LibraryComposition
	fillOwnLibrary(f.g, 0, &f.m, &lc)
	if allocs := testing.AllocsPerRun(100, func() { fillOwnLibrary(f.g, 0, &f.m, &lc) }); allocs != 0 {
		t.Fatalf("fillOwnLibrary allocates %.1f per refill", allocs)
	}
	if !lc.Known {
		t.Fatalf("fixture fold unknown: %+v", lc)
	}
}
