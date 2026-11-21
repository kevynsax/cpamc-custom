package api

// CUSTOM (cpamc-custom): public client setup kit. See CUSTOM_FEATURES.md.
//
// GET /setup             agent instructions (Markdown)
// GET /setup/<file>      installers, configs and skills from internal/api/setup
//
// No secrets live here: each installer takes the person's client key as an argument.

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed setup
var customSetupFiles embed.FS

func customSetupContentType(name string) string {
	switch path.Ext(name) {
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".mjs":
		return "text/javascript; charset=utf-8"
	}
	return "text/plain; charset=utf-8"
}

func customSetupHandler(c *gin.Context) {
	name := strings.TrimPrefix(c.Param("file"), "/")
	if name == "" {
		name = "README.md"
	}
	clean := path.Clean(name)
	if clean != name || strings.HasPrefix(clean, ".") {
		c.Status(http.StatusNotFound)
		return
	}
	data, err := fs.ReadFile(customSetupFiles, "setup/"+clean)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, customSetupContentType(clean), data)
}
