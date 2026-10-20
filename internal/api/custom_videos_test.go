package api

// CUSTOM (cpamc-custom): tests for the MiniMax Hailuo video route.

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCustomVideosMinimaxModel(t *testing.T) {
	cases := []struct {
		in, mode, want string
	}{
		{"minimax/MiniMax-Hailuo-2.3", "t2v", "MiniMax-Hailuo-2.3"},
		{"minimax/hailuo", "t2v", "MiniMax-Hailuo-2.3"},
		{"minimax/hailuo-2.3-fast", "i2v", "MiniMax-Hailuo-2.3-Fast"},
		{"minimax/hailuo-2.3-fast", "t2v", ""},
		{"minimax/I2V-01", "i2v", "I2V-01"},
		{"minimax/I2V-01", "t2v", ""},
		{"minimax/T2V-01", "t2v", "T2V-01"},
		{"MiniMax-Hailuo-2.3", "t2v", ""},
		{"grok-imagine-video", "t2v", ""},
		{"", "t2v", ""},
	}
	for _, tc := range cases {
		if got := customVideosMinimaxModel(tc.in, tc.mode); got != tc.want {
			t.Errorf("customVideosMinimaxModel(%q,%q)=%q want %q", tc.in, tc.mode, got, tc.want)
		}
	}
}

func TestCustomVideosSecondsAndResolution(t *testing.T) {
	if s, n, err := customVideosSeconds(""); err != nil || s != "6" || n != 6 {
		t.Fatalf("default seconds: %q %d %v", s, n, err)
	}
	if _, _, err := customVideosSeconds("4"); err == nil {
		t.Fatal("expected error for seconds=4")
	}
	if got := customVideosResolution("", "1280x720"); got != "768P" {
		t.Fatalf("resolution from size: %s", got)
	}
	if got := customVideosResolution("1080P", ""); got != "1080P" {
		t.Fatalf("explicit resolution: %s", got)
	}
}

func TestCustomVideosFirstFrame(t *testing.T) {
	raw := []byte(`{"model":"minimax/hailuo","prompt":"x","input_reference":{"image_url":"data:image/png;base64,AA=="}}`)
	if got := customVideosFirstFrame(raw); got != "data:image/png;base64,AA==" {
		t.Fatalf("input_reference: %q", got)
	}
	raw = []byte(`{"first_frame_image":"https://x/y.png"}`)
	if got := customVideosFirstFrame(raw); got != "https://x/y.png" {
		t.Fatalf("first_frame_image: %q", got)
	}
}

func TestCustomVideosOpenAIStatus(t *testing.T) {
	if customVideosOpenAIStatus("Queueing") != "queued" {
		t.Fatal("Queueing")
	}
	if customVideosOpenAIStatus("Processing") != "in_progress" {
		t.Fatal("Processing")
	}
	if customVideosOpenAIStatus("Success") != "completed" {
		t.Fatal("Success")
	}
	if customVideosOpenAIStatus("Fail") != "failed" {
		t.Fatal("Fail")
	}
}

func TestCustomVideosLooksLikeMinimaxTaskID(t *testing.T) {
	if !customVideosLooksLikeMinimaxTaskID("106916112212032") {
		t.Fatal("numeric task id")
	}
	if customVideosLooksLikeMinimaxTaskID("d1eaf469-6b13-9581-9959-f08eb0dc8d08") {
		t.Fatal("uuid should not look like minimax")
	}
	if customVideosLooksLikeMinimaxTaskID("video_abc") {
		t.Fatal("prefixed id")
	}
}

func TestCustomVideosCreateDispatchPassesOtherModelsUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{}
	var upstreamBody []byte
	upstream := func(c *gin.Context) {
		upstreamBody, _ = io.ReadAll(c.Request.Body)
		c.Status(http.StatusTeapot)
	}
	r := gin.New()
	r.POST("/v1/videos", s.customVideosCreateDispatch(upstream))

	body := []byte(`{"model":"grok-imagine-video","prompt":"cat"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body)))
	if w.Code != http.StatusTeapot {
		t.Fatalf("expected upstream, got %d", w.Code)
	}
	if !bytes.Equal(upstreamBody, body) {
		t.Fatalf("body not preserved: %s", upstreamBody)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader([]byte(`{"model":"minimax/hailuo","prompt":"cat"}`))))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without MiniMax key, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader([]byte(`{"model":"minimax/hailuo"}`))))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without prompt, got %d", w.Code)
	}
}

func TestCustomVideosResponseShape(t *testing.T) {
	job := &customVideoJob{
		TaskID:    "1234567890",
		Model:     "minimax/MiniMax-Hailuo-2.3",
		Prompt:    "cat",
		Seconds:   "6",
		Size:      "1280x720",
		CreatedAt: 1,
		Status:    "Processing",
	}
	out := customVideosResponse(job)
	if out["status"] != "in_progress" || out["progress"] != 50 || out["id"] != "1234567890" {
		t.Fatalf("unexpected response: %#v", out)
	}
}
