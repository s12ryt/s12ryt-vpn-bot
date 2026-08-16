package domain

import (
	"testing"
	"time"
)

func TestEvaluateQualificationAnyMode(t *testing.T) {
	tests := []struct {
		name    string
		results []MembershipResult
		want    QualificationDecision
	}{
		{name: "任一群符合即合格", results: []MembershipResult{MembershipNotMember, MembershipMember}, want: QualificationEligible},
		{name: "全數明確不符合才不合格", results: []MembershipResult{MembershipNotMember, MembershipNotMember}, want: QualificationIneligible},
		{name: "無符合且有暫時錯誤時不做撤銷決策", results: []MembershipResult{MembershipNotMember, MembershipIndeterminate}, want: QualificationIndeterminate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluateQualification(QualificationAny, tt.results)
			if err != nil {
				t.Fatalf("EvaluateQualification() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("EvaluateQualification() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEvaluateQualificationAllMode(t *testing.T) {
	tests := []struct {
		name    string
		results []MembershipResult
		want    QualificationDecision
	}{
		{name: "全部符合才合格", results: []MembershipResult{MembershipMember, MembershipMember}, want: QualificationEligible},
		{name: "任一明確不符合即不合格", results: []MembershipResult{MembershipMember, MembershipNotMember, MembershipIndeterminate}, want: QualificationIneligible},
		{name: "無不符合但有暫時錯誤時不做核發決策", results: []MembershipResult{MembershipMember, MembershipIndeterminate}, want: QualificationIndeterminate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluateQualification(QualificationAll, tt.results)
			if err != nil {
				t.Fatalf("EvaluateQualification() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("EvaluateQualification() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEvaluateQualificationRejectsInvalidInput(t *testing.T) {
	if _, err := EvaluateQualification(QualificationAny, nil); err == nil {
		t.Fatal("EvaluateQualification() error = nil, want error for no groups")
	}
	if _, err := EvaluateQualification(QualificationMode("some"), []MembershipResult{MembershipMember}); err == nil {
		t.Fatal("EvaluateQualification() error = nil, want error for unknown mode")
	}
	if _, err := EvaluateQualification(QualificationAny, []MembershipResult{MembershipResult("broken")}); err == nil {
		t.Fatal("EvaluateQualification() error = nil, want error for unknown membership result")
	}
}

func TestApplyQualificationDoesNotRevokeOnIndeterminateResult(t *testing.T) {
	issuedAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	account, err := NewAccessAccount(12345)
	if err != nil {
		t.Fatalf("NewAccessAccount() error = %v", err)
	}
	account.SetEligibility(true)
	if _, err := account.Claim(issuedAt); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}

	outcome, err := ApplyQualification(account, QualificationIndeterminate)
	if err != nil {
		t.Fatalf("ApplyQualification() error = %v", err)
	}
	if outcome.RevokeCredentialsImmediately {
		t.Fatal("indeterminate result must not revoke credentials")
	}
	if snapshot := account.Snapshot(); snapshot.Status != AccessStatusActive || !snapshot.Eligible {
		t.Fatalf("account after indeterminate result = %#v, want unchanged active account", snapshot)
	}
}

func TestApplyQualificationRevokesOnlyOnExplicitIneligibleResult(t *testing.T) {
	issuedAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	account, err := NewAccessAccount(12345)
	if err != nil {
		t.Fatalf("NewAccessAccount() error = %v", err)
	}
	account.SetEligibility(true)
	if _, err := account.Claim(issuedAt); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}

	outcome, err := ApplyQualification(account, QualificationIneligible)
	if err != nil {
		t.Fatalf("ApplyQualification() error = %v", err)
	}
	if !outcome.RevokeCredentialsImmediately {
		t.Fatal("explicit ineligible result must revoke credentials immediately")
	}
	if snapshot := account.Snapshot(); snapshot.Status != AccessStatusPendingApproval || snapshot.Eligible {
		t.Fatalf("account after ineligible result = %#v, want ineligible pending approval", snapshot)
	}
}

func TestApplyQualificationRejectsUnknownDecisionWithoutMutation(t *testing.T) {
	account, err := NewAccessAccount(12345)
	if err != nil {
		t.Fatalf("NewAccessAccount() error = %v", err)
	}
	account.SetEligibility(true)
	before := account.Snapshot()

	if _, err := ApplyQualification(account, QualificationDecision("broken")); err == nil {
		t.Fatal("ApplyQualification() error = nil, want unknown decision error")
	}
	if after := account.Snapshot(); after != before {
		t.Fatalf("account mutated from %#v to %#v", before, after)
	}
}
