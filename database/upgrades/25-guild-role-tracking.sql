-- v25: Guild role tracking
ALTER TABLE guild ADD COLUMN track_roles BOOLEAN NOT NULL DEFAULT false;
