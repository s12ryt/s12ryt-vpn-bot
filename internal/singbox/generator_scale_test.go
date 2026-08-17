package singbox

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

// TestGeneratorHandlesThousandUserDualStackScale covers the contracted
// deployment ceiling: 1000 known users across four protocols on both address
// families must generate a deterministic, structurally complete configuration
// in bounded time.
func TestGeneratorHandlesThousandUserDualStackScale(t *testing.T) {
	if testing.Short() {
		t.Skip("scale generation is skipped in short mode")
	}
	settings := testSettings()
	settings.Users = make([]User, 0, 1000)
	for id := int64(1); id <= 1000; id++ {
		settings.Users = append(settings.Users, User{TelegramID: id, Credentials: scaleCredentialBundle(id)})
	}

	started := time.Now()
	first, err := Generator{}.Generate(settings)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	elapsed := time.Since(started)
	if elapsed > 30*time.Second {
		t.Fatalf("Generate() took %s for 1000 users, want bounded generation time", elapsed)
	}
	t.Logf("generated 1000-user configuration in %s (%d bytes)", elapsed, len(first))

	second, err := Generator{}.Generate(settings)
	if err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("generation must stay deterministic at scale")
	}

	var document struct {
		Inbounds []struct {
			Tag   string `json:"tag"`
			Users []struct {
				Name string `json:"name"`
			} `json:"users"`
		} `json:"inbounds"`
		Experimental struct {
			V2RayAPI struct {
				Stats struct {
					Users []string `json:"users"`
				} `json:"stats"`
			} `json:"v2ray_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(first, &document); err != nil {
		t.Fatalf("decode generated configuration: %v", err)
	}
	if len(document.Inbounds) != 8 {
		t.Fatalf("inbounds = %d, want 8 (four protocols x dual stack)", len(document.Inbounds))
	}
	for _, inbound := range document.Inbounds {
		if len(inbound.Users) != 1000 {
			t.Fatalf("inbound %s carries %d users, want 1000", inbound.Tag, len(inbound.Users))
		}
	}
	if len(document.Experimental.V2RayAPI.Stats.Users) != 1000 {
		t.Fatalf("stats tracks %d users, want 1000", len(document.Experimental.V2RayAPI.Stats.Users))
	}
	if strings.Contains(string(first), settings.Users[0].Credentials.SubscriptionToken) {
		t.Fatal("subscription tokens must never appear in the core configuration")
	}
}

func scaleCredentialBundle(id int64) domain.CredentialBundle {
	token := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%032d", id)))
	digits := fmt.Sprintf("%022d", id)
	return domain.CredentialBundle{
		SubscriptionToken: token,
		VLESSUUID:         fmt.Sprintf("11111111-%s-4%s-8%s-%s", digits[0:4], digits[4:7], digits[7:10], digits[10:22]),
		Hysteria2Password: fmt.Sprintf("scale-hysteria2-%04d", id),
		TUICUUID:          fmt.Sprintf("22222222-%s-4%s-8%s-%s", digits[0:4], digits[4:7], digits[7:10], digits[10:22]),
		TUICPassword:      fmt.Sprintf("scale-tuic-%04d", id),
		AnyTLSPassword:    fmt.Sprintf("scale-anytls-%04d", id),
	}
}
