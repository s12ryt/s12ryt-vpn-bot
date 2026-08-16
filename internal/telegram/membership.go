package telegram

import "github.com/s12ryt/s12ryt-vpn-bot/internal/domain"

type MembershipEvent struct {
	ChatID     int64
	ChatType   ChatType
	TelegramID int64
	Result     domain.MembershipResult
}

func MembershipResult(member ChatMember) domain.MembershipResult {
	switch member.Status {
	case "creator", "administrator", "member":
		return domain.MembershipMember
	case "restricted":
		if member.IsMember == nil {
			return domain.MembershipIndeterminate
		}
		if *member.IsMember {
			return domain.MembershipMember
		}
		return domain.MembershipNotMember
	case "left", "kicked":
		return domain.MembershipNotMember
	default:
		return domain.MembershipIndeterminate
	}
}
