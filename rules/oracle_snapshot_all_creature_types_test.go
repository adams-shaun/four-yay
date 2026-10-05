package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestOracleSnapshotChangelingAllCreatureTypes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	scenario := `{"name":"changeling-all-types","setup":{"p0":{"battlefield":["Changeling Outcast"]}}}`
	res, err := RunOracleScenarioJSON(reg, []byte(scenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v", res.Fails)
	}
	if len(res.Snapshots) == 0 || len(res.Snapshots[0].Permanents) != 1 {
		t.Fatalf("precondition: Changeling Outcast is not on the battlefield: %+v", res.Snapshots)
	}
	p := res.Snapshots[0].Permanents[0]
	if p.Name != "Changeling Outcast" {
		t.Fatalf("precondition: snapshot permanent = %q", p.Name)
	}
	if !p.AllCreatureTypes {
		t.Fatalf("Changeling did not produce the semantic all-types snapshot flag: %+v", p)
	}
}

func TestOracleSnapshotMistformUltimusAllCreatureTypes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Mistform Ultimus")
	if !ok || len(card.Faces) == 0 || !card.Faces[0].AllCreatureTypesCDA() {
		t.Fatal("precondition: corpus Mistform Ultimus face lacks its all-creature-types CDA")
	}
	// Keep the precomputed intrinsic CDA while removing its active static
	// carrier. This isolates the derived-characteristic path: the snapshot
	// must not rely on a layer effect re-emitting the same semantics. Clone
	// the face so this test never mutates the process-wide corpus registry.
	cardCopy := *card
	faceCopy := *card.Faces[0]
	faceCopy.Statics = nil
	cardCopy.Faces = []*cards.Face{&faceCopy}
	isolated := cards.NewRegistry()
	isolated.Add(&cardCopy)
	if wastes, ok := reg.Lookup("Wastes"); ok {
		isolated.Add(wastes)
	} else {
		t.Fatal("precondition: corpus lacks Wastes used by the oracle scenario harness")
	}
	scenario := `{"name":"mistform-ultimus-all-types","setup":{"p0":{"battlefield":["Mistform Ultimus"]}}}`
	res, err := RunOracleScenarioJSON(isolated, []byte(scenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v", res.Fails)
	}
	if len(res.Snapshots) == 0 || len(res.Snapshots[0].Permanents) != 1 {
		t.Fatalf("precondition: Mistform Ultimus is not on the battlefield: %+v", res.Snapshots)
	}
	p := res.Snapshots[0].Permanents[0]
	if p.Name != "Mistform Ultimus" {
		t.Fatalf("precondition: snapshot permanent = %q", p.Name)
	}
	if !p.AllCreatureTypes {
		t.Fatalf("Mistform Ultimus CDA missing from all-types snapshot flag: %+v", p)
	}
}

func TestOracleSnapshotGlamerGifterAllCreatureTypes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	scenario := `{"name":"glamer-gifter-all-types","cr":["613.1d"],"why":"snapshot carries the derived all-creature-types grant","setup":{"p0":{"hand":["Glamer Gifter"],"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Glamer Gifter","mana":"CU"},{"op":"resolve","seat":0,"targets":["p0:Grizzly Bears"]}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(scenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	var bears *OracleSnapPerm
	for i := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		p := &res.Snapshots[len(res.Snapshots)-1].Permanents[i]
		if p.Name == "Grizzly Bears" {
			bears = p
		}
	}
	if bears == nil {
		t.Fatal("precondition: Grizzly Bears is not on the battlefield after Glamer Gifter resolves")
	}
	if !bears.AllCreatureTypes {
		t.Fatalf("Glamer Gifter's target snapshot lacks all_creature_types: %+v", *bears)
	}
	if bears.PT != "4/4" {
		t.Fatalf("precondition: Glamer Gifter did not apply its Animate effect to the battlefield Bears: %+v", *bears)
	}
}
