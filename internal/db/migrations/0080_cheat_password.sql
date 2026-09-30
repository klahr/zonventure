-- An adventure may have a cheat password: a tester who enters it on the
-- phone can solve puzzles without answering. Only the Argon2id hash is
-- kept; NULL means the adventure has none. A team that cheated is marked,
-- so it is left out of every ranking.
ALTER TABLE adventures ADD COLUMN cheat_password_hash TEXT;
ALTER TABLE team_graph_state ADD COLUMN cheated BOOLEAN NOT NULL DEFAULT false;
