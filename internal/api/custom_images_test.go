package api

// CUSTOM (cpamc-custom): tests for the MiniMax image-generation route.

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCustomImagesMinimaxModel(t *testing.T) {
	cases := map[string]string{
		"minimax/image-01":      "image-01",
		"MiniMax/Image-01":      "image-01",
		"minimax/image-01-live": "image-01-live",
		"image-01":              "",
		"gpt-image-2.5":         "",
		"minimax/MiniMax-M3":    "",
		"":                      "",
	}
	for in, want := range cases {
		if got := customImagesMinimaxModel(in); got != want {
			t.Errorf("customImagesMinimaxModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCustomImagesSize(t *testing.T) {
	cases := []struct {
		in   string
		w, h int
		ok   bool
	}{
		{"1024x1024", 1024, 1024, true},
		{"1536x1024", 1536, 1024, true},
		{"256x256", 512, 512, true},
		{"4096x1001", 2048, 1000, true},
		{"auto", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, tc := range cases {
		w, h, ok := customImagesSize(tc.in)
		if w != tc.w || h != tc.h || ok != tc.ok {
			t.Errorf("customImagesSize(%q) = %d,%d,%v want %d,%d,%v", tc.in, w, h, ok, tc.w, tc.h, tc.ok)
		}
	}
}

func TestCustomImagesTranslate(t *testing.T) {
	ok := []byte(`{"data":{"image_base64":["QUJD"]},"base_resp":{"status_code":0,"status_msg":"success"}}`)
	out, status, _ := customImagesTranslate(ok, 200, false)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	items := out["data"].([]gin.H)
	if len(items) != 1 || items[0]["b64_json"] != "QUJD" {
		t.Fatalf("unexpected data: %#v", items)
	}

	urls := []byte(`{"data":{"image_urls":["https://x/y.jpeg"]},"base_resp":{"status_code":0}}`)
	out, status, _ = customImagesTranslate(urls, 200, true)
	if status != http.StatusOK || out["data"].([]gin.H)[0]["url"] != "https://x/y.jpeg" {
		t.Fatalf("url translate failed: %d %#v", status, out)
	}

	sensitive := []byte(`{"base_resp":{"status_code":1026,"status_msg":"sensitive"}}`)
	if _, status, msg := customImagesTranslate(sensitive, 200, false); status != http.StatusBadRequest || msg == "" {
		t.Fatalf("sensitive: status=%d msg=%q", status, msg)
	}
	rate := []byte(`{"base_resp":{"status_code":1002,"status_msg":"rate"}}`)
	if _, status, _ := customImagesTranslate(rate, 200, false); status != http.StatusTooManyRequests {
		t.Fatalf("rate: status=%d", status)
	}
	empty := []byte(`{"data":{},"base_resp":{"status_code":0}}`)
	if _, status, _ := customImagesTranslate(empty, 200, false); status != http.StatusBadGateway {
		t.Fatalf("empty: status=%d", status)
	}
	if _, status, _ := customImagesTranslate([]byte("not json"), 500, false); status != http.StatusBadGateway {
		t.Fatalf("garbage: status=%d", status)
	}
}

func TestCustomImagesDispatchPassesOtherModelsUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{}
	var upstreamBody []byte
	upstream := func(c *gin.Context) {
		upstreamBody, _ = io.ReadAll(c.Request.Body)
		c.Status(http.StatusTeapot)
	}
	r := gin.New()
	r.POST("/v1/images/generations", s.customImagesDispatch(upstream))

	body := []byte(`{"model":"gpt-image-2.5","prompt":"apple"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body)))
	if w.Code != http.StatusTeapot {
		t.Fatalf("expected upstream handler, got %d", w.Code)
	}
	if !bytes.Equal(upstreamBody, body) {
		t.Fatalf("upstream body not preserved: %s", upstreamBody)
	}

	// MiniMax model with no credential configured: handled locally, 503.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader([]byte(`{"model":"minimax/image-01","prompt":"apple"}`))))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without MiniMax key, got %d: %s", w.Code, w.Body.String())
	}

	// Missing prompt: 400 before any credential lookup.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader([]byte(`{"model":"minimax/image-01"}`))))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without prompt, got %d", w.Code)
	}
}
