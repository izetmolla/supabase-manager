package supabase

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Status is the subset of `supabase status -o json` the UI shows.
type Status struct {
	Running        bool   `json:"running"`
	APIURL         string `json:"api_url"`
	RESTURL        string `json:"rest_url"`
	GraphQLURL     string `json:"graphql_url"`
	FunctionsURL   string `json:"functions_url"`
	StudioURL      string `json:"studio_url"`
	InbucketURL    string `json:"inbucket_url"`
	StorageS3URL   string `json:"storage_s3_url"`
	MCPURL         string `json:"mcp_url"`
	DBURL          string `json:"db_url"`
	AnonKey        string `json:"anon_key"`
	ServiceRoleKey string `json:"service_role_key"`
	PublishableKey string `json:"publishable_key"`
	SecretKey      string `json:"secret_key"`
	JWTSecret      string `json:"jwt_secret"`
	S3AccessKeyID  string `json:"s3_access_key_id"`
	S3SecretKey    string `json:"s3_secret_key"`
	S3Region       string `json:"s3_region"`
	Error          string `json:"error,omitempty"`
}

func (r *Runner) Status(ctx context.Context, workdir string, env []string, extraArgs ...string) Status {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := r.Run(ctx, workdir, env, append([]string{"status", "-o", "json"}, extraArgs...)...)
	if err != nil {
		return Status{Running: false, Error: firstLine(err.Error())}
	}
	start := strings.Index(string(out), "{")
	if start < 0 {
		return Status{Running: false}
	}
	var m map[string]string
	if err := json.Unmarshal(out[start:], &m); err != nil {
		return Status{Running: false, Error: err.Error()}
	}
	inbucket := m["MAILPIT_URL"]
	if inbucket == "" {
		inbucket = m["INBUCKET_URL"]
	}
	return Status{
		Running:        m["API_URL"] != "",
		APIURL:         m["API_URL"],
		RESTURL:        m["REST_URL"],
		GraphQLURL:     m["GRAPHQL_URL"],
		FunctionsURL:   m["FUNCTIONS_URL"],
		StudioURL:      m["STUDIO_URL"],
		InbucketURL:    inbucket,
		StorageS3URL:   m["STORAGE_S3_URL"],
		MCPURL:         m["MCP_URL"],
		DBURL:          m["DB_URL"],
		AnonKey:        m["ANON_KEY"],
		ServiceRoleKey: m["SERVICE_ROLE_KEY"],
		PublishableKey: m["PUBLISHABLE_KEY"],
		SecretKey:      m["SECRET_KEY"],
		JWTSecret:      m["JWT_SECRET"],
		S3AccessKeyID:  m["S3_PROTOCOL_ACCESS_KEY_ID"],
		S3SecretKey:    m["S3_PROTOCOL_ACCESS_KEY_SECRET"],
		S3Region:       m["S3_PROTOCOL_REGION"],
	}
}

func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}
	return s
}
