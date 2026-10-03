package domain

// Move is the kind of a state transition (docs/adr/0009 D2–D5).
type Move int

// The moves; MoveInvalid is a pair outside the matrix.
const (
	MoveInvalid Move = iota
	// MoveForward is the next step: filed → analysed → decided → in-progress.
	MoveForward
	// MoveBackward is in-progress → decided or analysed, decided → analysed,
	// with a reason.
	MoveBackward
	// MoveBlock enters blocked from a non-terminal state with a block.
	MoveBlock
	// MoveUnblock leaves blocked to the state it came from.
	MoveUnblock
	// MoveDone is in-progress → done, with a verification note.
	MoveDone
	// MoveDrop is any non-terminal state → dropped, with a reason.
	MoveDrop
	// MoveReopen is a terminal state → filed, with a reason.
	MoveReopen
)

var (
	forward = map[TicketState]TicketState{
		StateFiled: StateAnalysed, StateAnalysed: StateDecided, StateDecided: StateInProgress,
	}
	backward = map[[2]TicketState]bool{
		{StateInProgress, StateDecided}: true, {StateInProgress, StateAnalysed}: true, {StateDecided, StateAnalysed}: true,
	}
)

// ClassifyMove names the move from → to; origin is the state a blocked
// ticket came from.
func ClassifyMove(from, to, origin TicketState) Move {
	switch {
	case from.Terminal():
		if to == StateFiled {
			return MoveReopen
		}
	case to == StateDropped:
		return MoveDrop
	case to == StateBlocked:
		if from != StateBlocked {
			return MoveBlock
		}
	case from == StateBlocked:
		if to == origin {
			return MoveUnblock
		}
	case from == StateInProgress && to == StateDone:
		return MoveDone
	case forward[from] == to:
		return MoveForward
	case backward[[2]TicketState{from, to}]:
		return MoveBackward
	}
	return MoveInvalid
}

// NeedsReason reports whether the move requires a reason: a backward move, a
// drop, a reopen (docs/adr/0009 D3, D4) and a block, whose reason is the
// block's text (D2).
func (m Move) NeedsReason() bool {
	return m == MoveBackward || m == MoveDrop || m == MoveReopen || m == MoveBlock
}
