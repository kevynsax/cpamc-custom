package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestOpenAICompatDecisionsNative(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/api/alpha/decisions" || r.Header.Get("Authorization") != "Bearer provider-fixture" {
			t.Error("incorrect upstream route or credential")
		}
		body, _ := io.ReadAll(r.Body)
		if !gjson.GetBytes(body, "state").Exists() || !gjson.GetBytes(body, "questions.q").Exists() || gjson.GetBytes(body, "messages").Exists() {
			t.Error("native request translated into chat")
		}
		_, _ = io.WriteString(w, `{"answers":{"q":{"type":"choice","choice":"yes"}},"usage":{"input_tokens":2,"output_tokens":1}}`)
	}))
	defer upstream.Close()
	e := NewOpenAICompatExecutor("openrouter", &config.Config{})
	auth := &cliproxyauth.Auth{Provider: "openrouter", Attributes: map[string]string{"base_url": upstream.URL + "/api/v1", "api_key": "provider-fixture"}}
	req := cliproxyexecutor.Request{Model: "typesafe/jev-1.13", Payload: []byte(`{"model":"typesafe/jev-1.13","state":{"fixture":true},"questions":{"q":{"type":"choice","options":{"yes":"yes"}}}}`)}
	out, err := e.Execute(context.Background(), auth, req, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Alt: "jev-decisions"})
	if err != nil || !called || !gjson.GetBytes(out.Payload, "answers.q").Exists() {
		t.Fatal(err, string(out.Payload))
	}
}
func TestOpenAICompatDecisionsPayloadRulesAreFinal(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if gjson.GetBytes(body, "model").String() != "configured-upstream-model" || gjson.GetBytes(body, "state.private").Exists() {
			t.Error("payload rules were overwritten or filtered fields restored")
		}
		_, _ = io.WriteString(w, `{"answers":{}}`)
	}))
	defer upstream.Close()
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "typesafe/jev-*", Protocol: "openai"}}, Params: map[string]any{"model": "configured-upstream-model"}}},
		Filter:   []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "typesafe/jev-*", Protocol: "openai"}}, Params: []string{"state.private"}}},
	}}
	e := NewOpenAICompatExecutor("openrouter", cfg)
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": upstream.URL + "/api/v1", "api_key": "provider-fixture"}}
	_, err := e.Execute(context.Background(), auth, cliproxyexecutor.Request{Model: "typesafe/jev-1.13", Payload: []byte(`{"model":"typesafe/jev-1.13","state":{"private":"fixture"},"questions":{}}`)}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Alt: "jev-decisions"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenAICompatDecisionsNeverReplaysStateToRedirect(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(200) }))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()
	e := NewOpenAICompatExecutor("openrouter", &config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": upstream.URL + "/api/v1", "api_key": "provider-fixture"}}
	_, err := e.Execute(context.Background(), auth, cliproxyexecutor.Request{Model: "typesafe/jev-1.13", Payload: []byte(`{"state":{"private":"fixture"}}`)}, cliproxyexecutor.Options{Alt: "jev-decisions"})
	if err == nil || called {
		t.Fatal("native state replayed to redirect", err)
	}
}

func TestOpenAICompatDecisionsFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{429, `{"error":"provider-fixture"}`}, {200, `not json`}, {200, `{"choices":[]}`}, {200, strings.Repeat("x", (4<<20)+1)}} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		e := NewOpenAICompatExecutor("openrouter", &config.Config{})
		auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": upstream.URL + "/api/v1", "api_key": "provider-fixture"}}
		_, err := e.Execute(context.Background(), auth, cliproxyexecutor.Request{Model: "typesafe/jev-1.13", Payload: []byte(`{}`)}, cliproxyexecutor.Options{Alt: "jev-decisions"})
		upstream.Close()
		if err == nil || strings.Contains(err.Error(), "provider-fixture") {
			t.Fatal("failure missing or leaked upstream error", err)
		}
	}
	e := NewOpenAICompatExecutor("openrouter", &config.Config{})
	if _, err := e.Execute(context.Background(), nil, cliproxyexecutor.Request{Model: "typesafe/jev-1.13"}, cliproxyexecutor.Options{Alt: "jev-decisions"}); err == nil {
		t.Fatal("missing credential accepted")
	}
}

func TestJevDecisionsPathByProvider(t *testing.T) {
	cases := map[[2]string]string{
		{"api.typesafe.ai", "/v1"}:     "/v1/systemone",
		{"api.typesafe.ai", "/v1/"}:    "/v1/systemone",
		{"openrouter.ai", "/api/v1"}:   "/api/alpha/decisions",
		{"127.0.0.1:9", "/fixture/v1"}: "/fixture/alpha/decisions",
	}
	for in, want := range cases {
		if got := jevDecisionsPath(in[0], in[1]); got != want {
			t.Errorf("jevDecisionsPath(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
	for _, m := range []string{"jev-1.14.0", "jev-latest", "typesafe/jev-1.14"} {
		if !isJevModel(m) {
			t.Errorf("isJevModel(%q) = false", m)
		}
	}
	if isJevModel("gpt-5") {
		t.Error("isJevModel accepted a non-Jev model")
	}
}

// TypeSafe direct speaks the same native contract on /v1/systemone and takes its own model ids (jev-1.14.0).
func TestOpenAICompatDecisionsTypeSafeDirect(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer typesafe-fixture" || gjson.GetBytes(body, "model").String() != "jev-1.14.0" || !gjson.GetBytes(body, "questions.q").Exists() {
			t.Errorf("unexpected upstream request: auth=%q body=%s", r.Header.Get("Authorization"), body)
		}
		_, _ = io.WriteString(w, `{"model":"jev-1.14.0","answers":{"q":{"type":"choice","choice":"yes","confidence":0.9}},"usage":{"input_tokens":2,"output_tokens":1}}`)
	}))
	defer upstream.Close()
	e := NewOpenAICompatExecutor("typesafe", &config.Config{})
	auth := &cliproxyauth.Auth{Provider: "typesafe", Attributes: map[string]string{"base_url": upstream.URL + "/v1", "api_key": "typesafe-fixture"}}
	// The auth manager already resolved the client alias (typesafe/jev-1.14) to the upstream name.
	req := cliproxyexecutor.Request{Model: "jev-1.14.0", Payload: []byte(`{"model":"typesafe/jev-1.14","state":{"fixture":true},"questions":{"q":{"type":"choice","criteria":{"yes":"yes"}}}}`)}
	out, err := e.Execute(context.Background(), auth, req, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Alt: "jev-decisions"})
	if err != nil || !called || !gjson.GetBytes(out.Payload, "answers.q").Exists() {
		t.Fatal(err, string(out.Payload))
	}
}
