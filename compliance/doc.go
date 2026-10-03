// Package compliance holds gorge's declared-set compliance data: per-set card
// manifests derived from XMage's set classes (MIT, credited in NOTICE), the
// sets gorge declares compliant, and the verdict rows the CI gate reads. See
// docs/superpowers/specs/2026-10-02-xmage-compliance-oracle-design.md and,
// for how it scales to every set, section 11 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md:
//
//   - make compliance-pass SETS=... runs a pass (scripts/compliance-pass.sh)
//     and recomputes only stale verdicts;
//   - compliance/rulings/<id>.json are shape rulings (package shape) and
//     compliance/triage/<shape>.jsonl the clusters no ruling covers yet,
//     one triage item each;
//   - a passing verdict freezes only the fields its scenario changed
//     (VerdictRow.Frozen), and each scenario template versions itself
//     (compliance/oraclegen/templates).
//
// Nothing here reads or embeds a Forge card script.
package compliance
