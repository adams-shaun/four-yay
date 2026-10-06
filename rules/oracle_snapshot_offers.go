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
		if oracleOpCodes.Code(o.Kind) == oracleOpActivate {
			for _, label := range r.manaAbilityLabels(d.Player, o.Obj) {
				out = append(out, OracleSnapOffered{Source: source, Kind: "activate", Label: label})
			}
			continue
		}
		kind := o.Kind
		switch oracleOpCodes.Code(kind) {
		case oracleOpCast, oracleOpPlay:
		default:
			// The priority walk's existing real-play classification includes
			// printed, granted and keyword/special actions. Don't grow a
			// parallel list here when another activation kind is added.
			if !potentialPlayKind(kind) {
				continue
			}
			kind = "activate"
			if o.Kind == "play_land" {
				kind = "play"
			}
		}
		out = append(out, OracleSnapOffered{Source: source, Kind: kind, Label: o.Label})
	}
	return out
}
