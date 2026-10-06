package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// backFaceSetupPerm places card on its back face via the fixture runner and
// returns the permanent reported under the back face's name, failing loudly if
// the setup is not what the assertions are about.
func backFaceSetupPerm(t *testing.T, card string, wantSaga bool) OracleSnapPerm {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(card)
	if !ok || len(c.Faces) < 2 {
		t.Fatalf("%s is not a >=2-face card in the corpus", card)
	}
	back := c.Faces[1].Name
	if back == c.Faces[0].Name {
		t.Fatalf("faces of %s share the name %q", card, back)
	}
	res, _ := runFixtureScenario(t, `{"name":"backsaga","setup":{"p0":{"battlefield":["`+card+`"],"back_face":["`+card+`"]}},"steps":[]}`)
	snap := res.Snapshots[0]
	if setupPermCount(snap, back) != 1 || setupPermCount(snap, c.Faces[0].Name) != 0 {
		t.Fatalf("back face %q is not the only %s permanent: %+v", back, card, snap.Permanents)
	}
	for _, p := range snap.Permanents {
		if p.Name != back {
			continue
		}
		if oracleHasFold(p.Types, "Saga") != wantSaga {
			t.Fatalf("back face %q types %v: Saga = %v, want %v", back, p.Types, !wantSaga, wantSaga)
		}
		return p
	}
	t.Fatal("unreachable")
	return OracleSnapPerm{}
}

// CR 702.151a: a Saga placed on its back face takes its entry lore counter,
// plus the CR 505.4/703.4f precombat-main one, so XMage reports two.
func TestOracleSetupBackFaceSagaLore(t *testing.T) {
	p := backFaceSetupPerm(t, "Crystal Fragments", true)
	if got := p.Counters["LORE"]; got != 2 {
		t.Errorf("back-face Saga LORE = %d, want 2 (counters %v)", got, p.Counters)
	}
}

// A non-Saga back face must not inherit the front Saga's entry lore counter.
func TestOracleSetupNonSagaBackFaceNoLore(t *testing.T) {
	for _, card := range []string{"The Legend of Kyoshi", "The Rise of Sozin"} {
		p := backFaceSetupPerm(t, card, false)
		if got, ok := p.Counters["LORE"]; ok || got != 0 {
			t.Errorf("%s: non-Saga back face carries LORE = %d (counters %v), want none", card, got, p.Counters)
		}
	}
}
