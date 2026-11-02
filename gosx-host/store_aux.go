package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func atomicStoredBytes(dir *os.Root, rel string, data []byte) error {
	if err := safeProjectPath(dir, rel, true); err != nil {
		return err
	}
	id, err := newStorageID()
	if err != nil {
		return err
	}
	temp := rel + "." + id + ".tmp"
	f, err := dir.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer dir.Remove(temp)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = replaceStoredFile(dir, temp, rel); err != nil {
		return err
	}
	parent, err := dir.OpenRoot(filepath.Dir(rel))
	if err != nil {
		return err
	}
	defer parent.Close()
	return syncStorageDirectory(parent)
}
func (s *ProjectStore) readAux(project, rel string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	if !(strings.HasPrefix(rel, "cloud/") || (project != "" && strings.HasPrefix(rel, "jobs/"))) {
		return nil, fmt.Errorf("invalid auxiliary path")
	}
	dir := s.root
	if project != "" {
		var err error
		dir, err = projectDirectory(s.root, project)
		if err != nil {
			return nil, err
		}
		defer dir.Close()
	}
	if err := safeProjectPath(dir, rel, false); err != nil {
		return nil, err
	}
	info, err := dir.Stat(rel)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxUpload {
		return nil, fmt.Errorf("auxiliary file is too large")
	}
	return dir.ReadFile(rel)
}
func (s *ProjectStore) writeAux(project, rel string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	if !(strings.HasPrefix(rel, "cloud/") || (project != "" && strings.HasPrefix(rel, "jobs/"))) {
		return fmt.Errorf("invalid auxiliary path")
	}
	dir := s.root
	if project != "" {
		var err error
		dir, err = projectDirectory(s.root, project)
		if err != nil {
			return err
		}
		defer dir.Close()
	}
	return atomicStoredBytes(dir, rel, data)
}
func (s *ProjectStore) auxDocuments(folder string) ([]ProjectDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(folder, "cloud/") || !filepath.IsLocal(folder) {
		return nil, fmt.Errorf("invalid auxiliary folder")
	}
	if err := safeProjectPath(s.root, folder+"/entry.json", false); err != nil {
		if os.IsNotExist(err) {
			return []ProjectDocument{}, nil
		}
		return nil, err
	}
	dir, err := s.root.OpenRoot(folder)
	if os.IsNotExist(err) {
		return []ProjectDocument{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	f, err := dir.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	rows := []ProjectDocument{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := dir.Lstat(entry.Name())
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("invalid auxiliary document")
		}
		if info.Size() > maxProjectBytes {
			return nil, fmt.Errorf("auxiliary document too large")
		}
		data, err := dir.ReadFile(entry.Name())
		if err != nil {
			return nil, err
		}
		var doc ProjectDocument
		if json.Unmarshal(data, &doc) != nil || doc == nil {
			return nil, fmt.Errorf("invalid saved job document; reconcile it before generating")
		}
		rows = append(rows, doc)
	}
	return rows, nil
}
func (s *ProjectStore) auxJSON(project, rel string, defaultValue ProjectDocument) (ProjectDocument, error) {
	b, err := s.readAux(project, rel)
	if os.IsNotExist(err) {
		return defaultValue, nil
	}
	if err != nil {
		return nil, err
	}
	var p ProjectDocument
	if json.Unmarshal(b, &p) != nil || p == nil {
		return nil, fmt.Errorf("invalid saved auxiliary JSON")
	}
	return p, nil
}
func (s *ProjectStore) writeAuxJSON(project, rel string, doc ProjectDocument) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return s.writeAux(project, rel, append(data, '\n'))
}
