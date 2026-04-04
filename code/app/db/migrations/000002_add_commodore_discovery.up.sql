-- PRD: Global Seasonal Discovery & "Hidden Gem" Recommendations ("The Commodore")

-- Known sailing regions (Global)
CREATE TABLE sailing_regions (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL, -- e.g. "Sea of Cortez", "Chesapeake Bay"
    geometry JSONB,                    -- GeoJSON Polygon of the area
    type VARCHAR(50),                  -- "Coastal", "Island Group", "Ocean Crossing"
    created_at TIMESTAMP DEFAULT NOW()
);

-- Monthly suitability scores
CREATE TABLE region_seasonality (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    region_id INTEGER REFERENCES sailing_regions(id) ON DELETE CASCADE,
    month INTEGER NOT NULL CHECK (month >= 1 AND month <= 12),
    suitability_score INTEGER CHECK (suitability_score >= 0 AND suitability_score <= 100),
    is_hidden_gem BOOLEAN DEFAULT FALSE,
    summary TEXT,                      -- "Perfect trade winds, dry season."
    deep_cut_reasoning TEXT,           -- "While Caribbean is hot, this area is..."
    avg_wind_speed_knots INTEGER,
    avg_temp_c INTEGER,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(region_id, month)
);

CREATE INDEX idx_region_seasonality_month ON region_seasonality(month);
CREATE INDEX idx_region_seasonality_is_hidden_gem ON region_seasonality(is_hidden_gem);
