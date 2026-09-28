package spellbench

// knownUnsupportedFDN is TestFDNCoverage's ratchet table: the FDN Limited
// cards gorge does not fully support, with the primitives each is missing
// and the main-pipeline ticket that owns it, as measured at this commit.
// Rows only ever go away (TestFDNCoverage logs a stale row). Empty since the
// fdn-api-endturn and fdn-repl-cant-lose tickets merged: 281/281 supported.
var knownUnsupportedFDN = map[string][]string{}
