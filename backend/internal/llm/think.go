package llm

import (
	"strings"
	"unicode"
)

// The tags a reasoning model wraps its reasoning in, when the server hands it
// over as part of the answer instead of apart from it (LM Studio without its
// reasoning setting, with a Qwen model).
const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

// thinkFilter takes a reasoning model's <think>…</think> out of the text it
// passes on, the pieces of a stream arriving in any cut, and the white space
// before the answer's first word with it.
type thinkFilter struct {
	inside  bool
	started bool
	pending string
}

// feed passes on what of a piece is answer.
func (f *thinkFilter) feed(piece string) string {
	buf := f.pending + piece
	f.pending = ""
	var out strings.Builder
	for buf != "" {
		tag := thinkOpen
		if f.inside {
			tag = thinkClose
		}
		if i := strings.Index(buf, tag); i >= 0 {
			if !f.inside {
				out.WriteString(buf[:i])
			}
			buf, f.inside = buf[i+len(tag):], !f.inside
			continue
		}
		// What could be the start of the tag waits for the next piece.
		keep := partialSuffix(buf, tag)
		if !f.inside {
			out.WriteString(buf[:len(buf)-keep])
		}
		f.pending = buf[len(buf)-keep:]
		break
	}
	return f.answer(out.String())
}

// flush passes on what was held back at the end of the answer: a tag that
// never completed was text.
func (f *thinkFilter) flush() string {
	rest := f.pending
	f.pending = ""
	if f.inside {
		return ""
	}
	return f.answer(rest)
}

// answer drops the white space before the first word of the answer.
func (f *thinkFilter) answer(s string) string {
	if !f.started {
		s = strings.TrimLeftFunc(s, unicode.IsSpace)
		f.started = s != ""
	}
	return s
}

// partialSuffix is the length of the longest end of s that begins tag.
func partialSuffix(s, tag string) int {
	for n := min(len(tag)-1, len(s)); n > 0; n-- {
		if strings.HasPrefix(tag, s[len(s)-n:]) {
			return n
		}
	}
	return 0
}
