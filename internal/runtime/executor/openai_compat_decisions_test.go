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
