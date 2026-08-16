package trafficstats

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	spoolVersion = 1
	maxSpoolSize = 1 << 20
)

var ErrPendingBatchExists = errors.New("a traffic batch is already pending")

type PendingBatch struct {
	ID          string
	CollectedAt time.Time
	Samples     []Sample
}

type spoolEnvelope struct {
	Version     int       `json:"version"`
	ID          string    `json:"id"`
	CollectedAt time.Time `json:"collected_at"`
	Samples     []Sample  `json:"samples"`
}

func NewPendingBatch(collectedAt time.Time, samples []Sample) (PendingBatch, error) {
	batch := PendingBatch{
		CollectedAt: collectedAt.UTC(),
		Samples:     append([]Sample(nil), samples...),
	}
	if err := validatePendingSamples(batch.CollectedAt, batch.Samples); err != nil {
		return PendingBatch{}, err
	}
	batch.ID = pendingBatchID(batch.CollectedAt, batch.Samples)
	return batch, nil
}

type FileSpool struct {
	path string
	mu   sync.Mutex
}

func NewFileSpool(path string) (*FileSpool, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == string(os.PathSeparator) || filepath.Base(path) == "." {
		return nil, errors.New("traffic spool path must be a clean absolute file path")
	}
	return &FileSpool{path: path}, nil
}

func (spool *FileSpool) Save(ctx context.Context, batch PendingBatch) error {
	if spool == nil || spool.path == "" {
		return errors.New("traffic spool is not initialized")
	}
	if err := validatePendingBatch(batch); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	spool.mu.Lock()
	defer spool.mu.Unlock()

	if _, err := os.Lstat(spool.path); err == nil {
		return ErrPendingBatchExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect traffic spool: %w", err)
	}

	directory := filepath.Dir(spool.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create traffic spool directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".traffic-spool-*")
	if err != nil {
		return fmt.Errorf("create temporary traffic spool: %w", err)
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure temporary traffic spool: %w", err)
	}
	payload := spoolEnvelope{
		Version:     spoolVersion,
		ID:          batch.ID,
		CollectedAt: batch.CollectedAt.UTC(),
		Samples:     append([]Sample(nil), batch.Samples...),
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return fmt.Errorf("encode traffic spool: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary traffic spool: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary traffic spool: %w", err)
	}
	if err := os.Rename(temporaryPath, spool.path); err != nil {
		return fmt.Errorf("promote traffic spool: %w", err)
	}
	keep = true
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync traffic spool directory: %w", err)
	}
	return nil
}

func (spool *FileSpool) Load(ctx context.Context) (PendingBatch, bool, error) {
	if spool == nil || spool.path == "" {
		return PendingBatch{}, false, errors.New("traffic spool is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return PendingBatch{}, false, err
	}

	spool.mu.Lock()
	defer spool.mu.Unlock()

	info, err := os.Lstat(spool.path)
	if errors.Is(err, os.ErrNotExist) {
		return PendingBatch{}, false, nil
	}
	if err != nil {
		return PendingBatch{}, false, fmt.Errorf("inspect traffic spool: %w", err)
	}
	unsafePermissions := runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0
	if !info.Mode().IsRegular() || unsafePermissions || info.Size() > maxSpoolSize {
		return PendingBatch{}, false, errors.New("traffic spool has unsafe metadata")
	}

	file, err := os.Open(spool.path)
	if err != nil {
		return PendingBatch{}, false, fmt.Errorf("open traffic spool: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(bufio.NewReader(io.LimitReader(file, maxSpoolSize+1)))
	decoder.DisallowUnknownFields()
	var envelope spoolEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return PendingBatch{}, false, errors.New("traffic spool contains invalid JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return PendingBatch{}, false, errors.New("traffic spool contains trailing data")
	}
	if envelope.Version != spoolVersion {
		return PendingBatch{}, false, errors.New("traffic spool version is unsupported")
	}
	batch := PendingBatch{
		ID:          envelope.ID,
		CollectedAt: envelope.CollectedAt,
		Samples:     append([]Sample(nil), envelope.Samples...),
	}
	if err := validatePendingBatch(batch); err != nil {
		return PendingBatch{}, false, errors.New("traffic spool contains an invalid batch")
	}
	return batch, true, nil
}

func (spool *FileSpool) Delete(ctx context.Context) error {
	if spool == nil || spool.path == "" {
		return errors.New("traffic spool is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	spool.mu.Lock()
	defer spool.mu.Unlock()
	info, err := os.Lstat(spool.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect traffic spool: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("traffic spool is not a regular file")
	}
	if err := os.Remove(spool.path); err != nil {
		return fmt.Errorf("delete traffic spool: %w", err)
	}
	if err := syncDirectory(filepath.Dir(spool.path)); err != nil {
		return fmt.Errorf("sync traffic spool directory: %w", err)
	}
	return nil
}

func validatePendingBatch(batch PendingBatch) error {
	if len(batch.ID) != sha256.Size*2 {
		return errors.New("traffic batch ID is invalid")
	}
	decodedID, err := hex.DecodeString(batch.ID)
	if err != nil || len(decodedID) != sha256.Size {
		return errors.New("traffic batch ID is invalid")
	}
	if err := validatePendingSamples(batch.CollectedAt, batch.Samples); err != nil {
		return err
	}
	if batch.ID != pendingBatchID(batch.CollectedAt.UTC(), batch.Samples) {
		return errors.New("traffic batch ID does not match its content")
	}
	return nil
}

func validatePendingSamples(collectedAt time.Time, samples []Sample) error {
	if collectedAt.IsZero() {
		return errors.New("traffic batch collection time is required")
	}
	var previousID int64
	for index, sample := range samples {
		if sample.TelegramID <= 0 || sample.Uplink < 0 || sample.Downlink < 0 || sample.Uplink > math.MaxInt64-sample.Downlink {
			return errors.New("traffic batch contains an invalid sample")
		}
		if index > 0 && sample.TelegramID <= previousID {
			return errors.New("traffic batch users must be strictly increasing")
		}
		previousID = sample.TelegramID
	}
	return nil
}

func pendingBatchID(collectedAt time.Time, samples []Sample) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, "s12ryt-traffic-batch-v1\x00")
	_, _ = io.WriteString(hash, collectedAt.UTC().Format(time.RFC3339Nano))
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(len(samples)))
	_, _ = hash.Write(encoded[:])
	for _, sample := range samples {
		binary.BigEndian.PutUint64(encoded[:], uint64(sample.TelegramID))
		_, _ = hash.Write(encoded[:])
		binary.BigEndian.PutUint64(encoded[:], uint64(sample.Uplink))
		_, _ = hash.Write(encoded[:])
		binary.BigEndian.PutUint64(encoded[:], uint64(sample.Downlink))
		_, _ = hash.Write(encoded[:])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
