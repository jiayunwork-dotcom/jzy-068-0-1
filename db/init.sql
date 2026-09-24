-- The application creates its schema automatically on startup
-- (CREATE TABLE IF NOT EXISTS collab_workbook). This file is kept so the
-- database directory documents the persistence layout.
CREATE TABLE IF NOT EXISTS collab_workbook (
    id         TEXT PRIMARY KEY,
    data       JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
