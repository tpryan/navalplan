-- Temporary users: an optional expiration timestamp. NULL means the account
-- (or invitation) never expires. When set and in the past, the user loses access.
ALTER TABLE person ADD COLUMN expires_at TIMESTAMPTZ;
ALTER TABLE invitation ADD COLUMN expires_at TIMESTAMPTZ;
