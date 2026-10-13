package live

// CUSTOM (cpamc-custom): GPT Live over an OpenAI platform API key.
//
// The Codex Live route (chatgpt.com, OAuth) needs a Voice entitlement the ChatGPT plan may lack.
// A `/v1/live` request that names `gpt-live-1` (not the `-codex` model) is sent instead to the
// GPT Live API of the first Codex API-key credential: POST <base-url>/live/sessions with
// {session, transport:{type:"webrtc", sdp}}. The answer is returned the way the Codex route
// returns it: the SDP answer as application/sdp plus a Location header ending in the session id.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clienterror"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

const (
	apiKeyLiveModel   = "gpt-live-1"
	apiKeyLiveBaseURL = "https://api.openai.com/v1"
)

// isAPIKeyLiveModel reports whether a live request must use an API-key credential.
func isAPIKeyLiveModel(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), apiKeyLiveModel)
}

// apiKeyLiveRequest turns the Codex-shaped {sdp, session} body into the GPT Live API shape.
func apiKeyLiveRequest(body []byte) ([]byte, error) {
	var payload struct {
		SDP     string          `json:"sdp"`
		Session json.RawMessage `json:"session"`
	}
	if errUnmarshal := json.Unmarshal(body, &payload); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if strings.TrimSpace(payload.SDP) == "" {
		return nil, errors.New("live request is missing the SDP offer")
	}
	session := payload.Session
	if len(bytes.TrimSpace(session)) == 0 {
		session = json.RawMessage(`{}`)
	}
	return json.Marshal(map[string]any{
		"session":   session,
		"transport": map[string]any{"type": "webrtc", "sdp": payload.SDP},
	})
}

// apiKeyLiveAnswer extracts the SDP answer and session id from the GPT Live API response.
func apiKeyLiveAnswer(body []byte) (sdp, sessionID string, err error) {
	var answer struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
		Transport struct {
			SDP string `json:"sdp"`
		} `json:"transport"`
	}
	if errUnmarshal := json.Unmarshal(body, &answer); errUnmarshal != nil {
		return "", "", errUnmarshal
	}
	if !strings.HasPrefix(answer.Transport.SDP, "v=") {
		return "", "", errors.New("GPT Live response has no SDP answer")
	}
	return answer.Transport.SDP, answer.Session.ID, nil
}

func apiKeyLiveURL(selected *auth.Auth) string {
	base := apiKeyLiveBaseURL
	if selected != nil {
		if configured := strings.TrimSpace(selected.Attributes["base_url"]); configured != "" {
			base = configured
		}
	}
	return strings.TrimRight(base, "/") + "/live/sessions"
}

// handleAPIKeyLive serves `/v1/live` for `gpt-live-1` through a Codex API-key credential.
func (h *Handler) handleAPIKeyLive(c *gin.Context, upstreamBody []byte) {
	requestBody, errBody := apiKeyLiveRequest(upstreamBody)
	if errBody != nil {
		writeLiveError(c, http.StatusBadRequest, errBody.Error())
		return
	}
	ctx := context.WithValue(c.Request.Context(), "gin", c)
	selected, errSelect := h.authManager.SelectAuthByKind(ctx, "codex", "", auth.AuthKindAPIKey, coreexecutor.Options{Headers: liveSelectionHeaders(c)})
	if errSelect != nil {
		writeSelectionError(c, errSelect)
		return
	}
	if selected == nil {
		writeLiveError(c, http.StatusServiceUnavailable, "GPT Live API key credential unavailable")
		return
	}
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	req, errRequest := h.authManager.NewHttpRequest(ctx, selected, http.MethodPost, apiKeyLiveURL(selected), requestBody, headers)
	if errRequest != nil {
		writeLiveError(c, clienterror.HTTPStatusFromErrorOr(errRequest, http.StatusBadGateway), errRequest.Error())
		return
	}
	resp, errDo := h.authManager.HttpRequest(ctx, selected, req)
	if errDo != nil {
		writeLiveError(c, clienterror.HTTPStatusFromErrorOr(errDo, http.StatusBadGateway), errDo.Error())
		return
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("codex live api-key: close response body error: %v", errClose)
		}
	}()
	responseBody, errRead := readLimitedBody(resp.Body)
	if errRead != nil {
		writeLiveError(c, http.StatusBadGateway, "Failed to read GPT Live response")
		return
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		for _, name := range []string{"Retry-After", "X-Request-Id", "OpenAI-Request-Id"} {
			for _, value := range resp.Header.Values(name) {
				c.Writer.Header().Add(name, value)
			}
		}
		c.Data(resp.StatusCode, "application/json", responseBody)
		return
	}
	sdp, sessionID, errAnswer := apiKeyLiveAnswer(responseBody)
	if errAnswer != nil {
		writeLiveError(c, http.StatusBadGateway, errAnswer.Error())
		return
	}
	if sessionID != "" {
		c.Header("Location", "/v1/live/"+sessionID)
	}
	c.Data(resp.StatusCode, "application/sdp", []byte(sdp))
}
