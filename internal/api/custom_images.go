package api

// CUSTOM (cpamc-custom): OpenAI-style image generation backed by MiniMax image-01.
// See CUSTOM_FEATURES.md.
//
// POST /v1/images/generations  (client API key)
//   body: {"model": "minimax/image-01", "prompt": "...", "n": 1, "size": "1024x1024",
//          "response_format": "b64_json"}
//   -> {"created": ..., "data": [{"b64_json": "..."}]}
//
// Only MiniMax image models are handled here; every other model falls through to
// the upstream images handler. The MiniMax key comes from the openai-compatibility
// entry (customMinimaxKey) and never leaves the server.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	customImagesURL       = "https://api.minimax.io/v1/image_generation"
	customImagesMaxPrompt = 1500
	customImagesMaxN      = 9
)

type customImagesRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n"`
	Size           string `json:"size"`
	AspectRatio    string `json:"aspect_ratio"`
	ResponseFormat string `json:"response_format"`
	Seed           *int64 `json:"seed"`
	PromptOptim    *bool  `json:"prompt_optimizer"`
}

// customImagesMinimaxModel returns the MiniMax model id for a requested model, or "".
func customImagesMinimaxModel(model string) string {
	base := strings.ToLower(strings.TrimSpace(model))
	if !strings.HasPrefix(base, "minimax/") {
		// Bare "image-01" is MiniMax-only today, but require the prefix so a
		// future upstream model with the same name is never hijacked.
		return ""
	}
	base = strings.TrimSpace(strings.TrimPrefix(base, "minimax/"))
	switch base {
	case "image-01", "image-01-live":
		return base
	}
	return ""
}

// customImagesSize maps an OpenAI size ("WxH") to MiniMax width/height, clamped to
// [512, 2048] and rounded to a multiple of 8. "auto"/empty means MiniMax's 1:1 default.
func customImagesSize(size string) (int, int, bool) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(size)), "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	fit := func(v int) int {
		if v < 512 {
			v = 512
		}
		if v > 2048 {
			v = 2048
		}
		return v / 8 * 8
	}
	return fit(w), fit(h), true
}

func (s *Server) customImagesDispatch(upstream gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := io.ReadAll(c.Request.Body)
		_ = c.Request.Body.Close()
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		if err == nil {
			var peek struct {
				Model string `json:"model"`
			}
			if json.Unmarshal(raw, &peek) == nil && customImagesMinimaxModel(peek.Model) != "" {
				s.customImages(c, raw)
				return
			}
		}
		upstream(c)
	}
}

func (s *Server) customImages(c *gin.Context, raw []byte) {
	var req customImagesRequest
	if err := json.Unmarshal(raw, &req); err != nil || strings.TrimSpace(req.Prompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "prompt is required"}})
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if r := []rune(prompt); len(r) > customImagesMaxPrompt {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": fmt.Sprintf("prompt exceeds %d characters", customImagesMaxPrompt)}})
		return
	}
	key := s.customMinimaxKey()
	if key == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no MiniMax credential configured"}})
		return
	}
	n := req.N
	if n <= 0 {
		n = 1
	}
	if n > customImagesMaxN {
		n = customImagesMaxN
	}
	wantURL := strings.EqualFold(strings.TrimSpace(req.ResponseFormat), "url")
	format := "base64"
	if wantURL {
		format = "url"
	}
	payload := map[string]any{
		"model":           customImagesMinimaxModel(req.Model),
		"prompt":          prompt,
		"n":               n,
		"response_format": format,
	}
	if ar := strings.TrimSpace(req.AspectRatio); ar != "" {
		payload["aspect_ratio"] = ar
	} else if w, h, ok := customImagesSize(req.Size); ok {
		payload["width"] = w
		payload["height"] = h
	}
	if req.Seed != nil {
		payload["seed"] = *req.Seed
	}
	if req.PromptOptim != nil {
		payload["prompt_optimizer"] = *req.PromptOptim
	}
	body, _ := json.Marshal(payload)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 180*time.Second)
	defer cancel()
	up, _ := http.NewRequestWithContext(ctx, http.MethodPost, customImagesURL, bytes.NewReader(body))
	up.Header.Set("Authorization", "Bearer "+key)
	up.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(up)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "image upstream request failed"}})
		return
	}
	defer resp.Body.Close()
	respRaw, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<20))

	out, status, msg := customImagesTranslate(respRaw, resp.StatusCode, wantURL)
	if status != http.StatusOK {
		c.JSON(status, gin.H{"error": gin.H{"message": msg}})
		return
	}
	c.JSON(http.StatusOK, out)
}

// customImagesTranslate converts a MiniMax image_generation response into the
// OpenAI images response shape. Returns (body, httpStatus, errorMessage).
func customImagesTranslate(raw []byte, upstreamStatus int, wantURL bool) (gin.H, int, string) {
	var mm struct {
		Data struct {
			ImageURLs   []string `json:"image_urls"`
			ImageBase64 []string `json:"image_base64"`
		} `json:"data"`
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(raw, &mm); err != nil {
		return nil, http.StatusBadGateway, fmt.Sprintf("image failed: upstream status %d", upstreamStatus)
	}
	if mm.BaseResp.StatusCode != 0 {
		status := http.StatusBadGateway
		switch mm.BaseResp.StatusCode {
		case 1002:
			status = http.StatusTooManyRequests
		case 1026, 2013:
			status = http.StatusBadRequest
		case 1008:
			status = http.StatusPaymentRequired
		}
		return nil, status, fmt.Sprintf("image failed: %s (minimax %d)", mm.BaseResp.StatusMsg, mm.BaseResp.StatusCode)
	}
	items := make([]gin.H, 0, len(mm.Data.ImageBase64)+len(mm.Data.ImageURLs))
	if wantURL {
		for _, u := range mm.Data.ImageURLs {
			items = append(items, gin.H{"url": u})
		}
	} else {
		for _, b := range mm.Data.ImageBase64 {
			items = append(items, gin.H{"b64_json": b})
		}
	}
	if len(items) == 0 {
		return nil, http.StatusBadGateway, "image failed: no images returned (content may have been filtered)"
	}
	return gin.H{"created": time.Now().Unix(), "data": items}, http.StatusOK, ""
}
