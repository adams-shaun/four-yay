package manabrew

import (
	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// Payment prompt construction is owned by the priority prompt ticket; this stub reserves its file boundary.
func (t *Translator) promptPayment(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	return mb.PromptMessage{}, ErrUnmapped
}
