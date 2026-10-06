package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
)

const chatGPTEdgeBodyLimit = 1 << 20

type edgeCapture struct {
	mu       sync.Mutex
	encoding string
	wireLen  int
	model    string
	input    string
}

// newChatGPTEdgeMock mimics the ChatGPT edge: an uncompressed body above 1 MiB gets Envoy's
// pre-header 503, a zstd body is decoded and served.
func newChatGPTEdgeMock(t *testing.T, cap *edgeCapture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wire, _ := io.ReadAll(r.Body)
		body := wire
		if r.Header.Get("Content-Encoding") == "zstd" {
			dec, err := zstd.NewReader(nil)
			if err != nil {
				t.Errorf("zstd reader: %v", err)
				return
			}
			defer dec.Close()
			if body, err = dec.DecodeAll(wire, nil); err != nil {
				t.Errorf("decode zstd body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		cap.mu.Lock()
		cap.encoding = r.Header.Get("Content-Encoding")
		cap.wireLen = len(wire)
		cap.model = gjson.GetBytes(body, "model").String()
		cap.input = gjson.GetBytes(body, "input.0.content.0.text").String()
		cap.mu.Unlock()
		if len(wire) > chatGPTEdgeBodyLimit {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("upstream connect error or disconnect/reset before headers. reset reason: remote connection failure"))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","model":"gpt-5.5","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n"))
	}))
}

func largeResponsesPayload(size int) []byte {
	return []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"` +
		strings.Repeat("context line for a long session ", size/32) + `"}]}]}`)
}

// A long Codex session (>1 MiB request) must reach the ChatGPT edge compressed, as native
// Codex sends it; uncompressed it is rejected with a 503 on every retry.
func TestCodexExecutorCompressesLargeResponsesBody(t *testing.T) {
	for _, stream := range []bool{true, false} {
		name := "non-stream"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			cap := &edgeCapture{}
			server := newChatGPTEdgeMock(t, cap)
			defer server.Close()
			exec := NewCodexExecutor(&config.Config{})
			payload := largeResponsesPayload(1100 << 10)
			req := cliproxyexecutor.Request{Model: "gpt-5.5", Payload: payload}
			opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response"), Stream: stream}

			var err error
			if stream {
				var res *cliproxyexecutor.StreamResult
				res, err = exec.ExecuteStream(context.Background(), codexOAuthTestAuth(server.URL), req, opts)
				if err == nil {
					for chunk := range res.Chunks {
						if chunk.Err != nil {
							err = chunk.Err
						}
					}
				}
			} else {
				_, err = exec.Execute(context.Background(), codexOAuthTestAuth(server.URL), req, opts)
			}
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if cap.encoding != "zstd" {
				t.Fatalf("Content-Encoding = %q, want zstd", cap.encoding)
			}
			if cap.wireLen >= len(payload)/2 {
				t.Fatalf("wire body %d bytes, want well below %d", cap.wireLen, len(payload))
			}
			if cap.model != "gpt-5.5" || len(cap.input) < 1<<20 {
				t.Fatalf("decoded body lost content: model=%q input=%d bytes", cap.model, len(cap.input))
			}
		})
	}
}

func TestCodexExecutorSmallAndAPIKeyBodiesStayUncompressed(t *testing.T) {
	cap := &edgeCapture{}
	server := newChatGPTEdgeMock(t, cap)
	defer server.Close()
	exec := NewCodexExecutor(&config.Config{})
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response")}

	small := cliproxyexecutor.Request{Model: "gpt-5.5", Payload: largeResponsesPayload(4 << 10)}
	if _, err := exec.Execute(context.Background(), codexOAuthTestAuth(server.URL), small, opts); err != nil {
		t.Fatalf("small execute: %v", err)
	}
	if cap.encoding != "" {
		t.Fatalf("small body Content-Encoding = %q, want none", cap.encoding)
	}

	large := cliproxyexecutor.Request{Model: "gpt-5.5", Payload: largeResponsesPayload(512 << 10)}
	if _, err := exec.Execute(context.Background(), codexAPIKeyTestAuth(server.URL), large, opts); err != nil {
		t.Fatalf("api key execute: %v", err)
	}
	if cap.encoding != "" {
		t.Fatalf("API key Content-Encoding = %q, want none (native Codex compresses ChatGPT auth only)", cap.encoding)
	}
}

func TestCompressCodexRequestBodyRoundTrips(t *testing.T) {
	body := largeResponsesPayload(300 << 10)
	req := httptest.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	if !helps.CompressCodexRequestBody(req, body) {
		t.Fatal("expected compression")
	}
	read := func() []byte {
		rc, err := req.GetBody()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		return b
	}
	dec, _ := zstd.NewReader(nil)
	defer dec.Close()
	for i := 0; i < 2; i++ { // GetBody must be replayable for retries/redirects
		got, err := dec.DecodeAll(read(), nil)
		if err != nil || string(got) != string(body) {
			t.Fatalf("round %d: round trip mismatch (err=%v)", i, err)
		}
	}
	if req.ContentLength >= int64(len(body)) {
		t.Fatalf("ContentLength %d not reduced from %d", req.ContentLength, len(body))
	}
	if helps.CompressCodexRequestBody(req, body) {
		t.Fatal("already-encoded request was compressed twice")
	}
}

func TestCustomCodexLargeRequestUsesWebsocket(t *testing.T) {
	big := largeResponsesPayload(helps.CodexHTTPWebsocketThreshold + 1024)
	small := largeResponsesPayload(512 << 10)
	oauth := codexOAuthTestAuth("https://chatgpt.com/backend-api/codex")
	apiKey := codexAPIKeyTestAuth("https://api.openai.com/v1")
	resp := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response")}
	cases := []struct {
		name string
		got  bool
		want bool
	}{
		{"oauth large", customCodexLargeRequestUsesWebsocket(oauth, cliproxyexecutor.Request{Payload: big}, resp), true},
		{"oauth large original request", customCodexLargeRequestUsesWebsocket(oauth, cliproxyexecutor.Request{Payload: small}, cliproxyexecutor.Options{OriginalRequest: big}), true},
		{"oauth small", customCodexLargeRequestUsesWebsocket(oauth, cliproxyexecutor.Request{Payload: small}, resp), false},
		{"api key large", customCodexLargeRequestUsesWebsocket(apiKey, cliproxyexecutor.Request{Payload: big}, resp), false},
		{"compact large", customCodexLargeRequestUsesWebsocket(oauth, cliproxyexecutor.Request{Payload: big}, cliproxyexecutor.Options{Alt: "responses/compact"}), false},
		{"nil auth", customCodexLargeRequestUsesWebsocket(nil, cliproxyexecutor.Request{Payload: big}, resp), false},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// A >1.5 MiB session routed to the auto executor over HTTP must reach the edge as a WebSocket
// response.create, which carries no 1 MiB body limit.
func TestCodexAutoExecutorSendsLargeRequestOverWebsocket(t *testing.T) {
	var gotHTTPPost, gotWSFrame bool
	var frameLen int
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			gotHTTPPost = true
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		gotWSFrame = gjson.GetBytes(msg, "type").String() == "response.create"
		frameLen = len(msg)
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","model":"gpt-5.5","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`))
	}))
	defer server.Close()

	exec := NewCodexAutoExecutor(&config.Config{})
	payload := largeResponsesPayload(helps.CodexHTTPWebsocketThreshold + 64<<10)
	res, err := exec.ExecuteStream(context.Background(), codexOAuthTestAuth(server.URL),
		cliproxyexecutor.Request{Model: "gpt-5.5", Payload: payload},
		cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response"), Stream: true})
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	for chunk := range res.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if gotHTTPPost || !gotWSFrame {
		t.Fatalf("http post=%v ws frame=%v, want WebSocket only", gotHTTPPost, gotWSFrame)
	}
	if frameLen < len(payload)/2 {
		t.Fatalf("frame %d bytes, payload not forwarded", frameLen)
	}
}
