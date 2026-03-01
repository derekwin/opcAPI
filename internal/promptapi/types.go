package promptapi

import "time"

type Definition struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	Slug         string     `json:"slug"`
	Name         string     `json:"name"`
	Description  string     `json:"description,omitempty"`
	Model        string     `json:"model"`
	SystemPrompt string     `json:"system_prompt,omitempty"`
	UserTemplate string     `json:"user_template"`
	Variables    []string   `json:"variables,omitempty"`
	Status       string     `json:"status"`
	Version      int        `json:"version"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
}

type UpsertRequest struct {
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Model        string   `json:"model"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	UserTemplate string   `json:"user_template"`
	Variables    []string `json:"variables,omitempty"`
	Publish      bool     `json:"publish,omitempty"`
}

type ListQuery struct {
	TenantID string
	Status   string
	Limit    int
	Offset   int
}

type ListPage struct {
	Data   []Definition `json:"data"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}
