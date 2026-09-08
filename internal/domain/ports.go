package domain

import (
	"context"
	"time"
)

// ExampleRepo is a port: the domain declares what it needs from storage, and an
// adapter implements it. The dependency points inward, so swapping memstore
// for postgres changes one line in cmd/server and nothing here.
//
// Implementations must return ErrNotFound for a missing ID and
// ErrAlreadyExists for a duplicate Create.
type ExampleRepo interface {
	// Create stores a new example, or returns ErrAlreadyExists.
	Create(ctx context.Context, w Example) error
	// Get returns the example with this ID, or ErrNotFound.
	Get(ctx context.Context, id string) (Example, error)
	// List returns every example, ordered by creation time.
	List(ctx context.Context) ([]Example, error)
	// Update replaces an existing example, or returns ErrNotFound.
	Update(ctx context.Context, w Example) error
	// Delete removes a example, or returns ErrNotFound.
	Delete(ctx context.Context, id string) error
}

// IDGenerator is a port for identity, so the domain never reaches for a
// package-level random source and tests stay deterministic.
type IDGenerator interface {
	NewID() string
}

// Clock is a port for time, for the same reason.
type Clock interface {
	Now() time.Time
}
