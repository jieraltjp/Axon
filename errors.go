package axon

import (
	"errors"
	"fmt"
)

// ErrorKind classifies an error. Agents branch on the kind, never on the message.
type ErrorKind string

const (
	KindError                ErrorKind = "error"
	KindUsage                ErrorKind = "usage"
	KindNotFound             ErrorKind = "not_found"
	KindConfirmationRequired ErrorKind = "confirmation_required"
	KindPermissionDenied     ErrorKind = "permission_denied"
	KindUnavailable          ErrorKind = "unavailable"
)

// Exit codes are part of the machine contract and never change meaning.
const (
	ExitOK          = 0
	ExitError       = 1
	ExitUsage       = 2
	ExitNotFound    = 3
	ExitConfirm     = 4
	ExitPermission  = 5
	ExitUnavailable = 6
)

var exitCodes = map[ErrorKind]int{
	KindError:                ExitError,
	KindUsage:                ExitUsage,
	KindNotFound:             ExitNotFound,
	KindConfirmationRequired: ExitConfirm,
	KindPermissionDenied:     ExitPermission,
	KindUnavailable:          ExitUnavailable,
}

// Error is a structured error. Commands return it to control the error kind,
// code, hint and exit status that humans and agents see.
type Error struct {
	Kind      ErrorKind `json:"kind"`
	Code      string    `json:"code,omitempty"`
	Message   string    `json:"message"`
	Hint      string    `json:"hint,omitempty"`
	Retryable bool      `json:"retryable"`

	cause error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.cause }

// ExitCode returns the process exit code for the error's kind.
func (e *Error) ExitCode() int {
	if code, ok := exitCodes[e.Kind]; ok {
		return code
	}
	return ExitError
}

// WithHint returns a copy of e with the given hint.
func (e *Error) WithHint(hint string) *Error {
	c := *e
	c.Hint = hint
	return &c
}

// WithCause returns a copy of e that wraps cause.
func (e *Error) WithCause(cause error) *Error {
	c := *e
	c.cause = cause
	return &c
}

func newError(kind ErrorKind, code, format string, args ...any) *Error {
	return &Error{Kind: kind, Code: code, Message: fmt.Sprintf(format, args...)}
}

// Errorf returns a general error with the given code.
func Errorf(code, format string, args ...any) *Error {
	return newError(KindError, code, format, args...)
}

// NotFound reports that a requested resource does not exist.
func NotFound(code, format string, args ...any) *Error {
	return newError(KindNotFound, code, format, args...)
}

// Usage reports invalid arguments or flags.
func Usage(code, format string, args ...any) *Error {
	return newError(KindUsage, code, format, args...)
}

// PermissionDenied reports a failed authentication or authorization.
func PermissionDenied(code, format string, args ...any) *Error {
	return newError(KindPermissionDenied, code, format, args...)
}

// Unavailable reports a temporary failure; callers may retry.
func Unavailable(code, format string, args ...any) *Error {
	e := newError(KindUnavailable, code, format, args...)
	e.Retryable = true
	return e
}

// asError converts any error into a structured *Error.
func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Kind: KindError, Message: err.Error(), cause: err}
}
