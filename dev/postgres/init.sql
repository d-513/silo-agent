-- Runs once, on a fresh volume.
CREATE EXTENSION IF NOT EXISTS vector;
-- The audit page searches its rows (db.Migrate installs it too, in public).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Go tests: each test makes its own schema in here and drops it after.
CREATE DATABASE silo_test OWNER silo;
\connect silo_test
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
