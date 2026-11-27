package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/configfmt"
)

// formatConfigFile validates a config file supplied by the browser and returns
// its pretty form. It is read-only: nothing is written or audited.
func (s *server) formatConfigFile(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}
	var body apitypes.ConfigFormatRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	version := ""
	if body.Version != nil {
		version = *body.Version
	}
	result, err := configfmt.CheckVersion(body.Path, body.Content, version)
	if errors.Is(err, configfmt.ErrUnsupported) {
		writeProblem(w, http.StatusBadRequest, "unsupported_config", "path must be server.properties or end in .json, .yaml, .yml, or .toml")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "config_check_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apitypes.ConfigFormatDTO{
		Format: result.Format, Valid: result.Valid, Error: result.Error,
		Warning: new(result.Warning), Formatted: result.Formatted,
	})
}
