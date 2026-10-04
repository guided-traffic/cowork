package tools

import (
	"strings"

	"github.com/guided-traffic/cowork/backend/internal/auth"
)

// capsLine says which of the capabilities a tool can run into the agent
// holds — its token's, or the ones the person gave the chat (docs/adr/0043
// D5, D6): "" while the token is unknown or is a person's, whose acts the
// capabilities do not bound.
func capsLine(tok Token, caps ...string) string {
	if !tok.Known || !tok.Agent || len(caps) == 0 {
		return ""
	}
	var has, lacks []string
	for _, c := range caps {
		if tok.Can(c) {
			has = append(has, c)
		} else {
			lacks = append(lacks, c)
		}
	}
	var parts []string
	if len(has) > 0 {
		parts = append(parts, "holds "+strings.Join(has, ", "))
	}
	if len(lacks) > 0 {
		parts = append(parts, "lacks "+strings.Join(lacks, ", ")+" — those acts are refused and remain a person's")
	}
	return "This agent " + strings.Join(parts, "; ") + "."
}

// refusalNote is said once per tool that writes: a refusal is the API's
// answer, not a failure of the tool (docs/adr/0042 D3).
const refusalNote = "A refusal — 403 agent_forbidden, 409 state_conflict, 409 open_prerequisites, 412 precondition_failed — is " +
	"cowork saying no, not a failure of the tool; report it, do not work around it."

func limitsOf(text string, caps ...string) func(Token) string {
	return func(tok Token) string {
		out := text
		if line := capsLine(tok, caps...); line != "" {
			out += " " + line
		}
		return out
	}
}

// The capabilities the tools name.
const (
	capDecide          = auth.CapDecide
	capClose           = auth.CapClose
	capDrop            = auth.CapDrop
	capOverrideUrgency = auth.CapOverrideUrgency
	capInterest        = auth.CapInterest
	capCreateProject   = auth.CapCreateProject
	capRecordAnswer    = auth.CapRecordAnswer
)
