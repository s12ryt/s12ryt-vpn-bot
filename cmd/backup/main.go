package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/backup"
)

const (
	backupDirectoryEnv = "BACKUP_DIR"
	retentionDaysEnv   = "BACKUP_RETENTION_DAYS"
	fileMode           = 0600
	maxDumpBytes       = 512 << 20
)

func main() {
	if err := run(); err != nil {
		log.Printf("backup service stopped: %v", err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	masterKey, err := backup.DecodeMasterKey(os.Getenv("APP_MASTER_KEY"))
	if err != nil {
		return err
	}
	archive, err := backup.NewArchive(masterKey, nil)
	if err != nil {
		return err
	}
	directory := os.Getenv(backupDirectoryEnv)
	if directory == "" {
		directory = "/var/lib/s12ryt/backups"
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return errors.New("BACKUP_DIR must be a clean absolute path")
	}
	retention, err := positiveInteger(os.Getenv(retentionDaysEnv), 7)
	if err != nil {
		return fmt.Errorf("BACKUP_RETENTION_DAYS: %w", err)
	}
	if err := os.MkdirAll(directory, 0750); err != nil {
		return errors.New("create backup directory")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	for {
		if err := createBackup(ctx, archive, databaseURL, directory, retention, time.Now().UTC()); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("backup attempt failed: %v", err)
		}
		timer := time.NewTimer(24 * time.Hour)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func createBackup(ctx context.Context, archive backup.Archive, databaseURL, directory string, retention int, now time.Time) error {
	command := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--no-owner", "--no-privileges")
	command.Env = append(os.Environ(), "PGDATABASE="+databaseURL)
	var dump bytes.Buffer
	command.Stdout = &limitedWriter{writer: &dump, remaining: maxDumpBytes}
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return errors.New("pg_dump failed")
	}
	sealed, err := archive.Seal(dump.Bytes())
	if err != nil {
		return err
	}
	name := "vpn-" + now.Format("20060102T150405Z") + ".dump.enc"
	if err := atomicWrite(filepath.Join(directory, name), sealed); err != nil {
		return err
	}
	return prune(directory, retention, now)
}

type limitedWriter struct {
	writer    io.Writer
	remaining int
}

func (w *limitedWriter) Write(value []byte) (int, error) {
	if len(value) > w.remaining {
		return 0, errors.New("database dump exceeds safety limit")
	}
	n, err := w.writer.Write(value)
	w.remaining -= n
	return n, err
}

func atomicWrite(path string, content []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".backup-*.tmp")
	if err != nil {
		return errors.New("create backup temporary file")
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(fileMode); err != nil {
		temporary.Close()
		return errors.New("secure backup temporary file")
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return errors.New("write backup archive")
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return errors.New("sync backup archive")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("close backup archive")
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return errors.New("promote backup archive")
	}
	dir, err := os.Open(directory)
	if err != nil {
		return errors.New("open backup directory")
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return errors.New("sync backup directory")
	}
	return nil
}

func prune(directory string, retentionDays int, now time.Time) error {
	entries, err := filepath.Glob(filepath.Join(directory, "vpn-*.dump.enc"))
	if err != nil {
		return errors.New("list backup archives")
	}
	sort.Strings(entries)
	cutoff := now.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	for _, entry := range entries {
		info, err := os.Lstat(entry)
		if err != nil {
			return errors.New("inspect backup archive")
		}
		if info.Mode().IsRegular() && info.ModTime().Before(cutoff) {
			if err := os.Remove(entry); err != nil {
				return errors.New("remove expired backup archive")
			}
		}
	}
	return nil
}

func positiveInteger(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > 3650 {
		return 0, errors.New("must be between 1 and 3650")
	}
	return parsed, nil
}
