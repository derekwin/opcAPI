package promptapi

import "time"

type Definition struct {
	ID            string     `json:"id"`
	Slug          string     `json:"slug"`
	Name          string     `json:"name"`
	Description   string     `json:"description,omitempty"`
	Model         string     `json:"model"`
	InputSchema   string     `json:"input_schema,omitempty"`
	OutputSchema  string     `json:"output_schema,omitempty"`
	SystemPrompt  string     `json:"system_prompt,omitempty"`
	UserTemplate  string     `json:"user_template"`
	Variables     []string   `json:"variables,omitempty"`
	ExampleInput  string     `json:"example_input,omitempty"`
	ExampleOutput string     `json:"example_output,omitempty"`
	Status        string     `json:"status"`
	Version       int        `json:"version"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
}

type UpsertRequest struct {
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Model         string   `json:"model"`
	InputSchema   string   `json:"input_schema,omitempty"`
	OutputSchema  string   `json:"output_schema,omitempty"`
	SystemPrompt  string   `json:"system_prompt,omitempty"`
	UserTemplate  string   `json:"user_template"`
	Variables     []string `json:"variables,omitempty"`
	ExampleInput  string   `json:"example_input,omitempty"`
	ExampleOutput string   `json:"example_output,omitempty"`
	Publish       bool     `json:"publish,omitempty"`
}

type ListQuery struct {
	Status string
	Limit  int
	Offset int
}

type ListPage struct {
	Data   []Definition `json:"data"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

type DefaultAPIModelMapping struct {
	ModelID string `json:"model_id"`
	Model   string `json:"model"`
}

type DefaultAPIConfig struct {
	APIKey          string                   `json:"api_key"`
	APIPath         string                   `json:"api_path"`
	Name            string                   `json:"name"`
	Enabled         bool                     `json:"enabled"`
	DefaultModel    string                   `json:"default_model,omitempty"`
	AllowedModels   []string                 `json:"allowed_models,omitempty"`
	ModelIDMappings []DefaultAPIModelMapping `json:"model_id_mappings,omitempty"`
	UpdatedAt       time.Time                `json:"updated_at"`
}

type UpsertDefaultAPIRequest struct {
	APIKey          string                   `json:"api_key"`
	Enabled         bool                     `json:"enabled"`
	DefaultModel    string                   `json:"default_model,omitempty"`
	AllowedModels   []string                 `json:"allowed_models,omitempty"`
	ModelIDMappings []DefaultAPIModelMapping `json:"model_id_mappings,omitempty"`
}
