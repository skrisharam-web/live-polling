package apperr

import "errors"

// As is errors.As specialised to *Error, so callers do not need to import errors
// just to inspect a domain error.
func As(err error, target **Error) bool { return errors.As(err, target) }
