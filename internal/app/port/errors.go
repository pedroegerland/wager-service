package port

import (
	"errors"
)

var (
	ErrNotFound = errors.New("not found")

	ErrConflict = errors.New("conflict")

	ErrStale = errors.New("stale version")

	ErrUnavailable = errors.New("unavailable")
)

type ConflictError struct {
	Constraint string
}

func (e *ConflictError) Error() string        { return "conflict on " + e.Constraint }
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }
