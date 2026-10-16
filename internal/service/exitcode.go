package service

import (
	"errors"
	"fmt"
)

// ExitError carries a process exit status for interactive exec/connect/run.
// main should os.Exit(Code) without printing when Code is in 0–255.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	if e == nil {
		return "exit"
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

// ExitCode returns e.Code or -1.
func ExitCode(err error) (int, bool) {
	var e *ExitError
	if errors.As(err, &e) && e != nil {
		return e.Code, true
	}
	return -1, false
}
