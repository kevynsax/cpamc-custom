package api

// CUSTOM (cpamc-custom): OpenAI-style video generation backed by MiniMax Hailuo V1.
// See CUSTOM_FEATURES.md.
//
// POST   /v1/videos  or  /openai/v1/videos
//   body: {"model":"minimax/MiniMax-Hailuo-2.3","prompt":"...","seconds":"6","size":"1280x720",
//          "input_reference":{"image_url":"data:image/png;base64,..."}}  // optional I2V
//   -> {"object":"video","id":"<task_id>","status":"queued",...}
//
// GET    /v1/videos/:id  or  /openai/v1/videos/:id
//   -> OpenAI video status (queued|in_progress|completed|failed)
//
// GET    /v1/videos/:id/content  or  /openai/v1/videos/:id/content
//   -> video/mp4 bytes
//
// Only models with a minimax/ prefix are handled here. The MiniMax key comes from
// the openai-compatibility entry (customMinimaxKey) and never leaves the server.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	customVideosCreateURL   = "https://api.minimax.io/v1/video_generation"
	customVideosQueryURL    = "https://api.minimax.io/v1/query/video_generation"
	customVideosFileURL     = "https://api.minimax.io/v1/files/retrieve"
	customVideosContentURL  = "https://api.minimax.io/v1/files/retrieve_content"
	customVideosMaxPrompt   = 2000
	customVideosDefaultSec  = 6
	customVideosDefaultSize = "1280x720"
	customVideosDefaultRes  = "768P"
	customVideosDefaultModel = "MiniMax-Hailuo-2.3"
)

type customVideoJob struct {
	TaskID    string
	Model     string // client-facing (minimax/...)
	Upstream  string // MiniMax model id
	Prompt    string
	Seconds   string
	Size      string
	CreatedAt int64
	FileID    string
	Status    string // Preparing|Queueing|Processing|Success|Fail
	Width     int
	Height    int
	ErrCode   string
	ErrMsg    string
}

var customVideoJobs = struct {
	sync.RWMutex
	byID map[string]*customVideoJob
}{byID: make(map[string]*customVideoJob)}

func customVideoJobPut(job *customVideoJob) {
	if job == nil || job.TaskID == "" {
		return
	}
	customVideoJobs.Lock()
	customVideoJobs.byID[job.TaskID] = job
	customVideoJobs.Unlock()
}

func customVideoJobGet(id string) *customVideoJob {
	customVideoJobs.RLock()
	defer customVideoJobs.RUnlock()
	return customVideoJobs.byID[id]
}

// customVideosMinimaxModel returns the MiniMax V1 model id for a requested model, or "".
// mode is "t2v" or "i2v" and filters aliases that only exist in one mode.
func customVideosMinimaxModel(model, mode string) string {
	base := strings.TrimSpace(model)
	lower := strings.ToLower(base)
	if !strings.HasPrefix(lower, "minimax/") {
		return ""
	}
	base = strings.TrimSpace(base[len("minimax/"):])
	lower = strings.ToLower(base)

	aliases := map[string]string{
		"minimax-hailuo-2.3":      "MiniMax-Hailuo-2.3",
		"hailuo-2.3":              "MiniMax-Hailuo-2.3",
		"hailuo":                  "MiniMax-Hailuo-2.3",
		"minimax-hailuo-2.3-fast": "MiniMax-Hailuo-2.3-Fast",
		"hailuo-2.3-fast":         "MiniMax-Hailuo-2.3-Fast",
		"minimax-hailuo-02":       "MiniMax-Hailuo-02",
		"hailuo-02":               "MiniMax-Hailuo-02",
		"t2v-01-director":         "T2V-01-Director",
		"t2v-01":                  "T2V-01",
		"i2v-01-director":         "I2V-01-Director",
		"i2v-01-live":             "I2V-01-live",
		"i2v-01":                  "I2V-01",
	}
	if canon, ok := aliases[lower]; ok {
		base = canon
	}

	t2v := map[string]bool{
		"MiniMax-Hailuo-2.3": true,
		"MiniMax-Hailuo-02":  true,
		"T2V-01-Director":    true,
		"T2V-01":             true,
	}
	i2v := map[string]bool{
		"MiniMax-Hailuo-2.3":      true,
		"MiniMax-Hailuo-2.3-Fast": true,
		"MiniMax-Hailuo-02":       true,
		"I2V-01-Director":         true,
		"I2V-01-live":             true,
		"I2V-01":                  true,
	}
	switch mode {
	case "i2v":
		if i2v[base] {
			return base
		}
		return ""
	default:
		if t2v[base] {
			return base
		}
		return ""
	}
}

func customVideosIsMinimaxModel(model string) bool {
	return customVideosMinimaxModel(model, "t2v") != "" || customVideosMinimaxModel(model, "i2v") != ""
}

func customVideosFirstFrame(raw []byte) string {
	var peek map[string]json.RawMessage
	if json.Unmarshal(raw, &peek) != nil {
		return ""
	}
	readString := func(v json.RawMessage) string {
		var s string
		if json.Unmarshal(v, &s) == nil {
			return strings.TrimSpace(s)
		}
		var obj map[string]any
		if json.Unmarshal(v, &obj) != nil {
			return ""
		}
		if u, ok := obj["image_url"].(string); ok {
			return strings.TrimSpace(u)
		}
		if nested, ok := obj["image_url"].(map[string]any); ok {
			if u, ok := nested["url"].(string); ok {
				return strings.TrimSpace(u)
			}
		}
		if u, ok := obj["url"].(string); ok {
			return strings.TrimSpace(u)
		}
		return ""
	}
	for _, key := range []string{"first_frame_image", "image_url", "image", "input_reference"} {
		if v, ok := peek[key]; ok {
			if s := readString(v); s != "" {
				return s
			}
		}
	}
	return ""
}

func customVideosSeconds(raw string) (string, int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return strconv.Itoa(customVideosDefaultSec), customVideosDefaultSec, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || (n != 6 && n != 10) {
		return "", 0, fmt.Errorf("seconds must be 6 or 10 for MiniMax Hailuo V1")
	}
	return raw, n, nil
}

func customVideosResolution(resolution, size string) string {
	r := strings.ToUpper(strings.TrimSpace(resolution))
	switch r {
	case "512P", "720P", "768P", "1080P":
		return r
	}
	size = strings.ToLower(strings.TrimSpace(size))
	if size == "" {
		return customVideosDefaultRes
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return customVideosDefaultRes
	}
	w, errW := strconv.Atoi(parts[0])
	h, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil {
		return customVideosDefaultRes
	}
	long := w
	if h > long {
		long = h
	}
	switch {
	case long >= 1920:
		return "1080P"
	case long >= 1280:
		return "768P"
	case long >= 720:
		return "720P"
	default:
		return "512P"
	}
}

func customVideosOpenAIStatus(mmStatus string) string {
	switch strings.ToLower(strings.TrimSpace(mmStatus)) {
	case "preparing", "queueing", "queued", "pending":
		return "queued"
	case "processing", "running", "in_progress":
		return "in_progress"
	case "success", "succeeded", "completed", "done":
		return "completed"
	case "fail", "failed", "error", "cancelled", "canceled":
		return "failed"
	default:
		return "queued"
	}
}

func customVideosProgress(status string) int {
	switch customVideosOpenAIStatus(status) {
	case "completed":
		return 100
	case "in_progress":
		return 50
	case "failed":
		return 0
	default:
		return 0
	}
}

func customVideosResponse(job *customVideoJob) gin.H {
	out := gin.H{
		"object":     "video",
		"id":         job.TaskID,
		"model":      job.Model,
		"prompt":     job.Prompt,
		"seconds":    job.Seconds,
		"size":       job.Size,
		"created_at": job.CreatedAt,
		"status":     customVideosOpenAIStatus(job.Status),
		"progress":   customVideosProgress(job.Status),
	}
	if job.Width > 0 {
		out["width"] = job.Width
	}
	if job.Height > 0 {
		out["height"] = job.Height
	}
	if customVideosOpenAIStatus(job.Status) == "failed" {
		code := job.ErrCode
		if code == "" {
			code = "video_generation_failed"
		}
		msg := job.ErrMsg
		if msg == "" {
			msg = "Video generation failed"
		}
		out["error"] = gin.H{"code": code, "message": msg}
	}
	return out
}

func (s *Server) customVideosCreateDispatch(upstream gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		ct := strings.ToLower(c.ContentType())
		if strings.HasPrefix(ct, "multipart/") {
			if model := strings.TrimSpace(c.PostForm("model")); customVideosIsMinimaxModel(model) {
				s.customVideosCreateFromForm(c)
				return
			}
			upstream(c)
			return
		}
		raw, err := io.ReadAll(c.Request.Body)
		_ = c.Request.Body.Close()
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		if err == nil {
			var peek struct {
				Model string `json:"model"`
			}
			if json.Unmarshal(raw, &peek) == nil && customVideosIsMinimaxModel(peek.Model) {
				s.customVideosCreate(c, raw)
				return
			}
		}
		upstream(c)
	}
}

func customVideosLooksLikeMinimaxTaskID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(id) >= 8
}

func (s *Server) customVideosEnsureJob(id string) *customVideoJob {
	if job := customVideoJobGet(id); job != nil {
		return job
	}
	if !customVideosLooksLikeMinimaxTaskID(id) {
		return nil
	}
	job := &customVideoJob{
		TaskID:    id,
		Model:     "minimax/" + customVideosDefaultModel,
		Upstream:  customVideosDefaultModel,
		Seconds:   strconv.Itoa(customVideosDefaultSec),
		Size:      customVideosDefaultSize,
		CreatedAt: time.Now().Unix(),
		Status:    "Queueing",
	}
	customVideoJobPut(job)
	return job
}

func (s *Server) customVideosRetrieveDispatch(upstream gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.Param("video_id"))
		if id == "" {
			id = strings.TrimSpace(c.Param("request_id"))
		}
		if id != "" && (customVideoJobGet(id) != nil || customVideosLooksLikeMinimaxTaskID(id)) {
			s.customVideosRetrieve(c, id)
			return
		}
		upstream(c)
	}
}

func (s *Server) customVideosContentDispatch(upstream gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.Param("video_id"))
		if id == "" {
			id = strings.TrimSpace(c.Param("request_id"))
		}
		if id != "" && (customVideoJobGet(id) != nil || customVideosLooksLikeMinimaxTaskID(id)) {
			s.customVideosContent(c, id)
			return
		}
		if upstream != nil {
			upstream(c)
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "unknown video id", "type": "invalid_request_error"}})
	}
}

func (s *Server) customVideosCreateFromForm(c *gin.Context) {
	model := strings.TrimSpace(c.PostForm("model"))
	prompt := strings.TrimSpace(c.PostForm("prompt"))
	seconds := strings.TrimSpace(c.PostForm("seconds"))
	size := strings.TrimSpace(c.PostForm("size"))
	resolution := strings.TrimSpace(c.PostForm("resolution"))
	first := strings.TrimSpace(c.PostForm("first_frame_image"))
	if first == "" {
		first = strings.TrimSpace(c.PostForm("image_url"))
	}
	payload := map[string]any{
		"model":      model,
		"prompt":     prompt,
		"seconds":    seconds,
		"size":       size,
		"resolution": resolution,
	}
	if first != "" {
		payload["first_frame_image"] = first
	}
	raw, _ := json.Marshal(payload)
	s.customVideosCreate(c, raw)
}

func (s *Server) customVideosCreate(c *gin.Context, raw []byte) {
	var req struct {
		Model       string `json:"model"`
		Prompt      string `json:"prompt"`
		Seconds     any    `json:"seconds"`
		Size        string `json:"size"`
		Resolution  string `json:"resolution"`
		PromptOptim *bool  `json:"prompt_optimizer"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid request body", "type": "invalid_request_error"}})
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	first := customVideosFirstFrame(raw)
	mode := "t2v"
	if first != "" {
		mode = "i2v"
	}
	upstreamModel := customVideosMinimaxModel(req.Model, mode)
	if upstreamModel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": fmt.Sprintf("model %s is not supported for MiniMax %s", req.Model, mode), "type": "invalid_request_error"}})
		return
	}
	if mode == "t2v" && prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "prompt is required", "type": "invalid_request_error"}})
		return
	}
	if mode == "i2v" && first == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "first_frame_image is required for image-to-video", "type": "invalid_request_error"}})
		return
	}
	if r := []rune(prompt); len(r) > customVideosMaxPrompt {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": fmt.Sprintf("prompt exceeds %d characters", customVideosMaxPrompt), "type": "invalid_request_error"}})
		return
	}
	secStr := ""
	switch v := req.Seconds.(type) {
	case string:
		secStr = v
	case float64:
		secStr = strconv.Itoa(int(v))
	case json.Number:
		secStr = v.String()
	}
	seconds, duration, err := customVideosSeconds(secStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	size := strings.TrimSpace(req.Size)
	if size == "" {
		size = customVideosDefaultSize
	}
	resolution := customVideosResolution(req.Resolution, size)
	key := s.customMinimaxKey()
	if key == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no MiniMax credential configured"}})
		return
	}

	payload := map[string]any{
		"model":      upstreamModel,
		"duration":   duration,
		"resolution": resolution,
	}
	if prompt != "" {
		payload["prompt"] = prompt
	}
	if first != "" {
		payload["first_frame_image"] = first
	}
	if req.PromptOptim != nil {
		payload["prompt_optimizer"] = *req.PromptOptim
	}
	body, _ := json.Marshal(payload)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	up, _ := http.NewRequestWithContext(ctx, http.MethodPost, customVideosCreateURL, bytes.NewReader(body))
	up.Header.Set("Authorization", "Bearer "+key)
	up.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(up)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "video upstream request failed"}})
		return
	}
	defer resp.Body.Close()
	respRaw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	var mm struct {
		TaskID   string `json:"task_id"`
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(respRaw, &mm); err != nil || mm.TaskID == "" || mm.BaseResp.StatusCode != 0 {
		status, msg := customVideosMapBaseResp(mm.BaseResp.StatusCode, mm.BaseResp.StatusMsg, resp.StatusCode)
		c.JSON(status, gin.H{"error": gin.H{"message": msg, "type": "invalid_request_error"}})
		return
	}

	clientModel := strings.TrimSpace(req.Model)
	if clientModel == "" {
		clientModel = "minimax/" + customVideosDefaultModel
	}
	job := &customVideoJob{
		TaskID:    mm.TaskID,
		Model:     clientModel,
		Upstream:  upstreamModel,
		Prompt:    prompt,
		Seconds:   seconds,
		Size:      size,
		CreatedAt: time.Now().Unix(),
		Status:    "Queueing",
	}
	customVideoJobPut(job)
	c.JSON(http.StatusOK, customVideosResponse(job))
}

func customVideosMapBaseResp(code int, msg string, httpStatus int) (int, string) {
	if msg == "" {
		msg = fmt.Sprintf("video failed: upstream status %d", httpStatus)
	} else {
		msg = fmt.Sprintf("video failed: %s (minimax %d)", msg, code)
	}
	switch code {
	case 1002:
		return http.StatusTooManyRequests, msg
	case 1026, 1027, 2013:
		return http.StatusBadRequest, msg
	case 1008:
		return http.StatusPaymentRequired, msg
	case 1004, 2049:
		return http.StatusUnauthorized, msg
	case 0:
		if httpStatus >= 400 {
			return http.StatusBadGateway, msg
		}
		return http.StatusBadGateway, "video failed: empty task_id"
	default:
		if httpStatus >= 400 {
			return httpStatus, msg
		}
		return http.StatusBadGateway, msg
	}
}

func (s *Server) customVideosRefresh(ctx context.Context, job *customVideoJob, key string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, customVideosQueryURL+"?task_id="+job.TaskID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var mm struct {
		TaskID      string `json:"task_id"`
		Status      string `json:"status"`
		FileID      any    `json:"file_id"`
		VideoWidth  int    `json:"video_width"`
		VideoHeight int    `json:"video_height"`
		BaseResp    struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(raw, &mm); err != nil {
		return fmt.Errorf("invalid query response")
	}
	if mm.BaseResp.StatusCode != 0 && mm.Status == "" {
		job.Status = "Fail"
		job.ErrCode = strconv.Itoa(mm.BaseResp.StatusCode)
		job.ErrMsg = mm.BaseResp.StatusMsg
		customVideoJobPut(job)
		return nil
	}
	if mm.Status != "" {
		job.Status = mm.Status
	}
	switch v := mm.FileID.(type) {
	case string:
		job.FileID = strings.TrimSpace(v)
	case float64:
		job.FileID = strconv.FormatInt(int64(v), 10)
	case json.Number:
		job.FileID = v.String()
	}
	if mm.VideoWidth > 0 {
		job.Width = mm.VideoWidth
	}
	if mm.VideoHeight > 0 {
		job.Height = mm.VideoHeight
	}
	if strings.EqualFold(job.Status, "Fail") && job.ErrMsg == "" {
		job.ErrCode = "video_generation_failed"
		job.ErrMsg = mm.BaseResp.StatusMsg
		if job.ErrMsg == "" {
			job.ErrMsg = "Video generation failed"
		}
	}
	customVideoJobPut(job)
	return nil
}

func (s *Server) customVideosRetrieve(c *gin.Context, id string) {
	job := s.customVideosEnsureJob(id)
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "unknown video id", "type": "invalid_request_error"}})
		return
	}
	key := s.customMinimaxKey()
	if key == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no MiniMax credential configured"}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	_ = s.customVideosRefresh(ctx, job, key)
	c.JSON(http.StatusOK, customVideosResponse(job))
}

func (s *Server) customVideosContent(c *gin.Context, id string) {
	job := s.customVideosEnsureJob(id)
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "unknown video id", "type": "invalid_request_error"}})
		return
	}
	key := s.customMinimaxKey()
	if key == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no MiniMax credential configured"}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	if job.FileID == "" || customVideosOpenAIStatus(job.Status) != "completed" {
		if err := s.customVideosRefresh(ctx, job, key); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "video status refresh failed"}})
			return
		}
	}
	if customVideosOpenAIStatus(job.Status) != "completed" || job.FileID == "" {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"message": fmt.Sprintf("video not ready (status=%s)", customVideosOpenAIStatus(job.Status)), "type": "invalid_request_error"}})
		return
	}

	// Prefer retrieve_content; fall back to download_url from retrieve.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, customVideosContentURL+"?file_id="+job.FileID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "video download failed"}})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		ct := resp.Header.Get("Content-Type")
		if ct == "" || strings.Contains(ct, "json") {
			// Some MiniMax responses wrap; try file retrieve URL instead.
			_ = resp.Body.Close()
			s.customVideosContentViaURL(c, ctx, key, job.FileID)
			return
		}
		c.Header("Content-Type", "video/mp4")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", job.TaskID+".mp4"))
		c.Status(http.StatusOK)
		_, _ = io.Copy(c.Writer, io.LimitReader(resp.Body, 512<<20))
		return
	}
	_ = resp.Body.Close()
	s.customVideosContentViaURL(c, ctx, key, job.FileID)
}

func (s *Server) customVideosContentViaURL(c *gin.Context, ctx context.Context, key, fileID string) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, customVideosFileURL+"?file_id="+fileID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "video file lookup failed"}})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var mm struct {
		File struct {
			DownloadURL string `json:"download_url"`
		} `json:"file"`
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(raw, &mm); err != nil || mm.File.DownloadURL == "" || mm.BaseResp.StatusCode != 0 {
		status, msg := customVideosMapBaseResp(mm.BaseResp.StatusCode, mm.BaseResp.StatusMsg, resp.StatusCode)
		c.JSON(status, gin.H{"error": gin.H{"message": msg}})
		return
	}
	dl, err := http.NewRequestWithContext(ctx, http.MethodGet, mm.File.DownloadURL, nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "invalid download url"}})
		return
	}
	dlResp, err := http.DefaultClient.Do(dl)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "video download failed"}})
		return
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": fmt.Sprintf("video download status %d", dlResp.StatusCode)}})
		return
	}
	ct := dlResp.Header.Get("Content-Type")
	if ct == "" {
		ct = "video/mp4"
	}
	c.Header("Content-Type", ct)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileID+".mp4"))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, io.LimitReader(dlResp.Body, 512<<20))
}
