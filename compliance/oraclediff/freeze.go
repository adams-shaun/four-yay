package oraclediff

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/rules"
)

// view is one checkpoint as the frozen-expectation vocabulary sees it: the
// comparator's fields (fields), except that the permanents field becomes
// the permanents that arrived ("permanents+") and left ("permanents-")
// since the first checkpoint, so an untouched fixture never enters a
// verdict.
func view(s rules.OracleSnapshot, initialPerms []string, compare []string, ignore []string) []field {
	var out []field
	for _, f := range fields(s, false, compare) {
		if ignored(f.name, ignore) {
			continue
		}
		if f.name == "permanents" {
			var now []string
			if f.value != "" {
				now = strings.Split(f.value, "\n")
			}
			added, gone := symDiff(now, initialPerms)
			out = append(out, field{"permanents+", strings.Join(added, "\n")}, field{"permanents-", strings.Join(gone, "\n")})
			continue
		}
		out = append(out, f)
	}
	return out
}

func views(res rules.OracleResult, compare []string, ignore []string) [][]field {
	if len(res.Snapshots) == 0 {
		return nil
	}
	initial := permKeysOpts(res.Snapshots[0].Permanents, compare)
	out := make([][]field, len(res.Snapshots))
	for i, s := range res.Snapshots {
		out[i] = view(s, initial, compare, ignore)
	}
	return out
}

func checkpoints(res rules.OracleResult) string {
	names := make([]string, len(res.Snapshots))
	for i, s := range res.Snapshots {
		names[i] = s.Checkpoint
	}
	return strings.Join(names, " | ")
}

// Freeze turns gorge's result for a scenario the engines agreed on (or one
// ruled xmage_wrong) into its frozen expectation: the checkpoint sequence,
// any step failures, and every field that differs from the first
// checkpoint at some later checkpoint, with its value at every later
// checkpoint.
func Freeze(res rules.OracleResult, ignore ...string) []compliance.Frozen {
	return FreezeOpts(res, nil, ignore...)
}

// FreezeOpts is Freeze with the item's opt-in comparison fields
// (oraclegen.Item.Compare). A nil list is exactly Freeze.
func FreezeOpts(res rules.OracleResult, compare []string, ignore ...string) []compliance.Frozen {
	out := []compliance.Frozen{{Field: "checkpoints", Value: checkpoints(res)}}
	if len(res.Fails) > 0 {
		out = append(out, compliance.Frozen{Field: "fails", Value: strings.Join(res.Fails, "\n")})
	}
	vs := views(res, compare, ignore)
	if len(vs) < 2 {
		return out
	}
	changed := map[string]bool{}
	for _, v := range vs[1:] {
		for k, f := range v {
			if k >= len(vs[0]) || !fieldEqual(vs[0][k], f) {
				changed[f.name] = true
			}
		}
	}
	for i, v := range vs[1:] {
		at := res.Snapshots[i+1].Checkpoint
		for _, f := range v {
			if changed[f.name] {
				out = append(out, compliance.Frozen{At: at, Field: f.name, Value: f.value})
			}
		}
	}
	return out
}

// Meets reports whether gorge's result still meets a frozen expectation,
// and if not, the first frozen field it misses.
func Meets(frozen []compliance.Frozen, res rules.OracleResult, ignore ...string) (bool, string) {
	return MeetsOpts(frozen, res, nil, ignore...)
}

// MeetsOpts is Meets with the item's opt-in comparison fields
// (oraclegen.Item.Compare). A nil list is exactly Meets.
func MeetsOpts(frozen []compliance.Frozen, res rules.OracleResult, compare []string, ignore ...string) (bool, string) {
	vs := views(res, compare, ignore)
	at := map[string]map[string]string{}
	for i, v := range vs {
		m := map[string]string{}
		for _, f := range v {
			m[f.name] = f.value
		}
		at[res.Snapshots[i].Checkpoint] = m
	}
	whole := map[string]string{"checkpoints": checkpoints(res), "fails": strings.Join(res.Fails, "\n")}
	sawFails := false
	for _, fz := range frozen {
		var got string
		var ok bool
		if fz.At == "" {
			got, ok = whole[fz.Field]
			sawFails = sawFails || fz.Field == "fails"
		} else if m := at[fz.At]; m != nil {
			got, ok = m[fz.Field]
		}
		if !ok {
			return false, fmt.Sprintf("%s %s: gorge has no such field now", fz.At, fz.Field)
		}
		if !fieldEqual(field{fz.Field, got}, field{fz.Field, fz.Value}) {
			return false, fmt.Sprintf("%s %s: gorge now %q, frozen %q", strings.TrimSpace(fz.At), fz.Field, got, fz.Value)
		}
	}
	if !sawFails && len(res.Fails) > 0 {
		return false, "gorge now fails a step: " + res.Fails[0]
	}
	return true, ""
}
