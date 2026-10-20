package embeddings

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// ErrURLNotAllowed is a URL an application may not register a model at
var ErrURLNotAllowed = errors.New("the URL is not allowed")

// URLPolicy says where the models applications register may be. gogi calls them from inside its
// own network, so a model's URL must not lead gogi to its own services, or to the cloud's metadata
// endpoint, which hands out the server's credentials. Addresses are checked when a model is
// registered and again each time it is called, as a name can come to resolve somewhere else
type URLPolicy struct {
	// lets models be on private and loopback addresses, e.g. a model served on the developer's
	// machine; link-local addresses, where the metadata endpoint is, are never allowed
	AllowPrivate bool
}

// Check checks the URL a model is registered at
func (p URLPolicy) Check(ctx context.Context, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%w: %q is not a URL", ErrURLNotAllowed, raw)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%w: a model is called over https or http, not %q", ErrURLNotAllowed, parsed.Scheme)
	}
	if parsed.User != nil {
		return fmt.Errorf("%w: a model's URL carries no credentials; name a credential instead", ErrURLNotAllowed)
	}

	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", parsed.Hostname())
	if err != nil {
		// a name that doesn't resolve is a model that can't be reached, not a URL that isn't allowed
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	for _, address := range addresses {
		if err := p.checkAddress(address); err != nil {
			return err
		}
	}
	return nil
}

func (p URLPolicy) checkAddress(address netip.Addr) error {
	address = address.Unmap()
	switch {
	case address.IsLinkLocalUnicast(), address.IsLinkLocalMulticast(), address.IsUnspecified(),
		address.IsMulticast(), address.IsInterfaceLocalMulticast():
		return fmt.Errorf("%w: %s is not an address a model can be at", ErrURLNotAllowed, address)
	case !p.AllowPrivate && (address.IsLoopback() || address.IsPrivate() || sharedAddressSpace.Contains(address)):
		return fmt.Errorf("%w: %s is on a private network", ErrURLNotAllowed, address)
	}
	return nil
}

// 100.64.0.0/10, the carrier-grade NAT range, is private in all but name
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// HTTPClient calls registered models, refusing to connect to an address the policy doesn't allow
func (p URLPolicy) HTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			return p.checkAddress(ip)
		},
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	// a proxy would connect for gogi, out of the dialer's sight
	transport.Proxy = nil
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// a redirect is followed by the same dialer, but it's not where the model was registered
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
