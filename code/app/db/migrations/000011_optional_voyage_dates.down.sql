-- Note: This migration may fail if there are voyages with NULL dates.
ALTER TABLE voyage ALTER COLUMN start_date SET NOT NULL;
ALTER TABLE voyage ALTER COLUMN end_date SET NOT NULL;
