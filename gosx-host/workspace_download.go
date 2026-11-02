package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/desktop"
)

// Native exports use the host's dialog directly. WKWebView does not implement
// the browser's attachment-download UI on every supported desktop backend.
func (h *workspaceHandler) finishExport(w http.ResponseWriter, r *http.Request, id, rel string) error {
	if h.saveFile == nil {
		http.Redirect(w, r, mediaURL(id, rel), http.StatusSeeOther)
		return nil
	}
	if !strings.HasPrefix(rel, "exports/") {
		return fmt.Errorf("choose a saved export")
	}
	f, err := h.store.OpenMedia(id, rel)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("export is unavailable")
	}
	return h.saveDownload(w, r, filepath.Base(rel), f)
}

func (h *workspaceHandler) downloadSaved(w http.ResponseWriter, r *http.Request, id string, values url.Values) error {
	if h.saveFile == nil {
		return fmt.Errorf("native saving is unavailable in browser mode")
	}
	switch values.Get("kind") {
	case "export":
		return h.finishExport(w, r, id, values.Get("path"))
	case "plan", "script":
		p, err := h.store.Load(id)
		if err != nil {
			return err
		}
		if values.Get("kind") == "script" {
			return h.saveDownload(w, r, "storyboard.txt", strings.NewReader(scriptText(p)))
		}
		return h.saveDownload(w, r, "storyboard-plan.json", strings.NewReader(jsonText(projectPlan(p))))
	default:
		return fmt.Errorf("choose a saved export, plan or script")
	}
}

func (h *workspaceHandler) saveDownload(w http.ResponseWriter, r *http.Request, name string, source io.Reader) error {
	if !h.saveMu.TryLock() {
		return fmt.Errorf("finish the open Save dialog before saving another file")
	}
	defer h.saveMu.Unlock()
	ext := filepath.Ext(name)
	destination, err := h.saveFile(desktop.SaveFileOptions{
		Title: "Save " + name, InitialFilename: name, DefaultExt: strings.TrimPrefix(ext, "."),
		Filters:         []desktop.FileFilter{{Name: strings.ToUpper(strings.TrimPrefix(ext, ".")) + " file", Pattern: "*" + ext}},
		OverwritePrompt: true,
	})
	if err != nil {
		return fmt.Errorf("open Save dialog: %w", err)
	}
	if destination != "" {
		if !filepath.IsAbs(destination) || !strings.EqualFold(filepath.Ext(destination), ext) {
			return fmt.Errorf("save the file with its %s extension", ext)
		}
		if err = saveExportCopy(h.store.path, destination, source); err != nil {
			return fmt.Errorf("save export: %w", err)
		}
	}
	if r.Header.Get("Accept") == "application/json" {
		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(map[string]any{"saved": destination != "", "name": filepath.Base(destination)})
	}
	// An attachment or a cancelled dialog must preserve the current page and its
	// unsaved forms, including when the browser enhancement has not booted yet.
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func saveExportCopy(storage, destination string, source io.Reader) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(storage, parent)
	if err == nil && (rel == "." || filepath.IsLocal(rel)) {
		return fmt.Errorf("choose a folder outside the project's storage directory")
	}
	dir, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	name := filepath.Base(destination)
	if info, statErr := dir.Lstat(name); statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	} else if statErr == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("choose a regular destination file, without a symlink")
	}
	id, err := newStorageID()
	if err != nil {
		return err
	}
	temp := ".storyboard-export-" + id + ".tmp"
	f, err := dir.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer dir.Remove(temp)
	_, err = io.Copy(f, source)
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
	if err = replaceStoredFile(dir, temp, name); err != nil {
		return err
	}
	return syncStorageDirectory(dir)
}
