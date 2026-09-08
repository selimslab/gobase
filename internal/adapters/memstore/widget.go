// Package memstore is an in-memory outbound adapter. It implements
// domain.WidgetRepo, which proves the port inverts: the domain declares the
// interface, this package conforms to it, and cmd/server binds the two.
// Replace it with a real store by writing another implementation.
package memstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/selimslab/gobase/internal/domain"
)

// Compile-time proof that this adapter satisfies the ports it claims.
var (
	_ domain.WidgetRepo  = (*WidgetRepo)(nil)
	_ domain.IDGenerator = (*IDGenerator)(nil)
)

// WidgetRepo keeps widgets in a map guarded by a mutex. Safe for concurrent
// use; everything is lost on restart.
type WidgetRepo struct {
	mu      sync.RWMutex
	widgets map[string]domain.Widget
}

// NewWidgetRepo returns an empty in-memory repository.
func NewWidgetRepo() *WidgetRepo {
	return &WidgetRepo{widgets: make(map[string]domain.Widget)}
}

// Create stores a widget, or reports domain.ErrAlreadyExists.
func (r *WidgetRepo) Create(_ context.Context, w domain.Widget) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.widgets[w.ID]; ok {
		return fmt.Errorf("widget %q: %w", w.ID, domain.ErrAlreadyExists)
	}

	r.widgets[w.ID] = w

	return nil
}

// Get returns one widget, or domain.ErrNotFound.
func (r *WidgetRepo) Get(_ context.Context, id string) (domain.Widget, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	w, ok := r.widgets[id]
	if !ok {
		return domain.Widget{}, fmt.Errorf("widget %q: %w", id, domain.ErrNotFound)
	}

	return w, nil
}

// List returns every widget, oldest first, with ID breaking ties so the order
// is stable for equal timestamps.
func (r *WidgetRepo) List(_ context.Context) ([]domain.Widget, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	widgets := slices.Collect(maps.Values(r.widgets))
	slices.SortFunc(widgets, func(a, b domain.Widget) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}

		return strings.Compare(a.ID, b.ID)
	})

	return widgets, nil
}

// Update replaces an existing widget, or reports domain.ErrNotFound.
func (r *WidgetRepo) Update(_ context.Context, w domain.Widget) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.widgets[w.ID]; !ok {
		return fmt.Errorf("widget %q: %w", w.ID, domain.ErrNotFound)
	}

	r.widgets[w.ID] = w

	return nil
}

// Delete removes a widget, or reports domain.ErrNotFound.
func (r *WidgetRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.widgets[id]; !ok {
		return fmt.Errorf("widget %q: %w", id, domain.ErrNotFound)
	}

	delete(r.widgets, id)

	return nil
}

// IDGenerator issues random 128-bit identifiers.
type IDGenerator struct{}

// NewIDGenerator returns the default identifier source.
func NewIDGenerator() *IDGenerator { return &IDGenerator{} }

// NewID returns a fresh identifier. crypto/rand.Read never fails as of Go 1.24,
// so there is no error to surface.
func (*IDGenerator) NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])

	return hex.EncodeToString(b[:])
}
