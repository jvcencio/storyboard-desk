package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

var projectIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

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
