package main

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Lock the same byte as Python's msvcrt.locking. Closing the handle releases it.
func lockStorage(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}

func checkStorageVolume(string) error { return nil }

func replaceStoredFile(dir *os.Root, from, to string) error {
	src, err := windows.UTF16PtrFromString(filepath.Join(dir.Name(), from))
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(filepath.Join(dir.Name(), to))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(src, dst, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// File contents are flushed before replacement; MoveFileEx requests write-through.
// Windows does not expose POSIX directory fsync through os.File.Sync.
func syncStorageDirectory(*os.Root) error { return nil }

func storageFree(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free, total, totalFree uint64
	err = windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree)
	return free, err
}
