package httpauth

import (
	"net/http"
	"slices"
)

// CORS enables browser access to the REST API from separately hosted web
// UIs (the sample UI stays out of the core image by design; see design doc
// §5). Origins are matched exactly against the allowlist; with an empty
// allowlist no CORS headers are emitted. Preflight requests short-circuit
// here because browsers send them without the Authorization header.
func CORS(allowed []string, next http.Handler) http.Handler {
	if len(allowed) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && slices.Contains(allowed, origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				h.Set("Access-Control-Max-Age", "3600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
