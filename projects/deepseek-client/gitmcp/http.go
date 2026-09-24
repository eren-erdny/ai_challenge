package gitmcp

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTPHandler uses a personal shared bearer token, not an OAuth discovery flow.
func HTTPHandler(server *mcp.Server, token string) (http.Handler, error) {
	if len(token) < 32 || strings.ContainsAny(token, " \r\n\t") {
		return nil, fmt.Errorf("GIT_MCP_TOKEN must contain at least 32 non-whitespace characters")
	}
	want := sha256.Sum256([]byte("Bearer " + token))
	stream := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		got := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="git-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// This endpoint serves native clients only, not browser applications.
		if r.Header.Get("Origin") != "" {
			http.Error(w, "browser origins are not supported", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		stream.ServeHTTP(w, r)
	}), nil
}
