package api

// CUSTOM (cpamc-custom): full request log for Google-account clients.
// See CUSTOM_FEATURES.md ("Google-account clients").

import (
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/access/config_access"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api/middleware"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
)

// applyGoogleAccountClients installs the clients allowed to use Google-account models and
// builds the request logger that records each of them in full. Called on startup and
// every config reload.
func (s *Server) applyGoogleAccountClients(cfg *config.Config) {
	handlers.SetGoogleAccountClients(cfg.GoogleAccountClients)
	logsDir := logging.ResolveLogDirectory(cfg)
	configDir := filepath.Dir(s.configFilePath)
	maxBytes := int64(cfg.ClientRequestLogMaxTotalSizeMB) * 1024 * 1024
	loggers := logging.ConfigureClientRequestLog(logsDir, configDir, cfg.GoogleAccountClients, maxBytes)
	s.clientRequestLoggers.Store(loggers)
}

// clientRequestLoggerSelector hands a Google-account client's request to that client's
// own request logger and marks the request so its upstream exchange is recorded in full.
func (s *Server) clientRequestLoggerSelector() middleware.RequestLoggerSelector {
	return func(c *gin.Context, fallback logging.RequestLogger) logging.RequestLogger {
		loggers := s.clientRequestLoggers.Load()
		credentials := configaccess.RequestCredentials(c.Request)
		clientLogger := loggers.ForCredentials(credentials)
		if clientLogger == nil {
			return fallback
		}
		marked := logging.WithClientRequestLog(c.Request.Context())
		c.Request = c.Request.WithContext(marked)
		return clientLogger
	}
}
