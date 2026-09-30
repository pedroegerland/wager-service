package wager

type Status string

const (
	StatusPending          Status = "PENDING"
	StatusPendingReference Status = "PENDING_REFERENCE"
	StatusProcessed        Status = "PROCESSED"
	StatusRejected         Status = "REJECTED"
	StatusFailed           Status = "FAILED"
)

func (s Status) IsTerminal() bool {
	return s == StatusProcessed || s == StatusRejected || s == StatusFailed
}
