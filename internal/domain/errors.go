package domain

import "fmt"

type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Msg
	}
	return e.Field + ": " + e.Msg
}

func Invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Msg: fmt.Sprintf(format, args...)}
}

type TransitionError struct {
	From, To string
}

func (e *TransitionError) Error() string {
	return "invalid transition " + e.From + " -> " + e.To
}
