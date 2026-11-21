-- How long a "saved" banner stays before closing itself; 0 keeps it open.
ALTER TABLE settings ADD COLUMN notice_seconds INTEGER NOT NULL DEFAULT 5 CHECK (notice_seconds BETWEEN 0 AND 60);
