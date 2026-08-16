package telegram

import (
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestMembershipResultMapsTelegramStatusesWithoutUnsafeRevocation(t *testing.T) {
	member := true
	notMember := false
	tests := []struct {
		name       string
		chatMember ChatMember
		want       domain.MembershipResult
	}{
		{name: "creator", chatMember: ChatMember{Status: "creator"}, want: domain.MembershipMember},
		{name: "administrator", chatMember: ChatMember{Status: "administrator"}, want: domain.MembershipMember},
		{name: "member", chatMember: ChatMember{Status: "member"}, want: domain.MembershipMember},
		{name: "restricted member", chatMember: ChatMember{Status: "restricted", IsMember: &member}, want: domain.MembershipMember},
		{name: "restricted non-member", chatMember: ChatMember{Status: "restricted", IsMember: &notMember}, want: domain.MembershipNotMember},
		{name: "left", chatMember: ChatMember{Status: "left"}, want: domain.MembershipNotMember},
		{name: "kicked", chatMember: ChatMember{Status: "kicked"}, want: domain.MembershipNotMember},
		{name: "unknown status", chatMember: ChatMember{Status: "future_status"}, want: domain.MembershipIndeterminate},
		{name: "restricted without is_member", chatMember: ChatMember{Status: "restricted"}, want: domain.MembershipIndeterminate},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := MembershipResult(test.chatMember); got != test.want {
				t.Fatalf("MembershipResult() = %q, want %q", got, test.want)
			}
		})
	}
}
