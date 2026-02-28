package public

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/pion/webrtc/v4"
)

// DefaultURL points to the always-online-stun project, which publishes a
// regularly-validated list of publicly reachable STUN servers, one host:port
// entry per line.
const DefaultURL = "https://raw.githubusercontent.com/pradt2/always-online-stun/master/valid_ipv4s.txt"

// maxServers is the maximum number of STUN servers returned by GetICEServers.
// ICE gathers candidates from every server simultaneously; returning hundreds
// of servers would fire hundreds of parallel STUN requests and slow gathering
// down rather than helping it.
const maxServers = 5

// WebSTUNProvider fetches a list of public STUN servers from a remote plaintext
// URL and returns them as ICE servers.  It implements wrtc.Provider.
//
// The remote file must contain one host:port entry per line, e.g.:
//
//	23.21.150.121:3478
//
// Lines that are empty or cannot be parsed as a valid host:port pair are
// silently skipped.
type WebSTUNProvider struct {
	// URL is the address of the plaintext STUN-server list to fetch.
	// Use DefaultURL for the always-online-stun community list.
	URL string
}

// NewWebSTUNProvider returns a WebSTUNProvider that fetches its server list
// from the given URL.
func NewWebSTUNProvider(url string) *WebSTUNProvider {
	return &WebSTUNProvider{URL: url}
}

// GetICEServers fetches the remote list and returns up to maxServers STUN
// servers formatted as pion ICEServer values.  The context controls the HTTP
// request lifetime.
func (p *WebSTUNProvider) GetICEServers(ctx context.Context) ([]webrtc.ICEServer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("building STUN list request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching STUN list from %s: %w", p.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("STUN list request returned HTTP %d", resp.StatusCode)
	}

	var servers []webrtc.ICEServer

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if len(servers) >= maxServers {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Each valid line is host:port.  Reject anything that does not parse
		// as such or whose host part is not a valid IP address.
		host, port, err := net.SplitHostPort(line)
		if err != nil || net.ParseIP(host) == nil || port == "" {
			continue
		}

		servers = append(servers, webrtc.ICEServer{
			URLs: []string{"stun:" + line},
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading STUN list: %w", err)
	}

	if len(servers) == 0 {
		return nil, fmt.Errorf("no valid STUN servers found at %s", p.URL)
	}

	return servers, nil
}
