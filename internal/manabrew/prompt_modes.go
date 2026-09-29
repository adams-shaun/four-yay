package manabrew

import (
	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

func (t *Translator) promptModes(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	return mb.PromptMessage{}, ErrUnmapped
}
