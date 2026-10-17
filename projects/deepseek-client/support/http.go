package support

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

//go:embed ui.html
var ui string

func Handler(service *Service, token, host string, shutdown func()) http.Handler {
	busy := make(chan struct{}, 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Host != host {
			http.Error(w, "Invalid host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+host {
			http.Error(w, "Invalid origin", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/" && r.Method == "GET" {
			b := make([]byte, 18)
			if _, e := rand.Read(b); e != nil {
				http.Error(w, "Unavailable", 500)
				return
			}
			nonce := base64.StdEncoding.EncodeToString(b)
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+"'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, strings.ReplaceAll(ui, "{{NONCE}}", nonce))
			return
		}
		if r.URL.Path == "/health" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, "{\"status\":\"ok\"}")
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "Unauthorized", 401)
			return
		}
		supplied := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if len(token) < 32 || len(supplied) != len(token) || subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		send := func(value any) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			json.NewEncoder(w).Encode(value)
		}
		switch {
		case r.URL.Path == "/api/shutdown" && r.Method == "POST":
			send(map[string]bool{"stopping": true})
			if shutdown != nil {
				go shutdown()
			}
		case r.URL.Path == "/api/tickets" && r.Method == "GET":
			list, e := service.Tickets(r.Context())
			if e != nil {
				http.Error(w, "CRM unavailable", 502)
				return
			}
			send(list)
		case r.URL.Path == "/api/ask" && r.Method == "POST":
			mediaType, _, mediaError := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mediaError != nil || mediaType != "application/json" {
				http.Error(w, "Expected application/json", 415)
				return
			}
			data, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
			if e != nil {
				http.Error(w, "Request too large", 413)
				return
			}
			var req AskRequest
			if strict(data, &req) != nil {
				http.Error(w, "Invalid request", 400)
				return
			}
			select {
			case busy <- struct{}{}:
				defer func() { <-busy }()
			default:
				http.Error(w, "Request already running", 429)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
			defer cancel()
			result, e := service.Ask(ctx, req)
			if e != nil {
				http.Error(w, e.Error(), 422)
				return
			}
			send(result)
		default:
			http.NotFound(w, r)
		}
	})
}
