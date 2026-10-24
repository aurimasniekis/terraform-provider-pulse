package client

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Role sources in a user's role assignments.
const (
	RoleSourceManual = "manual"
	RoleSourceSSO    = "sso"
)

// RoleAssignment records where one of a user's roles came from.
type RoleAssignment struct {
	Role      string `json:"role"`
	Source    string `json:"source"` // manual or sso
	SourceRef string `json:"source_ref,omitempty"`
}

// User is a dashboard user as the admin API returns it (no secrets).
type User struct {
	ID              string           `json:"id"`
	Email           string           `json:"email,omitempty"`
	DisplayName     string           `json:"display_name"`
	Enabled         bool             `json:"enabled"`
	ProvisionedVia  string           `json:"provisioned_via"`
	Roles           []string         `json:"roles"`
	RoleAssignments []RoleAssignment `json:"role_assignments"`
	HasPassword     bool             `json:"has_password"`
	MFAEnrolled     bool             `json:"mfa_enrolled"`
	MFARequired     bool             `json:"mfa_required"`
	Version         int64            `json:"version"`
}

// RolesFrom returns the distinct roles assigned from the given source, sorted.
func (u *User) RolesFrom(source string) []string {
	out := []string{}
	for _, a := range u.RoleAssignments {
		if a.Source == source && !slices.Contains(out, a.Role) {
			out = append(out, a.Role)
		}
	}
	slices.Sort(out)
	return out
}

// UserCreateInput creates a user. The user has no password: they sign in with SSO or set one through a link an
// administrator issues.
type UserCreateInput struct {
	Email       string   `json:"email,omitempty"`
	DisplayName string   `json:"display_name"`
	Roles       []string `json:"roles"`
	MFARequired bool     `json:"mfa_required,omitempty"`
}

// UserUpdateInput changes a user; nil fields are left unchanged. Roles replaces the manually assigned roles (SSO-mapped
// roles are kept).
type UserUpdateInput struct {
	DisplayName *string   `json:"display_name,omitempty"`
	Roles       *[]string `json:"roles,omitempty"`
	Enabled     *bool     `json:"enabled,omitempty"`
	MFARequired *bool     `json:"mfa_required,omitempty"`
}

// NormalizeEmail lower-cases and trims an email address the way Pulse stores it.
func NormalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// ListUsers returns every user.
func (c *Client) ListUsers(ctx context.Context) ([]User, error) {
	out, err := get[[]User](ctx, c, "/users", nil)
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// GetUser returns one user.
func (c *Client) GetUser(ctx context.Context, id string) (*User, error) {
	return get[User](ctx, c, "/users/"+esc(id), nil)
}

// GetUserByEmail finds a user by email (case-insensitive).
func (c *Client) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	us, err := c.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	e := NormalizeEmail(email)
	for i := range us {
		if us[i].Email != "" && us[i].Email == e {
			return &us[i], nil
		}
	}
	return nil, notFound("/users", "user with email "+e)
}

// CreateUser creates a user.
func (c *Client) CreateUser(ctx context.Context, in UserCreateInput) (*User, error) {
	if in.Roles == nil {
		in.Roles = []string{}
	}
	return post[User](ctx, c, "/users", in)
}

// UpdateUser changes a user's name, roles, MFA requirement or status.
func (c *Client) UpdateUser(ctx context.Context, id string, in UserUpdateInput) (*User, error) {
	return patch[User](ctx, c, "/users/"+esc(id), in)
}

// DeleteUser deletes a user with their sessions, identities and tokens.
func (c *Client) DeleteUser(ctx context.Context, id string) error {
	return c.del(ctx, "/users/"+esc(id), nil)
}

// Identity is an SSO identity (issuer + subject) linked to a user.
type Identity struct {
	Issuer     string `json:"issuer"`
	Subject    string `json:"subject"`
	ProviderID string `json:"provider_id"`
	UserID     string `json:"user_id"`
	LinkedVia  string `json:"linked_via"`
}

// ListUserIdentities returns a user's linked SSO identities ([] for an unknown user).
func (c *Client) ListUserIdentities(ctx context.Context, userID string) ([]Identity, error) {
	out, err := get[[]Identity](ctx, c, "/users/"+esc(userID)+"/identities", nil)
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// GetUserIdentity finds a user's identity by provider and subject.
func (c *Client) GetUserIdentity(ctx context.Context, userID, providerID, subject string) (*Identity, error) {
	ids, err := c.ListUserIdentities(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range ids {
		if ids[i].ProviderID == providerID && ids[i].Subject == subject {
			return &ids[i], nil
		}
	}
	return nil, notFound("/users/"+userID+"/identities", "identity")
}

// LinkUserIdentity links the identity (the provider's issuer + subject) to a user. Linking an identity that is already
// linked to the same user succeeds; one linked to another user is a conflict.
func (c *Client) LinkUserIdentity(ctx context.Context, userID, providerID, subject string) error {
	return c.exec(ctx, &request{method: http.MethodPost, path: "/users/" + esc(userID) + "/identities",
		body: map[string]string{"provider_id": providerID, "subject": subject}})
}

// UnlinkUserIdentity removes a linked identity; the user loses their SSO-mapped roles and SSO sessions.
func (c *Client) UnlinkUserIdentity(ctx context.Context, userID, issuer, subject string) error {
	return c.del(ctx, "/users/"+esc(userID)+"/identities", url.Values{"issuer": {issuer}, "subject": {subject}})
}
