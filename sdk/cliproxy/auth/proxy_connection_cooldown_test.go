package auth

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

const proxyConnectionFailure = "upstream connect error or disconnect/reset before headers. reset reason: remote connection failure"

func TestProxyConnectionFailureCooldown(t *testing.T) {
	withQuotaCooldownEnabled(t)
	previous := transientErrorCooldownSeconds.Load()
	SetTransientErrorCooldownSeconds(5)
	t.Cleanup(func() { transientErrorCooldownSeconds.Store(previous) })

	for _, tc := range []struct {
		name    string
		status  int
		message string
		code    string
		cool    bool
	}{
		{name: "plain reset", status: 503, message: proxyConnectionFailure},
		{name: "plain timeout", status: 503, message: "upstream connect error or disconnect/reset before headers. reset reason: connection timeout"},
		{name: "JSON reset", status: 503, message: `{"error":{"message":"` + proxyConnectionFailure + `","type":"server_error","code":"internal_server_error"}}`},
		{name: "JSON top level", status: 503, message: `{"message":"` + proxyConnectionFailure + `"}`},
		{name: "unauthorized", status: 401, message: proxyConnectionFailure, cool: true},
		{name: "quota", status: 429, message: proxyConnectionFailure, cool: true},
		{name: "provider internal error", status: 500, message: proxyConnectionFailure, cool: true},
		{name: "generic unavailable", status: 503, message: "Service unavailable", cool: true},
		{name: "quoted connection text", status: 503, message: `{"error":{"message":"provider failure quoting: ` + proxyConnectionFailure + `"}}`, cool: true},
		{name: "forced cooldown", status: 503, message: proxyConnectionFailure, code: ErrorCodeForceCooldown, cool: true},
	} {
		for _, model := range []string{"", "gpt-6.1-sol"} {
			t.Run(tc.name+"/"+model, func(t *testing.T) {
				manager := NewManager(nil, nil, nil)
				authID := "proxy-connection-" + t.Name()
				if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "codex", Status: StatusActive}); errRegister != nil {
					t.Fatal(errRegister)
				}
				err := &statusBearingError{status: tc.status, msg: tc.message}
				resultErr := resultErrorFromError(err)
				if resultErr.HTTPStatus != tc.status || resultErr.Message != tc.message {
					t.Fatal("upstream error status or body lost")
				}
				if !tc.cool && (resultErr.Code != transientTransportErrorCode || isRequestInvalidError(err)) {
					t.Fatalf("connection failure must permit credential fallback: %+v", resultErr)
				}
				if tc.code != "" {
					resultErr.Code = tc.code
				}
				manager.MarkResult(context.Background(), Result{AuthID: authID, Provider: "codex", Model: model, Error: resultErr})
				if !tc.cool {
					assertNoCooldown(t, manager, authID, model)
					// Direct results must use the same classification as converted errors.
					manager.MarkResult(context.Background(), Result{AuthID: authID, Provider: "codex", Model: model, Error: &Error{HTTPStatus: tc.status, Message: tc.message}})
					assertNoCooldown(t, manager, authID, model)
					return
				}
				updated, _ := manager.GetByID(authID)
				next := updated.NextRetryAfter
				if model != "" {
					if state := updated.ModelStates[model]; state != nil {
						next = state.NextRetryAfter
					}
				}
				if !next.After(time.Now()) {
					t.Fatalf("expected real provider failure to retain cooldown: %+v", updated)
				}
			})
		}
	}
}

func TestProxyConnectionFailureRetriesWithoutCooldown(t *testing.T) {
	withQuotaCooldownEnabled(t)
	for _, retry := range []int{0, 1} {
		for _, path := range []string{"execute", "stream", "count"} {
			t.Run(fmt.Sprintf("retry=%d/%s", retry, path), func(t *testing.T) {
				manager := NewManager(nil, nil, nil)
				manager.SetRetryConfig(retry, 0, 0)
				executor := &proxyConnectionTestExecutor{transportThenSuccessExecutor: transportThenSuccessExecutor{identifier: "codex", fail: &statusBearingError{status: http.StatusServiceUnavailable, msg: proxyConnectionFailure}}}
				manager.RegisterExecutor(executor)
				model, authID := "gpt-6.1-sol", "proxy-retry-"+t.Name()
				registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
				if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "codex", Status: StatusActive}); errRegister != nil {
					t.Fatal(errRegister)
				}
				execute := func() error {
					req, opts := cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{}
					switch path {
					case "stream":
						result, errStream := manager.ExecuteStream(context.Background(), []string{"codex"}, req, opts)
						if errStream != nil {
							return errStream
						}
						for chunk := range result.Chunks {
							if chunk.Err != nil {
								return chunk.Err
							}
						}
						return nil
					case "count":
						_, errCount := manager.ExecuteCount(context.Background(), []string{"codex"}, req, opts)
						return errCount
					default:
						_, errExecute := manager.Execute(context.Background(), []string{"codex"}, req, opts)
						return errExecute
					}
				}
				errExecute := execute()
				if retry == 0 {
					if errExecute == nil || statusCodeFromError(errExecute) != http.StatusServiceUnavailable || executor.callCount() != 1 {
						t.Fatalf("expected original 503 after one attempt: %v; calls=%d", errExecute, executor.callCount())
					}
					assertNoCooldown(t, manager, authID, model)
					errExecute = execute()
				}
				if errExecute != nil || executor.callCount() != 2 {
					t.Fatalf("expected recovery on second attempt: %v; calls=%d", errExecute, executor.callCount())
				}
				assertNoCooldown(t, manager, authID, model)
			})
		}
	}
}

type proxyConnectionTestExecutor struct {
	transportThenSuccessExecutor
}

func (e *proxyConnectionTestExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	if e.recordCall() == 1 {
		return nil, e.fail
	}
	chunks := make(chan cliproxyexecutor.StreamChunk, 1)
	chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("data: ok\n\n")}
	close(chunks)
	return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
}
