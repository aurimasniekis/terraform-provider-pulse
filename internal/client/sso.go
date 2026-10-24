package client

import (
	"context"
	"time"
)

// JITRules restrict just-in-time provisioning; every configured rule must match.
type JITRules struct {
	EmailDomains  []string `json:"email_domains"`
	Tenants       []string `json:"tenants"`
	HostedDomains []string `json:"hosted_domains"`
	Groups        []string `json:"groups"`
}

// SSOProvider is an OpenID Connect provider for dashboard sign-in. The client secret is write-only.
type SSOProvider struct {
	ID                        string     `json:"id"`
	Slug                      string     `json:"slug"`
	DisplayName               string     `json:"display_name"`
	Issuer                    string     `json:"issuer"`
	ClientID                  string     `json:"client_id"`
	HasClientSecret           bool       `json:"has_client_secret"`
	Scopes                    []string   `json:"scopes"`
	GroupsClaim               string     `json:"groups_claim"`
	Provisioning              string     `json:"provisioning"`
	JIT                       JITRules   `json:"jit"`
	AllowPrivateNetwork       bool       `json:"allow_private_network"`
	AllowCrossOriginEndpoints bool       `json:"allow_cross_origin_endpoints"`
	CABundlePEM               string     `json:"ca_bundle_pem"`
	StepUpFallback            string     `json:"stepup_fallback"`
	ACRValues                 []string   `json:"acr_values"`
	EndSessionEnabled         bool       `json:"end_session_enabled"`
	Enabled                   bool       `json:"enabled"`
	TestLoginOKAt             *time.Time `json:"test_login_ok_at,omitempty"`
	Version                   int64      `json:"version"`
	RedirectURI               string     `json:"redirect_uri"`
}

// SSOProviderInput creates or updates a provider. Nil fields are left unchanged on update. ClientSecret: nil keeps
// the stored secret, "" removes it. Version is required on update.
type SSOProviderInput struct {
	Version                   int64     `json:"version,omitempty"`
	Slug                      *string   `json:"slug,omitempty"`
	DisplayName               *string   `json:"display_name,omitempty"`
	Issuer                    *string   `json:"issuer,omitempty"`
	ClientID                  *string   `json:"client_id,omitempty"`
	ClientSecret              *string   `json:"client_secret,omitempty"`
	Scopes                    *[]string `json:"scopes,omitempty"`
	GroupsClaim               *string   `json:"groups_claim,omitempty"`
	Provisioning              *string   `json:"provisioning,omitempty"`
	JIT                       *JITRules `json:"jit,omitempty"`
	AllowPrivateNetwork       *bool     `json:"allow_private_network,omitempty"`
	AllowCrossOriginEndpoints *bool     `json:"allow_cross_origin_endpoints,omitempty"`
	CABundlePEM               *string   `json:"ca_bundle_pem,omitempty"`
	StepUpFallback            *string   `json:"stepup_fallback,omitempty"`
	ACRValues                 *[]string `json:"acr_values,omitempty"`
	EndSessionEnabled         *bool     `json:"end_session_enabled,omitempty"`
	Enabled                   *bool     `json:"enabled,omitempty"`
}

// ListSSOProviders returns every SSO provider.
func (c *Client) ListSSOProviders(ctx context.Context) ([]SSOProvider, error) {
	out, err := get[[]SSOProvider](ctx, c, "/sso/providers", nil)
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// GetSSOProvider returns one provider by ID.
func (c *Client) GetSSOProvider(ctx context.Context, id string) (*SSOProvider, error) {
	return get[SSOProvider](ctx, c, "/sso/providers/"+esc(id), nil)
}

// GetSSOProviderBySlug finds a provider by slug.
func (c *Client) GetSSOProviderBySlug(ctx context.Context, slug string) (*SSOProvider, error) {
	ps, err := c.ListSSOProviders(ctx)
	if err != nil {
		return nil, err
	}
	for i := range ps {
		if ps[i].Slug == slug {
			return &ps[i], nil
		}
	}
	return nil, notFound("/sso/providers", "SSO provider "+slug)
}

// CreateSSOProvider adds a provider.
func (c *Client) CreateSSOProvider(ctx context.Context, in SSOProviderInput) (*SSOProvider, error) {
	return post[SSOProvider](ctx, c, "/sso/providers", in)
}

// UpdateSSOProvider changes a provider; in.Version must be the provider's current version.
func (c *Client) UpdateSSOProvider(ctx context.Context, id string, in SSOProviderInput) (*SSOProvider, error) {
	return put[SSOProvider](ctx, c, "/sso/providers/"+esc(id), in)
}

// DeleteSSOProvider removes a provider with its mappings and linked identities.
func (c *Client) DeleteSSOProvider(ctx context.Context, id string) error {
	return c.del(ctx, "/sso/providers/"+esc(id), nil)
}

// SSOMapping maps one groups-claim value of a provider to a role.
type SSOMapping struct {
	ID         string `json:"id"`
	ProviderID string `json:"provider_id"`
	GroupValue string `json:"group_value"`
	Role       string `json:"role"`
}

// ListSSOMappings returns a provider's mappings ([] for an unknown provider).
func (c *Client) ListSSOMappings(ctx context.Context, providerID string) ([]SSOMapping, error) {
	out, err := get[[]SSOMapping](ctx, c, "/sso/providers/"+esc(providerID)+"/mappings", nil)
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// GetSSOMapping finds one mapping of a provider.
func (c *Client) GetSSOMapping(ctx context.Context, providerID, id string) (*SSOMapping, error) {
	ms, err := c.ListSSOMappings(ctx, providerID)
	if err != nil {
		return nil, err
	}
	for i := range ms {
		if ms[i].ID == id {
			return &ms[i], nil
		}
	}
	return nil, notFound("/sso/providers/"+providerID+"/mappings", "SSO mapping")
}

// CreateSSOMapping maps a group to a role.
func (c *Client) CreateSSOMapping(ctx context.Context, providerID, groupValue, role string) (*SSOMapping, error) {
	return post[SSOMapping](ctx, c, "/sso/providers/"+esc(providerID)+"/mappings",
		map[string]string{"group_value": groupValue, "role": role})
}

// DeleteSSOMapping removes a mapping.
func (c *Client) DeleteSSOMapping(ctx context.Context, providerID, id string) error {
	return c.del(ctx, "/sso/providers/"+esc(providerID)+"/mappings/"+esc(id), nil)
}

// AuthSettings are the server-wide sign-in settings.
type AuthSettings struct {
	LocalLoginDisabled bool `json:"local_login_disabled"`
}

// GetAuthSettings returns the sign-in settings.
func (c *Client) GetAuthSettings(ctx context.Context) (*AuthSettings, error) {
	return get[AuthSettings](ctx, c, "/sso/settings", nil)
}

// PutAuthSettings changes the sign-in settings.
func (c *Client) PutAuthSettings(ctx context.Context, in AuthSettings) (*AuthSettings, error) {
	return put[AuthSettings](ctx, c, "/sso/settings", in)
}
