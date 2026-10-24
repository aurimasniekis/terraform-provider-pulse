package client

import (
	"context"
	"net/url"
	"strings"
)

// Stream is a JetStream stream as the admin API returns it.
type Stream struct {
	Name              string   `json:"name"`
	Kind              string   `json:"kind"`
	Managed           bool     `json:"managed"`
	Description       string   `json:"description"`
	Subjects          []string `json:"subjects"`
	Storage           string   `json:"storage"` // File or Memory
	Retention         string   `json:"retention"`
	Discard           string   `json:"discard"` // DiscardOld or DiscardNew
	MaxBytes          int64    `json:"max_bytes"`
	MaxMsgs           int64    `json:"max_msgs"`
	MaxAgeSeconds     int64    `json:"max_age_seconds"`
	MaxMsgSize        int64    `json:"max_msg_size"`
	Compression       string   `json:"compression"` // None or S2
	DuplicatesSeconds int64    `json:"duplicates_seconds"`
	AllowDirect       bool     `json:"allow_direct"`
}

// StorageName returns the storage type in the input vocabulary (file, memory).
func (s *Stream) StorageName() string { return strings.ToLower(s.Storage) }

// DiscardName returns the discard policy in the input vocabulary (old, new).
func (s *Stream) DiscardName() string {
	return strings.ToLower(strings.TrimPrefix(s.Discard, "Discard"))
}

// Compressed reports whether S2 compression is on.
func (s *Stream) Compressed() bool { return strings.EqualFold(s.Compression, "s2") }

// StreamInput creates or updates a user stream. Zero limits mean unlimited (-1); zero duplicates means the NATS
// default (2 minutes).
type StreamInput struct {
	Name              string   `json:"name"`
	Description       string   `json:"description,omitempty"`
	Subjects          []string `json:"subjects"`
	Storage           string   `json:"storage,omitempty"`
	MaxBytes          int64    `json:"max_bytes,omitempty"`
	MaxMsgs           int64    `json:"max_msgs,omitempty"`
	MaxAgeSeconds     int64    `json:"max_age_seconds,omitempty"`
	MaxMsgSize        int64    `json:"max_msg_size,omitempty"`
	Discard           string   `json:"discard,omitempty"`
	Compression       bool     `json:"compression,omitempty"`
	DuplicatesSeconds int64    `json:"duplicates_seconds,omitempty"`
	AllowDirect       bool     `json:"allow_direct,omitempty"`
}

// ListStreams returns every stream (including KV and object store backing streams).
func (c *Client) ListStreams(ctx context.Context) ([]Stream, error) {
	out, err := get[[]Stream](ctx, c, "/streams", nil)
	if err != nil {
		return nil, err
	}
	return *out, nil
}

// GetStream returns one stream.
func (c *Client) GetStream(ctx context.Context, name string) (*Stream, error) {
	return get[Stream](ctx, c, "/streams/"+esc(name), nil)
}

// CreateStream creates a user stream.
func (c *Client) CreateStream(ctx context.Context, in StreamInput) (*Stream, error) {
	return post[Stream](ctx, c, "/streams", in)
}

// UpdateStream updates a user stream (NATS refuses changes to immutable settings such as storage).
func (c *Client) UpdateStream(ctx context.Context, in StreamInput) (*Stream, error) {
	return put[Stream](ctx, c, "/streams/"+esc(in.Name), in)
}

// DeleteStream deletes a user stream and all its messages.
func (c *Client) DeleteStream(ctx context.Context, name string) error {
	return c.del(ctx, "/streams/"+esc(name), url.Values{"confirm": {name}})
}
