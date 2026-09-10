package store

import (
	"context"
	"time"
)

type Node struct {
	ID       string    `json:"id"`
	Hostname string    `json:"hostname"`
	OS       string    `json:"os"`
	Arch     string    `json:"arch"`
	CPUs     int       `json:"cpus"`
	MemBytes int64     `json:"mem_bytes"`
	LastSeen time.Time `json:"last_seen"`
}

type Registry interface {
	Upsert(ctx context.Context, n Node) error
	List(ctx context.Context) ([]Node, error)
}
