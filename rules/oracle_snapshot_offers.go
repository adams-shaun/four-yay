package rules

import "github.com/adams-shaun/gorge/decision"

// oracleSnapshotOffers reads the already-filtered priority actions. Generic
// mana actions expand through the same named legal-ability reader the oracle
// assertion uses; this must not implement another legality walk.
func oracleSnapshotOffers(r *oracleRun, d *decision.Decision) []OracleSnapOffered {
	var out []OracleSnapOffered
	for _, o := range d.Options {
		if o.Obj == 0 {
			continue
		}
		source := r.objRef(r.e.G.Obj(o.Obj))
		if o.Kind == "activate" {
			for _, label := range r.manaAbilityLabels(d.Player, o.Obj) {
				out = append(out, OracleSnapOffered{Source: source, Kind: "activate", Label: label})
			}
			continue
		}
		kind := o.Kind
		switch {
		case oracleActivateKind(kind), kind == "granted":
			kind = "activate"
		case kind == "play_land":
			kind = "play"
		case kind == "cast", kind == "play":
		default:
			continue
		}
		out = append(out, OracleSnapOffered{Source: source, Kind: kind, Label: o.Label})
	}
	return out
}
