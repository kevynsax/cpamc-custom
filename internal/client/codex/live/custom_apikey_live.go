package live

// CUSTOM (cpamc-custom): GPT Live over an OpenAI platform API key.
//
// The Codex Live route (chatgpt.com, OAuth) needs a Voice entitlement the ChatGPT plan may lack.
// A `/v1/live` request that names `gpt-live-1.1` (not the `-codex` model) is sent instead to the
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
	apiKeyLiveModel   = "gpt-live-1.1"
	apiKeyLiveBaseURL = "https://api.openai.com/v1"
	// apiKeyLiveFallbackModel is used while the platform API still refuses gpt-live-1.1
	// (`invalid_model`, "not supported in realtime mode"): 1.1 shipped to ChatGPT first. Once the
	// API accepts 1.1 the first attempt succeeds and the fallback is never taken.
	apiKeyLiveFallbackModel = "gpt-live-1"
)

// withSessionModel returns the GPT Live API body with session.model replaced.
func withSessionModel(requestBody []byte, model string) ([]byte, error) {
	var payload map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(requestBody, &payload); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	var session map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(payload["session"], &session); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	encodedModel, _ := json.Marshal(model)
	session["model"] = encodedModel
	encodedSession, errMarshal := json.Marshal(session)
	if errMarshal != nil {
		return nil, errMarshal
	}
	payload["session"] = encodedSession
	return json.Marshal(payload)
}

// isLiveModelUnavailable reports an upstream refusal of the model itself (not of the offer or the key).
func isLiveModelUnavailable(status int, body []byte) bool {
	if status != http.StatusBadRequest {
		return false
	}
	var answer struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return false
	}
	return answer.Error.Code == "invalid_model"
}

// isAPIKeyLiveModel reports whether a live request must use an API-key credential: the GPT Live API
// models (gpt-live-1.1 and gpt-live-1). Only gpt-live-1-codex stays on the ChatGPT OAuth route, which
// refuses voice sessions without a Voice entitlement.
func isAPIKeyLiveModel(model string) bool {
	name := strings.TrimSpace(model)
	return strings.EqualFold(name, apiKeyLiveModel) || strings.EqualFold(name, apiKeyLiveFallbackModel)
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

// handleAPIKeyLive serves `/v1/live` for `gpt-live-1.1` through a Codex API-key credential.
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
	resp, responseBody, errSend := h.sendAPIKeyLive(ctx, selected, requestBody)
	if errSend == nil && isLiveModelUnavailable(resp.StatusCode, responseBody) {
		log.Warnf("codex live api-key: upstream refused %s, retrying with %s", apiKeyLiveModel, apiKeyLiveFallbackModel)
		fallbackBody, errFallback := withSessionModel(requestBody, apiKeyLiveFallbackModel)
		if errFallback == nil {
			resp, responseBody, errSend = h.sendAPIKeyLive(ctx, selected, fallbackBody)
		}
	}
	if errSend != nil {
		writeLiveError(c, clienterror.HTTPStatusFromErrorOr(errSend, http.StatusBadGateway), errSend.Error())
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

// sendAPIKeyLive posts one session request and returns the response with its body read.
func (h *Handler) sendAPIKeyLive(ctx context.Context, selected *auth.Auth, requestBody []byte) (*http.Response, []byte, error) {
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	req, errRequest := h.authManager.NewHttpRequest(ctx, selected, http.MethodPost, apiKeyLiveURL(selected), requestBody, headers)
	if errRequest != nil {
		return nil, nil, errRequest
	}
	resp, errDo := h.authManager.HttpRequest(ctx, selected, req)
	if errDo != nil {
		return nil, nil, errDo
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("codex live api-key: close response body error: %v", errClose)
		}
	}()
	responseBody, errRead := readLimitedBody(resp.Body)
	if errRead != nil {
		return nil, nil, errors.New("Failed to read GPT Live response")
	}
	return resp, responseBody, nil
}
