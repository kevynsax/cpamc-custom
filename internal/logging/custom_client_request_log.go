package logging

// CUSTOM (cpamc-custom): full request log for Google-account clients.
// See CUSTOM_FEATURES.md ("Google-account clients").

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	log "github.com/sirupsen/logrus"
)

// ClientRequestLogDir is the directory under the logs directory that holds one
// request log directory per Google-account client.
const ClientRequestLogDir = "clients"

const clientLogCleanerInterval = time.Minute

type clientRequestLogKey struct{}

var (
	clientLogCleanerMu     sync.Mutex
	clientLogCleanerCancel context.CancelFunc
)

// WithClientRequestLog marks ctx as a request that is logged in full, so the upstream
// request and response are recorded although request-log is off.
func WithClientRequestLog(ctx context.Context) context.Context {
	return context.WithValue(ctx, clientRequestLogKey{}, true)
}

// ClientRequestLogEnabled reports whether ctx belongs to a request that is logged in full.
func ClientRequestLogEnabled(ctx context.Context) bool {
	enabled, _ := ctx.Value(clientRequestLogKey{}).(bool)
	return enabled
}

// InheritClientRequestLog propagates the full-request-log mark from src to dst.
func InheritClientRequestLog(dst, src context.Context) context.Context {
	if dst == nil {
		dst = context.Background()
	}
	if src == nil || !ClientRequestLogEnabled(src) || ClientRequestLogEnabled(dst) {
		return dst
	}
	return WithClientRequestLog(dst)
}

// ClientRequestLoggers holds the full request logger of every Google-account client, by API key.
type ClientRequestLoggers struct {
	dir   string
	byKey map[string]*FileRequestLogger
}

// ConfigureClientRequestLog builds one request logger per client under
// <logsDir>/clients/<name>, resolved against configDir like the main request logger, and
// enforces maxBytes on every client directory, deleting the oldest log files first.
// Without clients, or with maxBytes <= 0, the logs are left unlimited. It returns nil
// when no client is configured.
func ConfigureClientRequestLog(logsDir, configDir string, clients []config.GoogleAccountClient, maxBytes int64) *ClientRequestLoggers {
	loggers := newClientRequestLoggers(logsDir, configDir, clients)
	root := ""
	if loggers != nil {
		root = loggers.dir
	}
	startClientLogCleaner(root, maxBytes)
	return loggers
}

// ForCredentials returns the logger of the first client that owns one of the credentials,
// or nil when none does.
func (l *ClientRequestLoggers) ForCredentials(credentials []string) *FileRequestLogger {
	var found *FileRequestLogger
	if l != nil {
		for _, credential := range credentials {
			if clientLogger := l.byKey[credential]; clientLogger != nil {
				found = clientLogger
				break
			}
		}
	}
	return found
}

func newClientRequestLoggers(logsDir, configDir string, clients []config.GoogleAccountClient) *ClientRequestLoggers {
	if len(clients) == 0 {
		return nil
	}
	if !filepath.IsAbs(logsDir) && configDir != "" {
		logsDir = filepath.Join(configDir, logsDir)
	}
	dir := filepath.Join(logsDir, ClientRequestLogDir)
	byKey := make(map[string]*FileRequestLogger, len(clients))
	for _, client := range clients {
		clientDir := filepath.Join(dir, client.Name)
		byKey[client.APIKey] = NewFileRequestLogger(true, clientDir, "", 0)
	}
	return &ClientRequestLoggers{dir: dir, byKey: byKey}
}

// startClientLogCleaner replaces the running cleaner. maxBytes <= 0 leaves the logs unlimited.
func startClientLogCleaner(root string, maxBytes int64) {
	clientLogCleanerMu.Lock()
	defer clientLogCleanerMu.Unlock()
	if clientLogCleanerCancel != nil {
		clientLogCleanerCancel()
		clientLogCleanerCancel = nil
	}
	if maxBytes <= 0 || root == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	clientLogCleanerCancel = cancel
	go runClientLogCleaner(ctx, root, maxBytes)
}

func runClientLogCleaner(ctx context.Context, root string, maxBytes int64) {
	ticker := time.NewTicker(clientLogCleanerInterval)
	defer ticker.Stop()

	cleanOnce := func() {
		deleted, errClean := enforceClientLogDirSizeLimit(root, maxBytes)
		if errClean != nil {
			log.WithError(errClean).Warn("logging: failed to enforce client request log size limit")
			return
		}
		if deleted > 0 {
			log.Debugf("logging: removed %d old client request log file(s) to enforce the size limit", deleted)
		}
	}

	cleanOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanOnce()
		}
	}
}

// enforceClientLogDirSizeLimit applies maxBytes to each immediate subdirectory of root.
func enforceClientLogDirSizeLimit(root string, maxBytes int64) (int, error) {
	entries, errRead := os.ReadDir(root)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return 0, nil
		}
		return 0, errRead
	}

	deleted := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		clientDir := filepath.Join(root, entry.Name())
		removed, errClean := enforceLogDirSizeLimit(clientDir, maxBytes, "")
		if errClean != nil {
			return deleted, errClean
		}
		deleted += removed
	}
	return deleted, nil
}
