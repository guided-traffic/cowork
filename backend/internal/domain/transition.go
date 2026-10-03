package domain

// Move is the kind of a state transition (docs/adr/0009 D2–D5).
type Move int

// The moves; MoveInvalid is a pair outside the matrix.
const (
	MoveInvalid Move = iota
	// MoveForward is the next step: filed → analysed → decided → in-progress
	// → review.
	MoveForward
	// MoveBackward is in-progress → decided or analysed, decided → analysed,
	// review → in-progress, with a reason.
	MoveBackward
	// MoveBlock enters blocked from a non-terminal state with a block.
	MoveBlock
	// MoveUnblock leaves blocked to the state it came from.
	MoveUnblock
	// MoveDone is done by hand from any open state, with a verification note
	// (docs/adr/0009 D5); an agent's only from in-progress or review.
	MoveDone
	// MoveWithdraw takes a done by hand back, done → the state it came from,
	// with a reason (docs/adr/0009 D5).
	MoveWithdraw
	// MoveDrop is any non-terminal state → dropped, with a reason.
	MoveDrop
	// MoveReopen is dropped → filed, with a reason.
	MoveReopen
)

var (
	forward = map[TicketState]TicketState{
		StateFiled: StateAnalysed, StateAnalysed: StateDecided, StateDecided: StateInProgress, StateInProgress: StateReview,
	}
	backward = map[[2]TicketState]bool{
		{StateInProgress, StateDecided}: true, {StateInProgress, StateAnalysed}: true, {StateDecided, StateAnalysed}: true,
		{StateReview, StateInProgress}: true,
	}
)

// ClassifyMove names the move from → to; origin is the state a blocked
// ticket came from, or the state a done ticket was done from.
func ClassifyMove(from, to, origin TicketState) Move {
	switch {
	case from == StateDone:
		if to == origin {
			return MoveWithdraw
		}
	case from == StateDropped:
		if to == StateFiled {
			return MoveReopen
		}
	case to == StateDropped:
		return MoveDrop
	case to == StateDone:
		return MoveDone
	case to == StateBlocked:
		if from != StateBlocked {
			return MoveBlock
		}
	case from == StateBlocked:
		if to == origin {
			return MoveUnblock
		}
	case forward[from] == to:
		return MoveForward
	case backward[[2]TicketState{from, to}]:
		return MoveBackward
	}
	return MoveInvalid
}

// NeedsReason reports whether the move requires a reason: a backward move, a
// drop, a reopen, the withdrawal of a done by hand (docs/adr/0009 D3–D5) and a
// block, whose reason is the block's text (D2).
func (m Move) NeedsReason() bool {
	return m == MoveBackward || m == MoveDrop || m == MoveReopen || m == MoveBlock || m == MoveWithdraw
}

// AgentCloses reports whether an agent with close may close a ticket in s,
// by hand or by its stages: from in-progress and review only, so that close
// never stands in for decide (docs/adr/0043 D4, docs/adr/0009 D5).
func (s TicketState) AgentCloses() bool { return s == StateInProgress || s == StateReview }
