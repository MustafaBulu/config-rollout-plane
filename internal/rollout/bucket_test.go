package rollout

import "testing"

func TestBucketIsDeterministic(t *testing.T) {
	key := AssignmentKey{
		ConfigDefinitionID: "cfg-1",
		CandidateVersionID: "ver-2",
		AgentID:            "agent-1",
	}

	first, err := Bucket(key)
	if err != nil {
		t.Fatalf("bucket: %v", err)
	}
	second, err := Bucket(key)
	if err != nil {
		t.Fatalf("bucket: %v", err)
	}

	if first != second {
		t.Fatalf("expected same bucket, got %d and %d", first, second)
	}
}

func TestBucketIsInExpectedRange(t *testing.T) {
	for _, agentID := range []string{"agent-1", "agent-2", "agent-3", "agent-4"} {
		bucket, err := Bucket(AssignmentKey{
			ConfigDefinitionID: "cfg-1",
			CandidateVersionID: "ver-2",
			AgentID:            agentID,
		})
		if err != nil {
			t.Fatalf("bucket: %v", err)
		}
		if bucket < 0 || bucket >= BucketCount {
			t.Fatalf("bucket out of range: %d", bucket)
		}
	}
}

func TestIncreasingPercentageKeepsEarlierEligibleAgents(t *testing.T) {
	for i := 0; i < BucketCount; i++ {
		if Eligible(i, 5) && !Eligible(i, 25) {
			t.Fatalf("bucket %d was eligible at 5 percent but not at 25 percent", i)
		}
		if Eligible(i, 25) && !Eligible(i, 100) {
			t.Fatalf("bucket %d was eligible at 25 percent but not at 100 percent", i)
		}
	}
}

func TestEligibleUsesPercentageThresholds(t *testing.T) {
	tests := []struct {
		bucket     int
		percentage int
		want       bool
	}{
		{bucket: 0, percentage: 5, want: true},
		{bucket: 499, percentage: 5, want: true},
		{bucket: 500, percentage: 5, want: false},
		{bucket: 2499, percentage: 25, want: true},
		{bucket: 2500, percentage: 25, want: false},
		{bucket: 9999, percentage: 100, want: true},
	}

	for _, tt := range tests {
		if got := Eligible(tt.bucket, tt.percentage); got != tt.want {
			t.Fatalf("Eligible(%d, %d) = %v, want %v", tt.bucket, tt.percentage, got, tt.want)
		}
	}
}
