CREATE TABLE voyage_map (
    voyage_id INTEGER PRIMARY KEY REFERENCES voyage(id) ON DELETE CASCADE,
    image_data BYTEA NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);
