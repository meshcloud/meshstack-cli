package internal

import "fmt"

// ExitError makes cmd/meshstack exit with Code rather than 1, such as the exit code of a command it
// ran.
type ExitError struct {
	Code int
	Err  error
}

func (e ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit status %d", e.Code)
}

func (e ExitError) Unwrap() error {
	return e.Err
}
