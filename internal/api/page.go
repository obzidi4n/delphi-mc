package api

import (
	"net/http"
	"strconv"
)

// pageParams reads the shared list parameters; the store applies defaults.
func pageParams(r *http.Request) (int, string) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return limit, r.URL.Query().Get("after")
}

// cursorPtr renders an empty cursor as JSON null.
func cursorPtr(cursor string) *string {
	if cursor == "" {
		return nil
	}
	return &cursor
}
