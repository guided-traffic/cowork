package domain

import (
	"math"
	"time"
)

// The score (docs/adr/0014 D3, D4): a versioned function of a ticket's facts
// that warns where they disagree with the project's rank and orders the
// person-level lists. Version 1:
//
//	score = severity_weight + urgency_weight + Σ interest_weight + age_days / 30
//
// The urgency is the ticket's horizon (docs/adr/0010 D3), the interest its
// stakes (docs/adr/0013 D3), the age the time since it was opened. A change of
// a weight is a new version: an amendment of docs/adr/0014 and a migration that
// scores every ticket again.

// ScoreVersion is the version of the function Score computes.
const ScoreVersion = 1

// scoreAgeUnit is the age that adds one to a score: thirty days.
const scoreAgeUnit = 30 * 24 * time.Hour

// The weights of version 1.
var (
	severityWeight = map[Severity]float64{"critical": 8, "high": 5, "medium": 3, "low": 1, "cosmetic": 0}
	horizonWeight  = map[Urgency]float64{UrgencyNow: 8, UrgencyRelease: 5, UrgencyNext: 3, UrgencyLater: 1, UrgencyIcebox: -5}
)

// The interest weights of version 1: a need counts one, an urgent stake two,
// a watch nothing.
const (
	needWeight   = 1
	urgentWeight = 2
)

// ScoreInputs are the facts a score reads: the severity, the horizon the
// ticket shows, how many people hold a need and how many an urgent stake,
// and when it was opened.
type ScoreInputs struct {
	Severity Severity
	Horizon  Urgency
	Need     int
	Urgent   int
	OpenedAt time.Time
}

// ScoreKey is what a ticket's score is stored and ordered as: the score less
// the age it gains over time, so time adds the same to every ticket's score
// and leaves their order alone (docs/adr/0014 D4 as made concrete
// 2026-10-05). The age counts in elapsed time, not whole days: two tickets
// opened on the same day keep their order at midnight. ScoreAt turns the key
// into the score at a moment.
func ScoreKey(in ScoreInputs) float64 {
	base := severityWeight[in.Severity] + horizonWeight[in.Horizon] +
		float64(in.Need*needWeight+in.Urgent*urgentWeight)
	return base - ageUnits(in.OpenedAt)
}

// ScoreAt is the score of a key at a moment: the key plus the age every
// ticket has gained by then, rounded to one decimal — the figure a person
// reads, which moves by a tenth every three days of age.
func ScoreAt(key float64, now time.Time) float64 {
	return math.Round((key+ageUnits(now))*10) / 10
}

// ageUnits is t's distance from the Unix epoch in units of thirty days.
func ageUnits(t time.Time) float64 {
	return float64(t.UnixNano()) / float64(scoreAgeUnit)
}
