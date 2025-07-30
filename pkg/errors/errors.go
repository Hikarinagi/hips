package errors

import (
	"errors"
	"fmt"
)

var (
	ErrImageNotFound    = errors.New("image not found")
	ErrInvalidFormat    = errors.New("invalid image format")
	ErrProcessingFailed = errors.New("image processing failed")
	ErrInvalidParams    = errors.New("invalid parameters")
	ErrCacheError       = errors.New("cache error")
	ErrStorageError     = errors.New("storage error")
)

type ServiceError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

func (e ServiceError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e ServiceError) Unwrap() error {
	return e.Err
}

func NewServiceError(code int, message string, err error) *ServiceError {
	return &ServiceError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}
