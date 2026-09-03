-- app/db/schema.sql

DROP TABLE IF EXISTS commodore_discovery;
DROP TABLE IF EXISTS voyage_recommendation;
DROP TABLE IF EXISTS invitation;
DROP TABLE IF EXISTS voyage_map;
DROP TABLE IF EXISTS briefing;
DROP TABLE IF EXISTS voyage_guide;
DROP TABLE IF EXISTS facility;
DROP TABLE IF EXISTS stop;
DROP TABLE IF EXISTS voyage;
DROP TABLE IF EXISTS session;
DROP TABLE IF EXISTS person;

CREATE TABLE person (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    google_id VARCHAR(255) UNIQUE NOT NULL,
    email VARCHAR(255) NOT NULL,
    name VARCHAR(255),
    picture_url TEXT,
    is_admin BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE session (
    token TEXT PRIMARY KEY,
    person_id INTEGER NOT NULL REFERENCES person(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE voyage (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    person_id INTEGER REFERENCES person(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    start_date DATE,
    end_date DATE,
    location_name VARCHAR(255),
    precise_location TEXT,
    latitude FLOAT,
    longitude FLOAT,
    search_radius INTEGER DEFAULT 60,            -- Default Broad Search (e.g. 60nm)
    search_radius_unit VARCHAR(10) DEFAULT 'nm',
    google_doc_id VARCHAR(255),
    last_exported_at TIMESTAMP,
    share_token VARCHAR(64) UNIQUE,
    is_public BOOLEAN DEFAULT FALSE,
    checkin_latitude FLOAT,
    checkin_longitude FLOAT,
    checkin_location VARCHAR(255),
    checkin_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE stop (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    voyage_id INTEGER REFERENCES voyage(id) ON DELETE CASCADE,
    target_date DATE NOT NULL,
    stop_type VARCHAR(30) DEFAULT 'landfall' NOT NULL,  -- 'landfall' (anchorage/marina/dock) or 'passage_point' (at-sea tracking position)
    location_name VARCHAR(255),
    precise_location TEXT,
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
    sun_phase JSONB,
    tides JSONB,
    facilities JSONB,
    pilot_notes JSONB,
    safety_alerts JSONB,
    safety_alerts_updated_at TIMESTAMPTZ,
    weather_last_updated TIMESTAMPTZ,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE voyage_guide (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    voyage_id INTEGER REFERENCES voyage(id) ON DELETE CASCADE UNIQUE,
    summary TEXT,
    sailing_season JSONB,
    security_safety JSONB,
    hazards JSONB,
    hubs JSONB,
    charter_info JSONB,
    airports JSONB,
    country_info JSONB,
    currencies JSONB,
    points_of_interest JSONB,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE voyage_map (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    voyage_id INTEGER NOT NULL REFERENCES voyage(id) ON DELETE CASCADE UNIQUE,
    image_data BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE invitation (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code VARCHAR(64) UNIQUE NOT NULL,
    created_by INTEGER REFERENCES person(id) ON DELETE SET NULL,
    claimed_by INTEGER REFERENCES person(id) ON DELETE SET NULL,
    is_admin BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at TIMESTAMPTZ
);

CREATE TABLE voyage_recommendation (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    voyage_id INTEGER REFERENCES voyage(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL,
    latitude FLOAT NOT NULL,
    longitude FLOAT NOT NULL,
    radius_miles FLOAT NOT NULL DEFAULT 5,
    url TEXT,
    geometry JSONB NOT NULL,
    description TEXT,
    reasoning TEXT,
    reference_links JSONB,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE commodore_discovery (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    country VARCHAR(100) NOT NULL,
    summary TEXT NOT NULL,
    latitude FLOAT NOT NULL,
    longitude FLOAT NOT NULL,
    seasonality JSONB NOT NULL,
    raw_response JSONB NOT NULL,
    mined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    tier VARCHAR(20) NOT NULL DEFAULT 'standard'
);

CREATE INDEX idx_voyage_guide_voyage_id ON voyage_guide(voyage_id);
CREATE INDEX idx_session_expires_at ON session(expires_at);
CREATE INDEX idx_voyage_person_id ON voyage(person_id);
CREATE INDEX idx_stop_voyage_id ON stop(voyage_id);
CREATE INDEX idx_voyage_recommendation_voyage_id ON voyage_recommendation(voyage_id);
CREATE INDEX idx_commodore_discovery_tier ON commodore_discovery(tier);