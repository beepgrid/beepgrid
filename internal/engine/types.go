package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *Error) Error() string      { return e.Code + ": " + e.Message }
func Bad(message string) error      { return &Error{"INVALID_ARGUMENT", message, 400} }
func Denied() error                 { return &Error{"POLICY_DENIED", "This operation is not permitted.", 403} }
func Missing() error                { return &Error{"NOT_FOUND", "The resource is unavailable.", 404} }
func Conflict(message string) error { return &Error{"CONFLICT", message, 409} }
func ErrorStatus(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 500
}
func ID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
func Secret() string         { return ID("ntf") + ID("") }
func Digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func JSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func ValidText(s string, max int) bool { return strings.TrimSpace(s) != "" && len(s) <= max }

// Principal is private engine machinery. Public resource collections are people, agents and services.
type Principal struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	ExternalID string         `json:"external_id"`
	Name       string         `json:"name"`
	Metadata   map[string]any `json:"metadata"`
	Disabled   bool           `json:"disabled"`
	CreatedAt  time.Time      `json:"created_at"`
}
type Person struct {
	Principal
	Department string `json:"department,omitempty"`
	ManagerID  string `json:"manager_id,omitempty"`
}
type Agent struct {
	Principal
	Instances []Instance `json:"instances"`
}
type Actor struct {
	ProviderConnection string `json:"-"`

	Principal
	Scope          string `json:"scope"`
	CredentialID   string `json:"-"`
	CredentialKind string `json:"-"`
	Prefix         string `json:"-"`
	InstanceID     string `json:"instance_id,omitempty"`
	Admin          bool   `json:"admin"`
}
type CreatePerson struct {
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Department string `json:"department"`
	ManagerID  string `json:"manager_id"`
	Email      string `json:"email"`
}
type EnrollInput struct {
	Agent        string         `json:"agent"`
	Name         string         `json:"name"`
	Capabilities []string       `json:"capabilities"`
	Metadata     map[string]any `json:"metadata"`
}
type Instance struct {
	ID           string         `json:"id"`
	AgentID      string         `json:"agent_id"`
	Capabilities []string       `json:"capabilities"`
	Progress     map[string]any `json:"progress"`
	LastSeen     time.Time      `json:"last_seen"`
	Presence     string         `json:"presence"`
}
type Session struct {
	Token      string    `json:"token"`
	Agent      Principal `json:"agent"`
	InstanceID string    `json:"instance_id"`
	ExpiresAt  time.Time `json:"expires_at"`
}
type Credential struct {
	ID          string     `json:"id"`
	PrincipalID string     `json:"principal_id"`
	Kind        string     `json:"kind"`
	Prefix      string     `json:"prefix"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Revoked     bool       `json:"revoked"`
}
type Rule struct {
	Action       string `json:"action"`
	TargetKind   string `json:"target_kind"`
	TargetID     string `json:"target_id"`
	ConnectionID string `json:"connection_id,omitempty"`
	ActionName   string `json:"action_name,omitempty"`
}
type Role struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Rules []Rule `json:"rules"`
}
type Binding struct {
	RoleID      string `json:"role_id"`
	SubjectKind string `json:"subject_kind"`
	SubjectID   string `json:"subject_id"`
}
type Group struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
}
type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type Conversation struct {
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	CreatedBy    string      `json:"created_by"`
	CreatedAt    time.Time   `json:"created_at"`
	Participants []Principal `json:"participants"`
	Messages     []Message   `json:"messages,omitempty"`
}
type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	SenderID       string    `json:"sender_id"`
	InstanceID     string    `json:"instance_id,omitempty"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"created_at"`
}
type SendInput struct {
	Target         Target `json:"target"`
	ConversationID string `json:"conversation_id"`
	Body           string `json:"body"`
	Title          string `json:"title"`
}
type SendResult struct {
	Messages       []Message `json:"messages"`
	ConversationID string    `json:"conversation_id"`
}
type Action struct {
	Name            string         `json:"name"`
	ExecutorID      string         `json:"executor_id"`
	Resource        string         `json:"resource"`
	ResourceVersion string         `json:"resource_version"`
	Arguments       map[string]any `json:"arguments"`
	OperationKey    string         `json:"operation_key"`
}
type AskInput struct {
	Target         Target    `json:"target"`
	ConversationID string    `json:"conversation_id"`
	Kind           string    `json:"kind"`
	Title          string    `json:"title"`
	Body           string    `json:"body"`
	Choices        []string  `json:"choices"`
	ExpiresAt      time.Time `json:"expires_at"`
	Action         *Action   `json:"action,omitempty"`
	SharedKey      string    `json:"shared_key"`
}
type Request struct {
	ID             string     `json:"id"`
	RequesterID    string     `json:"requester_id"`
	ConversationID string     `json:"conversation_id"`
	Kind           string     `json:"kind"`
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	Choices        []string   `json:"choices"`
	Action         *Action    `json:"action,omitempty"`
	State          string     `json:"state"`
	Version        int        `json:"version"`
	Response       *Response  `json:"response,omitempty"`
	ResolvedBy     *string    `json:"resolved_by,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	ExpiresAt      time.Time  `json:"expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
	SharedKey      string     `json:"shared_key"`
}
type Response struct {
	Outcome string `json:"outcome"`
	Answer  any    `json:"answer,omitempty"`
	Version int    `json:"version"`
}
type Execution struct {
	ID           string `json:"id"`
	RequestID    string `json:"request_id"`
	ExecutorID   string `json:"executor_id"`
	OperationKey string `json:"operation_key"`
	Action       Action `json:"action"`
	State        string `json:"state"`
	Result       any    `json:"result"`
}
type Delivery struct {
	Address string `json:"address"`

	ID           string         `json:"id"`
	RecipientID  string         `json:"recipient_id"`
	OperationID  string         `json:"operation_id"`
	Payload      map[string]any `json:"payload"`
	State        string         `json:"state"`
	Generation   int64          `json:"generation"`
	Attempts     int            `json:"attempts"`
	LastError    string         `json:"last_error"`
	ConnectionID string         `json:"connection_id"`
	ExpiresAt    time.Time      `json:"expires_at"`
}
type Event struct {
	Sequence       int64     `json:"sequence"`
	Kind           string    `json:"kind"`
	ActorID        string    `json:"actor_id"`
	ConversationID string    `json:"conversation_id"`
	RecipientID    string    `json:"recipient_id"`
	Data           any       `json:"data"`
	CreatedAt      time.Time `json:"created_at"`
}

func (t Target) Validate() error {
	if !ValidText(t.ID, 256) {
		return Bad("A target ID is required.")
	}
	switch t.Kind {
	case "person", "agent", "service", "group", "instance":
		return nil
	}
	return Bad(fmt.Sprintf("Unsupported target kind %q.", t.Kind))
}
