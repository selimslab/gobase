package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ExampleService applies the example rules. It depends on ports only, so it can
// be exercised with no HTTP server and no store.
type ExampleService struct {
	repo  ExampleRepo
	ids   IDGenerator
	clock Clock
}

// systemClock is the default Clock, used when a caller passes nil.
type systemClock struct{}

// Now implements Clock.
func (systemClock) Now() time.Time { return time.Now() }

// NewExampleService binds a service to its ports. A nil clock falls back to the
// wall clock; ids is required, since identity must be explicit.
func NewExampleService(repo ExampleRepo, ids IDGenerator, clock Clock) (*ExampleService, error) {
	if repo == nil {
		return nil, errors.New("domain: example repo must not be nil")
	}

	if ids == nil {
		return nil, errors.New("domain: id generator must not be nil")
	}

	if clock == nil {
		clock = systemClock{}
	}

	return &ExampleService{repo: repo, ids: ids, clock: clock}, nil
}

// Create validates the input, assigns an identity, and stores the example.
func (s *ExampleService) Create(ctx context.Context, name string, quantity int) (Example, error) {
	w, err := NewExample(s.ids.NewID(), name, quantity, s.clock.Now())
	if err != nil {
		return Example{}, err
	}

	if err := s.repo.Create(ctx, w); err != nil {
		return Example{}, fmt.Errorf("create example: %w", err)
	}

	return w, nil
}

// Get returns one example by ID.
func (s *ExampleService) Get(ctx context.Context, id string) (Example, error) {
	if id == "" {
		return Example{}, fmt.Errorf("%w: id must not be empty", ErrInvalid)
	}

	w, err := s.repo.Get(ctx, id)
	if err != nil {
		return Example{}, fmt.Errorf("get example: %w", err)
	}

	return w, nil
}

// List returns every example.
func (s *ExampleService) List(ctx context.Context) ([]Example, error) {
	examples, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list examples: %w", err)
	}

	return examples, nil
}

// Restock adds to a example's quantity, enforcing the entity rules.
func (s *ExampleService) Restock(ctx context.Context, id string, by int) (Example, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return Example{}, err
	}

	next, err := w.Restock(by)
	if err != nil {
		return Example{}, err
	}

	if err := s.repo.Update(ctx, next); err != nil {
		return Example{}, fmt.Errorf("update example: %w", err)
	}

	return next, nil
}

// Delete removes a example.
func (s *ExampleService) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: id must not be empty", ErrInvalid)
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete example: %w", err)
	}

	return nil
}
