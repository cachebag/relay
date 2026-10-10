package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type Pool struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Token          string    `json:"token,omitempty"`
	AdminTokenHash string    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
}

type PoolStore interface {
	Create(ctx context.Context, p Pool) error
	Get(ctx context.Context, id string) (Pool, error)
	GetByToken(ctx context.Context, token string) (Pool, error)
	List(ctx context.Context) ([]Pool, error)
	SetToken(ctx context.Context, id, token string) error
	SetAdminTokenHash(ctx context.Context, id, hash string) error
	AdminTokenHash(ctx context.Context, id string) (string, error)
}

// HashToken returns the hex-encoded SHA-256 of s.
func HashToken(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}