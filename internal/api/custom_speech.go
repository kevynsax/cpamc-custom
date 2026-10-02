package api

// CUSTOM (cpamc-custom): OpenAI-style text-to-speech backed by MiniMax T2A.
// See CUSTOM_FEATURES.md.
//
// POST /v1/audio/speech  (client API key, same auth as /v1/chat/completions)
//   body: {"input": "...", "voice": "English_expressive_narrator", "speed": 1.0,
//          "model": "speech-2.8-hd", "response_format": "mp3"}
//   -> audio bytes (audio/mpeg)
//
// The MiniMax subscription key comes from the openai-compatibility entry whose
// base URL is MiniMax; it never leaves the server.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const (
	customSpeechURL          = "https://api.minimax.io/v1/t2a_v2"
	customSpeechDefaultModel = "speech-2.8-hd"
	customSpeechDefaultVoice = "English_expressive_narrator"
	customSpeechMaxChars     = 10000
)

type customSpeechRequest struct {
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	Model          string  `json:"model"`
	Speed          float64 `json:"speed"`
	ResponseFormat string  `json:"response_format"`
	LanguageBoost  string  `json:"language_boost"`
}

func (s *Server) customMinimaxKey() string {
	if s == nil || s.handlers == nil || s.handlers.AuthManager == nil {
		return ""
	}
	for _, a := range s.handlers.AuthManager.List() {
		if a == nil || a.Disabled || a.Status == coreauth.StatusDisabled || a.Attributes == nil {
			continue
		}
		base := strings.ToLower(a.Attributes["base_url"])
		if strings.Contains(base, "minimax.io") || strings.EqualFold(a.Attributes["compat_name"], "minimax") {
			if key := strings.TrimSpace(a.Attributes["api_key"]); key != "" {
				return key
			}
		}
	}
	return ""
}

func (s *Server) customSpeech(c *gin.Context) {
	var req customSpeechRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Input) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "input is required"}})
		return
	}
	text := strings.TrimSpace(req.Input)
	if r := []rune(text); len(r) > customSpeechMaxChars {
		text = string(r[:customSpeechMaxChars])
	}
	key := s.customMinimaxKey()
	if key == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no MiniMax credential configured"}})
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" || strings.HasPrefix(model, "tts-") || strings.HasPrefix(model, "gpt-") {
		model = customSpeechDefaultModel
	}
	voice := strings.TrimSpace(req.Voice)
	if voice == "" {
		voice = customSpeechDefaultVoice
	}
	speed := req.Speed
	if speed <= 0 {
		speed = 1
	}
	format := strings.ToLower(strings.TrimSpace(req.ResponseFormat))
	if format != "wav" && format != "flac" && format != "pcm" {
		format = "mp3"
	}
	payload := map[string]any{
		"model":         model,
		"text":          text,
		"stream":        false,
		"output_format": "hex",
		"voice_setting": map[string]any{"voice_id": voice, "speed": speed, "vol": 1, "pitch": 0},
		"audio_setting": map[string]any{"sample_rate": 32000, "bitrate": 128000, "format": format, "channel": 1},
	}
	if lb := strings.TrimSpace(req.LanguageBoost); lb != "" {
		payload["language_boost"] = lb
	} else {
		payload["language_boost"] = "auto"
	}
	body, _ := json.Marshal(payload)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	up, _ := http.NewRequestWithContext(ctx, http.MethodPost, customSpeechURL, bytes.NewReader(body))
	up.Header.Set("Authorization", "Bearer "+key)
	up.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(up)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "speech upstream request failed"}})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	var out struct {
		Data struct {
			Audio string `json:"audio"`
		} `json:"data"`
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if json.Unmarshal(raw, &out) != nil || out.BaseResp.StatusCode != 0 || out.Data.Audio == "" {
		msg := out.BaseResp.StatusMsg
		if msg == "" {
			msg = fmt.Sprintf("upstream status %d", resp.StatusCode)
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "speech failed: " + msg}})
		return
	}
	audio, err := hex.DecodeString(out.Data.Audio)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "invalid audio payload"}})
		return
	}
	contentType := map[string]string{"mp3": "audio/mpeg", "wav": "audio/wav", "flac": "audio/flac", "pcm": "audio/pcm"}[format]
	c.Data(http.StatusOK, contentType, audio)
}
