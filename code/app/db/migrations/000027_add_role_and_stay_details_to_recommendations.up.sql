ALTER TABLE voyage_recommendation
    ADD COLUMN IF NOT EXISTS voyage_role TEXT NOT NULL DEFAULT 'overnight_stop',
    ADD COLUMN IF NOT EXISTS stay_details JSONB NOT NULL DEFAULT '{}'::jsonb;
