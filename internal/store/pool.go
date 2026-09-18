package store

import (
	"context"
	"time"
)

type Pool struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Token     string    `json:"token,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type PoolStore interface {
	Create(ctx context.Context, p Pool) error
	Get(ctx context.Context, id string) (Pool, error)
	GetByToken(ctx context.Context, token string) (Pool, error)
	List(ctx context.Context) ([]Pool, error)
}
