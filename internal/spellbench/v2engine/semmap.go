// The G-side candidate projection (P§3.5, S0): gorge's posed decision
// surface, projected to v2 candidate semantics and handed to policynet's
// shared candidate encoder. The projection itself is the D§6 mapping the
// engine already poses on the wire (spec P§3.5 names it explicitly as "the
// mapping D§6 already needs, run in reverse"), so it is reused here — not
// re-spelled — and the parity test (P§3.9) holds the two ENCODING paths and
// the two pointer resolutions (the observation's object ids joined through
// gorge's state.ObjID vs the observation's own row map) to agreement. The
// entity-table half of parity — the two projections built from
// independently written code — is where S0's byte-identity bites.

package v2engine

import (
	"encoding/json"
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// SemCandsJSON returns the posed wire decision's canonical semantic JSON
// array (the same bytes the protocol serves) decoded to policynet.V2SemCand
// rows, in posed order. It errors when no decision is posed.
func (g *Game) SemCandsJSON() ([]byte, []policynet.V2SemCand, error) {
	if g.posed == nil {
		return nil, nil, fmt.Errorf("v2engine: no decision posed")
	}
	var buf []byte
	buf = append(buf, '[')
	sems := make([]policynet.V2SemCand, 0, len(g.posed.semantics))
	for i, raw := range g.posed.semantics {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, raw...)
		sc, err := SemCandFromJSON(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("semantics %d: %w", i, err)
		}
		sems = append(sems, sc)
	}
	return append(buf, ']'), sems, nil
}

// SemCandFromJSON decodes one canonical semantic.
func SemCandFromJSON(raw []byte) (policynet.V2SemCand, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return policynet.V2SemCand{}, err
	}
	return policynet.V2SemCandFromFields(m)
}

// WireCandidates shapes the posed semantics the way the protocol's agent
// role receives them (v2agent.Candidate), for the parity test's V side.
func (g *Game) WireCandidates() ([]v2agent.Candidate, error) {
	if g.posed == nil {
		return nil, fmt.Errorf("v2engine: no decision posed")
	}
	out := make([]v2agent.Candidate, len(g.posed.semantics))
	for i, raw := range g.posed.semantics {
		out[i] = v2agent.Candidate{ID: int64(i), Raw: raw}
		if err := json.Unmarshal(raw, &out[i].Fields); err != nil {
			return nil, fmt.Errorf("wire candidate %d: %w", i, err)
		}
	}
	return out, nil
}

// WireSemCands decodes the WIRE candidates of one posed decision (the
// front end V side of the dump and the parity test).
func WireSemCands(cands []v2agent.Candidate) ([]policynet.V2SemCand, error) {
	sems := make([]policynet.V2SemCand, 0, len(cands))
	for i := range cands {
		sc, err := policynet.V2SemCandFromFields(cands[i].Fields)
		if err != nil {
			return nil, fmt.Errorf("wire candidate %d: %w", i, err)
		}
		sems = append(sems, sc)
	}
	return sems, nil
}

// GResolver resolves v2 semantics' object ids against front end G's table:
// the posing's ObjectID strings join to gorge's state.ObjID rows.
type GResolver struct {
	t *policynet.V2GTable
}

// V2RefObject resolves a posed object id.
func (r GResolver) V2RefObject(id string) int32 { return r.t.V2RefObject(id) }

// V2RefPlayer resolves a seat word.
func (r GResolver) V2RefPlayer(seat string) int32 { return r.t.V2RefPlayer(seat) }

// JoinSemIDs fills a G table's semantic-id map from the posing's refs: for
// every object the posing referenced that has a table row, the posed
// ObjectID maps to that row. It also fills SpellPT from the posing's own
// spell characteristics, the one stack fact the view does not expose.
func JoinSemIDs(t *policynet.V2GTable, b *obsBuild) {
	if b == nil {
		return
	}
	for id, ref := range b.refs {
		if r, ok := t.ObjRows[id]; ok {
			if t.SemIDRows == nil {
				t.SemIDRows = map[string]int32{}
			}
			t.SemIDRows[ref.ObjectID] = r
		}
	}
}

// JoinSpellPT fills a G table's SpellPT from the posing's own spell
// characteristics, the one stack fact the view does not expose (a stack
// object's PT is not battlefield derived state). Call it BEFORE
// V2TableFromView: the table build reads the map.
func JoinSpellPT(t *policynet.V2GTable, b *obsBuild) {
	if b == nil {
		return
	}
	for _, se := range b.obs.Stack {
		if se.StackKind != "spell" || se.Characteristics == nil ||
			se.Characteristics.Power == nil || se.Characteristics.Toughness == nil {
			continue
		}
		for id, ref := range b.refs {
			if ref.ObjectID != se.ObjectID {
				continue
			}
			if t.SpellPT == nil {
				t.SpellPT = map[state.ObjID][2]int32{}
			}
			t.SpellPT[id] = [2]int32{*se.Characteristics.Power, *se.Characteristics.Toughness}
		}
	}
}

// ParityPair carries the two sides of one posed decision for the parity
// test (P§3.9) and the corpus dump: front end G's table built from
// view.Project, front end V's table built from the posing's own
// observation, and both sides' decoded candidate semantics.
type ParityPair struct {
	Seat   state.PlayerID
	Kind   decision.Kind
	GTable *policynet.V2GTable
	VTable *policynet.V2VTable
	GRes   GResolver
	GSem   []policynet.V2SemCand
	VSem   []policynet.V2SemCand
	Sem    []json.RawMessage // canonical semantics, wire order
}

// Pair fronts both S0 front ends over one posed decision: the gorge side
// from view.Project (front end G) and the wire side from the posing's own
// observation and candidates (front end V). Both sides see the same engine
// state the pose saw; nothing is re-derived.
func (g *Game) Pair() (*ParityPair, error) {
	if g.posed == nil {
		return nil, fmt.Errorf("v2engine: no decision posed")
	}
	d := g.e.Pending()
	v := view.Project(g.e.G, g.e, d.Player, d)
	gt := &policynet.V2GTable{}
	JoinSpellPT(gt, g.posed.obsBuild)
	gt = policynet.V2TableFromView(v, d.Player, d.Kind == decision.KPriority, gt)
	JoinSemIDs(gt, g.posed.obsBuild)
	vt := policynet.V2TableFromObservation(g.posed.obsBuild.obs, seatOf(d.Player))
	_, gsem, err := g.SemCandsJSON()
	if err != nil {
		return nil, err
	}
	vsem := make([]policynet.V2SemCand, len(gsem))
	copy(vsem, gsem)
	return &ParityPair{Seat: d.Player, Kind: d.Kind, GTable: gt, VTable: vt,
		GRes: GResolver{t: gt}, GSem: gsem, VSem: vsem, Sem: rawSemantics(g.posed.semantics)}, nil
}

// rawSemantics widens canonical semantics to json.RawMessage.
func rawSemantics(sem [][]byte) []json.RawMessage {
	out := make([]json.RawMessage, len(sem))
	for i, b := range sem {
		out[i] = json.RawMessage(b)
	}
	return out
}
