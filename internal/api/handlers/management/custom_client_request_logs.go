package management

// CUSTOM (cpamc-custom): read the full request logs of Google-account clients.
// See CUSTOM_FEATURES.md ("Google-account clients").

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
)

// clientRequestLogFile describes one request log file of a client.
type clientRequestLogFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

// clientRequestLogClient lists the configured clients and their log totals.
type clientRequestLogClient struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// GetClientRequestLogClients lists the Google-account clients and the size of their request logs.
func (h *Handler) GetClientRequestLogClients(c *gin.Context) {
	names := config.GoogleAccountClientNames(h.cfg.GoogleAccountClients)
	clients := make([]clientRequestLogClient, 0, len(names))
	for _, name := range names {
		files, errList := h.clientRequestLogFiles(name)
		if errList != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": errList.Error()})
			return
		}
		client := clientRequestLogClient{Name: name, Files: len(files)}
		for _, file := range files {
			client.Bytes += file.Size
		}
		clients = append(clients, client)
	}
	c.JSON(http.StatusOK, gin.H{"clients": clients})
}

// GetClientRequestLogs lists one client's request log files, newest first.
func (h *Handler) GetClientRequestLogs(c *gin.Context) {
	name := c.Param("client")
	if !h.isGoogleAccountClient(name) {
		c.JSON(http.StatusNotFound, gin.H{"error": "client not found"})
		return
	}
	files, errList := h.clientRequestLogFiles(name)
	if errList != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": errList.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

// DownloadClientRequestLog returns one request log file of a client.
func (h *Handler) DownloadClientRequestLog(c *gin.Context) {
	client := c.Param("client")
	name := c.Param("name")
	if !h.isGoogleAccountClient(client) {
		c.JSON(http.StatusNotFound, gin.H{"error": "client not found"})
		return
	}
	if name != filepath.Base(name) || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".log") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid log file name"})
		return
	}
	dir := h.clientRequestLogDir(client)
	path := filepath.Join(dir, name)
	info, errStat := os.Stat(path)
	if errStat != nil || !info.Mode().IsRegular() {
		c.JSON(http.StatusNotFound, gin.H{"error": "log file not found"})
		return
	}
	c.FileAttachment(path, name)
}

func (h *Handler) isGoogleAccountClient(name string) bool {
	for _, client := range h.cfg.GoogleAccountClients {
		if client.Name == name {
			return true
		}
	}
	return false
}

func (h *Handler) clientRequestLogDir(client string) string {
	return filepath.Join(h.logDirectory(), logging.ClientRequestLogDir, client)
}

func (h *Handler) clientRequestLogFiles(client string) ([]clientRequestLogFile, error) {
	dir := h.clientRequestLogDir(client)
	entries, errRead := os.ReadDir(dir)
	if os.IsNotExist(errRead) {
		return []clientRequestLogFile{}, nil
	}
	if errRead != nil {
		return nil, errRead
	}
	files := make([]clientRequestLogFile, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, errInfo := entry.Info()
		if errInfo != nil {
			continue
		}
		file := clientRequestLogFile{Name: name, Size: info.Size(), Modified: info.ModTime().Unix()}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Modified > files[j].Modified })
	return files, nil
}
