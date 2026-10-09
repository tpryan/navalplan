ALTER TABLE voyage_track
    ALTER COLUMN distance_nm TYPE NUMERIC(12,2),
    ALTER COLUMN max_speed_kts TYPE NUMERIC(8,2),
    ALTER COLUMN avg_speed_kts TYPE NUMERIC(8,2);
