package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"strings"
)

// errUnsupportedMedia reports a request body this service will not parse.
var errUnsupportedMedia = errors.New("unsupported media type")

// decodeJSON reads exactly one JSON object of type T from the request body.
//
// It rejects unknown fields, so a typo in a client payload fails loudly rather
// than being silently dropped, and rejects trailing content, so one request
// cannot smuggle a second document. The body is already capped by
// http.MaxBytesReader in the security middleware.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T

	if err := checkContentType(r); err != nil {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "content-type must be application/json")

		return v, false
	}

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeProblem(w, r, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("request body must not exceed %d bytes", maxErr.Limit))

			return v, false
		}

		writeProblem(w, r, http.StatusBadRequest, "request body is not valid JSON")

		return v, false
	}

	if dec.More() {
		writeProblem(w, r, http.StatusBadRequest, "request body must contain a single JSON object")

		return v, false
	}

	return v, true
}

// checkContentType accepts a missing Content-Type on an empty body and
// application/json otherwise.
func checkContentType(r *http.Request) error {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return nil
	}

	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return fmt.Errorf("%w: %q", errUnsupportedMedia, ct)
	}

	if mt != "application/json" && !strings.HasSuffix(mt, "+json") {
		return fmt.Errorf("%w: %q", errUnsupportedMedia, mt)
	}

	return nil
}

// slogErr is the one place an error becomes a log attribute, so the key is
// identical on every line.
func slogErr(err error) slog.Attr { return slog.String("error", err.Error()) }
