package azmcts

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// visitRecord builds one visit-corpus record (policynet.VisitRecord) for a
// searched decision d of engine e: the searching seat's REDACTED view
// encoded under fs (the student's input), the options any candidate
// references in decision order (the candidates re-expressed as positions in
// that list), and the search's visits, prior, Q, root value and choice. The
// opponent's hand rides beside it as the diagnostic FeaturesMZOppHand rows,
// read off an omniscient projection of the real engine -- recorded for the
// measurement-only student and never readable by a checkpointable one. The
// game fields and the outcome are the driver's to fill.
func visitRecord(e *rules.Engine, d *decision.Decision, res Result, fs policynet.FeatureSet, world string, sims int) policynet.VisitRecord {
	v := view.Project(e.G, e, d.Player, d)
	st := policynet.EncodeStateWith(fs, v, d.Player, nil)
	rec := policynet.VisitRecord{
		RecordType: policynet.VisitRecordType, SchemaVersion: policynet.VisitSchemaVersion,
		EncoderHash: fmt.Sprintf("%016x", policynet.EncoderHashFor(fs)),
		World:       world, Sims: sims,
		Seat: int(d.Player), Kind: d.Kind, Turn: e.G.Turn, Sequence: d.Seq,
		Subset: res.Kind == "attackers" || res.Kind == "blockers",
		State:  policynet.EncodeOnPolicyState(st),
		Visits: append([]int(nil), res.Visits...), Prior: append([]float64(nil), res.Prior...),
		Q: append([]float64(nil), res.Q...), RootValue: res.RootValue, Choice: res.Choice,
	}
	if fs == policynet.FeaturesMZ {
		omni := view.ProjectFor(e.G, e, d.Player, view.Omniscient, d)
		own, diag := policynet.SplitOmniscientView(omni, d.Player)
		rows := policynet.EncodeDiagRows(policynet.FeaturesMZOppHand, own, d.Player, diag)
		for _, f := range rows {
			rec.DiagRows = append(rec.DiagRows, f.Row)
			rec.DiagVals = append(rec.DiagVals, f.Value)
		}
	}
	// Option index -> decision position (rules builds Index == position, but
	// a candidate's Choices are spelt in Index terms).
	posOf := make(map[int]int, len(d.Options)) // lookup only, never ranged
	for i := range d.Options {
		posOf[d.Options[i].Index] = i
	}
	// A payment cast (Options.AutoPayment) has no Choices: it is recorded as
	// the cast option it stands for -- its base option when the decision has
	// one, else a synthetic "cast X" option appended after d.Options, the one
	// the prior scores (payCastOption) -- so its visits train the policy.
	opts := d.Options
	payChoice := make([]int, len(res.Candidates)) // the option position standing for a payment candidate, -1 none
	for c, in := range res.Candidates {
		payChoice[c] = -1
		if in.Payment == nil || len(in.Choices) > 0 {
			continue
		}
		for _, a := range e.EnsurePaymentActions() {
			if a.ID != in.Payment.ActionID {
				continue
			}
			if a.BaseOptionIndex != nil {
				if p, ok := posOf[*a.BaseOptionIndex]; ok {
					payChoice[c] = p
				}
				break
			}
			if len(opts) == len(d.Options) {
				opts = append(make([]decision.Option, 0, len(d.Options)+len(res.Candidates)), d.Options...)
			}
			payChoice[c] = len(opts)
			opts = append(opts, decision.Option{Kind: "cast", Obj: a.Cast.Object, Player: d.Player, Label: a.Label, Index: len(opts)})
			break
		}
	}
	used := make([]bool, len(opts))
	for c, in := range res.Candidates {
		for _, ch := range in.Choices {
			if p, ok := posOf[ch]; ok {
				used[p] = true
			}
		}
		if payChoice[c] >= 0 {
			used[payChoice[c]] = true
		}
	}
	bot := res.Candidates[0]
	enc := make([]policynet.Option, len(opts))
	for i := range opts {
		enc[i] = policynet.EncodeOptionWith(fs, v, d.Player, d.Kind, opts[i], i, len(opts))
	}
	seat.MarkBotPicks(d, enc[:len(d.Options)], bot)
	if payChoice[0] >= 0 {
		enc[payChoice[0]].BotPick = true
	}
	stored := make([]int, len(opts))
	for i := range opts {
		stored[i] = -1
		if used[i] {
			stored[i] = len(rec.Options)
			rec.Options = append(rec.Options, policynet.EncodeOnPolicyOption(enc[i], true))
		}
	}
	rec.Cands = make([][]int, len(res.Candidates))
	for c, in := range res.Candidates {
		rec.Cands[c] = []int{}
		for _, ch := range in.Choices {
			if p, ok := posOf[ch]; ok && stored[p] >= 0 {
				rec.Cands[c] = append(rec.Cands[c], stored[p])
			}
		}
		if p := payChoice[c]; p >= 0 && stored[p] >= 0 {
			rec.Cands[c] = append(rec.Cands[c], stored[p])
		}
	}
	return rec
}
