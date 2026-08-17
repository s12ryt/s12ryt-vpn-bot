package main

import (
	"bytes"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

func TestScaleSettingsGenerateContractedConfiguration(t *testing.T) {
	settings := scaleSettings()
	if len(settings.Users) != userCount {
		t.Fatalf("users = %d, want %d", len(settings.Users), userCount)
	}
	for index, user := range settings.Users {
		wantID := int64(index + 1)
		if user.TelegramID != wantID {
			t.Fatalf("user[%d].TelegramID = %d, want %d", index, user.TelegramID, wantID)
		}
	}

	first, err := (singbox.Generator{}).Generate(settings)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	second, err := (singbox.Generator{}).Generate(settings)
	if err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("scale configuration must be deterministic")
	}
}
