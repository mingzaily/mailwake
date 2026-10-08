// Package fault defines language-independent, safe application errors.
package fault

import "errors"

type Error struct {
	Code   string            `json:"code"`
	Params map[string]string `json:"params,omitempty"`
}

func New(code string) *Error   { return &Error{Code: code} }
func (e *Error) Error() string { return e.Code }

// From prevents internal or upstream error text from reaching public responses.
func From(err error, fallback string) *Error {
	var coded *Error
	if errors.As(err, &coded) {
		return coded
	}
	return New(fallback)
}
