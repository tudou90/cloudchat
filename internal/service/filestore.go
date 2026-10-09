package service

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"cloudchat/internal/database"
	"github.com/google/uuid"
)

// File contents live on disk at <fileDir>/<room ID>/<file ID>; Redis holds
// each file's metadata with the room's expiry. A janitor deletes files from
// disk once their metadata is gone (room expired, emptied or closed).

const tmpPrefix = ".tmp-"

var (
	fileDir   string
	filesUsed atomic.Int64 // bytes of files on disk
)

// InitFileStore sets (and creates) the directory that holds file contents.
func InitFileStore(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return err
	}
	fileDir = abs
	return nil
}

// FileDir is the directory holding file contents.
func FileDir() string { return fileDir }

// FilesUsed reports how many bytes of files are stored on disk.
func FilesUsed() int64 { return filesUsed.Load() }

// filePath is where a file's content is stored. Both IDs are validated by
// callers (UUID and random ID), so they can't escape fileDir.
func filePath(roomID, fileID string) string { return filepath.Join(fileDir, roomID, fileID) }

// DeleteRoomFiles removes a room's files from disk at once (e.g. when a
// moderator closes it) instead of waiting for the janitor.
func DeleteRoomFiles(roomID string) error {
	if _, err := uuid.Parse(roomID); err != nil || fileDir == "" {
		return errors.New("invalid room")
	}
	return os.RemoveAll(filepath.Join(fileDir, roomID))
}

// StartFileJanitor removes files whose metadata has expired every interval,
// and keeps FilesUsed up to date. Files younger than interval are left
// alone: their upload may still be finishing.
func StartFileJanitor(interval time.Duration) {
	SweepFiles(interval)
	go func() {
		for range time.Tick(interval) {
			SweepFiles(interval)
		}
	}()
}

// SweepFiles deletes expired files (and leftovers of failed uploads) older
// than grace and recounts the bytes stored.
func SweepFiles(grace time.Duration) {
	rooms, err := os.ReadDir(fileDir)
	if err != nil {
		log.Printf("File janitor: %v", err)
		return
	}
	var total int64
	removed := 0
	for _, r := range rooms {
		if _, err := uuid.Parse(r.Name()); err != nil || !r.IsDir() {
			continue
		}
		roomDir := filepath.Join(fileDir, r.Name())
		entries, err := os.ReadDir(roomDir)
		if err != nil {
			continue
		}
		pipe := database.RDB.Pipeline()
		exists := make([]func() int64, len(entries))
		for i, e := range entries {
			if !strings.HasPrefix(e.Name(), tmpPrefix) {
				exists[i] = pipe.Exists(database.Ctx, fileKey(r.Name(), e.Name())).Val
			}
		}
		if _, err := pipe.Exec(database.Ctx); err != nil {
			log.Printf("File janitor: checking Redis failed, skipping: %v", err)
			return // never delete on a Redis error
		}
		for i, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			young := time.Since(info.ModTime()) < grace
			if young || (exists[i] != nil && exists[i]() == 1) {
				total += info.Size()
				continue
			}
			if err := os.Remove(filepath.Join(roomDir, e.Name())); err == nil || errors.Is(err, fs.ErrNotExist) {
				removed++
			}
		}
		// Remove the room's directory once empty (fails harmlessly if not).
		if info, err := os.Stat(roomDir); err == nil && time.Since(info.ModTime()) >= grace {
			os.Remove(roomDir)
		}
	}
	filesUsed.Store(total)
	if removed > 0 {
		log.Printf("File janitor: deleted %d expired file(s)", removed)
	}
}
