-- 0002: the recipient count a release mail recorded (PLAN §7 "Bugs fixed
-- rather than ported": BlogCFC's mailEntry set mailed = 1 even when the
-- blog had no subscribers at all. The mark stays -- an entry is mailed
-- once -- and the number of addresses it actually reached is kept beside
-- it, so "mailed" and "mailed to nobody" can be told apart.
--
-- ADD COLUMN IF NOT EXISTS is MariaDB's own: migrate.Up re-runs a file
-- whose version record it has not written yet (FP O01), so every
-- statement here has to tolerate a second run.

ALTER TABLE entries ADD COLUMN IF NOT EXISTS mailed_count int NOT NULL DEFAULT 0;
