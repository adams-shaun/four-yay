package rules

// legalWalkScratch is the priority offer walk's per-engine scratch:
// incremental log-derived indexes the walk reads instead of re-deriving the
// same answer from the whole log on every walk. Every field is a pure
// function of a prefix of the event log (append-only history), named by its
// watermark, so it carries no state replay could disagree with.
//
// Clone carries the fields (cloneLegalWalkScratch): the clone's log is a copy
// of the original's, so each watermark still names the same prefix, and the
// indexes are copy-on-write, so the two engines never write shared words.
type legalWalkScratch struct {
	// airbendIx is the airbend recast permission index (airbend.go), folded
	// over the log's first airbendFolded events.
	airbendIx     airbendExileIndex
	airbendFolded int
}

// cloneLegalWalkScratch is the copy a clone takes: the indexes and their
// watermarks, shared copy-on-write.
func cloneLegalWalkScratch(s legalWalkScratch) legalWalkScratch {
	return legalWalkScratch{airbendIx: s.airbendIx, airbendFolded: s.airbendFolded}
}
