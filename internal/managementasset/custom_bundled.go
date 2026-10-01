package managementasset

// CUSTOM (cpamc-custom): serve the panel built from ./panel (baked into the image)
// instead of downloading the upstream release from GitHub. See CUSTOM_FEATURES.md.
//
// The Dockerfile copies the built panel to /CLIProxyAPI/bundled/management.html and
// sets CPAMC_BUNDLED_PANEL to that path. When the variable is set:
//   - the GitHub auto-updater never runs,
//   - EnsureLatestManagementHTML installs the bundled file into the static dir
//     (overwriting any previously downloaded upstream copy when it differs).

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

const customBundledPanelEnv = "CPAMC_BUNDLED_PANEL"

func customBundledPanelPath() string {
	return strings.TrimSpace(os.Getenv(customBundledPanelEnv))
}

// customInstallBundledPanel returns handled=true when a bundled panel is configured.
func customInstallBundledPanel(staticDir string) (handled bool, ok bool) {
	src := customBundledPanelPath()
	if src == "" {
		return false, false
	}
	data, err := os.ReadFile(src)
	if err != nil || len(data) == 0 {
		log.WithError(err).Errorf("bundled management panel unreadable at %s", src)
		return true, false
	}
	if staticDir == "" {
		return true, false
	}
	dst := filepath.Join(staticDir, managementAssetName)
	if current, errRead := os.ReadFile(dst); errRead == nil && bytes.Equal(current, data) {
		return true, true
	}
	if err = os.MkdirAll(staticDir, 0o755); err != nil {
		log.WithError(err).Error("bundled management panel: mkdir failed")
		return true, false
	}
	tmp := dst + ".tmp"
	if err = os.WriteFile(tmp, data, 0o644); err != nil {
		log.WithError(err).Error("bundled management panel: write failed")
		return true, false
	}
	if err = os.Rename(tmp, dst); err != nil {
		log.WithError(err).Error("bundled management panel: rename failed")
		return true, false
	}
	log.Infof("bundled management panel installed (%d bytes)", len(data))
	return true, true
}
