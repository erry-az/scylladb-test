CREATE KEYSPACE tracking WITH REPLICATION = { 'class' : 'NetworkTopologyStrategy','DC1' : 3};

USE tracking;
CREATE TABLE heart_rate_ttl (
id  uuid,
name text,
value int,
PRIMARY KEY (name)) WITH default_time_to_live = 500;
CREATE INDEX ON heart_rate_ttl(value);