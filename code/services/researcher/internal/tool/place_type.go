package tool

import (
	"regexp"
	"strings"
)

// Facility types the frontend renders (see activeFacilityFilters in main.js).
const (
	TypeAnchorage   = "Anchorage"
	TypeMarina      = "Marina"
	TypeMooring     = "Mooring"
	TypeFuelStation = "Fuel Station"
	TypeYachtClub   = "Yacht Club"
	TypeBoatLaunch  = "Boat Launch"
	TypeRestaurant  = "Restaurant"
	TypeBar         = "Bar"
	TypeCafe        = "Cafe"
	TypeStore       = "Store"
	TypePharmacy    = "Pharmacy"
	TypeServices    = "Services"
)

// queryRule maps a FindPlacesNearby query to a facility type. Authoritative
// rules describe nautical features Google does not type (anchorages, moorings,
// fuel docks, yacht clubs, launches), so the query wins over Google's types.
type queryRule struct {
	re            *regexp.Regexp
	facilityType  string
	authoritative bool
}

var queryRules = []queryRule{
	{regexp.MustCompile(`\banchorage`), TypeAnchorage, true},
	{regexp.MustCompile(`\bmooring`), TypeMooring, true},
	{regexp.MustCompile(`\b(fuel|diesel)\b`), TypeFuelStation, true},
	{regexp.MustCompile(`\byacht club`), TypeYachtClub, true},
	{regexp.MustCompile(`\b(boat launch|ramp)\b`), TypeBoatLaunch, true},
	{regexp.MustCompile(`\bmarina`), TypeMarina, false},
	{regexp.MustCompile(`\b(cove|bay|harbou?r)\b`), TypeAnchorage, false},
	{regexp.MustCompile(`\brestaurant`), TypeRestaurant, false},
	{regexp.MustCompile(`\b(bar|pub)s?\b`), TypeBar, false},
	{regexp.MustCompile(`\b(cafe|café|coffee)`), TypeCafe, false},
	{regexp.MustCompile(`\b(pharmacy|drugstore|chemist)`), TypePharmacy, false},
	{regexp.MustCompile(`\b(grocery|supermarket|convenience|market|provision)`), TypeStore, false},
	{regexp.MustCompile(`\b(hardware|chandlery|marine supply|atm|bank)\b`), TypeServices, false},
}

// googleTypeMap maps Google Places (New) types to facility types.
var googleTypeMap = map[string]string{
	"marina":               TypeMarina,
	"ferry_terminal":       TypeMarina,
	"gas_station":          TypeFuelStation,
	"cafe":                 TypeCafe,
	"coffee_shop":          TypeCafe,
	"tea_house":            TypeCafe,
	"bakery":               TypeCafe,
	"bar":                  TypeBar,
	"pub":                  TypeBar,
	"wine_bar":             TypeBar,
	"night_club":           TypeBar,
	"restaurant":           TypeRestaurant,
	"bar_and_grill":        TypeRestaurant,
	"meal_takeaway":        TypeRestaurant,
	"sandwich_shop":        TypeRestaurant,
	"ice_cream_shop":       TypeCafe,
	"pharmacy":             TypePharmacy,
	"drugstore":            TypePharmacy,
	"grocery_store":        TypeStore,
	"supermarket":          TypeStore,
	"convenience_store":    TypeStore,
	"market":               TypeStore,
	"food_store":           TypeStore,
	"liquor_store":         TypeStore,
	"department_store":     TypeStore,
	"hardware_store":       TypeServices,
	"sporting_goods_store": TypeServices,
	"laundry":              TypeServices,
	"bank":                 TypeServices,
	"atm":                  TypeServices,
}

func classifyGoogleType(t string) string {
	if ft, ok := googleTypeMap[t]; ok {
		return ft
	}
	if strings.HasSuffix(t, "_restaurant") {
		return TypeRestaurant
	}
	return ""
}

// classifyPlace assigns a single facility type to a Places result using the
// search query and Google's place types. Nautical queries are authoritative;
// otherwise the query's category is used when Google agrees (or has no
// opinion), and Google's first recognized type is used when it disagrees.
func classifyPlace(query string, googleTypes []string) string {
	q := strings.ToLower(query)
	var fromQuery string
	for _, r := range queryRules {
		if r.re.MatchString(q) {
			if r.authoritative {
				return r.facilityType
			}
			fromQuery = r.facilityType
			break
		}
	}
	var fromGoogle []string
	for _, t := range googleTypes {
		if ft := classifyGoogleType(t); ft != "" {
			if ft == fromQuery {
				return ft
			}
			fromGoogle = append(fromGoogle, ft)
		}
	}
	if len(fromGoogle) > 0 {
		return fromGoogle[0]
	}
	return fromQuery
}

// nearbySearchTypes lists the Google Places (New) Table A types used for
// Nearby Search per amenity facility type. Marine types are absent on purpose:
// anchorages, moorings, and coves are not Google place types, so they use
// Text Search.
var nearbySearchTypes = map[string][]string{
	TypeRestaurant: {"restaurant"},
	TypeBar:        {"bar", "pub", "wine_bar"},
	TypeCafe:       {"cafe", "coffee_shop"},
	TypePharmacy:   {"pharmacy", "drugstore"},
	TypeStore:      {"grocery_store", "supermarket", "convenience_store", "market"},
	TypeServices:   {"hardware_store", "bank", "atm"},
}

// nearbyTypesForQuery returns the Google types to use with Nearby Search for an
// amenity query, or nil when the query should use Text Search.
func nearbyTypesForQuery(query string) []string {
	q := strings.ToLower(query)
	if strings.Contains(q, "chandlery") || strings.Contains(q, "marine supply") {
		return nil
	}
	if strings.Contains(q, "convenience") && !strings.Contains(q, "grocery") && !strings.Contains(q, "supermarket") {
		return []string{"convenience_store"}
	}
	if strings.Contains(q, "grocery") || strings.Contains(q, "supermarket") {
		return []string{"grocery_store", "supermarket", "market"}
	}
	if strings.Contains(q, "hardware") && !strings.Contains(q, "atm") && !strings.Contains(q, "bank") {
		return []string{"hardware_store"}
	}
	if strings.Contains(q, "atm") || strings.Contains(q, "bank") {
		return []string{"bank", "atm"}
	}
	for _, r := range queryRules {
		if r.re.MatchString(q) {
			return nearbySearchTypes[r.facilityType]
		}
	}
	return nil
}

// wideAreaNearbyTypes returns Google Places (New) Table A types for wide-area
// sector fan-out (> 15 km). Using SearchNearby with IncludedTypes={"marina"} on
// each compass sector guarantees up to 20 marinas per sector ranked strictly by
// distance from that sector point, covering every shore around an open-water center.
func wideAreaNearbyTypes(query string) []string {
	q := strings.ToLower(query)
	if strings.Contains(q, "marina") {
		return []string{"marina"}
	}
	return nearbyTypesForQuery(query)
}

// shouldSectorFanOut reports whether a wide-area query (> 15 km) should fan out
// across 4 cardinal sector circles. Only core hub/overnight categories (marina,
// anchorage, mooring) fan out; secondary marine queries (yacht club, fuel dock,
// cove/bay/harbor, boat launch ramp) run a single center circle query.
func shouldSectorFanOut(query string) bool {
	q := strings.ToLower(query)
	return strings.Contains(q, "marina") ||
		strings.Contains(q, "anchorage") ||
		strings.Contains(q, "mooring")
}
