package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// clientMetadata is an OAuth Client ID Metadata Document (MCP spec
// 2025-11-25 / draft-ietf-oauth-client-id-metadata-document): the
// client_id is the HTTPS URL this document is fetched from.
type clientMetadata struct {
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

// cimdFetcher fetches client metadata from client-controlled URLs, which
// is an SSRF vector by construction. Guards: https only, public IPs only
// (checked at dial time, after DNS resolution), one same-policy redirect,
// bounded size and time, JSON content type.
type cimdFetcher struct {
	client *http.Client
	// insecureLoopback is set only by tests to reach httptest servers.
	insecureLoopback bool
}

const cimdMaxBytes = 64 << 10

func newCIMDFetcher() *cimdFetcher {
	f := &cimdFetcher{}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	f.client = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				for _, ip := range ips {
					if f.publicIP(ip.IP) {
						return dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
					}
				}
				return nil, fmt.Errorf("host %q resolves to no public IP", host)
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 2 {
				return fmt.Errorf("too many redirects")
			}
			if err := f.checkURL(req.URL); err != nil {
				return err
			}
			return nil
		},
	}
	return f
}

func (f *cimdFetcher) publicIP(ip net.IP) bool {
	if f.insecureLoopback && ip.IsLoopback() {
		return true
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified())
}

func (f *cimdFetcher) checkURL(u *url.URL) error {
	if u.Scheme != "https" && !(f.insecureLoopback && u.Scheme == "http") {
		return fmt.Errorf("client_id must be an https URL")
	}
	if u.User != nil || u.Fragment != "" {
		return fmt.Errorf("client_id must not carry credentials or a fragment")
	}
	return nil
}

// fetch retrieves and validates the metadata document at clientID.
func (f *cimdFetcher) fetch(ctx context.Context, clientID string) (clientMetadata, error) {
	u, err := url.Parse(clientID)
	if err != nil || u.Host == "" {
		return clientMetadata{}, fmt.Errorf("client_id is not an absolute URL")
	}
	if err := f.checkURL(u); err != nil {
		return clientMetadata{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return clientMetadata{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return clientMetadata{}, fmt.Errorf("fetch client metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return clientMetadata{}, fmt.Errorf("client metadata fetch returned %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return clientMetadata{}, fmt.Errorf("client metadata must be application/json, got %q", ct)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, cimdMaxBytes+1))
	if err != nil {
		return clientMetadata{}, err
	}
	if len(body) > cimdMaxBytes {
		return clientMetadata{}, fmt.Errorf("client metadata exceeds %d bytes", cimdMaxBytes)
	}
	var meta clientMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return clientMetadata{}, fmt.Errorf("client metadata is not valid JSON: %w", err)
	}
	// The document must claim the URL it lives at, so a document can't be
	// replayed as a different client's identity.
	if meta.ClientID != clientID {
		return clientMetadata{}, fmt.Errorf("client metadata client_id %q does not match its URL", meta.ClientID)
	}
	if len(meta.RedirectURIs) == 0 {
		return clientMetadata{}, fmt.Errorf("client metadata declares no redirect_uris")
	}
	if meta.ClientName == "" {
		meta.ClientName = u.Host
	}
	return meta, nil
}

// redirectURIAllowed reports whether a requested redirect_uri matches
// the client's registered ones. Matching is exact, except loopback
// redirects (RFC 8252 §7.3): for http://localhost, http://127.0.0.1, and
// http://[::1] the port is ignored, because native clients like Claude
// Code register a portless loopback URI and listen on an ephemeral port.
func redirectURIAllowed(requested string, registered []string) bool {
	for _, reg := range registered {
		if requested == reg {
			return true
		}
	}
	req, err := url.Parse(requested)
	if err != nil || req.Scheme != "http" || !isLoopbackHost(req.Hostname()) {
		return false
	}
	for _, reg := range registered {
		r, err := url.Parse(reg)
		if err != nil || r.Scheme != "http" || !isLoopbackHost(r.Hostname()) {
			continue
		}
		if req.Hostname() == r.Hostname() && req.EscapedPath() == r.EscapedPath() {
			return true
		}
	}
	return false
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
