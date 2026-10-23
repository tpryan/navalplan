package tool

import "testing"

func TestGetCandidateRegionsForMonth(t *testing.T) {
	_, rp, err := NewCandidateRegionsTool()
	if err != nil {
		t.Fatalf("NewCandidateRegionsTool() error = %v", err)
	}
	ctx, st := newStatefulMockContext()
	resp, err := rp.GetCandidateRegions(ctx, CandidateRegionsArgs{Month: 7})
	if err != nil {
		t.Fatalf("GetCandidateRegions() error = %v", err)
	}
	if len(resp.Regions) == 0 {
		t.Fatalf("expected non-empty candidate regions for month 7")
	}
	if _, err := st.Get(StateKeyCandidateRegions); err != nil {
		t.Errorf("expected candidate regions saved to session state, got %v", err)
	}
}
