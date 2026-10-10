// Package health implements the facade's active health check state machine.
// A backend becomes unhealthy after three consecutive failures and healthy
// after two consecutive successes. It starts unhealthy until those successes.
package health

// State is one backend's probe history.
type State struct {
	Up        bool
	Successes int
	Failures  int
}

// Checker tracks every environment the policy file names.
type Checker struct {
	FailThreshold int
	PassThreshold int
	States        map[string]*State
}

// New returns a checker with the Section 13.2 defaults (3 failures, 2 successes).
func New() *Checker {
	return &Checker{FailThreshold: 3, PassThreshold: 2, States: map[string]*State{}}
}

// Observe records one probe. changed is true when Up flips.
func (c *Checker) Observe(name string, ok bool) (up bool, changed bool) {
	if c.FailThreshold <= 0 {
		c.FailThreshold = 3
	}
	if c.PassThreshold <= 0 {
		c.PassThreshold = 2
	}
	state := c.States[name]
	if state == nil {
		state = &State{}
		c.States[name] = state
	}
	before := state.Up
	if ok {
		state.Failures = 0
		state.Successes++
		if state.Successes >= c.PassThreshold {
			state.Up = true
		}
	} else {
		state.Successes = 0
		state.Failures++
		if state.Failures >= c.FailThreshold {
			state.Up = false
		}
	}
	return state.Up, state.Up != before
}
