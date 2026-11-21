package api

// CUSTOM (cpamc-custom): tests for the public setup kit.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCustomSetupServesKitFilesOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/setup", customSetupHandler)
	r.GET("/setup/*file", customSetupHandler)
	cases := map[string]int{
		"/setup":                              http.StatusOK,
		"/setup/install.sh":                   http.StatusOK,
		"/setup/skills/image-gen/SKILL.md":    http.StatusOK,
		"/setup/../custom_setup.go":           http.StatusNotFound,
		"/setup/skills/../../custom_setup.go": http.StatusNotFound,
		"/setup/missing.txt":                  http.StatusNotFound,
	}
	for url, want := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != want {
			t.Errorf("%s: status %d, want %d", url, w.Code, want)
		}
	}
}
