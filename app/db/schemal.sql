-- app/db/schema.sql

DROP TABLE IF EXISTS briefing;
DROP TABLE IF EXISTS facility;
DROP TABLE IF EXISTS stop;
DROP TABLE IF EXISTS voyage;
DROP TABLE IF EXISTS session;
DROP TABLE IF EXISTS "user";

CREATE TABLE "user" (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    google_id VARCHAR(255) UNIQUE NOT NULL,
    email VARCHAR(255) NOT NULL,
    name VARCHAR(255),
    picture_url TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE session (
    token TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE voyage (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id INTEGER REFERENCES "user"(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    search_radius INTEGER DEFAULT 60,            -- Default Broad Search (e.g. 60nm)
    search_radius_unit VARCHAR(10) DEFAULT 'nm',
    google_doc_id VARCHAR(255),
    last_exported_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE stop (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    voyage_id INTEGER REFERENCES voyage(id) ON DELETE CASCADE,
    target_date DATE NOT NULL,
    location_name VARCHAR(255),
    latitude FLOAT NOT NULL,
    longitude FLOAT NOT NULL,
    search_radius INTEGER DEFAULT 5,             -- Default Deep Search (e.g. 5nm)
    search_radius_unit VARCHAR(10) DEFAULT 'nm',
    notes TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(voyage_id, target_date)
);

CREATE TABLE briefing (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    stop_id INTEGER REFERENCES stop(id) ON DELETE CASCADE UNIQUE,
    weather_summary JSONB,
    tides JSONB,
    facilities JSONB,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_session_expires_at ON session(expires_at);
CREATE INDEX idx_voyage_user_id ON voyage(user_id);
CREATE INDEX idx_stop_voyage_id ON stop(voyage_id);