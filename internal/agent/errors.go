package agent

import "errors"

var (
	errNoModel   = errors.New("native agent model provider is not configured")
	errBudget    = errors.New("token budget exceeded")
	errDecision  = errors.New("model did not return a valid agent decision")
	errSteps     = errors.New("native agent exceeded max steps without a final answer")
	errNoEvolver = errors.New("capability evolution is not available")
)
