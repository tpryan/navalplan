ALTER TABLE region_seasonality ADD COLUMN tier VARCHAR(50) DEFAULT 'Standard';

-- Backfill existing data based on is_hidden_gem
UPDATE region_seasonality SET tier = 'Hidden Gem' WHERE is_hidden_gem = TRUE;
UPDATE region_seasonality SET tier = 'Standard' WHERE is_hidden_gem = FALSE;
