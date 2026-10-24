package client

import (
	"context"
	"fmt"
	"net/netip"
	"time"
)

// Ban is an active ban.
type Ban struct {
	Network   string    `json:"network"`
	Reason    string    `json:"reason"`
	Source    string    `json:"source"`
	Manual    bool      `json:"manual"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"` // zero: no expiry
}

// AllowEntry is an operator allowlist entry (a network that is never banned).
type AllowEntry struct {
	Network string `json:"network"`
	Note    string `json:"note"`
}

// SecurityOverview is the subset of GET /security the provider uses.
type SecurityOverview struct {
	Active    []Ban        `json:"active"`
	Allowlist []AllowEntry `json:"allowlist"`
}

// BanInput creates a manual ban. DurationHours 0 means no expiry.
type BanInput struct {
	Network       string `json:"network"`
	DurationHours int64  `json:"duration_hours"`
	Reason        string `json:"reason"`
}

type networkInput struct {
	Network string `json:"network"`
	Note    string `json:"note,omitempty"`
}

// GetSecurity returns active bans and the allowlist.
func (c *Client) GetSecurity(ctx context.Context) (*SecurityOverview, error) {
	return get[SecurityOverview](ctx, c, "/security", nil)
}

// GetBan returns the active ban for a network in canonical form (see BanNetworkKey).
func (c *Client) GetBan(ctx context.Context, network string) (*Ban, error) {
	s, err := c.GetSecurity(ctx)
	if err != nil {
		return nil, err
	}
	for i := range s.Active {
		if s.Active[i].Network == network {
			return &s.Active[i], nil
		}
	}
	return nil, notFound("/security", "active ban for "+network)
}

// CreateBan bans a network.
func (c *Client) CreateBan(ctx context.Context, in BanInput) (*Ban, error) {
	return post[Ban](ctx, c, "/security/bans", in)
}

// LiftBan lifts an active ban (404 when there is none).
func (c *Client) LiftBan(ctx context.Context, network string) error {
	_, err := post[struct{}](ctx, c, "/security/bans/lift", networkInput{Network: network})
	return err
}

// GetAllowEntry returns the allowlist entry for a network in canonical form (see AllowNetworkKey).
func (c *Client) GetAllowEntry(ctx context.Context, network string) (*AllowEntry, error) {
	s, err := c.GetSecurity(ctx)
	if err != nil {
		return nil, err
	}
	for i := range s.Allowlist {
		if s.Allowlist[i].Network == network {
			return &s.Allowlist[i], nil
		}
	}
	return nil, notFound("/security", "allowlist entry "+network)
}

// AllowNetwork adds a network to the allowlist or changes its note. It also lifts active bans inside it.
func (c *Client) AllowNetwork(ctx context.Context, network, note string) error {
	_, err := post[struct{}](ctx, c, "/security/allowlist", networkInput{Network: network, Note: note})
	return err
}

// DisallowNetwork removes an allowlist entry; network must be in canonical form (404 when there is no such entry).
func (c *Client) DisallowNetwork(ctx context.Context, network string) error {
	_, err := post[struct{}](ctx, c, "/security/allowlist/remove", networkInput{Network: network})
	return err
}

// BanNetworkKey returns the canonical form Pulse stores a ban under: a single IPv4 address becomes /32, a single IPv6
// address becomes its /64, and CIDRs are masked (IPv4-mapped IPv6 prefixes become IPv4).
func BanNetworkKey(s string) (string, error) {
	if a, err := netip.ParseAddr(s); err == nil {
		a = a.Unmap()
		if a.Is4() {
			return netip.PrefixFrom(a, 32).String(), nil
		}
		p, _ := a.Prefix(64)
		return p.String(), nil
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return "", fmt.Errorf("%q is not an IP address or CIDR", s)
	}
	if p.Addr().Is4In6() {
		p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
	}
	return p.Masked().String(), nil
}

// AllowNetworkKey returns the canonical form Pulse stores an allowlist entry under: a single address becomes a /32 or
// /128, and CIDRs are masked.
func AllowNetworkKey(s string) (string, error) {
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked().String(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return "", fmt.Errorf("%q is not an IP address or CIDR", s)
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()).String(), nil
}
