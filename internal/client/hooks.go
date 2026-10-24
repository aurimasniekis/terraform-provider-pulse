package client

import (
	"context"
)

// InboundHookConfig tunes the hmac and secret verifiers. Empty fields mean the server default.
type InboundHookConfig struct {
	Header          string `json:"header,omitempty"`
	Encoding        string `json:"encoding,omitempty"`
	Prefix          string `json:"prefix,omitempty"`
	TimestampHeader string `json:"timestamp_header,omitempty"`
	ToleranceSec    int64  `json:"tolerance_sec,omitempty"`
	Type            string `json:"type,omitempty"`
}

// InboundHook is an inbound webhook endpoint as the admin API returns it (never includes the secret).
type InboundHook struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Kind     string            `json:"kind"`
	Subject  string            `json:"subject"`
	Config   InboundHookConfig `json:"config"`
	Enabled  bool              `json:"enabled"`
	URL      string            `json:"url"`
	Received int64             `json:"received"`
	Rejected int64             `json:"rejected"`
}

// InboundHookInput creates or updates an inbound hook. An empty Subject means evt.hook.<name>.received. An empty
// Secret generates one on create and keeps the current one on update; RotateSecret (update only) generates a new one.
type InboundHookInput struct {
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	Subject      string            `json:"subject,omitempty"`
	Config       InboundHookConfig `json:"config"`
	Secret       string            `json:"secret,omitempty"`
	RotateSecret bool              `json:"rotate_secret,omitempty"`
	Enabled      bool              `json:"enabled"`
}

// InboundHookResult is a created or updated inbound hook with its secret (set only on create and rotation).
type InboundHookResult struct {
	Hook   InboundHook `json:"hook"`
	Secret string      `json:"secret,omitempty"`
}

// ListInboundHooks returns every inbound hook.
func (c *Client) ListInboundHooks(ctx context.Context) ([]InboundHook, error) {
	out, err := get[[]InboundHook](ctx, c, "/hooks/inbound", nil)
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// GetInboundHook finds one inbound hook by ID (the API has no single-hook read, so it scans the list).
func (c *Client) GetInboundHook(ctx context.Context, id string) (*InboundHook, error) {
	hs, err := c.ListInboundHooks(ctx)
	if err != nil {
		return nil, err
	}
	for i := range hs {
		if hs[i].ID == id {
			return &hs[i], nil
		}
	}
	return nil, notFound("/hooks/inbound", "inbound hook "+id)
}

// CreateInboundHook creates an inbound hook.
func (c *Client) CreateInboundHook(ctx context.Context, in InboundHookInput) (*InboundHookResult, error) {
	return post[InboundHookResult](ctx, c, "/hooks/inbound", in)
}

// UpdateInboundHook replaces an inbound hook's settings.
func (c *Client) UpdateInboundHook(ctx context.Context, id string, in InboundHookInput) (*InboundHookResult, error) {
	return put[InboundHookResult](ctx, c, "/hooks/inbound/"+esc(id), in)
}

// DeleteInboundHook deletes an inbound hook.
func (c *Client) DeleteInboundHook(ctx context.Context, id string) error {
	return c.del(ctx, "/hooks/inbound/"+esc(id), nil)
}

// OutboundHook is an outbound webhook as the admin API returns it (never includes the secret).
type OutboundHook struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Filter   string            `json:"filter"`
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
	Paused   bool              `json:"paused"`
	Consumer string            `json:"consumer"`
	Pending  uint64            `json:"pending"`
}

// OutboundHookInput creates or updates an outbound hook. An empty Secret generates one on create and keeps the
// current one on update; RotateSecret (update only) generates a new one.
type OutboundHookInput struct {
	Name         string            `json:"name"`
	Filter       string            `json:"filter"`
	URL          string            `json:"url"`
	Headers      map[string]string `json:"headers,omitempty"`
	Secret       string            `json:"secret,omitempty"`
	RotateSecret bool              `json:"rotate_secret,omitempty"`
}

// OutboundHookResult is a created or updated outbound hook with its secret (set only on create and rotation).
type OutboundHookResult struct {
	Hook   OutboundHook `json:"hook"`
	Secret string       `json:"secret,omitempty"`
}

// ListOutboundHooks returns every outbound hook.
func (c *Client) ListOutboundHooks(ctx context.Context) ([]OutboundHook, error) {
	out, err := get[[]OutboundHook](ctx, c, "/hooks/outbound", nil)
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// GetOutboundHook returns one outbound hook.
func (c *Client) GetOutboundHook(ctx context.Context, id string) (*OutboundHook, error) {
	return get[OutboundHook](ctx, c, "/hooks/outbound/"+esc(id), nil)
}

// CreateOutboundHook creates an outbound hook; it delivers events published after creation.
func (c *Client) CreateOutboundHook(ctx context.Context, in OutboundHookInput) (*OutboundHookResult, error) {
	return post[OutboundHookResult](ctx, c, "/hooks/outbound", in)
}

// UpdateOutboundHook replaces an outbound hook's settings (the delivery position is kept).
func (c *Client) UpdateOutboundHook(ctx context.Context, id string, in OutboundHookInput) (*OutboundHookResult, error) {
	return put[OutboundHookResult](ctx, c, "/hooks/outbound/"+esc(id), in)
}

// SetOutboundHookPaused pauses or resumes deliveries (the delivery position is kept).
func (c *Client) SetOutboundHookPaused(ctx context.Context, id string, paused bool) (*OutboundHook, error) {
	action := "/resume"
	if paused {
		action = "/pause"
	}
	return post[OutboundHook](ctx, c, "/hooks/outbound/"+esc(id)+action, nil)
}

// DeleteOutboundHook deletes an outbound hook, its consumer and its delivery log.
func (c *Client) DeleteOutboundHook(ctx context.Context, id string) error {
	return c.del(ctx, "/hooks/outbound/"+esc(id), nil)
}
