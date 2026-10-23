package management

import (
	"compress/gzip"
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// writeCompressedJSON gzips large JSON reports when the client accepts it.
// CUSTOM (cpamc-custom): usage history is megabytes of repetitive JSON.
func writeCompressedJSON(c *gin.Context, status int, body any) {
	if !strings.Contains(strings.ToLower(c.GetHeader("Accept-Encoding")), "gzip") {
		c.JSON(status, body)
		return
	}
	data, err := json.Marshal(body)
	if err != nil {
		log.WithError(err).Error("Encode management JSON")
		c.JSON(500, gin.H{"error": "response encoding failed"})
		return
	}
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Encoding", "gzip")
	c.Header("Vary", "Accept-Encoding")
	c.Status(status)
	writer, _ := gzip.NewWriterLevel(c.Writer, gzip.BestSpeed)
	if _, errWrite := writer.Write(data); errWrite != nil {
		log.WithError(errWrite).Debug("Write compressed management JSON")
	}
	if errClose := writer.Close(); errClose != nil {
		log.WithError(errClose).Debug("Close compressed management JSON")
	}
}
