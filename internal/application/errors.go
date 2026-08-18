package application

import (
	"errors"
	"fmt"

	"github.com/ravinsharma7/missis/pkg/missis"
)

func invalidInput(format string, args ...any) error {
	return &missis.DomainError{Kind: missis.ErrInvalidInput, Message: fmt.Sprintf(format, args...)}
}

func notFound(format string, args ...any) error {
	return &missis.DomainError{Kind: missis.ErrNotFound, Message: fmt.Sprintf(format, args...)}
}

func validation(format string, args ...any) error {
	return &missis.DomainError{Kind: missis.ErrValidation, Message: fmt.Sprintf(format, args...)}
}

func conflict(err error) error {
	return &missis.DomainError{Kind: missis.ErrConflict, Message: err.Error()}
}

func storage(err error) error {
	return &missis.DomainError{Kind: missis.ErrStorage, Message: err.Error()}
}

// keepStorage preserves an already-typed error and wraps plain store errors
// as storage failures.
func keepStorage(err error) error {
	if err == nil {
		return nil
	}
	var de *missis.DomainError
	if errors.As(err, &de) {
		return err
	}
	return storage(err)
}
