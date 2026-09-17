CREATE TYPE track_kind AS ENUM ('planned', 'recorded');

CREATE TABLE voyage_track (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    voyage_id INTEGER NOT NULL REFERENCES voyage(id) ON DELETE CASCADE,
    voyage_stop_id INTEGER REFERENCES stop(id) ON DELETE SET NULL,
    kind track_kind NOT NULL DEFAULT 'planned',
    name VARCHAR(255) NOT NULL,
    file_name VARCHAR(255),
    start_time TIMESTAMPTZ,
    end_time TIMESTAMPTZ,
    distance_nm NUMERIC(8,2),
    duration_interval INTERVAL,
    max_speed_kts NUMERIC(5,2),
    avg_speed_kts NUMERIC(5,2),
    geojson JSONB NOT NULL,
    simplified_geojson JSONB NOT NULL,
    raw_gpx TEXT,
    debrief JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_voyage_track_voyage ON voyage_track(voyage_id, kind);
CREATE INDEX idx_voyage_track_stop ON voyage_track(voyage_stop_id);
