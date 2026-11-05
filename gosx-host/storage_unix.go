//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func lockStorage(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func checkStorageVolume(path string) error {
	if !strings.HasPrefix(path, "/Volumes/") {
		return nil
	}
	volume := filepath.Join("/Volumes", strings.Split(strings.TrimPrefix(path, "/Volumes/"), "/")[0])
	var mounted, parent unix.Stat_t
	if unix.Stat(volume, &mounted) != nil || unix.Stat("/Volumes", &parent) != nil || mounted.Dev == parent.Dev {
		return fmt.Errorf("connect the configured storage volume before opening Storyboard Desk")
	}
	return nil
}

func replaceStoredFile(dir *os.Root, from, to string) error { return dir.Rename(from, to) }

func syncStorageDirectory(dir *os.Root) error {
	f, err := dir.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func storageFree(path string) (uint64, error) {
	var info unix.Statfs_t
	err := unix.Statfs(path, &info)
	return uint64(info.Bavail) * uint64(info.Bsize), err
}
