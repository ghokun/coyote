package api

// NotFoundError maps to HTTP 404.
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string { return "task not found: " + e.ID }

// ErrNotFound builds a missing-task error.
func ErrNotFound(id string) error { return &NotFoundError{ID: id} }

// IsNotFound reports whether err is a missing-task error.
func IsNotFound(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}

// ConflictError maps to HTTP 409 (illegal lifecycle transition or a
// request that cannot be satisfied, e.g. missing credentials).
type ConflictError struct{ Msg string }

func (e *ConflictError) Error() string { return e.Msg }

// ErrConflict builds an illegal-transition error.
func ErrConflict(msg string) error { return &ConflictError{Msg: msg} }

// IsConflict reports whether err is an illegal-transition error.
func IsConflict(err error) bool {
	_, ok := err.(*ConflictError)
	return ok
}

// BadRequestError maps to HTTP 400.
type BadRequestError struct{ Msg string }

func (e *BadRequestError) Error() string { return e.Msg }

// ErrBadRequest builds a malformed-request error.
func ErrBadRequest(msg string) error { return &BadRequestError{Msg: msg} }

// IsBadRequest reports whether err is a malformed-request error.
func IsBadRequest(err error) bool {
	_, ok := err.(*BadRequestError)
	return ok
}
