package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	proxyconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCustomDecisionsNativeEndToEnd(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/api/alpha/decisions" || r.Header.Get("Authorization") != "Bearer provider-only-fixture" {
			t.Error("client credential reached upstream or incorrect path")
		}
		_, _ = io.WriteString(w, `{"answers":{"q":{"type":"choice","choice":"yes"}},"usage":{"input_tokens":2,"output_tokens":1}}`)
	}))
	defer upstream.Close()
	s := newTestServer(t)
	credential := &auth.Auth{ID: "decisions-e2e", Provider: "openrouter", Attributes: map[string]string{"base_url": upstream.URL + "/api/v1", "api_key": "provider-only-fixture"}}
	s.handlers.AuthManager.RegisterExecutor(executor.NewOpenAICompatExecutor("openrouter", s.cfg))
	if _, err := s.handlers.AuthManager.Register(context.Background(), credential); err != nil {
		t.Fatal(err)
	}
	registry.GetGlobalRegistry().RegisterClient(credential.ID, credential.Provider, []*registry.ModelInfo{{ID: "typesafe/jev-1.13"}})
	defer registry.GetGlobalRegistry().UnregisterClient(credential.ID)
	r := httptest.NewRequest(http.MethodPost, "/v1/decisions", strings.NewReader(`{"model":"typesafe/jev-1.13","state":{},"questions":{"q":{"type":"choice","options":{"yes":"yes"}}}}`))
	r.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, r)
	if w.Code != 200 || !called || !strings.Contains(w.Body.String(), `"answers"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestCustomDecisionsAuthAndValidation(t *testing.T) {
	s := newTestServer(t)
	valid := `{"model":"typesafe/jev-1.13","state":{},"questions":{"q":{"type":"choice","options":{"yes":"yes"}}}}`
	questions := map[string]any{}
	for i := 0; i < 49; i++ {
		questions[fmt.Sprint(i)] = map[string]any{"type": "noul"}
	}
	tooMany, _ := json.Marshal(map[string]any{"model": "typesafe/jev-1.13", "state": map[string]any{}, "questions": questions})
	cases := []struct {
		key, body string
		want      int
	}{{"", valid, 401}, {"wrong-key", valid, 401}, {"test-key", `{`, 400}, {"test-key", `{}`, 400}, {"test-key", `{"model":"gpt-5","state":{},"questions":{"q":{}}}`, 400}, {"test-key", `{"model":"typesafe/jev-1.13","state":{},"questions":{},"stream":true}`, 400}, {"test-key", strings.Repeat("x", customDecisionsMaxBody+1), 413}, {"test-key", string(tooMany), 400}}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodPost, "/v1/decisions", strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer "+tc.key)
		w := httptest.NewRecorder()
		s.engine.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("want %d got %d: %s", tc.want, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/decisions", strings.NewReader(valid))
	r.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatal("request without configured provider must fail")
	}
}

// A TypeSafe-direct openai-compat entry (prefix "typesafe", alias "jev-1.14" -> "jev-1.14.0") serves the
// client name typesafe/jev-1.14 on /v1/systemone with the upstream TypeSafe model id.
func TestCustomDecisionsTypeSafeDirectAlias(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		body, _ := io.ReadAll(r.Body)
		var got struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &got)
		if r.URL.Path != "/v1/systemone" && r.URL.Path != "/alpha/decisions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer typesafe-only-fixture" || got.Model != "jev-1.14.0" {
			t.Errorf("auth=%q model=%q", r.Header.Get("Authorization"), got.Model)
		}
		_, _ = io.WriteString(w, `{"model":"jev-1.14.0","answers":{"q":{"type":"choice","choice":"yes","confidence":0.9}},"usage":{"input_tokens":2,"output_tokens":1}}`)
	}))
	defer upstream.Close()
	s := newTestServer(t)
	s.cfg.OpenAICompatibility = append(s.cfg.OpenAICompatibility, proxyconfig.OpenAICompatibility{
		Name: "typesafe", Prefix: "typesafe", BaseURL: upstream.URL + "/v1",
		APIKeyEntries: []proxyconfig.OpenAICompatibilityAPIKey{{APIKey: "typesafe-only-fixture"}},
		Models:        []proxyconfig.OpenAICompatibilityModel{{Name: "jev-1.14.0", Alias: "jev-1.14"}},
	})
	s.handlers.AuthManager.SetConfig(s.cfg)
	credential := &auth.Auth{ID: "typesafe-direct", Provider: "openai-compatible-typesafe", Prefix: "typesafe", Attributes: map[string]string{
		"base_url": upstream.URL + "/v1", "api_key": "typesafe-only-fixture", "compat_name": "typesafe",
		"provider_key": "openai-compatible-typesafe", "source": "config:typesafe[t]", "config_index": "0",
	}}
	s.handlers.AuthManager.RegisterExecutor(executor.NewOpenAICompatExecutor("openai-compatible-typesafe", s.cfg))
	if _, err := s.handlers.AuthManager.Register(context.Background(), credential); err != nil {
		t.Fatal(err)
	}
	registry.GetGlobalRegistry().RegisterClient(credential.ID, credential.Provider, []*registry.ModelInfo{{ID: "typesafe/jev-1.14"}})
	defer registry.GetGlobalRegistry().UnregisterClient(credential.ID)
	r := httptest.NewRequest(http.MethodPost, "/v1/decisions", strings.NewReader(`{"model":"typesafe/jev-1.14","state":{"n":1},"questions":{"q":{"type":"choice","criteria":{"yes":"yes"}}}}`))
	r.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, r)
	if w.Code != 200 || !called {
		t.Fatal(w.Code, w.Body.String())
	}
}
