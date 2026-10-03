package domain

// Stages are a ticket's three progress stages, each 0 to 100 in steps of
// five (docs/adr/0017 D2): refinement, the work of filed and analysed;
// implementation, the work of in-progress and the progress of the first
// release; review, the work of review.
type Stages struct {
	Refinement, Implementation, Review int
}

// Full reports whether all three stages are at 100.
func (s Stages) Full() bool {
	return s.Refinement == 100 && s.Implementation == 100 && s.Review == 100
}

// Lowered reports whether next has a stage below s's.
func (s Stages) Lowered(next Stages) bool {
	return next.Refinement < s.Refinement || next.Implementation < s.Implementation || next.Review < s.Review
}

// StageEffect is what a write of a ticket's own stages does to its state.
type StageEffect int

// The effects.
const (
	// StagesKeep changes the stages and nothing else.
	StagesKeep StageEffect = iota
	// StagesComplete brings the last stage to 100: the done act, with the
	// verification note, the prerequisite refusal and for an agent close
	// (docs/adr/0009 D5, docs/adr/0017 D4).
	StagesComplete
	// StagesReopen lowers a stage of a ticket done by its stages: it reopens
	// to the state it was done from, with a reason (docs/adr/0009 D5).
	StagesReopen
)

// EffectOfStages decides a write of the stages of a ticket without children
// in state, done by hand or not, from before to after. A parent's stages are
// its children's and take no write: it is never done by them. Done by hand
// leaves the stages editable, and the ticket stays done while they change
// (docs/adr/0009 D5, docs/adr/0017 D5).
func EffectOfStages(state TicketState, doneByHand bool, before, after Stages) StageEffect {
	switch {
	case state == StateDropped:
		return StagesKeep
	case state == StateDone:
		if !doneByHand && before.Lowered(after) {
			return StagesReopen
		}
		return StagesKeep
	case !before.Full() && after.Full():
		return StagesComplete
	}
	return StagesKeep
}

// WithdrawalStaysDone reports whether withdrawing a done by hand leaves the
// ticket done, by its stages: a ticket without children whose three stages are
// full. A parent returns to the state it came from (docs/adr/0009 D5).
func WithdrawalStaysDone(hasChildren bool, s Stages) bool {
	return !hasChildren && s.Full()
}

// DoneByStages reports whether a done ticket is done by its stages: no done by
// hand on record, no children, and the three stages full. Every other done is
// by hand (docs/adr/0009 D5) — a parent's, and a leaf's whose stages are short
// of full, which the release before the stages leaves when it closes a ticket
// or when a parent it closed loses its last child (docs/adr/0028 D4).
func DoneByStages(doneByHand, hasChildren bool, s Stages) bool {
	return !doneByHand && WithdrawalStaysDone(hasChildren, s)
}
