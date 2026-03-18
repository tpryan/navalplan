CREATE TABLE voyage_recommendation (
    id SHORTKEY PRIMARY KEY,
    voyage_id INTEGER NOT NULL REFERENCES voyage(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL, -- "Anchorage", "Mooring", "Marina"
    latitude FLOAT NOT NULL,
    longitude FLOAT NOT NULL,
    geometry JSONB, -- GeoJSON Polygon for "blob" visualization
    description TEXT,
    reasoning TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TRIGGER voyage_recommendation_shortkey
    BEFORE INSERT ON voyage_recommendation
    FOR EACH ROW EXECUTE PROCEDURE shortkey_generate();

CREATE INDEX idx_voyage_recommendation_voyage_id ON voyage_recommendation(voyage_id);
