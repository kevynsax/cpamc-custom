package api

// CUSTOM (cpamc-custom): pass-through relay for the agy CLI. See CUSTOM_FEATURES.md
// ("agy relay").
//
// agy signs in to Google itself and reaches Cloud Code through CLOUD_CODE_URL. Pointing
// that at https://<gateway>/agy-relay/<client key> sends every call here. The relay strips
// the prefix and forwards the request to Google unchanged: no credential of cpamc is used,
// no header is added, and the hop headers added by the ingress are removed. Each exchange
// is written to the client's request log directory, <logs>/clients/<name>.

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	log "github.com/sirupsen/logrus"
)

const agyRelayPrefix = "/agy-relay/"

var agyRelayUpstream = &url.URL{Scheme: "https", Host: "daily-cloudcode-pa.googleapis.com"}

var agyRelaySequence atomic.Uint64

// agyRelayHopHeaders are added between agy and cpamc (ingress, load balancer); agy never sends them.
var agyRelayHopHeaders = []string{
	"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Port", "X-Forwarded-Proto",
	"X-Forwarded-Server", "X-Real-Ip", "Forwarded", "Via",
}

// agyRelay handles every method under /agy-relay/<client key>/.
func (s *Server) agyRelay(c *gin.Context) {
	key, upstreamPath, found := strings.Cut(strings.TrimPrefix(c.Request.URL.Path, agyRelayPrefix), "/")
	client, known := googleAccountClientByKey(s.cfg.GoogleAccountClients, key)
	if !found || !known {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	logsDir := logging.ResolveLogDirectory(s.cfg)
	clientDir := filepath.Join(logsDir, logging.ClientRequestLogDir, client.Name)
	target := *agyRelayUpstream
	target.Path = "/" + upstreamPath
	target.RawQuery = c.Request.URL.RawQuery
	outgoing, errRequest := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target.String(), c.Request.Body)
	if errRequest != nil {
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
	outgoing.Header = c.Request.Header.Clone()
	for _, header := range agyRelayHopHeaders {
		outgoing.Header.Del(header)
	}
	// agy asks for gzip. Letting the transport add that same header makes it unzip the
	// answer, so Google sees the same request and the log stays readable.
	if outgoing.Header.Get("Accept-Encoding") == "gzip" {
		outgoing.Header.Del("Accept-Encoding")
	}
	transport := &agyRelayTransport{base: http.DefaultTransport, dir: clientDir}
	response, errTrip := transport.RoundTrip(outgoing)
	if errTrip != nil {
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
	defer func() {
		if errClose := response.Body.Close(); errClose != nil {
			log.WithError(errClose).Warn("agy relay: close response")
		}
	}()
	for key, values := range response.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	c.Writer.WriteHeader(response.StatusCode)
	streamAgyRelayResponse(c.Writer, response.Body)
}

// streamAgyRelayResponse copies the response to agy, flushing each chunk so
// server-sent events arrive as Google sends them.
func streamAgyRelayResponse(w gin.ResponseWriter, body io.Reader) {
	buffer := make([]byte, 32*1024)
	for {
		n, errRead := body.Read(buffer)
		if n > 0 {
			if _, errWrite := w.Write(buffer[:n]); errWrite != nil {
				return
			}
			w.Flush()
		}
		if errRead != nil {
			return
		}
	}
}

func googleAccountClientByKey(clients []config.GoogleAccountClient, key string) (config.GoogleAccountClient, bool) {
	for _, client := range clients {
		if key != "" && client.APIKey == key {
			return client, true
		}
	}
	return config.GoogleAccountClient{}, false
}

// agyRelayTransport writes one log file per exchange around the real round trip.
type agyRelayTransport struct {
	base http.RoundTripper
	dir  string
}

func (t *agyRelayTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body, errRead := readAgyRelayBody(r)
	if errRead != nil {
		return nil, errRead
	}
	started := time.Now()
	response, errTrip := t.base.RoundTrip(r)
	file, errOpen := openAgyRelayLog(t.dir, r, body, started)
	if errOpen != nil {
		log.WithError(errOpen).Warn("agy relay: open log")
		return response, errTrip
	}
	if errTrip != nil {
		fmt.Fprintf(file, "=== RESPONSE ===\nError: %v\n", errTrip)
		closeAgyRelayLog(file)
		return nil, errTrip
	}
	var head bytes.Buffer
	fmt.Fprintf(&head, "=== RESPONSE ===\nTimestamp: %s\nStatus: %d\n\nHeaders:\n", time.Now().Format(time.RFC3339Nano), response.StatusCode)
	writeAgyRelayHeaders(&head, response.Header)
	head.WriteString("\nBody:\n")
	if _, errWrite := file.Write(head.Bytes()); errWrite != nil {
		log.WithError(errWrite).Warn("agy relay: write log")
	}
	response.Body = &agyRelayTeeBody{source: response.Body, file: file}
	return response, nil
}

func readAgyRelayBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, errRead := io.ReadAll(r.Body)
	if errClose := r.Body.Close(); errClose != nil {
		log.WithError(errClose).Warn("agy relay: close request body")
	}
	if errRead != nil {
		return nil, errRead
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return body, nil
}

func openAgyRelayLog(dir string, r *http.Request, body []byte, started time.Time) (*os.File, error) {
	if errMkdir := os.MkdirAll(dir, 0o700); errMkdir != nil {
		return nil, errMkdir
	}
	operation := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	operation = strings.NewReplacer(":", "-", "?", "-", "..", "-").Replace(operation)
	id := agyRelaySequence.Add(1)
	name := fmt.Sprintf("agy-relay-%s-%s-%d.log", started.Format("2006-01-02T150405.000"), operation, id)
	path := filepath.Join(dir, filepath.Base(name))
	file, errOpen := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errOpen != nil {
		return nil, errOpen
	}
	var request bytes.Buffer
	fmt.Fprintf(&request, "=== REQUEST ===\nTimestamp: %s\n%s %s\n\nHeaders:\n", started.Format(time.RFC3339Nano), r.Method, r.URL.String())
	writeAgyRelayHeaders(&request, r.Header)
	request.WriteString("\nBody:\n")
	request.Write(body)
	request.WriteString("\n\n")
	if _, errWrite := file.Write(request.Bytes()); errWrite != nil {
		closeAgyRelayLog(file)
		return nil, errWrite
	}
	return file, nil
}

func writeAgyRelayHeaders(w io.Writer, headers http.Header) {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range headers[key] {
			masked := util.MaskSensitiveHeaderValue(key, value)
			fmt.Fprintf(w, "%s: %s\n", key, masked)
		}
	}
}

func closeAgyRelayLog(file *os.File) {
	if errClose := file.Close(); errClose != nil {
		log.WithError(errClose).Warn("agy relay: close log")
	}
}

// agyRelayTeeBody copies the response into the log while agy reads it.
type agyRelayTeeBody struct {
	source io.ReadCloser
	file   *os.File
}

func (b *agyRelayTeeBody) Read(p []byte) (int, error) {
	n, errRead := b.source.Read(p)
	if n > 0 {
		if _, errWrite := b.file.Write(p[:n]); errWrite != nil {
			log.WithError(errWrite).Warn("agy relay: write log")
		}
	}
	return n, errRead
}

func (b *agyRelayTeeBody) Close() error {
	if _, errWrite := b.file.WriteString("\n"); errWrite != nil {
		log.WithError(errWrite).Warn("agy relay: write log")
	}
	closeAgyRelayLog(b.file)
	return b.source.Close()
}
