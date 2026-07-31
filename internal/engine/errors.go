package engine

import "fmt"

// RequestErrorKind classifies failures that callers may safely branch on.
// Mouse validation and protocol capability failures use these categories so
// the daemon does not misreport them as PTY I/O failures.
type RequestErrorKind uint8

const (
	RequestErrorInvalidArgument RequestErrorKind = iota + 1
	RequestErrorFailedPrecondition
	RequestErrorIO
)

// RequestError is a typed engine failure with optional structured details.
type RequestError struct {
	Kind    RequestErrorKind
	Message string
	Details any
	Err     error
}

func (e *RequestError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("engine request error (%d)", e.Kind)
}

func (e *RequestError) Unwrap() error { return e.Err }

func invalidRequest(message string, details any, err error) error {
	return &RequestError{
		Kind:    RequestErrorInvalidArgument,
		Message: message,
		Details: details,
		Err:     err,
	}
}

func failedPrecondition(message string, details any, err error) error {
	return &RequestError{
		Kind:    RequestErrorFailedPrecondition,
		Message: message,
		Details: details,
		Err:     err,
	}
}

func inputIO(err error) error {
	return &RequestError{
		Kind:    RequestErrorIO,
		Message: err.Error(),
		Err:     err,
	}
}
