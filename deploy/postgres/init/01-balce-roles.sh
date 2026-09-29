#!/bin/sh
set -eu

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  -v owner_password="$BALCE_OWNER_PASSWORD" \
  -v app_password="$BALCE_APP_PASSWORD" <<'SQL'
CREATE ROLE balce_owner LOGIN PASSWORD :'owner_password';
CREATE ROLE balce_app LOGIN PASSWORD :'app_password' NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
CREATE DATABASE balce OWNER balce_owner;
REVOKE ALL ON DATABASE balce FROM PUBLIC;
GRANT CONNECT ON DATABASE balce TO balce_owner, balce_app;
\connect balce
REVOKE ALL ON SCHEMA public FROM PUBLIC;
ALTER SCHEMA public OWNER TO balce_owner;
GRANT USAGE ON SCHEMA public TO balce_app;
ALTER DEFAULT PRIVILEGES FOR ROLE balce_owner IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO balce_app;
ALTER DEFAULT PRIVILEGES FOR ROLE balce_owner IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO balce_app;
SQL
