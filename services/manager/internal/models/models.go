package models

import (
	"time"
)

const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Email        string    `gorm:"uniqueIndex;size:255;not null" json:"email"`
	Name         string    `gorm:"size:255" json:"name"`
	PasswordHash string    `gorm:"not null" json:"-"`
	Role         string    `gorm:"size:20;not null;default:member" json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RefreshToken struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"index;not null"`
	TokenHash string    `gorm:"uniqueIndex;size:64;not null"`
	ExpiresAt time.Time `gorm:"index"`
	CreatedAt time.Time
}

const (
	StatusStopped  = "stopped"
	StatusRunning  = "running"
	StatusStarting = "starting"
	StatusStopping = "stopping"
	StatusError    = "error"
	StatusUnknown  = "unknown"
)

type Project struct {
	ID                uint          `gorm:"primaryKey" json:"id"`
	Slug              string        `gorm:"uniqueIndex;size:40;not null" json:"slug"`
	Name              string        `gorm:"size:255;not null" json:"name"`
	Path              string        `gorm:"not null" json:"path"`
	SupabaseProjectID string        `gorm:"uniqueIndex;size:64;not null" json:"supabase_project_id"`
	PortBase          int           `gorm:"uniqueIndex;not null" json:"port_base"`
	InspectorPort     int           `gorm:"not null" json:"inspector_port"`
	Status            string        `gorm:"size:20;not null;default:stopped" json:"status"`
	Imported          bool          `gorm:"not null;default:false" json:"imported"`
	CreatedByID       uint          `json:"created_by_id"`
	Network           NetworkConfig `gorm:"serializer:json;type:text" json:"network"`
	Storage           StorageConfig `gorm:"serializer:json;type:text" json:"storage"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

type ProjectSecret struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ProjectID      uint      `gorm:"uniqueIndex:idx_project_secret;not null" json:"project_id"`
	Key            string    `gorm:"uniqueIndex:idx_project_secret;size:128;not null" json:"key"`
	ValueEncrypted string    `gorm:"type:text;not null" json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

const (
	JobQueued    = "queued"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

type Job struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	ProjectID  uint       `gorm:"index;not null" json:"project_id"`
	Command    string     `gorm:"size:255;not null" json:"command"`
	Status     string     `gorm:"size:20;not null" json:"status"`
	ExitCode   int        `json:"exit_code"`
	Output     string     `gorm:"type:text" json:"output,omitempty"`
	StartedBy  uint       `json:"started_by"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

type AuditLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ActorID   uint      `gorm:"index" json:"actor_id"`
	Action    string    `gorm:"size:64;not null" json:"action"`
	Target    string    `gorm:"size:255" json:"target"`
	Metadata  string    `gorm:"type:text" json:"metadata"`
	CreatedAt time.Time `json:"created_at"`
}

func All() []any {
	return []any{&User{}, &RefreshToken{}, &Project{}, &ProjectSecret{}, &Job{}, &AuditLog{}, &Setting{}}
}
