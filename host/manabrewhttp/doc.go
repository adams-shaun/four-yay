// Package manabrewhttp is MB-10 of the ManaBrew protocol scoping spec
// (docs/superpowers/specs/2026-09-28-manabrew-protocol-scope.md §5.2): the
// wire transport that carries internal/manabrew's translation over HTTP.
//
// It is stdlib-only SSE plus POST, at the host tier: it drives a
// *host.Registry exactly as host/httpapi does (ViewAtSeat, Pending,
// SubmitIntent, Undo), and never touches the engine directly. A connection
// is bound to one host/httpapi.SeatClaim through an injected resolver
// (Options.Seat), so a token can never read or act as a seat other than its
// own claim — the same fence claimForTable enforces in host/httpapi.
//
// Support is opt-in: nothing in this package is imported by any engine
// package, host/httpapi, or the rest of the host tier (internal/archtest
// pins the reverse rows), and cmd/gorged (MB-9) decides whether to mount it
// at all. An unmounted server behaves byte-identically to today.
//
// Routes (§5.2), mounted by NewHandler under the caller's own prefix:
//
//   - GET  .../stream — one SSE connection per client: the current state,
//     the open prompt if one exists, and a live feed of engine→client
//     messages as the match progresses. It never replays history.
//   - POST .../send   — one ClientToServerMessage: a response is validated
//     against the pending decision and turned into a decision.Intent
//     through SubmitIntent; a directive queues or answers a concession;
//     restoreSnapshot asks for host.Registry.Undo. A rejection is returned
//     in the HTTP body and, when a stream is open, pushed on it too.
//   - GET  .../state  — one-shot state plus the open prompt, for poll-only
//     clients that never open a stream.
package manabrewhttp
