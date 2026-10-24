package client

import (
	"context"
	"net/http"
	"time"
)

// Grant is one permission of a client or HTTP token.
type Grant struct {
	Action  string `json:"action"`
	Subject string `json:"subject,omitempty"`
	Stream  string `json:"stream,omitempty"`
	Name    string `json:"name,omitempty"`
}

// NATSClient is a tool's NATS identity (a "client" in Pulse).
type NATSClient struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	Grants      []Grant   `json:"grants"`
	UserKey     string    `json:"user_key"`
	Status      string    `json:"status"` // active, revoked (or expired)
	ExpiresAt   time.Time `json:"expires_at"`
	Version     int64     `json:"version"`
}

// ClientInput creates a client.
type ClientInput struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Tags          []string `json:"tags"`
	Grants        []Grant  `json:"grants"`
	ExpiresInDays int64    `json:"expires_in_days,omitempty"`
}

// IssuedClient is a client with its .creds file, returned once at creation and rotation.
type IssuedClient struct {
	Client NATSClient `json:"client"`
	Creds  string     `json:"creds"`
}

// ListClients returns every client, revoked ones included.
func (c *Client) ListClients(ctx context.Context) ([]NATSClient, error) {
	out, err := get[[]NATSClient](ctx, c, "/clients", nil)
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// GetClient returns one client.
func (c *Client) GetClient(ctx context.Context, id string) (*NATSClient, error) {
	return get[NATSClient](ctx, c, "/clients/"+esc(id), nil)
}

// FindClient returns the client with the given name.
func (c *Client) FindClient(ctx context.Context, name string) (*NATSClient, error) {
	all, err := c.ListClients(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, notFound("/clients", "client "+name)
}

// CreateClient creates a client and returns its credentials.
func (c *Client) CreateClient(ctx context.Context, in ClientInput) (*IssuedClient, error) {
	return post[IssuedClient](ctx, c, "/clients", in)
}

// UpdateClient changes a client's name, description and tags (version is the current version).
func (c *Client) UpdateClient(ctx context.Context, id string, version int64, name, description string, tags []string) (*NATSClient, error) {
	return put[NATSClient](ctx, c, "/clients/"+esc(id), map[string]any{"name": name, "description": description, "tags": tags, "version": version})
}

// UpdateClientGrants replaces a client's grants; its live connections pick them up.
func (c *Client) UpdateClientGrants(ctx context.Context, id string, version int64, grants []Grant) (*NATSClient, error) {
	return put[NATSClient](ctx, c, "/clients/"+esc(id)+"/grants", map[string]any{"grants": grants, "version": version})
}

// RotateClient issues new credentials; the old ones stop working.
func (c *Client) RotateClient(ctx context.Context, id string, expiresInDays int64) (*IssuedClient, error) {
	return post[IssuedClient](ctx, c, "/clients/"+esc(id)+"/rotate", map[string]any{"expires_in_days": expiresInDays})
}

// RevokeClient revokes a client permanently (its connections are closed).
func (c *Client) RevokeClient(ctx context.Context, id string) error {
	return c.exec(ctx, &request{method: http.MethodPost, path: "/clients/" + esc(id) + "/revoke"})
}

// DeleteClient removes a revoked client's record (the revocation itself stays), freeing its name.
func (c *Client) DeleteClient(ctx context.Context, id string) error {
	return c.del(ctx, "/clients/"+esc(id), nil)
}

// HTTPToken is a bearer token for the public HTTP API.
type HTTPToken struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Grants      []Grant   `json:"grants"`
	CIDRs       []string  `json:"cidrs"`
	Prefix      string    `json:"prefix"`
	Status      string    `json:"status"` // active, revoked, expired
	ExpiresAt   time.Time `json:"expires_at"`
}

// HTTPTokenInput creates an HTTP token.
type HTTPTokenInput struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Grants        []Grant  `json:"grants"`
	CIDRs         []string `json:"cidrs"`
	ExpiresInDays int64    `json:"expires_in_days,omitempty"`
}

// ListHTTPTokens returns every HTTP token.
func (c *Client) ListHTTPTokens(ctx context.Context) ([]HTTPToken, error) {
	out, err := get[[]HTTPToken](ctx, c, "/tokens", nil)
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// GetHTTPToken returns one HTTP token (from the list; there is no single-token read).
func (c *Client) GetHTTPToken(ctx context.Context, id string) (*HTTPToken, error) {
	all, err := c.ListHTTPTokens(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, notFound("/tokens", "HTTP token "+id)
}

// CreateHTTPToken creates a token and returns it with its secret (shown once).
func (c *Client) CreateHTTPToken(ctx context.Context, in HTTPTokenInput) (*HTTPToken, string, error) {
	out, err := post[struct {
		Token  HTTPToken `json:"token"`
		Secret string    `json:"secret"`
	}](ctx, c, "/tokens", in)
	if err != nil {
		return nil, "", err
	}
	return &out.Token, out.Secret, nil
}

// RevokeHTTPToken revokes a token.
func (c *Client) RevokeHTTPToken(ctx context.Context, id string) error {
	return c.exec(ctx, &request{method: http.MethodPost, path: "/tokens/" + esc(id) + "/revoke"})
}

// DeleteHTTPToken removes a revoked or expired token's record, freeing its name.
func (c *Client) DeleteHTTPToken(ctx context.Context, id string) error {
	return c.del(ctx, "/tokens/"+esc(id), nil)
}
