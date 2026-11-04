package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"

	jsonv2 "github.com/go-json-experiment/json"
	jsonv1 "github.com/go-json-experiment/json/v1"
)

var projectIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// decodeProjectJSON keeps raw field values and legacy semantics, including
// duplicate keys and unpaired surrogate escapes. If the faster decoder fails,
// start the standard-library pass from zero to discard any partial result.
func decodeProjectJSON[T any](data []byte) (T, error) {
	var value T
	if err := jsonv2.Unmarshal(data, &value, jsonv1.DefaultOptionsV1()); err == nil {
		return value, nil
	}
	var legacy T
	err := json.Unmarshal(data, &legacy)
	return legacy, err
}

// readStoredProject reads the on-disk document, not Python's enriched/derived view.
// It deliberately has no write path and never creates the configured root.
func readStoredProject(root, id string) (map[string]json.RawMessage, error) {
	if !projectIDPattern.MatchString(id) {
		return nil, fmt.Errorf("invalid project identifier")
	}
	if root == "" {
		return nil, fmt.Errorf("storage root is required")
	}
	handle, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	dir, err := projectDirectory(handle, id)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return readProjectDocument(dir, id)
}
