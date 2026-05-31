ALTER TABLE stop ADD COLUMN stop_type VARCHAR(30) DEFAULT 'landfall' NOT NULL;
-- Supported types: 'landfall' (anchorage/marina/dock) or 'passage_point' (at-sea tracking position)

-- Passage points are open-water transits without a named place; allow null location names.
ALTER TABLE stop ALTER COLUMN location_name DROP NOT NULL;
