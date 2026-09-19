-- 0001: the whole schema (PLAN §13). MariaDB, InnoDB, utf8mb4.
-- Ids are char(36) UUIDs generated in Go so an importer from an as-is
-- BlogCFC database stays possible. Datetimes hold UTC.

CREATE TABLE IF NOT EXISTS entries (
  id char(36) NOT NULL,
  title varchar(100) NOT NULL DEFAULT '',
  alias varchar(100) NOT NULL DEFAULT '',
  body longtext NOT NULL,
  morebody longtext NOT NULL,
  posted datetime NOT NULL,
  username varchar(50) NOT NULL DEFAULT '',
  allowcomments tinyint(1) NOT NULL DEFAULT 1,
  released tinyint(1) NOT NULL DEFAULT 0,
  mailed tinyint(1) NOT NULL DEFAULT 0,
  sendemail tinyint(1) NOT NULL DEFAULT 0,
  views int NOT NULL DEFAULT 0,
  enclosure varchar(255) NOT NULL DEFAULT '',
  filesize bigint NOT NULL DEFAULT 0,
  mimetype varchar(100) NOT NULL DEFAULT '',
  summary varchar(255) NOT NULL DEFAULT '',
  subtitle varchar(100) NOT NULL DEFAULT '',
  keywords varchar(100) NOT NULL DEFAULT '',
  duration varchar(10) NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  KEY entries_released_posted (released, posted),
  KEY entries_alias (alias),
  KEY entries_title (title),
  KEY entries_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS categories (
  id char(36) NOT NULL,
  name varchar(50) NOT NULL,
  alias varchar(50) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY categories_alias (alias),
  UNIQUE KEY categories_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS entry_categories (
  entry_id char(36) NOT NULL,
  category_id char(36) NOT NULL,
  PRIMARY KEY (entry_id, category_id),
  KEY entry_categories_category (category_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS comments (
  id char(36) NOT NULL,
  entry_id char(36) NOT NULL,
  name varchar(50) NOT NULL DEFAULT '',
  email varchar(50) NOT NULL DEFAULT '',
  website varchar(255) NOT NULL DEFAULT '',
  comment text NOT NULL,
  posted datetime NOT NULL,
  subscribe tinyint(1) NOT NULL DEFAULT 0,
  moderated tinyint(1) NOT NULL DEFAULT 1,
  subscribeonly tinyint(1) NOT NULL DEFAULT 0,
  kill_token char(36) NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  KEY comments_entry_posted (entry_id, posted),
  KEY comments_kill_token (kill_token)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS subscribers (
  email varchar(190) NOT NULL,
  token char(36) NOT NULL DEFAULT '',
  verified tinyint(1) NOT NULL DEFAULT 0,
  PRIMARY KEY (email),
  KEY subscribers_token (token),
  KEY subscribers_verified (verified)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS related_entries (
  entry_id char(36) NOT NULL,
  related_id char(36) NOT NULL,
  PRIMARY KEY (entry_id, related_id),
  KEY related_entries_related (related_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS pages (
  id char(36) NOT NULL,
  title varchar(255) NOT NULL DEFAULT '',
  alias varchar(100) NOT NULL,
  body longtext NOT NULL,
  showlayout tinyint(1) NOT NULL DEFAULT 1,
  PRIMARY KEY (id),
  UNIQUE KEY pages_alias (alias),
  KEY pages_title (title)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS page_categories (
  page_id char(36) NOT NULL,
  category_id char(36) NOT NULL,
  PRIMARY KEY (page_id, category_id),
  KEY page_categories_category (category_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS textblocks (
  id char(36) NOT NULL,
  label varchar(255) NOT NULL,
  body longtext NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY textblocks_label (label)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS search_stats (
  term varchar(255) NOT NULL,
  searched datetime NOT NULL,
  KEY search_stats_term (term),
  KEY search_stats_searched (searched)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS enclosure_downloads (
  id char(36) NOT NULL,
  entry_id char(36) NOT NULL,
  ip varchar(45) NOT NULL DEFAULT '',
  referrer varchar(255) NOT NULL DEFAULT '',
  user_agent varchar(255) NOT NULL DEFAULT '',
  downloaded_at datetime NOT NULL,
  enclosure varchar(255) NOT NULL DEFAULT '',
  online tinyint(1) NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY enclosure_downloads_entry (entry_id),
  KEY enclosure_downloads_when (downloaded_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS roles (
  id int NOT NULL,
  role varchar(50) NOT NULL,
  description varchar(255) NOT NULL DEFAULT '',
  legacy_id char(36) NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  UNIQUE KEY roles_role (role)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS users (
  username varchar(50) NOT NULL,
  password_hash varchar(255) NOT NULL DEFAULT '',
  name varchar(100) NOT NULL DEFAULT '',
  PRIMARY KEY (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS user_roles (
  username varchar(50) NOT NULL,
  role_id int NOT NULL,
  PRIMARY KEY (username, role_id),
  KEY user_roles_role (role_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS settings (
  `key` varchar(100) NOT NULL,
  value longtext NOT NULL,
  PRIMARY KEY (`key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
