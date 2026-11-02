package main

import (
	"fmt"
	"os"
	"strings"

	"m31labs.dev/gosx"
)

func (s *ProjectStore) Exports(id string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	dir, err := projectDirectory(s.root, id)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	info, err := dir.Lstat("exports")
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("exports must be a real directory")
	}
	f, err := dir.Open("exports")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	rows := []string{}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".part") || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		rel := "exports/" + entry.Name()
		if err = safeProjectPath(dir, rel, false); err != nil {
			return nil, err
		}
		rows = append(rows, rel)
	}
	return rows, nil
}
func (w workspacePage) exports() gosx.Node {
	rows := []any{el("h2", txt("Exports")), el("p", txt("Review packages, silent reels and their source-cut maps stay with this project. Exports use saved project data."))}
	for _, rel := range w.exportFiles {
		rows = append(rows, el("p", link(strings.TrimPrefix(rel, "exports/"), mediaURL(shotText(w.project, "id"), rel), "button secondary")))
	}
	if len(w.exportFiles) == 0 {
		rows = append(rows, el("p", txt("Export a review package above, or create a reel/editor package from a song section.")))
	}
	return cls("section", "paper", rows...)
}
