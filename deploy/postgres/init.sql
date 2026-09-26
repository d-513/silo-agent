-- Runs once, on a fresh volume.
CREATE EXTENSION IF NOT EXISTS vector;

-- Go tests: each test makes its own schema in here and drops it after.
CREATE DATABASE silo_test OWNER silo;
\connect silo_test
CREATE EXTENSION IF NOT EXISTS vector;
