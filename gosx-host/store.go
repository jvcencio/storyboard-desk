package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const storageLockName = ".storyboard-owner.lock"
const maxProjectBytes = 64 << 20

var ErrRevision = errors.New("project changed; reload before saving")
var ErrNotFound = errors.New("project or shot not found")
var errNoProjectChange = errors.New("project already contains this operation")

// ProjectDocument retains all fields, including data owned by unported workflows.
type ProjectDocument map[string]json.RawMessage

// ProjectStore is the sole writer for a data directory. Every command reads the
// latest document under mu and patches only fields it owns. The OS lock is also
// honored by the Python Store and is released automatically after a crash.
type ProjectStore struct {
	workflowMu  sync.RWMutex
	featuresMu  sync.Mutex
	settings    *AppSettings
	cloud       *CloudService
	drawing     *DrawingService
	mu          sync.Mutex
	root        *os.Root
	path        string
	identity    os.FileInfo
	owner       *os.File
	legacyOwner *os.File
	closed      bool
}

func OpenProjectStore(path string, create bool) (*ProjectStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("choose a data directory with -data-dir")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = checkStorageVolume(abs); err != nil {
		return nil, err
	}
	if create {
		// Resolve an existing parent before creating anything, including aliases
		// into a disconnected /Volumes mount point.
		for parent := abs; ; parent = filepath.Dir(parent) {
			resolved, e := filepath.EvalSymlinks(parent)
			if e == nil {
				rel, e := filepath.Rel(parent, abs)
				if e != nil {
					return nil, e
				}
				abs = filepath.Join(resolved, rel)
				break
			}
			if !os.IsNotExist(e) || filepath.Dir(parent) == parent {
				return nil, e
			}
		}
		if err = checkStorageVolume(abs); err != nil {
			return nil, err
		}
		if err = os.MkdirAll(abs, 0700); err != nil {
			return nil, err
		}
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("storage folder is unavailable (use -init-data-dir only for a new folder): %w", err)
	}
	if err = checkStorageVolume(abs); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	s := &ProjectStore{root: root, path: abs}
	ok := false
	defer func() {
		if !ok {
			s.Close()
		}
	}()
	s.identity, err = root.Stat(".")
	if err != nil {
		return nil, err
	}
	if info, e := root.Lstat(storageLockName); e == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("storage ownership file must be a regular file")
	} else if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	s.owner, err = root.OpenFile(storageLockName, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockStorage(s.owner); err != nil {
		return nil, fmt.Errorf("another Storyboard Desk process owns this data directory: %w", err)
	}
	if err = s.check(); err != nil {
		return nil, err
	}
	// A real byte keeps the file compatible with Python's Windows lock adapter.
	info, err := s.owner.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		if _, err = s.owner.WriteAt([]byte{0}, 0); err != nil {
			return nil, err
		}
		if err = s.owner.Sync(); err != nil {
			return nil, err
		}
	}
	if err = s.holdLegacyWorker(); err != nil {
		return nil, err
	}
	ok = true
	return s, nil
}

func (s *ProjectStore) Close() error {
	s.featuresMu.Lock()
	drawing := s.drawing
	s.featuresMu.Unlock()
	if drawing != nil {
		drawing.Close()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.legacyOwner != nil {
		err = s.legacyOwner.Close()
	}
	if s.owner != nil {
		err = errors.Join(err, s.owner.Close())
	}
	if s.root != nil {
		err = errors.Join(err, s.root.Close())
	}
	return err
}

func (s *ProjectStore) check() error {
	if s.closed {
		return fmt.Errorf("project store is closed")
	}
	if err := checkStorageVolume(s.path); err != nil {
		return err
	}
	current, err := os.Stat(s.path)
	if err != nil || !os.SameFile(s.identity, current) {
		return fmt.Errorf("storage folder is disconnected or was replaced; reopen it")
	}
	locked, err := s.owner.Stat()
	if err != nil {
		return err
	}
	current, err = s.root.Lstat(storageLockName)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(locked, current) {
		return fmt.Errorf("storage ownership file changed; reopen the store")
	}
	if s.legacyOwner != nil {
		locked, err = s.legacyOwner.Stat()
		if err != nil {
			return err
		}
		current, err = s.root.Lstat("cloud/worker.lock")
		if err != nil || !current.Mode().IsRegular() || !os.SameFile(locked, current) {
			return fmt.Errorf("legacy worker ownership file changed; reopen the store")
		}
	}
	return nil
}

// Older running Python services hold only cloud/worker.lock. Refuse to open
// their data and retain that lock until close. Older binaries still need to be
// stopped before upgrading; they cannot honor a protocol they do not implement.
func (s *ProjectStore) holdLegacyWorker() error {
	dir, err := projectDirectory(s.root, "cloud")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	info, err := dir.Lstat("worker.lock")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("legacy worker lock must be a regular file")
	}
	s.legacyOwner, err = dir.OpenFile("worker.lock", os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err = lockStorage(s.legacyOwner); err != nil {
		return fmt.Errorf("an older Storyboard Desk generation worker owns this data directory; stop it before opening the Go editor: %w", err)
	}
	return s.check()
}

func newStorageID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func rawValue(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func documentRevision(p ProjectDocument) int {
	var n int
	_ = json.Unmarshal(p["revision"], &n)
	return n
}

func projectDirectory(root *os.Root, id string) (*os.Root, error) {
	if !projectIDPattern.MatchString(id) {
		return nil, fmt.Errorf("invalid project identifier")
	}
	info, err := root.Lstat(id)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project directory must be a real directory")
	}
	return root.OpenRoot(id)
}

func readProjectDocument(dir *os.Root, id string) (ProjectDocument, error) {
	info, err := dir.Lstat("project.json")
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("project file must be a regular file")
	}
	f, err := dir.Open("project.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxProjectBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxProjectBytes {
		return nil, fmt.Errorf("project document is too large")
	}
	var p ProjectDocument
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("project JSON must be UTF-8")
	}
	if err = json.Unmarshal(data, &p); err != nil || p == nil {
		return nil, fmt.Errorf("invalid project JSON")
	}
	if err = validateDocument(p, id); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *ProjectStore) load(id string) (ProjectDocument, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	dir, err := projectDirectory(s.root, id)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return readProjectDocument(dir, id)
}

func (s *ProjectStore) Load(id string) (ProjectDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(id)
}

func (s *ProjectStore) List() ([]ProjectDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	f, err := s.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	projects := []ProjectDocument{}
	for _, entry := range entries {
		// Ignore private creation staging and auxiliary directories (cloud etc.).
		if !projectIDPattern.MatchString(entry.Name()) {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink in project directory: %s", entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		if _, e := s.root.Lstat(filepath.Join(entry.Name(), "project.json")); os.IsNotExist(e) {
			continue
		} else if e != nil {
			return nil, e
		}
		p, e := s.load(entry.Name())
		if e != nil {
			return nil, fmt.Errorf("project %s: %w", entry.Name(), e)
		}
		projects = append(projects, p)
	}
	sort.Slice(projects, func(i, j int) bool {
		a, b := shotText(projects[i], "created_at"), shotText(projects[j], "created_at")
		if a == b {
			return shotText(projects[i], "id") < shotText(projects[j], "id")
		}
		return a < b
	})
	return projects, nil
}

// A temp file and rename in the same directory make readers see the complete
// previous or next revision. Incomplete temps are never treated as projects.
func writeProjectDocument(dir *os.Root, p ProjectDocument) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > maxProjectBytes {
		return fmt.Errorf("project document is too large")
	}
	id, err := newStorageID()
	if err != nil {
		return err
	}
	name := ".project-" + id + ".tmp"
	f, err := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer dir.Remove(name)
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = replaceStoredFile(dir, name, "project.json"); err != nil {
		return err
	}
	if err = syncStorageDirectory(dir); err != nil {
		return fmt.Errorf("project replaced but directory sync failed; reload before retrying: %w", err)
	}
	return nil
}

func (s *ProjectStore) mutate(id string, revision int, edit func(ProjectDocument) error) (ProjectDocument, error) {
	s.workflowMu.RLock()
	defer s.workflowMu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.load(id)
	if err != nil {
		return nil, err
	}
	if revision < 0 || documentRevision(p) != revision {
		return nil, ErrRevision
	}
	if err = edit(p); errors.Is(err, errNoProjectChange) {
		return s.load(id)
	} else if err != nil {
		return nil, err
	}
	if revision == int(^uint(0)>>1) {
		return nil, fmt.Errorf("project revision overflow")
	}
	p["revision"] = rawValue(revision + 1)
	p["updated_at"] = rawValue(time.Now().UTC().Format(time.RFC3339Nano))
	// These values are recalculated by Python on load, not authoritative state.
	delete(p, "music_timing")
	if err = validateDocument(p, id); err != nil {
		return nil, err
	}
	if err = s.check(); err != nil {
		return nil, err
	}
	dir, err := projectDirectory(s.root, id)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	if err = writeProjectDocument(dir, p); err != nil {
		return nil, err
	}
	return p, nil
}
