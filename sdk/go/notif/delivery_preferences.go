package notif

import (
	"context"
	"net/url"
)

type ChannelPolicy struct {
	Mode            string   `json:"mode"`
	Destinations    []string `json:"destinations"`
	FollowupSeconds int      `json:"followup_seconds"`
}
type ChannelOverride struct {
	Source Target        `json:"source"`
	Policy ChannelPolicy `json:"policy"`
}
type DeliveryPreferences struct {
	PersonID  string            `json:"person_id"`
	Version   int64             `json:"version"`
	Default   *ChannelPolicy    `json:"default"`
	Overrides []ChannelOverride `json:"overrides"`
}

func (c *Client) DeliveryPreferences(ctx context.Context, person string) (DeliveryPreferences, error) {
	var out DeliveryPreferences
	err := c.Do(ctx, "GET", "/api/v1/people/"+url.PathEscape(person)+"/delivery-preferences", nil, &out, "")
	return out, err
}
func (c *Client) SaveDeliveryPreferences(ctx context.Context, person string, in DeliveryPreferences) (DeliveryPreferences, error) {
	var out DeliveryPreferences
	err := c.Do(ctx, "POST", "/api/v1/people/"+url.PathEscape(person)+"/delivery-preferences", in, &out, "")
	return out, err
}
