package httpx

import (
	"net/http"
	"time"

	"github.com/selimslab/gobase/internal/domain"
)

// widgetResponse is the wire shape of a widget. It is deliberately separate
// from domain.Widget: the API contract and the entity change for different
// reasons, and a field added to the entity must not leak automatically.
type widgetResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Quantity  int       `json:"quantity"`
	CreatedAt time.Time `json:"created_at"`
}

// createWidgetRequest is the body of POST /widgets.
type createWidgetRequest struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

// restockWidgetRequest is the body of POST /widgets/{id}/restock.
type restockWidgetRequest struct {
	Quantity int `json:"quantity"`
}

// widgetHandler serves the widget routes. It holds the service, never a store.
type widgetHandler struct {
	svc *domain.WidgetService
}

func newWidgetView(w domain.Widget) widgetResponse {
	return widgetResponse{
		ID:        w.ID,
		Name:      w.Name,
		Quantity:  w.Quantity,
		CreatedAt: w.CreatedAt,
	}
}

// create handles POST /widgets.
func (h widgetHandler) create(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[createWidgetRequest](w, r)
	if !ok {
		return
	}

	widget, err := h.svc.Create(r.Context(), req.Name, req.Quantity)
	if err != nil {
		problemForError(w, r, err)

		return
	}

	w.Header().Set("Location", "/widgets/"+widget.ID)
	writeJSON(w, r, http.StatusCreated, newWidgetView(widget))
}

// list handles GET /widgets.
func (h widgetHandler) list(w http.ResponseWriter, r *http.Request) {
	widgets, err := h.svc.List(r.Context())
	if err != nil {
		problemForError(w, r, err)

		return
	}

	views := make([]widgetResponse, 0, len(widgets))
	for _, widget := range widgets {
		views = append(views, newWidgetView(widget))
	}

	writeJSON(w, r, http.StatusOK, map[string]any{
		"widgets": views,
		"count":   len(views),
	})
}

// get handles GET /widgets/{id}.
func (h widgetHandler) get(w http.ResponseWriter, r *http.Request) {
	widget, err := h.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		problemForError(w, r, err)

		return
	}

	writeJSON(w, r, http.StatusOK, newWidgetView(widget))
}

// restock handles POST /widgets/{id}/restock.
func (h widgetHandler) restock(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[restockWidgetRequest](w, r)
	if !ok {
		return
	}

	widget, err := h.svc.Restock(r.Context(), r.PathValue("id"), req.Quantity)
	if err != nil {
		problemForError(w, r, err)

		return
	}

	writeJSON(w, r, http.StatusOK, newWidgetView(widget))
}

// remove handles DELETE /widgets/{id}.
func (h widgetHandler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), r.PathValue("id")); err != nil {
		problemForError(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
