package live

// CUSTOM (cpamc-custom): tests for GPT Live over an API-key credential.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestHandlerRoutesGPTLiveToAPIKeyLiveSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := auth.NewManager(nil, &auth.RoundRobinSelector{}, nil)
	answer := `{"session":{"id":"live_abc"},"transport":{"sdp":"v=0\r\na=answer\r\n"}}`
	executor := &captureExecutor{responseBody: &trackedResponseBody{Reader: strings.NewReader(answer)}}
	manager.RegisterExecutor(executor)
	registerCredential(t, manager, &auth.Auth{
		ID: "codex-oauth", Provider: "codex", Status: auth.StatusActive,
		Metadata: map[string]any{"access_token": "oauth-token"},
	})
	registerCredential(t, manager, &auth.Auth{
		ID: "codex-api-key", Provider: "codex", Status: auth.StatusActive,
		Attributes: map[string]string{auth.AttributeAPIKey: "sk-test", "base_url": "https://live.example.com/v1/"},
	})

	handler := NewHandler(manager, nil)
	router := gin.New()
	router.POST("/v1/live", handler.Handle)

	body := `{"sdp":"v=0\r\na=offer\r\n","session":{"model":"gpt-live-1.1","instructions":"oi","audio":{"output":{"voice":"marin"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/live", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Alpha", "quicksilver=v2")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	if executor.selectedAuth == nil || executor.selectedAuth.ID != "codex-api-key" {
		t.Fatalf("selected auth = %+v, want codex-api-key", executor.selectedAuth)
	}
	if got := executor.request.URL.String(); got != "https://live.example.com/v1/live/sessions" {
		t.Fatalf("upstream URL = %q", got)
	}
	var sent struct {
		Session   map[string]any `json:"session"`
		Transport map[string]any `json:"transport"`
	}
	if err := json.Unmarshal(executor.body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Session["model"] != "gpt-live-1.1" || sent.Transport["type"] != "webrtc" || !strings.HasPrefix(sent.Transport["sdp"].(string), "v=0") {
		t.Fatalf("unexpected upstream body: %s", executor.body)
	}
	if recorder.Header().Get("Content-Type") != "application/sdp" || recorder.Body.String() != "v=0\r\na=answer\r\n" {
		t.Fatalf("answer = %q (%s)", recorder.Body.String(), recorder.Header().Get("Content-Type"))
	}
	if recorder.Header().Get("Location") != "/v1/live/live_abc" {
		t.Fatalf("Location = %q", recorder.Header().Get("Location"))
	}
}

func TestHandlerKeepsCodexModelOnOAuthRoute(t *testing.T) {
	if isAPIKeyLiveModel("gpt-live-1-codex") || !isAPIKeyLiveModel("gpt-live-1") || isAPIKeyLiveModel("") || !isAPIKeyLiveModel(" GPT-Live-1.1 ") {
		t.Fatal("model routing is wrong")
	}
}

func TestAPIKeyLiveRejectsMissingCredentialAndOffer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := auth.NewManager(nil, &auth.RoundRobinSelector{}, nil)
	manager.RegisterExecutor(&captureExecutor{})
	registerCredential(t, manager, &auth.Auth{
		ID: "codex-oauth", Provider: "codex", Status: auth.StatusActive,
		Metadata: map[string]any{"access_token": "oauth-token"},
	})
	handler := NewHandler(manager, nil)
	router := gin.New()
	router.POST("/v1/live", handler.Handle)
	for name, body := range map[string]string{
		"no api key": `{"sdp":"v=0","session":{"model":"gpt-live-1.1"}}`,
		"no sdp":     `{"session":{"model":"gpt-live-1.1"}}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/live", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code < 400 {
			t.Fatalf("%s: status %d, want an error", name, recorder.Code)
		}
	}
}

func TestAPIKeyLiveAnswerRejectsBadResponse(t *testing.T) {
	if _, _, err := apiKeyLiveAnswer([]byte(`{"transport":{"sdp":"nope"}}`)); err == nil {
		t.Fatal("expected error")
	}
}
