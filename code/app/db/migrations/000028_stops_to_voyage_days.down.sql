CREATE TABLE stop (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    voyage_id INTEGER REFERENCES voyage(id) ON DELETE CASCADE,
    target_date DATE NOT NULL,
    stop_type VARCHAR(30) DEFAULT 'landfall' NOT NULL,
    location_name VARCHAR(255),
    precise_location TEXT,
    latitude FLOAT NOT NULL,
    longitude FLOAT NOT NULL,
    search_radius INTEGER DEFAULT 5,
    search_radius_unit VARCHAR(10) DEFAULT 'nm',
    notes TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(voyage_id, target_date)
);

CREATE INDEX idx_stop_voyage_id ON stop(voyage_id);

-- Re-populate start stops from voyage_day
INSERT INTO stop (
    voyage_id,
    target_date,
    stop_type,
    location_name,
    precise_location,
    latitude,
    longitude,
    search_radius,
    search_radius_unit,
    notes,
    created_at
)
SELECT
    voyage_id,
    target_date,
    day_type,
    start_location_name,
    start_precise_location,
    start_latitude,
    start_longitude,
    search_radius,
    search_radius_unit,
    notes,
    created_at
FROM voyage_day
ORDER BY voyage_id, target_date ASC, id ASC;

-- Restore the final end stop when start != end on the last day of a voyage
WITH last_days AS (
    SELECT DISTINCT ON (voyage_id)
        voyage_id,
        target_date,
        day_type,
        start_location_name,
        start_latitude,
        start_longitude,
        end_location_name,
        end_precise_location,
        end_latitude,
        end_longitude,
        search_radius,
        search_radius_unit,
        notes,
        created_at
    FROM voyage_day
    ORDER BY voyage_id, target_date DESC, id DESC
)
INSERT INTO stop (
    voyage_id,
    target_date,
    stop_type,
    location_name,
    precise_location,
    latitude,
    longitude,
    search_radius,
    search_radius_unit,
    notes,
    created_at
)
SELECT
    ld.voyage_id,
    (ld.target_date + INTERVAL '1 day')::date,
    ld.day_type,
    ld.end_location_name,
    ld.end_precise_location,
    ld.end_latitude,
    ld.end_longitude,
    ld.search_radius,
    ld.search_radius_unit,
    ld.notes,
    ld.created_at
FROM last_days ld
WHERE (
    ld.start_latitude <> ld.end_latitude
    OR ld.start_longitude <> ld.end_longitude
    OR COALESCE(ld.start_location_name, '') <> COALESCE(ld.end_location_name, '')
)
AND NOT EXISTS (
    SELECT 1 FROM stop s
    WHERE s.voyage_id = ld.voyage_id
      AND s.target_date = (ld.target_date + INTERVAL '1 day')::date
);

-- Restore briefing.stop_id from briefing.day_id and drop day_id
ALTER TABLE briefing ADD COLUMN stop_id INTEGER REFERENCES stop(id) ON DELETE CASCADE UNIQUE;

UPDATE briefing b
SET stop_id = s.id
FROM voyage_day vd
JOIN stop s ON s.voyage_id = vd.voyage_id AND s.target_date = vd.target_date
WHERE b.day_id = vd.id;

DELETE FROM briefing WHERE stop_id IS NULL;
ALTER TABLE briefing DROP COLUMN day_id;

-- Restore voyage_track.voyage_stop_id from voyage_track.voyage_day_id
ALTER TABLE voyage_track ADD COLUMN voyage_stop_id INTEGER REFERENCES stop(id) ON DELETE SET NULL;

UPDATE voyage_track vt
SET voyage_stop_id = s.id
FROM voyage_day vd
JOIN stop s ON s.voyage_id = vd.voyage_id AND s.target_date = vd.target_date
WHERE vt.voyage_day_id = vd.id;

CREATE INDEX idx_voyage_track_stop ON voyage_track(voyage_stop_id);
DROP INDEX IF EXISTS idx_voyage_track_day;
ALTER TABLE voyage_track DROP COLUMN voyage_day_id;

-- Drop voyage_day index and table
DROP INDEX IF EXISTS idx_voyage_day_voyage_id;
DROP TABLE voyage_day;
