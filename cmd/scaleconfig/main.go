// Command scaleconfig emits a deterministic 1000-user dual-stack
// four-protocol sing-box configuration on stdout. The release workflow feeds
// the output to a real sing-box binary (check -c) to prove the generated
// schema stays valid at the contracted deployment scale.
package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

const userCount = 1000

func main() {
	settings := scaleSettings()
	config, err := singbox.Generator{}.Generate(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scaleconfig: generate: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(config); err != nil {
		fmt.Fprintf(os.Stderr, "scaleconfig: write: %v\n", err)
		os.Exit(1)
	}
}

func scaleSettings() singbox.Settings {
	users := make([]singbox.User, 0, userCount)
	for id := int64(1); id <= userCount; id++ {
		users = append(users, singbox.User{TelegramID: id, Credentials: scaleCredentials(id)})
	}
	return singbox.Settings{
		ListenIPv4:         "203.0.113.10",
		ListenIPv6:         "2001:db8::10",
		VLESSPort:          443,
		Hysteria2Port:      443,
		TUICPort:           8443,
		AnyTLSPort:         8443,
		TLSServerName:      "vpn.example.com",
		TLSCertificatePath: "/run/tls/fullchain.pem",
		TLSKeyPath:         "/run/tls/privkey.pem",
		RealityServer:      "www.example.com",
		RealityServerPort:  443,
		RealityPrivateKey:  base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32))),
		RealityShortID:     "0123456789abcdef",
		StatsListen:        "127.0.0.1:10085",
		AllowIPv4Outbound:  false,
		Users:              users,
	}
}

func scaleCredentials(id int64) domain.CredentialBundle {
	token := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%032d", id)))
	return domain.CredentialBundle{
		SubscriptionToken: token,
		VLESSUUID:         scaleUUID("11111111", id),
		Hysteria2Password: "scale-hysteria2-" + strconv.FormatInt(id, 10),
		TUICUUID:          scaleUUID("22222222", id),
		TUICPassword:      "scale-tuic-" + strconv.FormatInt(id, 10),
		AnyTLSPassword:    "scale-anytls-" + strconv.FormatInt(id, 10),
	}
}

// scaleUUID builds a canonical RFC 4122 version-4 shaped UUID from a fixed
// eight-character prefix and the numeric id; the layout keeps version 4 and
// the RFC variant nibble so generator validation accepts every value.
func scaleUUID(prefix string, id int64) string {
	digits := fmt.Sprintf("%022d", id)
	return fmt.Sprintf("%s-%s-4%s-8%s-%s", prefix, digits[0:4], digits[4:7], digits[7:10], digits[10:22])
}
