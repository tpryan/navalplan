CREATE TABLE IF NOT EXISTS voyage_day (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    voyage_id INTEGER REFERENCES voyage(id) ON DELETE CASCADE,
    target_date DATE NOT NULL,
    day_type VARCHAR(30) DEFAULT 'landfall' NOT NULL,
    start_location_name VARCHAR(255),
    start_precise_location TEXT,
    start_latitude FLOAT NOT NULL,
    start_longitude FLOAT NOT NULL,
    end_location_name VARCHAR(255),
    end_precise_location TEXT,
    end_latitude FLOAT NOT NULL,
    end_longitude FLOAT NOT NULL,
    search_radius INTEGER DEFAULT 5,
    search_radius_unit VARCHAR(10) DEFAULT 'nm',
    notes TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(voyage_id, target_date)
);

-- Migrate existing stop rows into voyage_day ordered by target_date ASC, id ASC per voyage_id.
-- For a multi-stop voyage (S_1 .. S_n), leg k (k = 1 .. n-1) sails from S_k (start) to S_{k+1} (end)
-- on S_k.target_date. For a single-stop voyage (cnt = 1), start and end are both S_1.
WITH ordered_stops AS (
    SELECT
        id,
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
        created_at,
        ROW_NUMBER() OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS rn,
        COUNT(*) OVER (PARTITION BY voyage_id) AS cnt,
        LEAD(stop_type) OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS next_stop_type,
        LEAD(location_name) OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS next_location_name,
        LEAD(precise_location) OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS next_precise_location,
        LEAD(latitude) OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS next_latitude,
        LEAD(longitude) OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS next_longitude,
        LEAD(search_radius) OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS next_search_radius,
        LEAD(search_radius_unit) OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS next_search_radius_unit,
        LEAD(notes) OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS next_notes
    FROM stop
)
INSERT INTO voyage_day (
    voyage_id,
    target_date,
    day_type,
    start_location_name,
    start_precise_location,
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
)
SELECT
    voyage_id,
    target_date,
    CASE WHEN cnt = 1 THEN stop_type ELSE next_stop_type END,
    location_name,
    precise_location,
    latitude,
    longitude,
    CASE WHEN cnt = 1 THEN location_name ELSE next_location_name END,
    CASE WHEN cnt = 1 THEN precise_location ELSE next_precise_location END,
    CASE WHEN cnt = 1 THEN latitude ELSE next_latitude END,
    CASE WHEN cnt = 1 THEN longitude ELSE next_longitude END,
    CASE WHEN cnt = 1 THEN search_radius ELSE next_search_radius END,
    CASE WHEN cnt = 1 THEN search_radius_unit ELSE next_search_radius_unit END,
    CASE WHEN cnt = 1 THEN notes ELSE COALESCE(NULLIF(next_notes, ''), notes) END,
    created_at
FROM ordered_stops
WHERE cnt = 1 OR rn < cnt
ORDER BY voyage_id, rn;

-- Adjust voyage.end_date for migrated multi-stop voyages whose N stops collapsed into N-1 legs,
-- so the voyage date range matches the migrated voyage_day rows without leaving a blank trailing day.
WITH stop_bounds AS (
    SELECT
        voyage_id,
        COUNT(*) AS stop_cnt,
        MAX(target_date) AS max_stop_date
    FROM stop
    GROUP BY voyage_id
),
day_bounds AS (
    SELECT
        voyage_id,
        MAX(target_date) AS max_day_date
    FROM voyage_day
    GROUP BY voyage_id
)
UPDATE voyage v
SET end_date = db.max_day_date
FROM stop_bounds sb
JOIN day_bounds db ON db.voyage_id = sb.voyage_id
WHERE v.id = sb.voyage_id
  AND sb.stop_cnt > 1
  AND v.end_date = sb.max_stop_date
  AND db.max_day_date >= v.start_date;

-- Migrate briefing:
-- In the old Stop model, the destination briefing (and pilot_notes for leg S_k -> S_{k+1})
-- was attached to the arrival stop S_{k+1} (rn = k + 1), which corresponds to voyage_day k (day_rn = k).
ALTER TABLE briefing ADD COLUMN day_id INTEGER REFERENCES voyage_day(id) ON DELETE CASCADE UNIQUE;

WITH ordered_stops AS (
    SELECT
        id AS stop_id,
        voyage_id,
        ROW_NUMBER() OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS stop_rn,
        COUNT(*) OVER (PARTITION BY voyage_id) AS stop_cnt
    FROM stop
),
ordered_days AS (
    SELECT
        id AS day_id,
        voyage_id,
        ROW_NUMBER() OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS day_rn
    FROM voyage_day
)
UPDATE briefing b
SET day_id = od.day_id
FROM ordered_stops os
JOIN ordered_days od
  ON od.voyage_id = os.voyage_id
 AND od.day_rn = CASE WHEN os.stop_cnt = 1 THEN 1 ELSE os.stop_rn - 1 END
WHERE b.stop_id = os.stop_id
  AND (os.stop_cnt = 1 OR os.stop_rn > 1);

-- Fallback: if Day 1 has no briefing yet (e.g. only S_1 was researched), attach S_1's briefing to Day 1
WITH first_stops AS (
    SELECT DISTINCT ON (voyage_id)
        id AS stop_id,
        voyage_id
    FROM stop
    ORDER BY voyage_id, target_date ASC, id ASC
),
first_days AS (
    SELECT DISTINCT ON (voyage_id)
        id AS day_id,
        voyage_id
    FROM voyage_day
    ORDER BY voyage_id, target_date ASC, id ASC
)
UPDATE briefing b
SET day_id = fd.day_id
FROM first_stops fs
JOIN first_days fd ON fd.voyage_id = fs.voyage_id
WHERE b.stop_id = fs.stop_id
  AND b.day_id IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM briefing b2 WHERE b2.day_id = fd.day_id
  );

DELETE FROM briefing WHERE day_id IS NULL;
ALTER TABLE briefing DROP COLUMN stop_id;

-- Migrate voyage_track:
-- In the old Stop model, GPX tracks for leg S_k -> S_{k+1} were linked to EndStopID = S_{k+1} (stop_rn = k + 1),
-- which corresponds to voyage_day k (day_rn = k).
ALTER TABLE voyage_track ADD COLUMN voyage_day_id INTEGER REFERENCES voyage_day(id) ON DELETE SET NULL;

WITH ordered_stops AS (
    SELECT
        id AS stop_id,
        voyage_id,
        ROW_NUMBER() OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS stop_rn,
        COUNT(*) OVER (PARTITION BY voyage_id) AS stop_cnt
    FROM stop
),
ordered_days AS (
    SELECT
        id AS day_id,
        voyage_id,
        ROW_NUMBER() OVER (PARTITION BY voyage_id ORDER BY target_date ASC, id ASC) AS day_rn
    FROM voyage_day
)
UPDATE voyage_track vt
SET voyage_day_id = od.day_id
FROM ordered_stops os
JOIN ordered_days od
  ON od.voyage_id = os.voyage_id
 AND od.day_rn = CASE
     WHEN os.stop_cnt = 1 THEN 1
     WHEN os.stop_rn > 1 THEN os.stop_rn - 1
     ELSE 1
 END
WHERE vt.voyage_stop_id = os.stop_id;

-- Also update embedded JSONB debrief.voyage_stop_id -> debrief.voyage_day_id when present
UPDATE voyage_track
SET debrief = (debrief - 'voyage_stop_id') || jsonb_build_object('voyage_day_id', voyage_day_id)
WHERE debrief IS NOT NULL
  AND jsonb_typeof(debrief) = 'object'
  AND voyage_day_id IS NOT NULL
  AND (debrief ? 'voyage_stop_id');

DROP INDEX IF EXISTS idx_voyage_track_stop;
ALTER TABLE voyage_track DROP COLUMN voyage_stop_id;
CREATE INDEX idx_voyage_track_day ON voyage_track(voyage_day_id);

-- Drop stop and create voyage_day index
DROP INDEX IF EXISTS idx_stop_voyage_id;
DROP TABLE stop;
CREATE INDEX idx_voyage_day_voyage_id ON voyage_day(voyage_id);
