package domain

import "fmt"

type QualificationMode string

const (
	QualificationAny QualificationMode = "any"
	QualificationAll QualificationMode = "all"
)

type MembershipResult string

const (
	MembershipMember        MembershipResult = "member"
	MembershipNotMember     MembershipResult = "not_member"
	MembershipIndeterminate MembershipResult = "indeterminate"
)

type QualificationDecision string

const (
	QualificationEligible      QualificationDecision = "eligible"
	QualificationIneligible    QualificationDecision = "ineligible"
	QualificationIndeterminate QualificationDecision = "indeterminate"
)

func EvaluateQualification(mode QualificationMode, results []MembershipResult) (QualificationDecision, error) {
	if mode != QualificationAny && mode != QualificationAll {
		return "", fmt.Errorf("unknown qualification mode %q", mode)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("at least one membership result is required")
	}

	hasMember := false
	hasNotMember := false
	hasIndeterminate := false
	for _, result := range results {
		switch result {
		case MembershipMember:
			hasMember = true
		case MembershipNotMember:
			hasNotMember = true
		case MembershipIndeterminate:
			hasIndeterminate = true
		default:
			return "", fmt.Errorf("unknown membership result %q", result)
		}
	}

	if mode == QualificationAny {
		if hasMember {
			return QualificationEligible, nil
		}
		if hasIndeterminate {
			return QualificationIndeterminate, nil
		}
		return QualificationIneligible, nil
	}

	if hasNotMember {
		return QualificationIneligible, nil
	}
	if hasIndeterminate {
		return QualificationIndeterminate, nil
	}
	return QualificationEligible, nil
}

func ApplyQualification(account *AccessAccount, decision QualificationDecision) (AccessChange, error) {
	if account == nil {
		return AccessChange{}, fmt.Errorf("access account is required")
	}
	switch decision {
	case QualificationEligible:
		return account.SetEligibility(true), nil
	case QualificationIneligible:
		return account.SetEligibility(false), nil
	case QualificationIndeterminate:
		return AccessChange{}, nil
	default:
		return AccessChange{}, fmt.Errorf("unknown qualification decision %q", decision)
	}
}
