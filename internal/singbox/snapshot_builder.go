package singbox

import (
	"context"
	"errors"
	"fmt"
)

type SnapshotSettingsLoader interface {
	Load(ctx context.Context) (Settings, error)
}

type SnapshotUserLoader interface {
	ListActive(ctx context.Context) ([]User, error)
}

type SnapshotGenerator interface {
	Generate(settings Settings) ([]byte, error)
}

type SnapshotBuilder struct {
	settings  SnapshotSettingsLoader
	users     SnapshotUserLoader
	generator SnapshotGenerator
}

func NewSnapshotBuilder(settings SnapshotSettingsLoader, users SnapshotUserLoader, generator SnapshotGenerator) *SnapshotBuilder {
	return &SnapshotBuilder{settings: settings, users: users, generator: generator}
}

func (builder *SnapshotBuilder) Build(ctx context.Context) ([]byte, error) {
	if builder == nil || builder.settings == nil || builder.users == nil || builder.generator == nil {
		return nil, errors.New("snapshot builder dependencies are required")
	}
	settings, err := builder.settings.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load sing-box settings: %w", err)
	}
	users, err := builder.users.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("load active sing-box users: %w", err)
	}
	settings.Users = append([]User(nil), users...)
	generated, err := builder.generator.Generate(settings)
	if err != nil {
		return nil, fmt.Errorf("generate sing-box snapshot: %w", err)
	}
	return generated, nil
}
