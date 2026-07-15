package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// metadataTokenSource supplies OAuth access tokens from the GCE metadata
// server for Cloud SQL IAM database authentication: the token is used as
// the connection password, so no database password exists anywhere.
// Tokens are cached and refreshed shortly before expiry; authentication
// happens per new connection, so pooled connections are unaffected.
type metadataTokenSource struct {
	mu      sync.Mutex
	token   string
	expires time.Time
}

func (s *metadataTokenSource) password(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.expires) {
		return s.token, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Cloud SQL IAM auth needs the GCE metadata server (Cloud Run/GCE only): %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata token: %s: %s", resp.Status, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("metadata token: unexpected response")
	}
	s.token = out.AccessToken
	// Refresh well before expiry so new connections never race it.
	s.expires = time.Now().Add(time.Duration(out.ExpiresIn)*time.Second - 5*time.Minute)
	return s.token, nil
}
