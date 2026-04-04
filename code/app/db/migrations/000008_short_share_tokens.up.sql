CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE OR REPLACE FUNCTION generate_voyage_share_token() RETURNS TEXT AS $$
DECLARE
  key TEXT;
  -- found TEXT; -- Removed to avoid shadowing special FOUND variable
BEGIN
  LOOP
    -- Generate 8 random bytes, encode as base64 (approx 11 chars)
    key := encode(gen_random_bytes(8), 'base64');
    -- Make URL safe
    key := replace(key, '/', '_');
    key := replace(key, '+', '-');
    -- Remove padding
    key := rtrim(key, '=');
    
    -- Check for collision in voyage table
    PERFORM share_token FROM voyage WHERE share_token = key;
    
    -- If not found (no collision), return the key
    IF NOT FOUND THEN
      RETURN key;
    END IF;
  END LOOP;
END;
$$ LANGUAGE plpgsql;