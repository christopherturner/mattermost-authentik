-- Initialize Authentik database and user alongside Mattermost
SELECT 'CREATE DATABASE authentik'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'authentik')\gexec

DO
$do$
BEGIN
   IF NOT EXISTS (
      SELECT FROM pg_catalog.pg_roles
      WHERE  rolname = 'akuser') THEN
      CREATE ROLE akuser LOGIN PASSWORD 'akuser_password';
   END IF;
END
$do$;

GRANT ALL PRIVILEGES ON DATABASE authentik TO akuser;
ALTER DATABASE authentik OWNER TO akuser;
