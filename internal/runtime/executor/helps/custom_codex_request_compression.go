package helps

// CUSTOM (cpamc-custom): zstd request-body compression for ChatGPT Codex HTTP requests.
// See docs/custom-fork-maintenance.md. The ChatGPT edge (Envoy) rejects uncompressed
// request bodies above 1 MiB with a 503 "upstream connect error ... before headers", so long
// Codex sessions (~230k tokens of context) failed on every retry through cpamc. Native Codex
// (feature enable_request_compression, default on) sends `Content-Encoding: zstd` at level 3
// for Responses requests on ChatGPT auth, which keeps the same sessions far below the limit.
//
// Only bodies of at least CodexRequestCompressionMinBytes are compressed: that keeps a wide
// margin below the 1 MiB edge limit, while small requests stay byte-identical to before.
//
// This is transport framing only: it runs after payload rules on the final body bytes and
// does not change payload semantics.

import (
	"bytes"
	"io"
	"net/http"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// CodexRequestCompressionMinBytes is the smallest body that gets compressed.
const CodexRequestCompressionMinBytes = 128 << 10

var (
	codexZstdEncoderOnce sync.Once
	codexZstdEncoder     *zstd.Encoder
	codexZstdEncoderErr  error
)

func codexZstd() (*zstd.Encoder, error) {
	codexZstdEncoderOnce.Do(func() {
		// SpeedDefault corresponds to zstd level 3, the level native Codex uses.
		codexZstdEncoder, codexZstdEncoderErr = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	})
	return codexZstdEncoder, codexZstdEncoderErr
}

// CompressCodexRequestBody replaces req's body with a zstd-compressed copy of body and sets
// Content-Encoding when body is large enough. On encoder failure the request is left
// uncompressed. It reports whether the body was compressed.
func CompressCodexRequestBody(req *http.Request, body []byte) bool {
	if req == nil || len(body) < CodexRequestCompressionMinBytes || req.Header.Get("Content-Encoding") != "" {
		return false
	}
	enc, err := codexZstd()
	if err != nil {
		return false
	}
	compressed := enc.EncodeAll(body, make([]byte, 0, len(body)/3))
	req.Body = io.NopCloser(bytes.NewReader(compressed))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(compressed)), nil
	}
	req.ContentLength = int64(len(compressed))
	req.Header.Set("Content-Encoding", "zstd")
	return true
}
