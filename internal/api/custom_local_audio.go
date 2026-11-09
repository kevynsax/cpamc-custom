package api

// CUSTOM (cpamc-custom): local GPU audio through the home-gpu llama-swap provider.
// See CUSTOM_FEATURES.md ("Local GPU audio").
//
// POST /v1/audio/speech with model "chatterbox" and every
// POST /v1/audio/transcriptions request are relayed unchanged to the
// openai-compatibility entry named "home-gpu" (llama-swap), so they work from
// outside the LAN with a client API key. Other speech models keep their routes.

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

const customLocalAudioProvider = "home-gpu"

// customLocalSpeechModel reports the speech models served by llama-swap.
func customLocalSpeechModel(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), "chatterbox")
}

// customLocalAudioBaseURL returns the configured home-gpu base URL, or "" when absent.
func (s *Server) customLocalAudioBaseURL() string {
	cfg := s.getConfig()
	if cfg == nil {
		return ""
	}
	for _, compat := range cfg.OpenAICompatibility {
		if compat.Disabled || !strings.EqualFold(compat.Name, customLocalAudioProvider) {
			continue
		}
		return strings.TrimRight(strings.TrimSpace(compat.BaseURL), "/")
	}
	return ""
}

// customLocalAudioRelay forwards the request body to path on the home-gpu provider.
func (s *Server) customLocalAudioRelay(c *gin.Context, path string, body []byte) {
	base := s.customLocalAudioBaseURL()
	if base == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "local GPU audio provider is not configured"}})
		return
	}
	up, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "invalid local audio request"}})
		return
	}
	up.Header.Set("Content-Type", c.GetHeader("Content-Type"))
	resp, err := http.DefaultClient.Do(up)
	if err != nil {
		log.WithError(err).Warn("local GPU audio upstream request failed")
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "local GPU audio upstream request failed"}})
		return
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.WithError(errClose).Debug("close local GPU audio response")
		}
	}()
	c.Status(resp.StatusCode)
	c.Header("Content-Type", resp.Header.Get("Content-Type"))
	if _, errCopy := io.Copy(c.Writer, resp.Body); errCopy != nil {
		log.WithError(errCopy).Debug("copy local GPU audio response")
	}
}

// customTranscriptions relays OpenAI-style transcription uploads to llama-swap whisper.
func (s *Server) customTranscriptions(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "unreadable request body"}})
		return
	}
	s.customLocalAudioRelay(c, "/audio/transcriptions", body)
}
