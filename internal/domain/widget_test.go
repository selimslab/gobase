package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/selimslab/gobase/internal/domain"
)

// stubRepo is a hand-written stand-in for the WidgetRepo port. The domain can
// be tested with no store and no server, which is the point of the port.
type stubRepo struct {
	widgets map[string]domain.Widget
	err     error
	creates int
	updates int
}

func newStubRepo() *stubRepo {
	return &stubRepo{widgets: make(map[string]domain.Widget)}
}

func (s *stubRepo) Create(_ context.Context, w domain.Widget) error {
	s.creates++

	if s.err != nil {
		return s.err
	}

	if _, ok := s.widgets[w.ID]; ok {
		return domain.ErrAlreadyExists
	}

	s.widgets[w.ID] = w

	return nil
}

func (s *stubRepo) Get(_ context.Context, id string) (domain.Widget, error) {
	if s.err != nil {
		return domain.Widget{}, s.err
	}

	w, ok := s.widgets[id]
	if !ok {
		return domain.Widget{}, domain.ErrNotFound
	}

	return w, nil
}

func (s *stubRepo) List(context.Context) ([]domain.Widget, error) {
	if s.err != nil {
		return nil, s.err
	}

	out := make([]domain.Widget, 0, len(s.widgets))
	for _, w := range s.widgets {
		out = append(out, w)
	}

	return out, nil
}

func (s *stubRepo) Update(_ context.Context, w domain.Widget) error {
	s.updates++

	if s.err != nil {
		return s.err
	}

	if _, ok := s.widgets[w.ID]; !ok {
		return domain.ErrNotFound
	}

	s.widgets[w.ID] = w

	return nil
}

func (s *stubRepo) Delete(_ context.Context, id string) error {
	if s.err != nil {
		return s.err
	}

	if _, ok := s.widgets[id]; !ok {
		return domain.ErrNotFound
	}

	delete(s.widgets, id)

	return nil
}

// seqIDs hands out predictable identifiers so assertions can name them.
type seqIDs struct{ n int }

func (s *seqIDs) NewID() string {
	s.n++

	return string(rune('a' + s.n - 1))
}

// fixedClock freezes time, so CreatedAt is exact.
type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

func newTestService(t *testing.T, repo domain.WidgetRepo) *domain.WidgetService {
	t.Helper()

	svc, err := domain.NewWidgetService(repo, &seqIDs{}, fixedClock{t: time.Unix(1700000000, 0)})
	if err != nil {
		t.Fatalf("NewWidgetService() error = %v", err)
	}

	return svc
}

func TestNewWidgetValidation(t *testing.T) {
	t.Parallel()

	now := time.Unix(1700000000, 0)

	tests := []struct {
		name     string
		id       string
		widget   string
		quantity int
		wantErr  bool
	}{
		{name: "valid", id: "w1", widget: "bolt", quantity: 3},
		{name: "zero quantity is valid", id: "w1", widget: "bolt", quantity: 0},
		{name: "empty id", id: "", widget: "bolt", quantity: 1, wantErr: true},
		{name: "whitespace id", id: "   ", widget: "bolt", quantity: 1, wantErr: true},
		{name: "empty name", id: "w1", widget: "", quantity: 1, wantErr: true},
		{name: "name too long", id: "w1", widget: string(make([]byte, 65)), quantity: 1, wantErr: true},
		{name: "negative quantity", id: "w1", widget: "bolt", quantity: -1, wantErr: true},
		{name: "quantity over cap", id: "w1", widget: "bolt", quantity: 1_000_001, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, err := domain.NewWidget(tt.id, tt.widget, tt.quantity, now)

			if tt.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("error = %v, want ErrInvalid", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("NewWidget() error = %v", err)
			}

			if !w.CreatedAt.Equal(now.UTC()) {
				t.Errorf("CreatedAt = %s, want %s", w.CreatedAt, now.UTC())
			}
		})
	}
}

func TestNewWidgetTrimsInput(t *testing.T) {
	t.Parallel()

	w, err := domain.NewWidget("  w1  ", "  bolt  ", 1, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("NewWidget() error = %v", err)
	}

	if w.ID != "w1" || w.Name != "bolt" {
		t.Errorf("got id=%q name=%q, want trimmed values", w.ID, w.Name)
	}
}

func TestRestock(t *testing.T) {
	t.Parallel()

	w, err := domain.NewWidget("w1", "bolt", 5, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("NewWidget() error = %v", err)
	}

	next, err := w.Restock(3)
	if err != nil {
		t.Fatalf("Restock() error = %v", err)
	}

	if next.Quantity != 8 {
		t.Errorf("Quantity = %d, want 8", next.Quantity)
	}

	if w.Quantity != 5 {
		t.Errorf("Restock must not mutate the receiver: got %d", w.Quantity)
	}

	for _, by := range []int{0, -1} {
		if _, err := w.Restock(by); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Restock(%d) error = %v, want ErrInvalid", by, err)
		}
	}

	if _, err := w.Restock(1_000_000); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("Restock past the cap error = %v, want ErrInvalid", err)
	}
}

func TestNewWidgetServiceRequiresPorts(t *testing.T) {
	t.Parallel()

	if _, err := domain.NewWidgetService(nil, &seqIDs{}, nil); err == nil {
		t.Error("a nil repo must be rejected")
	}

	if _, err := domain.NewWidgetService(newStubRepo(), nil, nil); err == nil {
		t.Error("a nil id generator must be rejected")
	}

	if _, err := domain.NewWidgetService(newStubRepo(), &seqIDs{}, nil); err != nil {
		t.Errorf("a nil clock must default to the wall clock: %v", err)
	}
}

func TestServiceCreate(t *testing.T) {
	t.Parallel()

	repo := newStubRepo()
	svc := newTestService(t, repo)

	w, err := svc.Create(context.Background(), "bolt", 2)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if w.ID != "a" {
		t.Errorf("ID = %q, want the generated id", w.ID)
	}

	if _, ok := repo.widgets["a"]; !ok {
		t.Error("widget was not handed to the repo")
	}
}

func TestServiceCreateRejectsInvalidInputBeforeTouchingTheRepo(t *testing.T) {
	t.Parallel()

	repo := newStubRepo()
	svc := newTestService(t, repo)

	if _, err := svc.Create(context.Background(), "", 1); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}

	if repo.creates != 0 {
		t.Errorf("repo was called %d times for invalid input, want 0", repo.creates)
	}
}

func TestServiceGetPropagatesNotFound(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, newStubRepo())

	if _, err := svc.Get(context.Background(), "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}

	if _, err := svc.Get(context.Background(), ""); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("empty id error = %v, want ErrInvalid", err)
	}
}

func TestServiceRestock(t *testing.T) {
	t.Parallel()

	repo := newStubRepo()
	svc := newTestService(t, repo)
	ctx := context.Background()

	created, err := svc.Create(ctx, "bolt", 1)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := svc.Restock(ctx, created.ID, 4)
	if err != nil {
		t.Fatalf("Restock() error = %v", err)
	}

	if got.Quantity != 5 {
		t.Errorf("Quantity = %d, want 5", got.Quantity)
	}

	if _, err := svc.Restock(ctx, created.ID, 0); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("error = %v, want ErrInvalid", err)
	}

	if repo.updates != 1 {
		t.Errorf("repo.Update called %d times, want 1 (the invalid restock must not persist)", repo.updates)
	}
}

func TestServiceDelete(t *testing.T) {
	t.Parallel()

	repo := newStubRepo()
	svc := newTestService(t, repo)
	ctx := context.Background()

	created, err := svc.Create(ctx, "bolt", 1)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if err := svc.Delete(ctx, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second Delete error = %v, want ErrNotFound", err)
	}
}

func TestServiceWrapsRepoFailures(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("storage exploded")
	repo := newStubRepo()
	repo.err = sentinel
	svc := newTestService(t, repo)

	if _, err := svc.List(context.Background()); !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want the repo error to survive wrapping", err)
	}
}
