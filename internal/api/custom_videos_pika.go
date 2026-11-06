package api

// CUSTOM (cpamc-custom): Pika H3 Flex image/text-to-video on the Spark ComfyUI.
// See CUSTOM_FEATURES.md.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const (
	customPikaModel         = "pika-h3-flex"
	customPikaIDPrefix      = "pika_"
	customPikaMaxPrompt     = 8000
	customPikaMaxImageBytes = 20 << 20
	customPikaMaxVideoBytes = 512 << 20
	customPikaDefaultSec    = 5
	customPikaMinSec        = 5
	customPikaMaxSec        = 15
	customPikaFPS           = 24
	customPikaSigmas        = "1.0, 0.972973, 0.923077, 0.8, 0.571429, 0.333333, 0.0"
	customPikaJobTTL        = 24 * time.Hour
)

type customPikaJob struct {
	ID        string
	PromptID  string
	Prompt    string
	Seconds   string
	Size      string
	Width     int
	Height    int
	CreatedAt int64
	Status    string
	Filename  string
	Subfolder string
	FileType  string
	ErrMsg    string
}

var customPikaJobs = struct {
	sync.RWMutex
	byID map[string]*customPikaJob
}{byID: make(map[string]*customPikaJob)}

func customPikaJobPut(job *customPikaJob) {
	customPikaJobs.Lock()
	defer customPikaJobs.Unlock()
	cutoff := time.Now().Add(-customPikaJobTTL).Unix()
	for id, j := range customPikaJobs.byID {
		if j.CreatedAt < cutoff {
			delete(customPikaJobs.byID, id)
		}
	}
	customPikaJobs.byID[job.ID] = job
}

func customPikaJobGet(id string) *customPikaJob {
	customPikaJobs.RLock()
	defer customPikaJobs.RUnlock()
	return customPikaJobs.byID[id]
}

func customPikaIsModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	for _, prefix := range []string{"spark/", "pika/"} {
		m = strings.TrimPrefix(m, prefix)
	}
	return m == customPikaModel || m == "pika" || m == "pika-flex"
}

func customPikaIsID(id string) bool {
	if !strings.HasPrefix(id, customPikaIDPrefix) {
		return false
	}
	rest := id[len(customPikaIDPrefix):]
	if len(rest) < 36 {
		return false
	}
	for _, r := range rest {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r == '-') {
			return false
		}
	}
	return true
}

func customPikaNewID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	h := hex.EncodeToString(buf)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}

// customPikaBaseURL returns the llama-swap upstream root for pika-h3-flex on the Spark,
// taken from the openai-compatibility entry whose prefix is "spark".
func (s *Server) customPikaBaseURL() string {
	if s == nil || s.handlers == nil || s.handlers.AuthManager == nil {
		return ""
	}
	for _, a := range s.handlers.AuthManager.List() {
		if a == nil || a.Disabled || a.Status == coreauth.StatusDisabled || a.Attributes == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(a.Prefix), "spark") && !strings.EqualFold(a.Attributes["compat_name"], "spark") {
			continue
		}
		base := strings.TrimRight(strings.TrimSpace(a.Attributes["base_url"]), "/")
		if base == "" {
			continue
		}
		base = strings.TrimSuffix(base, "/v1")
		return base + "/upstream/" + customPikaModel
	}
	return ""
}

func customPikaSeconds(raw any) (string, int, error) {
	n := customPikaDefaultSec
	switch v := raw.(type) {
	case nil:
	case string:
		if strings.TrimSpace(v) != "" {
			parsed, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return "", 0, fmt.Errorf("invalid_seconds")
			}
			n = parsed
		}
	case float64:
		n = int(v)
	default:
		return "", 0, fmt.Errorf("invalid_seconds")
	}
	if n < customPikaMinSec || n > customPikaMaxSec {
		return "", 0, fmt.Errorf("invalid_seconds")
	}
	return strconv.Itoa(n), n, nil
}

// Frame count on the model's 17k+5 grid, matching the official ComfyUI template.
func customPikaFrames(seconds int) int {
	f := seconds * customPikaFPS
	if f < 5 {
		f = 5
	}
	return f + (5-(f%17)+17)%17
}

func customPikaDimensions(size, resolution string) (int, int, string) {
	w, h := 1344, 768
	size = strings.ToLower(strings.TrimSpace(size))
	if parts := strings.Split(size, "x"); len(parts) == 2 {
		pw, errW := strconv.Atoi(parts[0])
		ph, errH := strconv.Atoi(parts[1])
		if errW == nil && errH == nil && pw > 0 && ph > 0 {
			w, h = pw, ph
		}
	}
	long := 1344
	if strings.EqualFold(strings.TrimSpace(resolution), "480p") {
		long = 832
	}
	if w >= h {
		h = h * long / w
		w = long
	} else {
		w = w * long / h
		h = long
	}
	w = (w + 16) / 32 * 32
	h = (h + 16) / 32 * 32
	return w, h, fmt.Sprintf("%dx%d", w, h)
}

func customPikaDecodeImage(ref string) ([]byte, string, error) {
	if !strings.HasPrefix(ref, "data:") {
		return nil, "", fmt.Errorf("first_frame_must_be_data_url")
	}
	comma := strings.Index(ref, ",")
	if comma < 0 || !strings.Contains(ref[:comma], ";base64") {
		return nil, "", fmt.Errorf("first_frame_must_be_data_url")
	}
	mime := strings.TrimPrefix(strings.SplitN(ref[:comma], ";", 2)[0], "data:")
	ext := ""
	switch mime {
	case "image/png":
		ext = "png"
	case "image/jpeg", "image/jpg":
		ext = "jpg"
	case "image/webp":
		ext = "webp"
	default:
		return nil, "", fmt.Errorf("unsupported_first_frame_type")
	}
	data, err := base64.StdEncoding.DecodeString(ref[comma+1:])
	if err != nil || len(data) == 0 {
		return nil, "", fmt.Errorf("invalid_first_frame")
	}
	if len(data) > customPikaMaxImageBytes {
		return nil, "", fmt.Errorf("first_frame_too_large")
	}
	return data, ext, nil
}

func customPikaWorkflow(prompt, image, filenamePrefix string, width, height, frames int, seed int64) map[string]any {
	i2v := map[string]any{
		"clip":   []any{"3", 0},
		"vae":    []any{"4", 0},
		"prompt": prompt,
		"width":  width,
		"height": height,
		"length": frames,
	}
	wf := map[string]any{
		"1":  map[string]any{"class_type": "UNETLoader", "inputs": map[string]any{"unet_name": "minimax_h3_fl2va_pruned_int8_convrot.safetensors", "weight_dtype": "default"}},
		"2":  map[string]any{"class_type": "LoraLoaderModelOnly", "inputs": map[string]any{"model": []any{"1", 0}, "lora_name": "pika_h3_flex_fl2va_v1_bf16.safetensors", "strength_model": 1}},
		"3":  map[string]any{"class_type": "CLIPLoader", "inputs": map[string]any{"clip_name": "qwen3vl_32b_minimax_h3_nvfp4_awq.safetensors", "type": "minimax", "device": "default"}},
		"4":  map[string]any{"class_type": "VAELoader", "inputs": map[string]any{"vae_name": "minimax_h3_video_vae_int8_convrot.safetensors"}},
		"5":  map[string]any{"class_type": "VAELoader", "inputs": map[string]any{"vae_name": "minimax_h3_audio_vae_fp32.safetensors"}},
		"7":  map[string]any{"class_type": "MiniMaxH3ImageToVideo", "inputs": i2v},
		"8":  map[string]any{"class_type": "RandomNoise", "inputs": map[string]any{"noise_seed": seed}},
		"9":  map[string]any{"class_type": "BasicGuider", "inputs": map[string]any{"model": []any{"2", 0}, "conditioning": []any{"7", 0}}},
		"10": map[string]any{"class_type": "KSamplerSelect", "inputs": map[string]any{"sampler_name": "lcm"}},
		"11": map[string]any{"class_type": "ManualSigmas", "inputs": map[string]any{"sigmas": customPikaSigmas}},
		"12": map[string]any{"class_type": "SamplerCustomAdvanced", "inputs": map[string]any{"noise": []any{"8", 0}, "guider": []any{"9", 0}, "sampler": []any{"10", 0}, "sigmas": []any{"11", 0}, "latent_image": []any{"7", 1}}},
		"13": map[string]any{"class_type": "VAEDecode", "inputs": map[string]any{"samples": []any{"12", 0}, "vae": []any{"4", 0}}},
		"14": map[string]any{"class_type": "VAEDecodeAudio", "inputs": map[string]any{"samples": []any{"12", 0}, "vae": []any{"5", 0}}},
		"15": map[string]any{"class_type": "CreateVideo", "inputs": map[string]any{"images": []any{"13", 0}, "audio": []any{"14", 0}, "fps": customPikaFPS}},
		"16": map[string]any{"class_type": "SaveVideo", "inputs": map[string]any{"video": []any{"15", 0}, "filename_prefix": filenamePrefix, "format": "mp4", "codec": "h264"}},
	}
	if image != "" {
		wf["6"] = map[string]any{"class_type": "LoadImage", "inputs": map[string]any{"image": image}}
		i2v["first_frame"] = []any{"6", 0}
	}
	return wf
}

func customPikaError(c *gin.Context, status int, code string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": code, "type": "invalid_request_error"}})
}

func customPikaUpload(ctx context.Context, base string, data []byte, filename string) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("image", filename)
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(data); err != nil {
		return "", err
	}
	_ = mw.WriteField("type", "input")
	_ = mw.WriteField("subfolder", "pika")
	_ = mw.WriteField("overwrite", "true")
	if err := mw.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/upload/image", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload status %d", resp.StatusCode)
	}
	var out struct {
		Name      string `json:"name"`
		Subfolder string `json:"subfolder"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Name == "" {
		return "", fmt.Errorf("upload response invalid")
	}
	if out.Subfolder != "" {
		return out.Subfolder + "/" + out.Name, nil
	}
	return out.Name, nil
}

func (s *Server) customPikaCreate(c *gin.Context, raw []byte) {
	var req struct {
		Prompt     string `json:"prompt"`
		Seconds    any    `json:"seconds"`
		Size       string `json:"size"`
		Resolution string `json:"resolution"`
		Seed       *int64 `json:"seed"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		customPikaError(c, http.StatusBadRequest, "invalid_request_body")
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		customPikaError(c, http.StatusBadRequest, "prompt_required")
		return
	}
	if len([]rune(prompt)) > customPikaMaxPrompt {
		customPikaError(c, http.StatusBadRequest, "prompt_too_long")
		return
	}
	seconds, duration, err := customPikaSeconds(req.Seconds)
	if err != nil {
		customPikaError(c, http.StatusBadRequest, err.Error())
		return
	}
	var image []byte
	ext := ""
	if first := customVideosFirstFrame(raw); first != "" {
		image, ext, err = customPikaDecodeImage(first)
		if err != nil {
			customPikaError(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	base := s.customPikaBaseURL()
	if base == "" {
		customPikaError(c, http.StatusServiceUnavailable, "video_backend_unavailable")
		return
	}
	uuid, err := customPikaNewID()
	if err != nil {
		customPikaError(c, http.StatusInternalServerError, "id_generation_failed")
		return
	}
	id := customPikaIDPrefix + uuid
	width, height, size := customPikaDimensions(req.Size, req.Resolution)
	seed := time.Now().UnixNano() & 0x7fffffffffff
	if req.Seed != nil && *req.Seed >= 0 {
		seed = *req.Seed
	}

	// Cold start loads ComfyUI behind llama-swap (and evicts the resident LLMs).
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()
	imageName := ""
	if image != nil {
		imageName, err = customPikaUpload(ctx, base, image, id+"."+ext)
		if err != nil {
			customPikaError(c, http.StatusBadGateway, "video_upload_failed")
			return
		}
	}
	body, _ := json.Marshal(map[string]any{
		"prompt":    customPikaWorkflow(prompt, imageName, "pika/"+id, width, height, customPikaFrames(duration), seed),
		"client_id": id,
	})
	up, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/prompt", bytes.NewReader(body))
	up.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(up)
	if err != nil {
		customPikaError(c, http.StatusBadGateway, "video_upstream_request_failed")
		return
	}
	defer resp.Body.Close()
	respRaw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		PromptID string `json:"prompt_id"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(respRaw, &out) != nil || out.PromptID == "" {
		customPikaError(c, http.StatusBadGateway, "video_upstream_rejected")
		return
	}
	job := &customPikaJob{
		ID:        id,
		PromptID:  out.PromptID,
		Prompt:    prompt,
		Seconds:   seconds,
		Size:      size,
		Width:     width,
		Height:    height,
		CreatedAt: time.Now().Unix(),
		Status:    "queued",
	}
	customPikaJobPut(job)
	c.JSON(http.StatusOK, customPikaResponse(job))
}

func customPikaResponse(job *customPikaJob) gin.H {
	progress := 0
	switch job.Status {
	case "in_progress":
		progress = 50
	case "completed":
		progress = 100
	}
	out := gin.H{
		"object":     "video",
		"id":         job.ID,
		"model":      customPikaModel,
		"prompt":     job.Prompt,
		"seconds":    job.Seconds,
		"size":       job.Size,
		"width":      job.Width,
		"height":     job.Height,
		"created_at": job.CreatedAt,
		"status":     job.Status,
		"progress":   progress,
	}
	if job.Status == "failed" {
		code := job.ErrMsg
		if code == "" {
			code = "video_generation_failed"
		}
		out["error"] = gin.H{"code": code, "message": code}
	}
	return out
}

func (s *Server) customPikaRefresh(ctx context.Context, job *customPikaJob) error {
	if job.Status == "completed" || job.Status == "failed" {
		return nil
	}
	base := s.customPikaBaseURL()
	if base == "" {
		return fmt.Errorf("video backend unavailable")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/history/"+url.PathEscape(job.PromptID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("history status %d", resp.StatusCode)
	}
	var history map[string]struct {
		Status struct {
			StatusStr string `json:"status_str"`
			Completed bool   `json:"completed"`
		} `json:"status"`
		Outputs map[string]map[string]json.RawMessage `json:"outputs"`
	}
	if err := json.Unmarshal(raw, &history); err != nil {
		return err
	}
	entry, ok := history[job.PromptID]
	if !ok {
		if job.Status == "queued" && s.customPikaRunning(ctx, base, job.PromptID) {
			job.Status = "in_progress"
			customPikaJobPut(job)
		}
		return nil
	}
	if strings.EqualFold(entry.Status.StatusStr, "error") {
		job.Status = "failed"
		job.ErrMsg = "video_generation_failed"
		customPikaJobPut(job)
		return nil
	}
	if !entry.Status.Completed {
		job.Status = "in_progress"
		customPikaJobPut(job)
		return nil
	}
	for _, node := range entry.Outputs {
		for _, key := range []string{"images", "videos", "gifs"} {
			list, ok := node[key]
			if !ok {
				continue
			}
			var files []struct {
				Filename  string `json:"filename"`
				Subfolder string `json:"subfolder"`
				Type      string `json:"type"`
			}
			if json.Unmarshal(list, &files) != nil {
				continue
			}
			for _, f := range files {
				if strings.HasSuffix(strings.ToLower(f.Filename), ".mp4") {
					job.Filename, job.Subfolder, job.FileType = f.Filename, f.Subfolder, f.Type
				}
			}
		}
	}
	if job.Filename == "" {
		job.Status = "failed"
		job.ErrMsg = "video_output_missing"
	} else {
		job.Status = "completed"
	}
	customPikaJobPut(job)
	return nil
}

func (s *Server) customPikaRunning(ctx context.Context, base, promptID string) bool {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/queue", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var q struct {
		Running [][]json.RawMessage `json:"queue_running"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&q) != nil {
		return false
	}
	for _, item := range q.Running {
		if len(item) < 2 {
			continue
		}
		var id string
		if json.Unmarshal(item[1], &id) == nil && id == promptID {
			return true
		}
	}
	return false
}

func (s *Server) customPikaRetrieve(c *gin.Context, id string) {
	job := customPikaJobGet(id)
	if job == nil {
		customPikaError(c, http.StatusNotFound, "unknown_video_id")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	_ = s.customPikaRefresh(ctx, job)
	c.JSON(http.StatusOK, customPikaResponse(job))
}

func (s *Server) customPikaContent(c *gin.Context, id string) {
	job := customPikaJobGet(id)
	if job == nil {
		customPikaError(c, http.StatusNotFound, "unknown_video_id")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	if err := s.customPikaRefresh(ctx, job); err != nil {
		customPikaError(c, http.StatusBadGateway, "video_status_refresh_failed")
		return
	}
	if job.Status != "completed" {
		customPikaError(c, http.StatusConflict, "video_not_ready")
		return
	}
	base := s.customPikaBaseURL()
	if base == "" {
		customPikaError(c, http.StatusServiceUnavailable, "video_backend_unavailable")
		return
	}
	q := url.Values{"filename": {job.Filename}, "subfolder": {job.Subfolder}, "type": {job.FileType}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/view?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		customPikaError(c, http.StatusBadGateway, "video_download_failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		customPikaError(c, http.StatusBadGateway, "video_download_failed")
		return
	}
	c.Header("Content-Type", "video/mp4")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", job.ID+".mp4"))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, io.LimitReader(resp.Body, customPikaMaxVideoBytes))
}
