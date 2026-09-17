package tool

import (
	"fmt"
	"testing"
	"time"

	"github.com/tpryan/openmeteogo"
)

// mockWeatherClient is a mock implementation of the WeatherClient interface.
type mockWeatherClient struct {
	GetFunc func(opts *openmeteogo.Options) (*openmeteogo.WeatherData, error)
}

func (m *mockWeatherClient) Get(opts *openmeteogo.Options) (*openmeteogo.WeatherData, error) {
	if m.GetFunc != nil {
		return m.GetFunc(opts)
	}
	return nil, nil
}

func TestDegreesToDirection(t *testing.T) {
	tests := []struct {
		deg  float64
		want string
	}{
		{0, "N"},
		{22.5, "NNE"},
		{45, "NE"},
		{90, "E"},
		{180, "S"},
		{270, "W"},
		{350, "N"}, // 350 is close to 360/0
		{337.5, "NNW"},
		{11.24, "N"},
		{11.26, "NNE"},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.deg)), func(t *testing.T) {
			if got := DegreesToDirection(tt.deg); got != tt.want {
				t.Errorf("DegreesToDirection(%v) = %v, want %v", tt.deg, got, tt.want)
			}
		})
	}
}

func TestNewWeatherTool(t *testing.T) {
	tool, _, err := NewWeatherTool()
	if err != nil {
		t.Fatalf("NewWeatherTool() error = %v", err)
	}

	if tool.Name() != "get_weather_forecast" {
		t.Errorf("NewWeatherTool().Name() = %v, want %v", tool.Name(), "get_weather_forecast")
	}
}

func TestGetWeatherForecast(t *testing.T) {
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	futureDate := now.AddDate(0, 2, 0).Format("2006-01-02")

	makeHourly := func(val float64) []float64 {
		res := make([]float64, 24)
		for i := range res {
			res[i] = val
		}
		return res
	}

	tests := []struct {
		name       string
		args       WeatherArgs
		mockFunc   func(opts *openmeteogo.Options) (*openmeteogo.WeatherData, error)
		wantErr    bool
		wantTemp   float64
		wantWaves  float64
		wantType   string
		wantHourly int
	}{
		{
			name: "Live forecast success",
			args: WeatherArgs{Latitude: 10.0, Longitude: 20.0, Date: today},
			mockFunc: func(opts *openmeteogo.Options) (*openmeteogo.WeatherData, error) {
				return &openmeteogo.WeatherData{
					Daily: openmeteogo.Daily{
						Time:                     []string{today},
						WeatherCode:              []int{1},
						Temperature2mMax:         []float64{75.0},
						Temperature2mMin:         []float64{65.0},
						WindSpeed10mMax:          []float64{15.0},
						WindGusts10mMax:          []float64{20.0},
						WindDirection10mDominant: []int{90},
						PrecipitationSum:         []float64{0.1},
						WaveHeightMax:            []float64{1.5},
						WaveDirectionDominant:    []float64{180.0},
						WavePeriodMax:            []float64{8.0},
					},
					Hourly: openmeteogo.Hourly{
						WindSpeed10m:     makeHourly(12.0),
						WindDirection10m: make([]int, 24),
						WeatherCode:      make([]int, 24),
						Temperature2m:    makeHourly(70.0),
						WindGusts10m:     makeHourly(16.0),
						Precipitation:    makeHourly(0.0),
						WaveHeight:       makeHourly(1.5),
					},
				}, nil
			},
			wantErr:    false,
			wantTemp:   75.0,
			wantWaves:  4.92,
			wantType:   "Standard",
			wantHourly: 24,
		},
		{
			name: "Future date queries historical archive",
			args: WeatherArgs{Latitude: 51.05, Longitude: 2.37, Date: futureDate},
			mockFunc: func(opts *openmeteogo.Options) (*openmeteogo.WeatherData, error) {
				return &openmeteogo.WeatherData{
					Daily: openmeteogo.Daily{
						Time:                     []string{futureDate},
						WeatherCode:              []int{2},
						Temperature2mMax:         []float64{68.0},
						Temperature2mMin:         []float64{55.0},
						WindSpeed10mMax:          []float64{14.0},
						WindGusts10mMax:          []float64{18.0},
						WindDirection10mDominant: []int{270},
						PrecipitationSum:         []float64{0.0},
					},
					Hourly: openmeteogo.Hourly{
						WindSpeed10m:     makeHourly(10.0),
						WindDirection10m: make([]int, 24),
						WeatherCode:      make([]int, 24),
						Temperature2m:    makeHourly(62.0),
						WindGusts10m:     makeHourly(14.0),
						Precipitation:    makeHourly(0.0),
					},
				}, nil
			},
			wantErr:    false,
			wantTemp:   68.0,
			wantWaves:  0.0,
			wantType:   "Historical Archive",
			wantHourly: 24,
		},
		{
			name: "Future date fallback on API error",
			args: WeatherArgs{Latitude: 51.05, Longitude: 2.37, Date: futureDate},
			mockFunc: func(opts *openmeteogo.Options) (*openmeteogo.WeatherData, error) {
				return nil, fmt.Errorf("historical archive down")
			},
			wantErr:    false,
			wantTemp:   78.0,
			wantWaves:  3.0,
			wantType:   "Climatology Projection",
			wantHourly: 24,
		},
		{
			name: "Invalid date format returns error",
			args: WeatherArgs{Latitude: 41.497, Longitude: -71.362, Date: "invalid-date"},
			mockFunc: func(opts *openmeteogo.Options) (*openmeteogo.WeatherData, error) {
				return nil, nil
			},
			wantErr: true,
		},
		{
			name: "Near-term API error",
			args: WeatherArgs{Latitude: 10.0, Longitude: 20.0, Date: today},
			mockFunc: func(opts *openmeteogo.Options) (*openmeteogo.WeatherData, error) {
				return nil, fmt.Errorf("network error")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wp := &WeatherProvider{client: &mockWeatherClient{GetFunc: tt.mockFunc}}
			result, err := wp.GetWeatherForecast(newMockContext(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetWeatherForecast() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if result.MaxTemp != tt.wantTemp {
				t.Errorf("MaxTemp = %v, want %v", result.MaxTemp, tt.wantTemp)
			}
			if tt.wantWaves > 0 && (result.WaveHeight < tt.wantWaves-0.1 || result.WaveHeight > tt.wantWaves+0.1) {
				t.Errorf("WaveHeight = %v, want ~%v", result.WaveHeight, tt.wantWaves)
			}
			if result.ForecastType != tt.wantType {
				t.Errorf("ForecastType = %v, want %v", result.ForecastType, tt.wantType)
			}
			if len(result.HourlyWind) != tt.wantHourly {
				t.Errorf("len(HourlyWind) = %v, want %v", len(result.HourlyWind), tt.wantHourly)
			}
		})
	}
}
