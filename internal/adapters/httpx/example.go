package httpx

import (
	"net/http"
	"time"

	"github.com/selimslab/gobase/internal/domain"
)

// exampleResponse is the wire shape of a example. It is deliberately separate
// from domain.Example: the API contract and the entity change for different
// reasons, and a field added to the entity must not leak automatically.
type exampleResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Quantity  int       `json:"quantity"`
	CreatedAt time.Time `json:"created_at"`
}

// createExampleRequest is the body of POST /examples.
type createExampleRequest struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

// restockExampleRequest is the body of POST /examples/{id}/restock.
type restockExampleRequest struct {
	Quantity int `json:"quantity"`
}

// exampleHandler serves the example routes. It holds the service, never a store.
type exampleHandler struct {
	svc *domain.ExampleService
}

func newExampleView(w domain.Example) exampleResponse {
	return exampleResponse{
		ID:        w.ID,
		Name:      w.Name,
		Quantity:  w.Quantity,
		CreatedAt: w.CreatedAt,
	}
}

// create handles POST /examples.
func (h exampleHandler) create(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[createExampleRequest](w, r)
	if !ok {
		return
	}

	example, err := h.svc.Create(r.Context(), req.Name, req.Quantity)
	if err != nil {
		problemForError(w, r, err)

		return
	}

	w.Header().Set("Location", "/examples/"+example.ID)
	writeJSON(w, r, http.StatusCreated, newExampleView(example))
}

// list handles GET /examples.
func (h exampleHandler) list(w http.ResponseWriter, r *http.Request) {
	examples, err := h.svc.List(r.Context())
	if err != nil {
		problemForError(w, r, err)

		return
	}

	views := make([]exampleResponse, 0, len(examples))
	for _, example := range examples {
		views = append(views, newExampleView(example))
	}

	writeJSON(w, r, http.StatusOK, map[string]any{
		"examples": views,
		"count":    len(views),
	})
}

// get handles GET /examples/{id}.
func (h exampleHandler) get(w http.ResponseWriter, r *http.Request) {
	example, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		problemForError(w, r, err)

		return
	}

	writeJSON(w, r, http.StatusOK, newExampleView(example))
}

// restock handles POST /examples/{id}/restock.
func (h exampleHandler) restock(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[restockExampleRequest](w, r)
	if !ok {
		return
	}

	example, err := h.svc.Restock(r.Context(), r.PathValue("id"), req.Quantity)
	if err != nil {
		problemForError(w, r, err)

		return
	}

	writeJSON(w, r, http.StatusOK, newExampleView(example))
}

// remove handles DELETE /examples/{id}.
func (h exampleHandler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), r.PathValue("id")); err != nil {
		problemForError(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
