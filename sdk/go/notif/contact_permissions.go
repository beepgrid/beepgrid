package notif

import "context"

// PermissionRule defaults to allow. Explicit deny is limited to communication
// toward exact contacts/groups and wins over matching grants.
type PermissionRule struct {
	Effect     string `json:"effect,omitempty"`
	Action     string `json:"action"`
	TargetKind string `json:"target_kind"`
	TargetID   string `json:"target_id"`
}
type ContactPermissionsInput struct {
	Subject Target           `json:"subject"`
	Rules   []PermissionRule `json:"rules"`
	Version int64            `json:"version"`
}
type ContactPreviewInput struct {
	Subject Target            `json:"subject"`
	Rules   *[]PermissionRule `json:"rules,omitempty"`
	Version int64             `json:"version"`
	Targets []Target          `json:"targets,omitempty"`
}

func (c *Client) PreviewContactPermissions(ctx context.Context, in ContactPreviewInput) (map[string]any, error) {
	var out map[string]any
	err := c.Do(ctx, "POST", "/api/v1/contact-permissions/preview", in, &out, "")
	return out, err
}
func (c *Client) SaveContactPermissions(ctx context.Context, in ContactPermissionsInput) (map[string]any, error) {
	var out map[string]any
	err := c.Do(ctx, "POST", "/api/v1/contact-permissions", in, &out, "")
	return out, err
}
