package errors

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// AppError is a structured application error
type AppError struct {
	Code           int    `json:"-"`
	ErrorCode      string `json:"error"`
	DisplayMessage string `json:"display_message,omitempty"`
	Message        string `json:"message"`
}

func (e *AppError) Error() string { return e.Message }

// WithDisplayMessage sets a user-facing display message on the error
func (e *AppError) WithDisplayMessage(displayMsg string) *AppError {
	e.DisplayMessage = displayMsg
	return e
}

// NotFound creates a 404 error
func NotFound(msg string) *AppError {
	return &AppError{Code: http.StatusNotFound, ErrorCode: "not_found", Message: msg}
}

// BadRequest creates a 400 error
func BadRequest(msg string) *AppError {
	return &AppError{Code: http.StatusBadRequest, ErrorCode: "bad_request", Message: msg}
}

// Unauthorized creates a 401 error
func Unauthorized(msg string) *AppError {
	return &AppError{Code: http.StatusUnauthorized, ErrorCode: "unauthorized", Message: msg}
}

// InternalError creates a 500 error
func InternalError(msg string) *AppError {
	return &AppError{Code: http.StatusInternalServerError, ErrorCode: "internal_server_error", Message: msg}
}

// ServiceUnavailable creates a 503 error
func ServiceUnavailable(msg string) *AppError {
	return &AppError{Code: http.StatusServiceUnavailable, ErrorCode: "service_unavailable", Message: msg}
}

// HTTPErrorHandler returns a custom Echo error handler
func HTTPErrorHandler(logger *zap.Logger) echo.HTTPErrorHandler {
	return func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}

		var appErr *AppError
		if errors.As(err, &appErr) {
			if jsonErr := c.JSON(appErr.Code, appErr); jsonErr != nil {
				logger.Error("Failed to send error response", zap.Error(jsonErr))
			}
			return
		}

		var echoErr *echo.HTTPError
		if errors.As(err, &echoErr) {
			if jsonErr := c.JSON(echoErr.Code, map[string]any{
				"error":   http.StatusText(echoErr.Code),
				"message": echoErr.Message,
			}); jsonErr != nil {
				logger.Error("Failed to send error response", zap.Error(jsonErr))
			}
			return
		}

		// Log unexpected errors
		logger.Error("Unexpected error", zap.Error(err))
		if jsonErr := c.JSON(http.StatusInternalServerError, map[string]any{
			"error":   "internal_server_error",
			"message": "An unexpected error occurred",
		}); jsonErr != nil {
			logger.Error("Failed to send error response", zap.Error(jsonErr))
		}
	}
}
