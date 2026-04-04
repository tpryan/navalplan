ALTER TABLE person ADD COLUMN invited_by INTEGER REFERENCES person(id);

CREATE TABLE invitation (
    email VARCHAR(255) PRIMARY KEY,
    invited_by INTEGER REFERENCES person(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);