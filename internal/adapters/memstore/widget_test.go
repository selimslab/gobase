package memstore_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/selimslab/gobase/internal/adapters/memstore"
	"github.com/selimslab/gobase/internal/domain"
)

// The point of the example slice: the adapter satisfies the port the domain
// declared, and the domain knows nothing about this package.
func TestWidgetRepoSatisfiesPort(t *testing.T) {
	t.Parallel()

	// These assignments are the assertion: they do not compile unless the
	// adapter satisfies the port the domain declared.
	var (
		repo domain.WidgetRepo  = memstore.NewWidgetRepo()
		ids  domain.IDGenerator = memstore.NewIDGenerator()
	)

	if _, err := repo.List(context.Background()); err != nil {
		t.Errorf("List() through the port interface: %v", err)
	}

	if ids.NewID() == "" {
		t.Error("NewID() through the port interface returned an empty id")
	}
}

func mustWidget(t *testing.T, id, name string, qty int, at time.Time) domain.Widget {
	t.Helper()

	w, err := domain.NewWidget(id, name, qty, at)
	if err != nil {
		t.Fatalf("NewWidget(%q) error = %v", id, err)
	}

	return w
}

func TestCreateAndGet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := memstore.NewWidgetRepo()
	want := mustWidget(t, "w1", "bolt", 3, time.Unix(1, 0))

	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.Get(ctx, "w1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got != want {
		t.Errorf("Get() = %+v, want %+v", got, want)
	}
}

func TestCreateDuplicate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := memstore.NewWidgetRepo()
	w := mustWidget(t, "w1", "bolt", 3, time.Unix(1, 0))

	if err := repo.Create(ctx, w); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := repo.Create(ctx, w); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("duplicate Create error = %v, want ErrAlreadyExists", err)
	}
}

func TestGetUpdateDeleteMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := memstore.NewWidgetRepo()
	w := mustWidget(t, "ghost", "bolt", 1, time.Unix(1, 0))

	if _, err := repo.Get(ctx, "ghost"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get error = %v, want ErrNotFound", err)
	}

	if err := repo.Update(ctx, w); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update error = %v, want ErrNotFound", err)
	}

	if err := repo.Delete(ctx, "ghost"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Delete error = %v, want ErrNotFound", err)
	}
}

func TestListIsOrderedByCreation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := memstore.NewWidgetRepo()

	// Inserted out of order on purpose.
	for _, w := range []domain.Widget{
		mustWidget(t, "c", "third", 1, time.Unix(300, 0)),
		mustWidget(t, "a", "first", 1, time.Unix(100, 0)),
		mustWidget(t, "b", "second", 1, time.Unix(200, 0)),
	} {
		if err := repo.Create(ctx, w); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	got, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("List() returned %d widgets, want %d", len(got), len(want))
	}

	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("List()[%d].ID = %q, want %q", i, got[i].ID, id)
		}
	}
}

func TestListTieBreaksOnID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := memstore.NewWidgetRepo()
	at := time.Unix(100, 0)

	for _, id := range []string{"z", "m", "a"} {
		if err := repo.Create(ctx, mustWidget(t, id, "same-time", 1, at)); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// Map iteration is randomized, so an unstable sort would flake here.
	for range 20 {
		got, err := repo.List(ctx)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		if got[0].ID != "a" || got[1].ID != "m" || got[2].ID != "z" {
			t.Fatalf("List() order = %q, %q, %q; want a, m, z", got[0].ID, got[1].ID, got[2].ID)
		}
	}
}

func TestUpdateReplaces(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := memstore.NewWidgetRepo()
	w := mustWidget(t, "w1", "bolt", 1, time.Unix(1, 0))

	if err := repo.Create(ctx, w); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	w.Quantity = 9
	if err := repo.Update(ctx, w); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := repo.Get(ctx, "w1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.Quantity != 9 {
		t.Errorf("Quantity = %d, want 9", got.Quantity)
	}
}

// Run with -race: the mutex is the whole point of this adapter.
func TestConcurrentAccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := memstore.NewWidgetRepo()

	const n = 50

	var wg sync.WaitGroup

	wg.Add(n)

	for i := range n {
		go func() {
			defer wg.Done()

			id := fmt.Sprintf("w%02d", i)

			if err := repo.Create(ctx, mustWidgetNoT(id, time.Unix(int64(i), 0))); err != nil {
				t.Errorf("Create(%s) error = %v", id, err)

				return
			}

			if _, err := repo.Get(ctx, id); err != nil {
				t.Errorf("Get(%s) error = %v", id, err)
			}

			if _, err := repo.List(ctx); err != nil {
				t.Errorf("List() error = %v", err)
			}
		}()
	}

	wg.Wait()

	got, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(got) != n {
		t.Errorf("List() returned %d widgets, want %d", len(got), n)
	}
}

// mustWidgetNoT builds a widget off the test goroutine, where t.Fatalf is
// illegal.
func mustWidgetNoT(id string, at time.Time) domain.Widget {
	return domain.Widget{ID: id, Name: "bolt", Quantity: 1, CreatedAt: at.UTC()}
}

func TestNewIDGeneratorIsUnique(t *testing.T) {
	t.Parallel()

	gen := memstore.NewIDGenerator()
	seen := make(map[string]bool, 1000)

	for range 1000 {
		id := gen.NewID()

		if len(id) != 32 {
			t.Fatalf("NewID() = %q, want 32 hex characters", id)
		}

		if seen[id] {
			t.Fatalf("NewID() repeated %q", id)
		}

		seen[id] = true
	}
}
