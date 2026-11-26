package agyproxy

// CUSTOM (cpamc-custom)

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	log "github.com/sirupsen/logrus"
)

func (p *Proxy) exchange(w http.ResponseWriter, r *http.Request, host, key string, client config.GoogleAccountClient) {
	logger := p.logger(key)
	if logger == nil {
		http.Error(w, "request logging unavailable", http.StatusServiceUnavailable)
		return
	}
	started := time.Now()
	var requestBody []byte
	if r.Body != nil {
		body, errRead := io.ReadAll(r.Body)
		errClose := r.Body.Close()
		if errRead != nil || errClose != nil {
			http.Error(w, "request body unavailable", http.StatusBadRequest)
			return
		}
		requestBody = body
	}
	requestURL := *r.URL
	requestURL.Scheme = "https"
	requestURL.Host = r.Host
	loggedURL := requestURL
	query := loggedURL.Query()
	for name := range query {
		if sensitive(name) {
			query.Set(name, "[MASKED]")
		}
	}
	loggedURL.RawQuery = query.Encode()
	requestHeaders := maskedHeaders(r.Header)
	readableRequest := readable(requestBody, r.Header)
	maskedRequest := maskBody(readableRequest)
	var response *http.Response
	if host == "oauth2.googleapis.com" && r.Method == http.MethodPost && r.URL.Path == "/token" {
		doc, status := p.token(requestBody, client)
		data := doc.Bytes()
		reader := bytes.NewReader(data)
		body := io.NopCloser(reader)
		headers := make(http.Header)
		headers.Set("Content-Type", "application/json")
		headers.Set("Cache-Control", "no-store")
		headers.Set("Pragma", "no-cache")
		response = &http.Response{StatusCode: status, Header: headers, Body: body, ContentLength: int64(len(data))}
	} else {
		ctx := r.Context()
		out := r.Clone(ctx)
		out.URL = &requestURL
		out.RequestURI = ""
		if r.Body != nil {
			reader := bytes.NewReader(requestBody)
			out.Body = io.NopCloser(reader)
		}
		out.Header.Del("Proxy-Authorization")
		if _, present := out.Header["User-Agent"]; !present {
			out.Header["User-Agent"] = nil
		}
		forwarded, errTrip := p.transport.RoundTrip(out)
		if errTrip != nil {
			headers := make(http.Header)
			headers.Set("Content-Type", "text/plain")
			data := []byte("upstream connection failed\n")
			reader := bytes.NewReader(data)
			body := io.NopCloser(reader)
			response = &http.Response{StatusCode: http.StatusBadGateway, Header: headers, Body: body}
		} else {
			response = forwarded
		}
	}
	arrived := time.Now()
	defer func() {
		if errClose := response.Body.Close(); errClose != nil {
			log.Debug("agy proxy: response closed")
		}
	}()
	for name, values := range response.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	if _, present := response.Header["Date"]; !present {
		w.Header()["Date"] = nil
	}
	if _, present := response.Header["Content-Type"]; !present {
		w.Header()["Content-Type"] = nil
	}
	for name := range response.Trailer {
		w.Header().Add("Trailer", name)
	}
	w.WriteHeader(response.StatusCode)
	var recorded bytes.Buffer
	chunk := make([]byte, 32*1024)
	controller := http.NewResponseController(w)
	for {
		n, errRead := response.Body.Read(chunk)
		if n > 0 {
			data := chunk[:n]
			recorded.Write(data)
			if _, errWrite := w.Write(data); errWrite != nil {
				break
			}
			if errFlush := controller.Flush(); errFlush != nil {
				break
			}
		}
		if errRead != nil {
			break
		}
	}
	for name, values := range response.Trailer {
		w.Header()[name] = values
	}
	responseHeaders := maskedHeaders(response.Header)
	maskedTrailers := maskedHeaders(response.Trailer)
	for name, values := range maskedTrailers {
		responseHeaders["Trailer-"+name] = values
	}
	responseData := recorded.Bytes()
	readableResponse := readable(responseData, response.Header)
	maskedResponse := maskBody(readableResponse)
	urlString := loggedURL.String()
	if errLog := logger.LogRequest(urlString, r.Method, requestHeaders, maskedRequest, response.StatusCode, responseHeaders, nil, nil, nil, maskedResponse, nil, nil, "", started, arrived); errLog != nil {
		log.WithError(errLog).Error("agy proxy: exchange logging failed")
	}
}

func readable(body []byte, headers http.Header) []byte {
	if len(body) == 0 {
		return body
	}
	source := bytes.NewReader(body)
	raw := io.NopCloser(source)
	reader, errReader := helps.DecodeResponseBody(raw, headers)
	if errReader != nil {
		return []byte("[unreadable encoded content]")
	}
	decoded, errRead := io.ReadAll(reader)
	errClose := reader.Close()
	if errRead != nil || errClose != nil {
		return []byte("[unreadable encoded content]")
	}
	return decoded
}
