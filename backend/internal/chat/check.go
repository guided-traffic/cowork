package chat

import (
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cowork/backend/internal/api/apigen"
	"github.com/guided-traffic/cowork/backend/internal/problem"
)

// Check holds a turn's conversation and decisions to what the model can read
// in their place (the API document, runChatTurn): the conversation begins
// with the person's message; a person's message is text, the model's text or
// tool calls with ids of their own, a tool's message the answer to a call of
// the model's message before it; the conversation ends with the person's
// message, with an answer of a tool, or with calls that wait — and a decision
// names the first of them, the one the turn before proposed. The schema has
// bounded every size already.
func Check(msgs []apigen.ChatMessage, decisions []apigen.ChatConfirmation) *problem.Error {
	if len(msgs) == 0 || msgs[0].Role != apigen.ChatRoleUser {
		return problem.Field("/messages/0/role", "a conversation begins with the person's message")
	}
	c := conversation{answered: map[string]bool{}}
	for i, m := range msgs {
		if perr := c.message(fmt.Sprintf("/messages/%d", i), m); perr != nil {
			return perr
		}
	}
	waiting := c.waiting()
	if msgs[len(msgs)-1].Role == apigen.ChatRoleAssistant && len(waiting) == 0 {
		return problem.Field("/messages", "the conversation ends with the model's answer: a turn needs the person's new message, or a decision on a call that waits")
	}
	for j, d := range decisions {
		at := fmt.Sprintf("/confirmations/%d/tool_call_id", j)
		switch {
		case len(waiting) == 0 || d.ToolCallId != waiting[0]:
			return problem.Field(at, "only the first call that waits takes a decision: the one the turn before proposed")
		case j > 0:
			return problem.Field(at, "this call is decided twice")
		}
	}
	return nil
}

// conversation is what Check knows as it reads: the calls of the model's last
// message, and which of them have their answer.
type conversation struct {
	group    []string
	answered map[string]bool
}

func (c *conversation) message(at string, m apigen.ChatMessage) *problem.Error {
	switch m.Role {
	case apigen.ChatRoleUser:
		c.group = nil
		return checkUser(at, m)
	case apigen.ChatRoleAssistant:
		if perr := checkAssistant(at, m); perr != nil {
			return perr
		}
		c.group, c.answered = nil, map[string]bool{}
		if m.ToolCalls != nil {
			for _, call := range *m.ToolCalls {
				c.group = append(c.group, call.Id)
			}
		}
		return nil
	case apigen.ChatRoleTool:
		return checkTool(at, m, c.group, c.answered)
	}
	return nil
}

// waiting are the calls of the model's last message without an answer.
func (c *conversation) waiting() []string {
	var out []string
	for _, id := range c.group {
		if !c.answered[id] {
			out = append(out, id)
		}
	}
	return out
}

func checkUser(at string, m apigen.ChatMessage) *problem.Error {
	switch {
	case m.ToolCalls != nil || m.ToolCallId != nil || m.Ok != nil:
		return problem.Field(at, "a message of the person carries text only")
	case strings.TrimSpace(deref(m.Text)) == "":
		return problem.Field(at+"/text", "a message of the person is not empty")
	}
	return nil
}

func checkAssistant(at string, m apigen.ChatMessage) *problem.Error {
	switch {
	case m.ToolCallId != nil || m.Ok != nil:
		return problem.Field(at, "a message of the model carries text and tool calls only")
	case strings.TrimSpace(deref(m.Text)) == "" && m.ToolCalls == nil:
		return problem.Field(at, "a message of the model carries text or tool calls")
	}
	if m.ToolCalls != nil {
		seen := map[string]bool{}
		for j, c := range *m.ToolCalls {
			if seen[c.Id] {
				return problem.Field(fmt.Sprintf("%s/tool_calls/%d/id", at, j), "two calls of the message share this id")
			}
			seen[c.Id] = true
		}
	}
	return nil
}

func checkTool(at string, m apigen.ChatMessage, group []string, answered map[string]bool) *problem.Error {
	switch {
	case m.ToolCalls != nil:
		return problem.Field(at, "a tool's answer carries no tool calls")
	case m.ToolCallId == nil:
		return problem.Field(at+"/tool_call_id", "a tool's answer names the call it answers")
	case m.Ok == nil:
		return problem.Field(at+"/ok", "a tool's answer says whether the call succeeded")
	case !slices.Contains(group, *m.ToolCallId) || answered[*m.ToolCallId]:
		return problem.Field(at+"/tool_call_id", "this answers no call of the model's message before it")
	}
	answered[*m.ToolCallId] = true
	return nil
}
