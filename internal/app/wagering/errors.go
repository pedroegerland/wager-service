package wagering

import (
	"errors"
)

var (
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different payload")
	ErrExternalIDReused    = errors.New("external transaction id already used with another idempotency key")
	ErrInboxHashMismatch   = errors.New("inbox message redelivered with a different payload")
	ErrInboxInconsistent   = errors.New("inbox says handled but no transaction found")
)
