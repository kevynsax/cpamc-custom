package middleware

// CUSTOM (cpamc-custom): per-request logger selection for the request logging middleware.
// See CUSTOM_FEATURES.md ("Google-account clients").

import (
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
)

// RequestLoggerSelector returns the logger that records the current request. It receives
// the logger chosen so far, so a request it does not claim keeps that logger.
type RequestLoggerSelector func(c *gin.Context, logger logging.RequestLogger) logging.RequestLogger
