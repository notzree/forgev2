package handler

import "github.com/labstack/echo/v4"

// Handler is implemented by all HTTP handlers for self-registration
type Handler interface {
	Register(e *echo.Echo)
}
