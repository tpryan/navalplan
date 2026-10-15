package gpx

import "math"

// DefaultEpsilonNM is the default perpendicular tolerance for Ramer-Douglas-Peucker simplification (~18.5 meters).
const DefaultEpsilonNM = 0.01

// nmPerDegreeLat is the nautical-mile length of one degree of latitude (60 arcminutes = 60 NM).
const nmPerDegreeLat = 60.0

// SimplifyRDP reduces the number of points in a polyline using the Ramer-Douglas-Peucker algorithm.
// epsilonNM is the maximum allowable perpendicular distance from the simplified line in nautical miles.
func SimplifyRDP(points []Point, epsilonNM float64) []Point {
	if len(points) <= 2 {
		result := make([]Point, len(points))
		copy(result, points)
		return result
	}
	if epsilonNM <= 0 {
		epsilonNM = DefaultEpsilonNM
	}

	dmax := 0.0
	index := 0
	last := len(points) - 1

	for i := 1; i < last; i++ {
		d := perpendicularDistanceNM(points[i], points[0], points[last])
		if d > dmax {
			index = i
			dmax = d
		}
	}

	if dmax > epsilonNM {
		rec1 := SimplifyRDP(points[:index+1], epsilonNM)
		rec2 := SimplifyRDP(points[index:], epsilonNM)
		result := append(rec1[:len(rec1)-1], rec2...)
		return result
	}

	return []Point{points[0], points[last]}
}

// perpendicularDistanceNM computes the perpendicular distance in nautical miles
// from point p to segment p1-p2 using a local equirectangular (flat-earth)
// projection: 1° latitude = 60 NM, and 1° longitude = 60 * cos(midLat) NM.
// Flat-earth projection is appropriate here because RDP operates on local track
// segments whose span is orders of magnitude smaller than Earth-curvature error.
func perpendicularDistanceNM(p, p1, p2 Point) float64 {
	midLatRad := ((p1.Lat + p2.Lat) / 2.0) * (math.Pi / 180.0)
	cosLat := math.Cos(midLatRad)

	dx := (p2.Lng - p1.Lng) * cosLat * nmPerDegreeLat
	dy := (p2.Lat - p1.Lat) * nmPerDegreeLat

	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return pointDistanceNM(p, p1)
	}

	pdx := (p.Lng - p1.Lng) * cosLat * nmPerDegreeLat
	pdy := (p.Lat - p1.Lat) * nmPerDegreeLat

	t := (pdx*dx + pdy*dy) / l2
	if t <= 0 {
		return math.Sqrt(pdx*pdx + pdy*pdy)
	}
	if t >= 1 {
		p2dx := (p.Lng - p2.Lng) * cosLat * nmPerDegreeLat
		p2dy := (p.Lat - p2.Lat) * nmPerDegreeLat
		return math.Sqrt(p2dx*p2dx + p2dy*p2dy)
	}

	return math.Abs(pdx*dy-pdy*dx) / math.Sqrt(l2)
}

func pointDistanceNM(p1, p2 Point) float64 {
	midLatRad := ((p1.Lat + p2.Lat) / 2.0) * (math.Pi / 180.0)
	cosLat := math.Cos(midLatRad)
	dx := (p2.Lng - p1.Lng) * cosLat * nmPerDegreeLat
	dy := (p2.Lat - p1.Lat) * nmPerDegreeLat
	return math.Sqrt(dx*dx + dy*dy)
}
