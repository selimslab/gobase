package domain

import (
	"context"
	"time"
)

// WidgetRepo is a port: the domain declares what it needs from storage, and an
// adapter implements it. The dependency points inward, so swapping memstore
// for postgres changes one line in cmd/server and nothing here.
//
// Implementations must return ErrNotFound for a missing ID and
// ErrAlreadyExists for a duplicate Create.
type WidgetRepo interface {
	// Create stores a new widget, or returns ErrAlreadyExists.
	Create(ctx context.Context, w Widget) error
	// Get returns the widget with this ID, or ErrNotFound.
	Get(ctx context.Context, id string) (Widget, error)
	// List returns every widget, ordered by creation time.
	List(ctx context.Context) ([]Widget, error)
	// Update replaces an existing widget, or returns ErrNotFound.
	Update(ctx context.Context, w Widget) error
	// Delete removes a widget, or returns ErrNotFound.
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
