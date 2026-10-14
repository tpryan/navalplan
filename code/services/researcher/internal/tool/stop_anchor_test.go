package tool

import "testing"

func TestAnchorCoords(t *testing.T) {
	// Stop 1 (Sunrise Blvd bridge) vs downtown Fort Lauderdale city centre.
	const stopLat, stopLng = 26.1371, -80.1189
	tests := []struct {
		name             string
		reqLat, reqLng   float64
		wantLat, wantLng float64
		wantOverridden   bool
	}{
		{"city centre is pulled back to the stop", 26.1224, -80.1373, stopLat, stopLng, true},
		{"request at the stop is kept", stopLat, stopLng, stopLat, stopLng, false},
		{"small offset within 1 NM is kept", 26.1400, -80.1200, 26.1400, -80.1200, false},
	}
	for _, tc := range tests {
		lat, lng, over := anchorCoords(tc.reqLat, tc.reqLng, stopLat, stopLng)
		if lat != tc.wantLat || lng != tc.wantLng || over != tc.wantOverridden {
			t.Errorf("%s: got (%v, %v, %v), want (%v, %v, %v)", tc.name, lat, lng, over, tc.wantLat, tc.wantLng, tc.wantOverridden)
		}
	}
}

func TestStopAnchorWithoutState(t *testing.T) {
	if _, _, ok := stopAnchor(nil); ok {
		t.Error("stopAnchor(nil) ok = true, want false")
	}
	// The strict mock panics on State(); stopAnchor must recover and report no anchor.
	if _, _, ok := stopAnchor(newMockContext()); ok {
		t.Error("stopAnchor(strict mock) ok = true, want false")
	}
	req := PlacesRequest{Query: "pharmacy", Latitude: 1, Longitude: 2}
	if got := anchorRequest(newMockContext(), req); got != req {
		t.Errorf("anchorRequest without anchor changed request: %+v", got)
	}
}
