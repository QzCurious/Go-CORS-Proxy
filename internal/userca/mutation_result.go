package userca

// MutationResult reports the current postcondition and whether the operation
// changed owned UserCA facts.
type MutationResult struct {
	current CurrentState
	changed bool
}

// NewMutationResult constructs a mutation result for adapters at the UserCA
// seam. Production results are created by Install and Uninstall.
func NewMutationResult(current CurrentState, changed bool) MutationResult {
	return MutationResult{current: current, changed: changed}
}

func (r MutationResult) Current() CurrentState { return r.current }

func (r MutationResult) Changed() bool { return r.changed }
