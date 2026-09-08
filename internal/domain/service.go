package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// WidgetService applies the widget rules. It depends on ports only, so it can
// be exercised with no HTTP server and no store.
type WidgetService struct {
	repo  WidgetRepo
	ids   IDGenerator
	clock Clock
}

// systemClock is the default Clock, used when a caller passes nil.
type systemClock struct{}

// Now implements Clock.
func (systemClock) Now() time.Time { return time.Now() }

// NewWidgetService binds a service to its ports. A nil clock falls back to the
// wall clock; ids is required, since identity must be explicit.
func NewWidgetService(repo WidgetRepo, ids IDGenerator, clock Clock) (*WidgetService, error) {
	if repo == nil {
		return nil, errors.New("domain: widget repo must not be nil")
	}

	if ids == nil {
		return nil, errors.New("domain: id generator must not be nil")
	}

	if clock == nil {
		clock = systemClock{}
	}

	return &WidgetService{repo: repo, ids: ids, clock: clock}, nil
}

// Create validates the input, assigns an identity, and stores the widget.
func (s *WidgetService) Create(ctx context.Context, name string, quantity int) (Widget, error) {
	w, err := NewWidget(s.ids.NewID(), name, quantity, s.clock.Now())
	if err != nil {
		return Widget{}, err
	}

	if err := s.repo.Create(ctx, w); err != nil {
		return Widget{}, fmt.Errorf("create widget: %w", err)
	}

	return w, nil
}

// Get returns one widget by ID.
func (s *WidgetService) Get(ctx context.Context, id string) (Widget, error) {
	if id == "" {
		return Widget{}, fmt.Errorf("%w: id must not be empty", ErrInvalid)
	}

	w, err := s.repo.Get(ctx, id)
	if err != nil {
		return Widget{}, fmt.Errorf("get widget: %w", err)
	}

	return w, nil
}

// List returns every widget.
func (s *WidgetService) List(ctx context.Context) ([]Widget, error) {
	widgets, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list widgets: %w", err)
	}

	return widgets, nil
}

// Restock adds to a widget's quantity, enforcing the entity rules.
func (s *WidgetService) Restock(ctx context.Context, id string, by int) (Widget, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return Widget{}, err
	}

	next, err := w.Restock(by)
	if err != nil {
		return Widget{}, err
	}

	if err := s.repo.Update(ctx, next); err != nil {
		return Widget{}, fmt.Errorf("update widget: %w", err)
	}

	return next, nil
}

// Delete removes a widget.
func (s *WidgetService) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: id must not be empty", ErrInvalid)
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete widget: %w", err)
	}

	return nil
}
