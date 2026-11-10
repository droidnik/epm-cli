// Package apperr defines error types that carry a process exit code.
package apperr

import (
	"errors"
	"fmt"
)

// Exit codes (documented in `epm-cli agent-context` and the README).
const (
	OK        = 0
	General   = 1
	Usage     = 2 // bad flags/arguments, missing config
	Auth      = 3 // 401/403, token errors
	NotFound  = 4 // 404
	Client    = 5 // other 4xx: validation, conflict, locked
	Server    = 6 // 5xx, network, timeouts
	Cancelled = 130
)

// Error is an error with an exit code and optional structured details.
type Error struct {
	Msg     string
	Code    int
	Status  int // HTTP status, if any
	Details any
}

func (e *Error) Error() string { return e.Msg }

func New(code int, format string, a ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, a...), Code: code}
}

func Usagef(format string, a ...any) *Error  { return New(Usage, format, a...) }
func Authf(format string, a ...any) *Error   { return New(Auth, format, a...) }
func Serverf(format string, a ...any) *Error { return New(Server, format, a...) }

// FromStatus builds an API error with the exit code matching an HTTP status.
func FromStatus(status int, msg string, details any) *Error {
	code := Server
	switch {
	case status == 401 || status == 403:
		code = Auth
	case status == 404:
		code = NotFound
	case status >= 400 && status < 500:
		code = Client
	}
	return &Error{Msg: msg, Code: code, Status: status, Details: details}
}

// ExitCode returns the exit code for any error (General for foreign errors).
func ExitCode(err error) int {
	if err == nil {
		return OK
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return General
}
