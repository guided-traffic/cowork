package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var opened = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// scoreOf is the score of the inputs at a moment.
func scoreOf(in ScoreInputs, now time.Time) float64 { return ScoreAt(ScoreKey(in), now) }

// docs/adr/0014 D4, version 1: severity, horizon and stakes add their weights;
// a watch counts nothing.
func TestScoreWeights(t *testing.T) {
	at := opened
	for _, c := range []struct {
		in   ScoreInputs
		want float64
	}{
		{ScoreInputs{Severity: "critical", Horizon: UrgencyNow}, 16},
		{ScoreInputs{Severity: "high", Horizon: UrgencyRelease}, 10},
		{ScoreInputs{Severity: "medium", Horizon: UrgencyNext}, 6},
		{ScoreInputs{Severity: "low", Horizon: UrgencyLater}, 2},
		{ScoreInputs{Severity: "cosmetic", Horizon: UrgencyIcebox}, -5},
		{ScoreInputs{Severity: "medium", Horizon: UrgencyLater, Need: 2}, 6},
		{ScoreInputs{Severity: "medium", Horizon: UrgencyLater, Urgent: 1}, 6},
		{ScoreInputs{Severity: "medium", Horizon: UrgencyLater, Need: 1, Urgent: 2}, 9},
	} {
		c.in.OpenedAt = opened
		assert.InDelta(t, c.want, scoreOf(c.in, at), 1e-9, "%+v", c.in)
	}
}

// Age adds one for every thirty days since the ticket was opened, in elapsed
// time; the figure is rounded to a tenth.
func TestScoreAge(t *testing.T) {
	in := ScoreInputs{Severity: "low", Horizon: UrgencyLater, OpenedAt: opened}
	assert.InDelta(t, 2, scoreOf(in, opened), 1e-9)
	assert.InDelta(t, 2.5, scoreOf(in, opened.Add(15*24*time.Hour)), 1e-9)
	assert.InDelta(t, 3, scoreOf(in, opened.Add(30*24*time.Hour)), 1e-9)
	assert.InDelta(t, 5, scoreOf(in, opened.Add(90*24*time.Hour)), 1e-9)
	assert.InDelta(t, 2.1, scoreOf(in, opened.Add(3*24*time.Hour)), 1e-9, "a tenth every three days")
	assert.InDelta(t, 2, scoreOf(in, opened.Add(12*time.Hour)), 1e-9, "half a day is below a tenth")
}

// The key is what the score is stored and ordered as: its order is the
// score's at every moment, because time adds the same to every ticket.
func TestScoreKeyOrdersAsTheScoreDoes(t *testing.T) {
	older := ScoreInputs{Severity: "medium", Horizon: UrgencyLater, OpenedAt: opened}
	newer := ScoreInputs{Severity: "medium", Horizon: UrgencyLater, OpenedAt: opened.Add(time.Hour)}
	weightier := ScoreInputs{Severity: "high", Horizon: UrgencyLater, OpenedAt: opened.Add(48 * time.Hour)}
	assert.Greater(t, ScoreKey(older), ScoreKey(newer), "an hour older is higher, within the same day as well")
	assert.Greater(t, ScoreKey(weightier), ScoreKey(older), "two of severity outweigh two days of age")
	for _, later := range []time.Duration{0, 24 * time.Hour, 400 * 24 * time.Hour} {
		now := opened.Add(48*time.Hour + later)
		raw := func(in ScoreInputs) float64 { return ScoreKey(in) + ageUnits(now) }
		assert.Greater(t, raw(weightier), raw(older))
		assert.Greater(t, raw(older), raw(newer))
	}
}

// Every value of the vocabularies has its weight: a vocabulary that grows
// without one would score as zero silently.
func TestScoreKnowsEveryValue(t *testing.T) {
	for _, s := range []Severity{"critical", "high", "medium", "low", "cosmetic"} {
		_, ok := severityWeight[s]
		assert.True(t, ok, s)
	}
	for _, u := range []Urgency{UrgencyNow, UrgencyRelease, UrgencyNext, UrgencyLater, UrgencyIcebox} {
		_, ok := horizonWeight[u]
		assert.True(t, ok, u)
	}
	assert.Equal(t, 1, ScoreVersion)
}
