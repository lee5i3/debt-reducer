-- Display preferences shown on the Settings page.
ALTER TABLE settings ADD COLUMN theme TEXT NOT NULL DEFAULT 'dark' CHECK (theme IN ('dark', 'light'));
ALTER TABLE settings ADD COLUMN expand_current INTEGER NOT NULL DEFAULT 1 CHECK (expand_current IN (0, 1));
