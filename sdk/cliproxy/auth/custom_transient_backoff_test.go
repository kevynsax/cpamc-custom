package auth

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

const envoyPreHeaderFailure = "upstream connect error or disconnect/reset before headers. reset reason: remote connection failure"

func TestCustomTransientRetryWait(t *testing.T) {
	prev := customTransientRetryBackoffBase.Load()
	SetCustomTransientRetryBackoffBase(time.Second)
	t.Cleanup(func() { customTransientRetryBackoffBase.Store(prev) })

	envoy := &Error{HTTPStatus: http.StatusServiceUnavailable, Message: envoyPreHeaderFailure}
	cases := []struct {
		name    string
		err     error
		attempt int
		wait    time.Duration
		maxWait time.Duration
		want    time.Duration
	}{
		{name: "first round", err: envoy, attempt: 0, maxWait: 30 * time.Second, want: time.Second},
		{name: "doubles", err: envoy, attempt: 3, maxWait: 30 * time.Second, want: 8 * time.Second},
		{name: "capped per round", err: envoy, attempt: 10, maxWait: 30 * time.Second, want: 8 * time.Second},
		{name: "capped by max wait", err: envoy, attempt: 10, maxWait: 3 * time.Second, want: 3 * time.Second},
		{name: "cooldown wait kept", err: envoy, attempt: 0, wait: 5 * time.Second, maxWait: 30 * time.Second, want: 5 * time.Second},
		{name: "no waiting configured", err: envoy, attempt: 0, maxWait: 0, want: 0},
		{name: "bad gateway", err: &Error{HTTPStatus: http.StatusBadGateway}, attempt: 1, maxWait: 30 * time.Second, want: 2 * time.Second},
		{name: "transport", err: dialRefusedError(), attempt: 0, maxWait: 30 * time.Second, want: time.Second},
		{name: "quota not delayed", err: &Error{HTTPStatus: http.StatusTooManyRequests}, attempt: 0, maxWait: 30 * time.Second, want: 0},
		{name: "forbidden not delayed", err: &Error{HTTPStatus: http.StatusForbidden}, attempt: 0, maxWait: 30 * time.Second, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := customTransientRetryWait(tc.err, tc.attempt, tc.wait, tc.maxWait); got != tc.want {
				t.Fatalf("customTransientRetryWait() = %v, want %v", got, tc.want)
			}
		})
	}

	SetCustomTransientRetryBackoffBase(0)
	if got := customTransientRetryWait(envoy, 2, 0, 30*time.Second); got != 0 {
		t.Fatalf("disabled backoff wait = %v, want 0", got)
	}
}

// The ChatGPT edge's Envoy 503 must not retry instantly: every request-retry round used to
// fire within milliseconds, so a few seconds of edge trouble failed the client request.
func TestShouldRetryAfterError_BacksOffEnvoyPreHeaderFailure(t *testing.T) {
	prev := customTransientRetryBackoffBase.Load()
	SetCustomTransientRetryBackoffBase(time.Second)
	t.Cleanup(func() { customTransientRetryBackoffBase.Store(prev) })

	m := NewManager(nil, nil, nil)
	m.SetRetryConfig(3, 30*time.Second, 0)
	model := "gpt-backoff-" + uuid.NewString()
	authID := "backoff-" + uuid.NewString()
	registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
	if _, errRegister := m.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	envoy := &Error{HTTPStatus: http.StatusServiceUnavailable, Message: envoyPreHeaderFailure}
	for attempt, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		wait, retry := m.shouldRetryAfterError(envoy, attempt, []string{"codex"}, model, 30*time.Second)
		if !retry || wait != want {
			t.Fatalf("attempt %d: shouldRetryAfterError() = (%v, %t), want (%v, true)", attempt, wait, retry, want)
		}
	}
	if _, retry := m.shouldRetryAfterError(envoy, 3, []string{"codex"}, model, 30*time.Second); retry {
		t.Fatal("retried beyond request-retry")
	}
}

type flakyThenOKExecutor struct {
	mu       sync.Mutex
	failures int
	calls    []time.Time
}

func (e *flakyThenOKExecutor) Identifier() string { return "codex" }

func (e *flakyThenOKExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, time.Now())
	if len(e.calls) <= e.failures {
		return cliproxyexecutor.Response{}, &Error{HTTPStatus: http.StatusServiceUnavailable, Message: envoyPreHeaderFailure}
	}
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}

func (e *flakyThenOKExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, &Error{HTTPStatus: http.StatusNotImplemented, Message: "not implemented"}
}

func (e *flakyThenOKExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) { return auth, nil }

func (e *flakyThenOKExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &Error{HTTPStatus: http.StatusNotImplemented, Message: "not implemented"}
}

func (e *flakyThenOKExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, &Error{HTTPStatus: http.StatusNotImplemented, Message: "not implemented"}
}

// A single credential behind a short edge outage recovers because the retry rounds are spread
// out, and the credential is not cooled by the transport-neutral failure.
func TestExecute_RecoversAcrossShortEdgeOutageWithBackoff(t *testing.T) {
	prev := customTransientRetryBackoffBase.Load()
	SetCustomTransientRetryBackoffBase(20 * time.Millisecond)
	t.Cleanup(func() { customTransientRetryBackoffBase.Store(prev) })

	m := NewManager(nil, nil, nil)
	m.SetRetryConfig(3, 30*time.Second, 0)
	exec := &flakyThenOKExecutor{failures: 3}
	m.RegisterExecutor(exec)
	model := "gpt-outage-" + uuid.NewString()
	authID := "outage-" + uuid.NewString()
	registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
	if _, errRegister := m.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	resp, err := m.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("Execute() error = %v, want recovery after backoff", err)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("payload = %q", resp.Payload)
	}
	exec.mu.Lock()
	calls := append([]time.Time(nil), exec.calls...)
	exec.mu.Unlock()
	if len(calls) != 4 {
		t.Fatalf("calls = %d, want 4", len(calls))
	}
	// Rounds wait at least 20ms, 40ms, 80ms (jitter only adds).
	for i, minGap := range []time.Duration{20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond} {
		if gap := calls[i+1].Sub(calls[i]); gap < minGap {
			t.Fatalf("gap before round %d = %v, want >= %v", i+1, gap, minGap)
		}
	}
	assertNoCooldown(t, m, authID, model)
}
