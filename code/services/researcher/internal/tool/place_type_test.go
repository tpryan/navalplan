package tool

import "testing"

func TestClassifyPlace(t *testing.T) {
	tests := []struct {
		query string
		types []string
		want  string
	}{
		// Every query in facility_discovery.md yields a renderable type.
		{"anchorage", nil, TypeAnchorage},
		{"marina", []string{"marina", "point_of_interest"}, TypeMarina},
		{"yacht charter boat rental", []string{"point_of_interest", "travel_agency"}, TypeMarina},
		{"yacht club", []string{"point_of_interest"}, TypeYachtClub},
		{"mooring", nil, TypeMooring},
		{"diesel fuel dock", []string{"marina"}, TypeFuelStation},
		{"cove bay harbor", nil, TypeAnchorage},
		{"boat launch ramp", []string{"park"}, TypeBoatLaunch},
		{"restaurant", []string{"seafood_restaurant", "restaurant"}, TypeRestaurant},
		{"bar pub", []string{"bar", "restaurant"}, TypeBar},
		{"grocery store supermarket", []string{"grocery_store"}, TypeStore},
		{"convenience store", []string{"convenience_store", "food_store"}, TypeStore},
		{"pharmacy", []string{"pharmacy", "health"}, TypePharmacy},
		{"cafe coffee shop", []string{"coffee_shop", "cafe"}, TypeCafe},
		{"hardware store marine chandlery", []string{"hardware_store"}, TypeServices},
		{"marine supply chandlery", []string{"point_of_interest"}, TypeServices},
		{"hardware store", []string{"hardware_store"}, TypeServices},
		{"ATM bank", []string{"atm"}, TypeServices},
		// Google's type wins when the result is not what the query asked for.
		{"marina", []string{"seafood_restaurant"}, TypeRestaurant},
		{"cove bay harbor", []string{"marina"}, TypeMarina},
		{"pharmacy", []string{"supermarket", "pharmacy"}, TypePharmacy},
		// Query fallback when Google types are unrecognized.
		{"pharmacy", []string{"point_of_interest"}, TypePharmacy},
		// "harbor" must not match the Bar rule.
		{"harbor", nil, TypeAnchorage},
		{"something odd", []string{"bakery_shop"}, ""},
	}
	for _, tc := range tests {
		if got := classifyPlace(tc.query, tc.types); got != tc.want {
			t.Errorf("classifyPlace(%q, %v) = %q, want %q", tc.query, tc.types, got, tc.want)
		}
	}
}
