package botpolicy

// The botpolicy test binary checks every incremental board refill against a
// scratch fill (board_inc.go's boardIncVerify).
func init() { boardIncVerify = true }
