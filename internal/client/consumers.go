package client

import (
	"context"
	"net/url"
	"time"
)

// Consumer is a JetStream consumer as the admin API returns it.
type Consumer struct {
	Name           string     `json:"name"`
	Durable        bool       `json:"durable"`
	Description    string     `json:"description"`
	FilterSubjects []string   `json:"filter_subjects"`
	DeliverPolicy  string     `json:"deliver_policy"`
	AckPolicy      string     `json:"ack_policy"`
	AckWaitSeconds int64      `json:"ack_wait_seconds"`
	MaxDeliver     int64      `json:"max_deliver"`
	MaxAckPending  int64      `json:"max_ack_pending"`
	OptStartSeq    uint64     `json:"opt_start_seq,omitempty"`
	OptStartTime   *time.Time `json:"opt_start_time,omitempty"`
	Internal       bool       `json:"internal"`
}

// ConsumerInput creates or updates a durable pull consumer. Zero values mean the server defaults.
type ConsumerInput struct {
	Description    string     `json:"description,omitempty"`
	FilterSubjects []string   `json:"filter_subjects"`
	DeliverPolicy  string     `json:"deliver_policy,omitempty"`
	OptStartSeq    uint64     `json:"opt_start_seq,omitempty"`
	OptStartTime   *time.Time `json:"opt_start_time,omitempty"`
	AckWaitSeconds int64      `json:"ack_wait_seconds,omitempty"`
	MaxDeliver     int64      `json:"max_deliver,omitempty"`
	MaxAckPending  int64      `json:"max_ack_pending,omitempty"`
}

func consumerPath(stream, name string) string {
	return "/streams/" + esc(stream) + "/consumers/" + esc(name)
}

// GetConsumer returns one consumer.
func (c *Client) GetConsumer(ctx context.Context, stream, name string) (*Consumer, error) {
	return get[Consumer](ctx, c, consumerPath(stream, name), nil)
}

// PutConsumer creates or updates a durable consumer.
func (c *Client) PutConsumer(ctx context.Context, stream, name string, in ConsumerInput) (*Consumer, error) {
	return put[Consumer](ctx, c, consumerPath(stream, name), in)
}

// DeleteConsumer deletes a consumer.
func (c *Client) DeleteConsumer(ctx context.Context, stream, name string) error {
	return c.del(ctx, consumerPath(stream, name), url.Values{"confirm": {name}})
}
