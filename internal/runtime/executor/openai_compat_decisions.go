package executor

// CUSTOM (cpamc-custom): Jev is a native decisions API, not a chat prompt.
// Keep the auth manager's routing, credential rotation, proxy and usage pipeline.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/tidwall/sjson"
)

func (e *OpenAICompatExecutor) executeDecisions(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	model := thinking.ParseSuffix(req.Model).ModelName
	reporter := helps.NewExecutorUsageReporter(ctx, e, model, auth)
	defer reporter.TrackFailure(ctx, &err)
	base, key := e.resolveCredentials(auth)
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/v1") || key == "" || !strings.HasPrefix(model, "typesafe/jev-") {
		return resp, statusErr{code: http.StatusServiceUnavailable, msg: "native Jev requires a configured OpenRouter-compatible provider and credential"}
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/v1") + "/alpha/decisions"
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	payload, err := sjson.SetBytes(req.Payload, "model", model)
	if err != nil {
		return resp, statusErr{code: 400, msg: "invalid decisions payload"}
	}
	// Final semantic barrier: nothing may restore or overwrite configured payload fields.
	payload = helps.ApplyPayloadConfigWithRequest(e.cfg, model, "openai", opts.SourceFormat.String(), "", payload, opts.OriginalRequest, helps.PayloadRequestedModel(opts, req.Model), helps.PayloadRequestPath(opts), opts.Headers)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return resp, err
	}
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(request, attrs, opts.Headers)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	client := reporter.TrackHTTPClient(helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0))
	// Native state may hold person data: never replay it to a redirect target.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	upstream, err := client.Do(request)
	if err != nil {
		return resp, statusErr{code: 502, msg: "decisions upstream unreachable"}
	}
	defer func() { _ = upstream.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(upstream.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return resp, statusErr{code: 502, msg: "invalid decisions upstream response"}
	}
	if upstream.StatusCode >= 300 && upstream.StatusCode < 400 {
		return resp, statusErr{code: http.StatusBadGateway, msg: "decisions upstream redirect refused"}
	}
	if upstream.StatusCode < 200 || upstream.StatusCode >= 300 {
		return resp, newOpenAICompatStatusError(upstream.StatusCode, upstream.Header, []byte(`{"error":{"message":"decisions upstream failed"}}`))
	}
	var parsed struct {
		Answers map[string]json.RawMessage `json:"answers"`
	}
	if json.Unmarshal(body, &parsed) != nil || parsed.Answers == nil {
		return resp, statusErr{code: 502, msg: "invalid decisions upstream response"}
	}
	reporter.Publish(ctx, helps.ParseOpenAIUsage(body))
	return cliproxyexecutor.Response{Payload: body, Headers: upstream.Header.Clone()}, nil
}
