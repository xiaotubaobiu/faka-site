package store

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL DEFAULT 'user',
  balance       INTEGER NOT NULL DEFAULT 0,
  status        INTEGER NOT NULL DEFAULT 1,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS orders (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id         INTEGER NOT NULL,
  code_count      INTEGER NOT NULL,
  quota_per_code  INTEGER NOT NULL,
  total_cost      INTEGER NOT NULL,
  status          TEXT NOT NULL,
  succeeded_count INTEGER NOT NULL DEFAULT 0,
  failed_count    INTEGER NOT NULL DEFAULT 0,
  refunded_amount INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS order_codes (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  order_id   INTEGER NOT NULL,
  user_id    INTEGER NOT NULL,
  code       TEXT NOT NULL,
  quota      INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS balance_ledger (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL,
  delta        INTEGER NOT NULL,
  balance_after INTEGER NOT NULL,
  reason       TEXT NOT NULL,
  admin_id     INTEGER,
  order_id     INTEGER,
  created_at   INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS config (
  key        TEXT PRIMARY KEY,
  value      TEXT,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_orders_user ON orders(user_id);
CREATE INDEX IF NOT EXISTS idx_codes_order ON order_codes(order_id);
CREATE INDEX IF NOT EXISTS idx_ledger_user ON balance_ledger(user_id);
`
