package trading

type ErrorKind string

const (
	ErrorKindValidation ErrorKind = "VALIDATION"
	ErrorKindNotFound   ErrorKind = "NOT_FOUND"
	ErrorKindConflict   ErrorKind = "CONFLICT"
	ErrorKindClosed     ErrorKind = "CLOSED"
)

type DomainError struct {
	Kind    ErrorKind
	Code    string
	Message string
}

func (e *DomainError) Error() string {
	return e.Message
}

func validationError(code, message string) error {
	return &DomainError{Kind: ErrorKindValidation, Code: code, Message: message}
}

func notFoundError(code, message string) error {
	return &DomainError{Kind: ErrorKindNotFound, Code: code, Message: message}
}

func conflictError(code, message string) error {
	return &DomainError{Kind: ErrorKindConflict, Code: code, Message: message}
}

func closedError(message string) error {
	return &DomainError{Kind: ErrorKindClosed, Code: "trade_closed", Message: message}
}
