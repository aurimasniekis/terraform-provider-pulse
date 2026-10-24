package client

import "context"

// SystemInfo is the subset of GET /system/info the provider uses.
type SystemInfo struct {
	Version string `json:"version"`
	Profile string `json:"profile"`
	Ready   bool   `json:"ready"`
	Connect struct {
		CACert    string `json:"ca_cert"`
		NATSURL   string `json:"nats_url"`
		PublicURL string `json:"public_url"`
	} `json:"connect"`
	NATS struct {
		Connected  bool   `json:"connected"`
		ServerName string `json:"server_name"`
		Version    string `json:"version"`
	} `json:"nats"`
}

// GetSystemInfo returns the server's version and connection details.
func (c *Client) GetSystemInfo(ctx context.Context) (*SystemInfo, error) {
	return get[SystemInfo](ctx, c, "/system/info", nil)
}
