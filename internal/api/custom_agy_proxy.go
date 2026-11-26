package api

// CUSTOM (cpamc-custom)

import (
	"fmt"
	"net"
	"net/http"

	"github.com/MoeForge/moego/json"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/agyproxy"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	log "github.com/sirupsen/logrus"
)

type customAgyBootstrapRequest struct {
	Client    string `json:"client"`
	AccountID string `json:"account_id"`
}

func (s *Server) startAgyProxy(cfg *config.Config) error {
	if cfg == nil || cfg.AgyProxy.Listen == "" {
		return nil
	}
	logger := func(key string) *logging.FileRequestLogger {
		loggers := s.clientRequestLoggers.Load()
		credentials := []string{key}
		selected := loggers.ForCredentials(credentials)
		return selected
	}
	proxy, errProxy := agyproxy.New(cfg, s.handlers.AuthManager, s.getConfig, logger)
	if errProxy != nil {
		return fmt.Errorf("initialize agy proxy: %w", errProxy)
	}
	listener, errListen := net.Listen("tcp", cfg.AgyProxy.Listen)
	if errListen != nil {
		proxy.Close()
		return fmt.Errorf("listen agy proxy: %w", errListen)
	}
	s.agyProxy.Store(proxy)
	go func() {
		if errServe := proxy.Serve(listener); errServe != nil {
			log.WithError(errServe).Error("agy proxy stopped")
			if errClose := s.server.Close(); errClose != nil {
				log.WithError(errClose).Warn("close gateway after proxy failure")
			}
		}
	}()
	return nil
}

func (s *Server) stopAgyProxy() {
	proxy := s.agyProxy.Swap(nil)
	if proxy == nil {
		return
	}
	proxy.Close()
}

func (s *Server) customAgySetup(c *gin.Context) {
	if c.Param("file") != "/agy-ca.pem" {
		customSetupHandler(c)
		return
	}
	proxy := s.agyProxy.Load()
	if proxy == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	public := proxy.Certificate()
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/x-pem-file", public)
}

func (s *Server) customAgyBootstrap(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	proxy := s.agyProxy.Load()
	if proxy == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	var request customAgyBootstrapRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if errDecode := decoder.Decode(&request); errDecode != nil || request.Client == "" || request.AccountID == "" {
		c.String(http.StatusBadRequest, "client and account_id are required")
		return
	}
	data, errBootstrap := proxy.Bootstrap(request.Client, request.AccountID)
	if errBootstrap != nil {
		c.String(http.StatusBadRequest, "%s", errBootstrap.Error())
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}
