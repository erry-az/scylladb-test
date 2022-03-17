begin;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE TABLE heart_rate_ttl (
id  UUID NOT NULL DEFAULT uuid_generate_v4(),
name text,
value int,
PRIMARY KEY (name, value));
commit;