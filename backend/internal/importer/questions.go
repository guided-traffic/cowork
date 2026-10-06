package importer

import (
	"regexp"
	"strconv"
	"strings"
)

// The open questions section (docs/adr/0011 D4, docs/adr/0044 D1): grammar v1
// writes `## Open questions` last, a repository before `## Not verified` and
// `## Related`. The section is the last such heading outside fenced code, up
// to the next heading of its level; each `### Q<n>: <question>` in it is a
// question, its options, `**Recommendation:**` and `**Answer:**` the lines that
// follow it.
const (
	questionsHeading  = "Open questions"
	answerPrefix      = "**Answer:**"
	recommendPrefix   = "**Recommendation:**"
	answerOpen        = "_open_"
	answerWithdrawn   = "_withdrawn_"
	maxQuestionLength = 2000
	headingTwoPrefix  = "## "
)

var questionHead = regexp.MustCompile(`^### Q([0-9]+):[ \t]*(.*)$`)

// splitBody cuts the text after the frontmatter into the body and the
// questions. first is the file line of text's first line.
func splitBody(text string, first int, f *File) (string, []Question) {
	lines := strings.Split(text, "\n")
	fenced := fences(lines)
	var heads []int
	for i, l := range lines {
		if !fenced[i] && isHeadingTwo(l) && strings.TrimSpace(l[len(headingTwoPrefix):]) == questionsHeading {
			heads = append(heads, i)
		}
	}
	if len(heads) == 0 {
		return strings.TrimSpace(text), nil
	}
	start := heads[len(heads)-1]
	for _, h := range heads[:len(heads)-1] {
		f.warn(fieldBody, first+h, "the body keeps a ## Open questions heading of its own; the questions are the last such heading's (docs/adr/0011)")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if !fenced[i] && isHeadingTwo(lines[i]) {
			end = i
			break
		}
	}
	questions, preamble := readQuestions(lines[start+1:end], fenced[start+1:end], first+start+1, f)
	if len(questions) == 0 && strings.TrimSpace(preamble) != "" {
		f.warn(fieldBody, first+start, "the ## Open questions section holds no ### Q<n> question; its text stays in the body")
		return strings.TrimSpace(text), nil
	}
	before := strings.TrimSpace(strings.Join(lines[:start], "\n"))
	after := strings.TrimSpace(strings.Join(lines[end:], "\n"))
	switch {
	case before == "":
		return after, questions
	case after == "":
		return before, questions
	}
	return before + "\n\n" + after, questions
}

func isHeadingTwo(l string) bool { return strings.HasPrefix(l, headingTwoPrefix) }

// readQuestions reads the section's questions and returns the text before the
// first of them. A preamble beside questions joins the first one's options.
func readQuestions(lines []string, fenced []bool, first int, f *File) ([]Question, string) {
	var heads []int
	for i, l := range lines {
		if !fenced[i] && questionHead.MatchString(l) {
			heads = append(heads, i)
		}
	}
	if len(heads) == 0 {
		return nil, strings.Join(lines, "\n")
	}
	preamble := strings.TrimSpace(strings.Join(lines[:heads[0]], "\n"))
	seen := map[int32]int{}
	out := make([]Question, 0, len(heads))
	for k, h := range heads {
		next := len(lines)
		if k+1 < len(heads) {
			next = heads[k+1]
		}
		q := readQuestion(lines[h], lines[h+1:next], fenced[h+1:next], first+h, f)
		if q.Number == 0 {
			continue
		}
		if line, dup := seen[q.Number]; dup {
			f.fail(questionField(q.Number), q.Line, "Q%d appears twice, also on line %d", q.Number, line)
			continue
		}
		seen[q.Number] = q.Line
		out = append(out, q)
	}
	if preamble != "" && len(out) > 0 {
		f.warn(fieldBody, first, "text before the first question joins Q%d's options", out[0].Number)
		out[0].Options = strings.TrimSpace(preamble + "\n\n" + out[0].Options)
	}
	return out, preamble
}

func questionField(n int32) string { return "Q" + strconv.Itoa(int(n)) }

// readQuestion reads one question: its heading, then its options up to the
// last `**Recommendation:**` line before its answer, then the last
// `**Answer:**` line and what follows it.
func readQuestion(head string, block []string, fenced []bool, line int, f *File) Question {
	m := questionHead.FindStringSubmatch(head)
	q := Question{Number: number(m[1]), Question: oneLine(m[2]), Line: line}
	field := questionField(q.Number)
	switch {
	case q.Number == 0:
		f.fail(fieldBody, line, "the question heading %q names no number in range", head)
		return q
	case q.Question == "":
		f.fail(field, line, "Q%d has no question", q.Number)
	case len([]rune(q.Question)) > maxQuestionLength:
		f.fail(field, line, "Q%d is longer than %d characters", q.Number, maxQuestionLength)
	}
	answer := lastPrefixed(block, fenced, len(block), answerPrefix)
	limit := len(block)
	if answer >= 0 {
		limit = answer
	}
	rec := lastPrefixed(block, fenced, limit, recommendPrefix)
	optionsEnd := limit
	if rec >= 0 {
		optionsEnd = rec
		q.Recommendation = rest(block, rec, limit, recommendPrefix)
	}
	q.Options = strings.TrimSpace(strings.Join(block[:optionsEnd], "\n"))
	q.Status, q.Answer = answerOf(q, block, answer, f)
	return q
}

// answerOf reads the answer line: _open_, _withdrawn_, or the answer given.
func answerOf(q Question, block []string, answer int, f *File) (string, string) {
	field := questionField(q.Number)
	if answer < 0 {
		f.warn(field, q.Line, "Q%d has no **Answer:** line; it is imported as open", q.Number)
		return questionOpen, ""
	}
	text := rest(block, answer, len(block), answerPrefix)
	switch text {
	case answerOpen:
		return questionOpen, ""
	case answerWithdrawn:
		return questionWithdrawn, ""
	case "":
		f.warn(field, q.Line, "Q%d's answer is empty; it is imported as open", q.Number)
		return questionOpen, ""
	}
	return questionAnswered, text
}

// lastPrefixed is the last line before limit, outside fenced code, that
// starts with prefix; -1 for none.
func lastPrefixed(block []string, fenced []bool, limit int, prefix string) int {
	for i := limit - 1; i >= 0; i-- {
		if !fenced[i] && strings.HasPrefix(block[i], prefix) {
			return i
		}
	}
	return -1
}

// rest is the text of the line at i after prefix and the lines up to limit,
// trimmed.
func rest(block []string, i, limit int, prefix string) string {
	parts := append([]string{strings.TrimPrefix(block[i], prefix)}, block[i+1:limit]...)
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// fences marks the lines inside fenced code blocks, the fences included
// (CommonMark: three or more backticks or tildes, indented three spaces at
// most, closed by the same character at least as often).
func fences(lines []string) []bool {
	inside := make([]bool, len(lines))
	var marker byte
	var width int
	open := false
	for i, l := range lines {
		t := strings.TrimLeft(l, " ")
		c, n := fenceRun(t)
		switch {
		case len(l)-len(t) > 3 || n < 3:
			inside[i] = open
		case !open:
			open, marker, width = true, c, n
			inside[i] = true
		case c == marker && n >= width && strings.TrimSpace(t[n:]) == "":
			open = false
			inside[i] = true
		default:
			inside[i] = true
		}
	}
	return inside
}

func fenceRun(t string) (byte, int) {
	if t == "" || (t[0] != '`' && t[0] != '~') {
		return 0, 0
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	return t[0], n
}
