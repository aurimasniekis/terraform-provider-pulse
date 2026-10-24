package client

import (
	"context"
	"net/url"
	"time"
)

// KVBucket is a key-value bucket.
type KVBucket struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	History     int64  `json:"history"`
	TTLSeconds  int64  `json:"ttl_seconds"`
	MaxBytes    int64  `json:"max_bytes"`
}

// KVBucketInput creates or updates a KV bucket (max_bytes 0 or -1: unlimited).
type KVBucketInput struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	History     int64  `json:"history,omitempty"`
	TTLSeconds  int64  `json:"ttl_seconds,omitempty"`
	MaxBytes    int64  `json:"max_bytes,omitempty"`
}

// GetKVBucket returns a KV bucket's configuration.
func (c *Client) GetKVBucket(ctx context.Context, name string) (*KVBucket, error) {
	return get[KVBucket](ctx, c, "/kv/"+esc(name)+"/config", nil)
}

// CreateKVBucket creates a KV bucket.
func (c *Client) CreateKVBucket(ctx context.Context, in KVBucketInput) (*KVBucket, error) {
	return post[KVBucket](ctx, c, "/kv", in)
}

// UpdateKVBucket replaces a KV bucket's configuration.
func (c *Client) UpdateKVBucket(ctx context.Context, name string, in KVBucketInput) (*KVBucket, error) {
	in.Name = ""
	return put[KVBucket](ctx, c, "/kv/"+esc(name)+"/config", in)
}

// DeleteKVBucket deletes a KV bucket and all its keys.
func (c *Client) DeleteKVBucket(ctx context.Context, name string) error {
	return c.del(ctx, "/kv/"+esc(name), url.Values{"confirm": {name}})
}

// KVEntry is a key's current value.
type KVEntry struct {
	Key      string    `json:"key"`
	Revision uint64    `json:"revision"`
	Created  time.Time `json:"created"`
	Value    string    `json:"value"`
	Encoding string    `json:"encoding"` // utf8 or base64
	History  []struct {
		Operation string `json:"operation"` // put, delete, purge
	} `json:"history"` // newest first
}

// GetKVEntry returns a key's value (404 when the key does not exist or its latest revision is a delete or purge).
func (c *Client) GetKVEntry(ctx context.Context, bucket, key string) (*KVEntry, error) {
	path := "/kv/" + esc(bucket) + "/entry"
	e, err := get[KVEntry](ctx, c, path, url.Values{"key": {key}})
	if err != nil {
		return nil, err
	}
	if len(e.History) > 0 && e.History[0].Operation != "put" {
		return nil, notFound(path, "key "+key)
	}
	return e, nil
}

// PutKVEntry stores a value (encoding utf8 or base64) and returns the new revision.
func (c *Client) PutKVEntry(ctx context.Context, bucket, key, value, encoding string) (uint64, error) {
	out, err := call[struct {
		Revision uint64 `json:"revision"`
	}](ctx, c, &request{method: "PUT", path: "/kv/" + esc(bucket) + "/entry", query: url.Values{"key": {key}},
		body: map[string]string{"value": value, "encoding": encoding}})
	if err != nil {
		return 0, err
	}
	return out.Revision, nil
}

// DeleteKVEntry deletes a key (purge also drops its history).
func (c *Client) DeleteKVEntry(ctx context.Context, bucket, key string, purge bool) error {
	q := url.Values{"key": {key}}
	if purge {
		q.Set("purge", "true")
	}
	return c.del(ctx, "/kv/"+esc(bucket)+"/entry", q)
}

// ObjectBucket is an object store bucket.
type ObjectBucket struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	TTLSeconds  int64  `json:"ttl_seconds"`
	MaxBytes    int64  `json:"max_bytes"`
	Managed     bool   `json:"managed"`
}

// ObjectBucketInput creates or updates an object bucket (max_bytes 0 or -1: unlimited).
type ObjectBucketInput struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	TTLSeconds  int64  `json:"ttl_seconds,omitempty"`
	MaxBytes    int64  `json:"max_bytes,omitempty"`
}

// GetObjectBucket returns an object bucket's configuration.
func (c *Client) GetObjectBucket(ctx context.Context, name string) (*ObjectBucket, error) {
	return get[ObjectBucket](ctx, c, "/objects/"+esc(name)+"/config", nil)
}

// CreateObjectBucket creates an object bucket.
func (c *Client) CreateObjectBucket(ctx context.Context, in ObjectBucketInput) (*ObjectBucket, error) {
	return post[ObjectBucket](ctx, c, "/objects", in)
}

// UpdateObjectBucket replaces an object bucket's configuration.
func (c *Client) UpdateObjectBucket(ctx context.Context, name string, in ObjectBucketInput) (*ObjectBucket, error) {
	in.Name = ""
	return put[ObjectBucket](ctx, c, "/objects/"+esc(name)+"/config", in)
}

// DeleteObjectBucket deletes an object bucket and all its objects.
func (c *Client) DeleteObjectBucket(ctx context.Context, name string) error {
	return c.del(ctx, "/objects/"+esc(name), url.Values{"confirm": {name}})
}
