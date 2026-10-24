package client

import (
	"context"
)

// AlertRuleParams are a rule's tunables; each kind uses a subset. Zero values are omitted.
type AlertRuleParams struct {
	Days          float64 `json:"days,omitempty"`
	Ratio         float64 `json:"ratio,omitempty"`
	Hours         float64 `json:"hours,omitempty"`
	Count         int64   `json:"count,omitempty"`
	WindowMinutes int64   `json:"window_minutes,omitempty"`

	// Threshold rules.
	Stream    string  `json:"stream,omitempty"`
	Dimension string  `json:"dimension,omitempty"`
	Key       string  `json:"key,omitempty"`
	Metric    string  `json:"metric,omitempty"`
	Op        string  `json:"op,omitempty"`
	Threshold float64 `json:"threshold,omitempty"`
}

// AlertRule is a built-in or custom alert rule (without its live state).
type AlertRule struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Builtin     bool            `json:"builtin"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Enabled     bool            `json:"enabled"`
	Severity    string          `json:"severity"`
	Params      AlertRuleParams `json:"params"`
	ForSeconds  int64           `json:"for_seconds"`
}

// AlertRuleInput creates or updates a rule. Built-in rules ignore Name.
type AlertRuleInput struct {
	Name       string          `json:"name,omitempty"`
	Enabled    bool            `json:"enabled"`
	Severity   string          `json:"severity"`
	Params     AlertRuleParams `json:"params"`
	ForSeconds int64           `json:"for_seconds,omitempty"`
}

type alertsOverview struct {
	Rules []AlertRule `json:"rules"`
}

// ListAlertRules returns every built-in and custom rule.
func (c *Client) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	out, err := get[alertsOverview](ctx, c, "/alerts", nil)
	if err != nil {
		return nil, err
	}
	return out.Rules, nil
}

// GetAlertRule finds one rule by ID (built-in rules use their kind as ID). The API has no single-rule read, so it
// scans the list.
func (c *Client) GetAlertRule(ctx context.Context, id string) (*AlertRule, error) {
	rules, err := c.ListAlertRules(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i], nil
		}
	}
	return nil, notFound("/alerts", "alert rule "+id)
}

// CreateAlertRule creates a custom threshold rule.
func (c *Client) CreateAlertRule(ctx context.Context, in AlertRuleInput) (*AlertRule, error) {
	return post[AlertRule](ctx, c, "/alerts/rules", in)
}

// UpdateAlertRule updates a built-in or custom rule.
func (c *Client) UpdateAlertRule(ctx context.Context, id string, in AlertRuleInput) (*AlertRule, error) {
	return put[AlertRule](ctx, c, "/alerts/rules/"+esc(id), in)
}

// DeleteAlertRule deletes a custom rule (built-in rules cannot be deleted: 409).
func (c *Client) DeleteAlertRule(ctx context.Context, id string) error {
	return c.del(ctx, "/alerts/rules/"+esc(id), nil)
}
