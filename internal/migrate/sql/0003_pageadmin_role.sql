-- 0003: the PageAdmin role (issue #2). The as-is admin menu checks
-- isBlogAuthorized('PageAdmin') for the Pages screens, but the MySQL
-- installer never seeds the role, so only Admin ever reaches them
-- (client/tags/adminlayout.cfm line 77 against
-- client/installer/mysql/script.txt, which inserts five roles). The
-- rewrite seeds it as role 6; BlogCFC gives it no description of its
-- own, so it gets a plain one.
--
-- INSERT IGNORE because migrate.Up re-runs a file whose version record
-- it has not written yet (FP O01), and because migrate.Seed inserts the
-- same row from SeedRoles on every start.

INSERT IGNORE INTO roles (id, role, description, legacy_id) VALUES (6, 'PageAdmin', 'Manage pages', '');
