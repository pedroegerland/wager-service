package wager

type FailureCode string

const (
	CodeInsufficientFunds         FailureCode = "INSUFFICIENT_FUNDS"
	CodeReversalInsufficientFunds FailureCode = "REVERSAL_INSUFFICIENT_FUNDS"
	CodeReferenceNotFound         FailureCode = "REFERENCE_NOT_FOUND"
	CodeReferenceMismatch         FailureCode = "REFERENCE_MISMATCH"
	CodeReferenceNotProcessed     FailureCode = "REFERENCE_NOT_PROCESSED"
	CodeReferenceKindNotAllowed   FailureCode = "REFERENCE_KIND_NOT_ALLOWED"
	CodeReferenceAlreadyReversed  FailureCode = "REFERENCE_ALREADY_REVERSED"
	CodeWalletMismatch            FailureCode = "WALLET_MISMATCH"
	CodeCurrencyMismatch          FailureCode = "CURRENCY_MISMATCH"

	CodeInternalError FailureCode = "INTERNAL_ERROR"
)
