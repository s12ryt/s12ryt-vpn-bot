package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/backup"
)

const maxArchiveBytes = 512 << 20

func main() {
	if err := run(); err != nil {
		log.Printf("restore failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	archivePath := os.Getenv("RESTORE_ARCHIVE")
	if !filepath.IsAbs(archivePath) || filepath.Clean(archivePath) != archivePath {
		return errors.New("RESTORE_ARCHIVE must be a clean absolute path")
	}
	masterKey, err := backup.DecodeMasterKey(os.Getenv("APP_MASTER_KEY"))
	if err != nil {
		return err
	}
	archive, err := backup.NewArchive(masterKey, nil)
	if err != nil {
		return err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return errors.New("open restore archive")
	}
	defer file.Close()
	sealed, err := io.ReadAll(io.LimitReader(file, maxArchiveBytes+1))
	if err != nil || len(sealed) > maxArchiveBytes {
		return errors.New("read restore archive")
	}
	plaintext, err := archive.Open(sealed)
	if err != nil {
		return err
	}
	command := exec.CommandContext(context.Background(), "pg_restore", "--clean", "--if-exists", "--no-owner", "--no-privileges")
	command.Env = append(os.Environ(), "PGDATABASE="+databaseURL)
	command.Stdin = bytes.NewReader(plaintext)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return errors.New("pg_restore failed")
	}
	return nil
}
