// Package domain holds the entities, their rules, and the ports they declare.
// It is pure: no net/http, no database driver, no framework. This is the layer
// you replace when you clone this template.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Sentinel errors the outer layers map to transport status codes. Compare with
// errors.Is; never string-match.
var (
	// ErrNotFound reports that no entity exists for the given identifier.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists reports a uniqueness conflict.
	ErrAlreadyExists = errors.New("already exists")
	// ErrInvalid reports that the caller's input broke a domain rule. Wrapped
	// errors carry the specific rule.
	ErrInvalid = errors.New("invalid")
)

// Widget is the example entity. Delete it and put your own here.
type Widget struct {
	ID        string
	Name      string
	Quantity  int
	CreatedAt time.Time
}

// Widget rules, stated once so both the service and the store obey the same
// limits.
const (
	nameMinLen  = 1
	nameMaxLen  = 64
	maxQuantity = 1_000_000
)

// NewWidget builds a valid Widget or explains why it cannot.
func NewWidget(id, name string, quantity int, now time.Time) (Widget, error) {
	w := Widget{
		ID:        strings.TrimSpace(id),
		Name:      strings.TrimSpace(name),
		Quantity:  quantity,
		CreatedAt: now.UTC(),
	}

	if err := w.Validate(); err != nil {
		return Widget{}, err
	}

	return w, nil
}

// Validate reports every rule the Widget breaks, joined into one error.
func (w Widget) Validate() error {
	var errs []error

	if w.ID == "" {
		errs = append(errs, fmt.Errorf("%w: id must not be empty", ErrInvalid))
	}

	if n := utf8.RuneCountInString(w.Name); n < nameMinLen || n > nameMaxLen {
		errs = append(errs, fmt.Errorf("%w: name must be %d-%d characters", ErrInvalid, nameMinLen, nameMaxLen))
	}

	if w.Quantity < 0 || w.Quantity > maxQuantity {
		errs = append(errs, fmt.Errorf("%w: quantity must be within [0,%d]", ErrInvalid, maxQuantity))
	}

	return errors.Join(errs...)
}

// Restock increases the quantity, refusing a change that breaks the rules.
func (w Widget) Restock(by int) (Widget, error) {
	if by <= 0 {
		return Widget{}, fmt.Errorf("%w: restock amount must be positive", ErrInvalid)
	}

	next := w
	next.Quantity += by

	if err := next.Validate(); err != nil {
		return Widget{}, err
	}

	return next, nil
}
