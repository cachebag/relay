package store

import (
	"context"
	"testing"
	"time"
)

func TestRotateKeepsPreviousTokenHash(t *testing.T) {
	ctx := context.Background()
	m := NewMemPools()
	if err := m.Create(ctx, Pool{ID: "p1", Name: "lab", Token: "old-token", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetToken(ctx, "p1", "new-token"); err != nil {
		t.Fatal(err)
	}

	for token, want := range map[string]bool{
		"old-token":     true,
		"new-token":     false,
		"never-existed": false,
	} {
		got, err := m.WasRotated(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("WasRotated(%q) = %v, want %v", token, got, want)
		}
	}
}
