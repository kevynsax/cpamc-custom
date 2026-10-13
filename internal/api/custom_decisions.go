package api

// CUSTOM (cpamc-custom): native Jev decisions through the normal authenticated
// model/credential selection pipeline. OpenRouter secrets stay on the gateway.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
)

const customDecisionsMaxBody = 1 << 20

func (s *Server) customDecisions(c *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, customDecisionsMaxBody))
	if err != nil {
		status := http.StatusBadRequest
		if _, ok := err.(*http.MaxBytesError); ok {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{"error": gin.H{"message": "invalid decisions request"}})
		return
	}
	var req struct {
		Model     string                     `json:"model"`
		State     json.RawMessage            `json:"state"`
		Questions map[string]json.RawMessage `json:"questions"`
		Stream    bool                       `json:"stream"`
	}
	if json.Unmarshal(body, &req) != nil || !strings.HasPrefix(req.Model, "typesafe/jev-") || len(req.State) == 0 || len(req.Questions) == 0 || len(req.Questions) > 48 || req.Stream {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "model, state and 1..48 questions are required; streaming is unsupported"}})
		return
	}
	if s == nil || s.handlers == nil || s.handlers.AuthManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "decisions gateway unavailable"}})
		return
	}
	ctx, cancel := s.handlers.GetContextWithCancel(nil, c, context.Background())
	defer cancel(nil)
	body, headers, errMsg := s.handlers.ExecuteWithAuthManager(ctx, "openai", req.Model, body, "jev-decisions")
	if errMsg != nil {
		s.handlers.WriteErrorResponse(c, errMsg)
		return
	}
	handlers.WriteUpstreamHeaders(c.Writer.Header(), headers)
	c.Data(http.StatusOK, "application/json", body)
}
