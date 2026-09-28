package spellbench

// knownUnsupportedFDN is TestFDNCoverage's ratchet table: the FDN Limited
// cards gorge does not fully support, with the primitives each is missing
// and the main-pipeline ticket that owns it, as measured at this commit.
// Rows only ever go away (TestFDNCoverage logs a stale row).
var knownUnsupportedFDN = map[string][]string{
	"Time Stop":              {"api:EndTurn"},                   // fdn-api-endturn
	"Herald of Eternal Dawn": {"repl:GameLoss", "repl:GameWin"}, // fdn-repl-cant-lose
}
