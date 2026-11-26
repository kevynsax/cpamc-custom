package agyproxy

// CUSTOM (cpamc-custom)

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
	"gopkg.in/yaml.v3"
)

type Proxy struct {
	manager   *auth.Manager
	config    func() *config.Config
	logger    func(string) *logging.FileRequestLogger
	ca        *authority
	dir       string
	transport *http.Transport
	server    *http.Server
	mu        sync.Mutex
	grants    map[string]grant
	tunnels   map[net.Conn]struct{}
	closed    bool
}

func New(cfg *config.Config, manager *auth.Manager, currentConfig func() *config.Config, logger func(string) *logging.FileRequestLogger) (*Proxy, error) {
	settings := cfg.AgyProxy
	if settings.Cert == "" || settings.Key == "" || settings.StateDir == "" {
		return nil, fmt.Errorf("agy proxy requires cert, key and state-dir")
	}
	ca, errCA := loadAuthority(settings.StateDir)
	if errCA != nil {
		return nil, errCA
	}
	pair, errPair := tls.LoadX509KeyPair(settings.Cert, settings.Key)
	if errPair != nil {
		return nil, errPair
	}
	transport := &http.Transport{DisableCompression: true, ForceAttemptHTTP2: true, DialContext: dialPublic}
	if errHTTP2 := http2.ConfigureTransport(transport); errHTTP2 != nil {
		return nil, errHTTP2
	}
	proxy := &Proxy{manager: manager, config: currentConfig, logger: logger, ca: ca, dir: settings.StateDir, transport: transport, grants: make(map[string]grant), tunnels: make(map[net.Conn]struct{})}
	grantsPath := filepath.Join(settings.StateDir, "grants.yaml")
	data, errRead := os.ReadFile(grantsPath)
	if errRead != nil && !os.IsNotExist(errRead) {
		return nil, errRead
	}
	if len(data) > 0 {
		if errDecode := yaml.Unmarshal(data, &proxy.grants); errDecode != nil {
			return nil, fmt.Errorf("read agy grants: %w", errDecode)
		}
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}, Certificates: []tls.Certificate{pair}}
	protocols := make(map[string]func(*http.Server, *tls.Conn, http.Handler))
	proxy.server = &http.Server{Addr: settings.Listen, Handler: proxy, TLSConfig: tlsConfig, TLSNextProto: protocols}
	return proxy, nil
}

func (p *Proxy) Serve(listener net.Listener) error {
	errServe := p.server.ServeTLS(listener, "", "")
	if errServe != nil && !errors.Is(errServe, http.ErrServerClosed) {
		return errServe
	}
	return nil
}

func (p *Proxy) Close() {
	p.mu.Lock()
	p.closed = true
	for conn := range p.tunnels {
		if errClose := conn.Close(); errClose != nil {
			log.Debug("agy proxy: tunnel closed")
		}
	}
	p.mu.Unlock()
	p.transport.CloseIdleConnections()
	if errClose := p.server.Close(); errClose != nil {
		log.WithError(errClose).Warn("agy proxy: close server")
	}
}

func (p *Proxy) Certificate() []byte {
	public := make([]byte, len(p.ca.public))
	copy(public, p.ca.public)
	return public
}

func (p *Proxy) client(key string) (config.GoogleAccountClient, bool) {
	cfg := p.config()
	if cfg.AgyProxy.Listen == "" || key == "" || !slices.Contains(cfg.APIKeys, key) {
		return config.GoogleAccountClient{}, false
	}
	keyBytes := []byte(key)
	want := sha256.Sum256(keyBytes)
	var selected config.GoogleAccountClient
	matched := false
	for _, client := range cfg.GoogleAccountClients {
		candidateBytes := []byte(client.APIKey)
		candidate := sha256.Sum256(candidateBytes)
		if subtle.ConstantTimeCompare(want[:], candidate[:]) == 1 {
			selected = client
			matched = true
			break
		}
	}
	if !matched {
		return config.GoogleAccountClient{}, false
	}
	return selected, true
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	credentials := r.Clone(ctx)
	credentials.Header = r.Header.Clone()
	proxyAuth := credentials.Header.Get("Proxy-Authorization")
	credentials.Header.Set("Authorization", proxyAuth)
	username, key, okBasic := credentials.BasicAuth()
	client, okClient := p.client(key)
	if !okBasic || username != "agy" || !okClient || p.logger(key) == nil {
		w.Header().Set("Proxy-Authenticate", `Basic realm="cpamc-agy"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	host, port, errHost := net.SplitHostPort(r.Host)
	if errHost != nil || port != "443" || host == "" {
		http.Error(w, "only public HTTPS destinations on port 443 are permitted", http.StatusForbidden)
		return
	}
	leaf, errLeaf := p.ca.leaf(host)
	if errLeaf != nil {
		http.Error(w, "certificate unavailable", http.StatusInternalServerError)
		return
	}
	hijacker, okHijack := w.(http.Hijacker)
	if !okHijack {
		http.Error(w, "HTTP/1 CONNECT required", http.StatusHTTPVersionNotSupported)
		return
	}
	conn, buffered, errHijack := hijacker.Hijack()
	if errHijack != nil {
		return
	}
	defer func() {
		if errClose := conn.Close(); errClose != nil {
			log.Debug("agy proxy: connection closed")
		}
		p.mu.Lock()
		delete(p.tunnels, conn)
		p.mu.Unlock()
	}()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.tunnels[conn] = struct{}{}
	p.mu.Unlock()
	if _, errWrite := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); errWrite != nil {
		return
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{*leaf}, NextProtos: []string{"h2", "http/1.1"}}
	bufferedConn := &tunnelConn{Conn: conn, reader: buffered.Reader}
	secured := tls.Server(bufferedConn, tlsConfig)
	if errHandshake := secured.HandshakeContext(ctx); errHandshake != nil {
		return
	}
	serveInner := func(innerW http.ResponseWriter, inner *http.Request) {
		currentClient, valid := p.client(key)
		if !valid || currentClient.Name != client.Name {
			http.Error(innerW, "proxy access revoked", http.StatusForbidden)
			return
		}
		if inner.Host != host && inner.Host != r.Host {
			http.Error(innerW, "tunnel authority mismatch", http.StatusForbidden)
			return
		}
		canonicalHost := strings.ToLower(host)
		canonicalHost = strings.TrimSuffix(canonicalHost, ".")
		p.exchange(innerW, inner, canonicalHost, key, client)
	}
	handler := http.HandlerFunc(serveInner)
	state := secured.ConnectionState()
	if state.NegotiatedProtocol == "h2" {
		server := &http2.Server{}
		options := &http2.ServeConnOpts{Context: ctx, Handler: handler}
		server.ServeConn(secured, options)
		return
	}
	done := make(chan struct{})
	listener := &tunnelListener{conn: secured, done: done}
	server := &http.Server{Handler: handler, ConnState: func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			listener.once.Do(func() { close(done) })
		}
	}}
	if errServe := server.Serve(listener); errServe != nil && !errors.Is(errServe, net.ErrClosed) {
		log.Debug("agy proxy: tunnel finished")
	}
}

func dialPublic(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, errSplit := net.SplitHostPort(address)
	if errSplit != nil {
		return nil, errSplit
	}
	addresses, errLookup := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if errLookup != nil {
		return nil, errLookup
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("proxy destination has no addresses")
	}
	var dialer net.Dialer
	var errDial error
	var connected net.Conn
	for _, ip := range addresses {
		ip = ip.Unmap()
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return nil, fmt.Errorf("private proxy destination refused")
		}
	}
	for _, ip := range addresses {
		ipString := ip.String()
		target := net.JoinHostPort(ipString, port)
		conn, errConnect := dialer.DialContext(ctx, network, target)
		if errConnect == nil {
			connected = conn
			errDial = nil
			break
		}
		errDial = errConnect
	}
	if errDial != nil {
		return nil, errDial
	}
	return connected, nil
}

type tunnelConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *tunnelConn) Read(data []byte) (int, error) {
	n, errRead := c.reader.Read(data)
	return n, errRead
}

type tunnelListener struct {
	conn     net.Conn
	done     chan struct{}
	once     sync.Once
	accepted bool
}

func (l *tunnelListener) Accept() (net.Conn, error) {
	if l.accepted {
		<-l.done
		return nil, net.ErrClosed
	}
	l.accepted = true
	return l.conn, nil
}

func (l *tunnelListener) Close() error {
	l.once.Do(func() { close(l.done) })
	errClose := l.conn.Close()
	return errClose
}

func (l *tunnelListener) Addr() net.Addr {
	addr := l.conn.LocalAddr()
	return addr
}
