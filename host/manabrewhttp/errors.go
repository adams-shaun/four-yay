package manabrewhttp

import (
	"encoding/json"
	"net/http"

	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// errorBody is the plain HTTP-layer rejection shape (claim/routing
// failures): it is deliberately NOT an mb.ProtocolError, because these
// rejections happen before any ManaBrew message was accepted for
// processing — no promptId is ever in scope, and none of the protocol's
// five closed error codes describes "no claim" or "wrong table".
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Code: code, Message: msg})
}

// writeProtocolError writes an mb.ProtocolError body (§6.4/Appendix A.1):
// this is the shape a rejected /send response gets, both in the HTTP reply
// (§5.2: "The HTTP reply body carries the same ProtocolError") and pushed
// onto any open stream for the seat.
func writeProtocolError(w http.ResponseWriter, pe mb.ProtocolError) {
	writeJSON(w, statusForCode(pe.Code), pe)
}

// statusForCode maps the protocol's five closed error codes onto an HTTP
// status for the /send reply. stalePrompt and wrongPlayer are conflicts
// (the request disagrees with what is actually pending); the rest are
// client mistakes against the offered shape.
func statusForCode(code mb.ErrorCode) int {
	switch code {
	case mb.CodeStalePrompt, mb.CodeWrongPlayer:
		return http.StatusConflict
	case mb.CodeWrongPromptType, mb.CodeUnknownActionID, mb.CodeInvalidShape:
		return http.StatusBadRequest
	default:
		return http.StatusBadRequest
	}
}
