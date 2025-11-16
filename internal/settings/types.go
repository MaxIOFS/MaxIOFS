package settings

import "time"

// Setting represents a system configuration setting
type Setting struct {
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	Type        string    `json:"type"` // string, int, bool, json
	Category    string    `json:"category"`
	Description string    `json:"description"`
	Editable    bool      `json:"editable"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Category represents a group of related settings
type Category string

const (
	CategorySecurity Category = "security"
	CategoryAudit    Category = "audit"
	CategoryStorage  Category = "storage"
	CategoryMetrics  Category = "metrics"
	CategorySystem   Category = "system"
)

// Type represents the data type of a setting
type Type string

const (
	TypeString Type = "string"
	TypeInt    Type = "int"
	TypeBool   Type = "bool"
	TypeJSON   Type = "json"
)

// UpdateRequest represents a request to update a setting
type UpdateRequest struct {
	Value string `json:"value"`
}

// BulkUpdateRequest represents a request to update multiple settings
type BulkUpdateRequest struct {
	Settings map[string]string `json:"settings"` // key -> value
}
