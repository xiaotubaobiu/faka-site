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
CREATE TABLE IF NOT EXISTS recharge_orders (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id       INTEGER NOT NULL,
  provider      TEXT NOT NULL,
  out_trade_no  TEXT NOT NULL UNIQUE,
  amount_fen    INTEGER NOT NULL,
  quota         INTEGER NOT NULL,
  trade_no      TEXT,
  status        TEXT NOT NULL,
  created_at    INTEGER NOT NULL,
  paid_at       INTEGER
);
CREATE INDEX IF NOT EXISTS idx_recharge_user ON recharge_orders(user_id);
CREATE TABLE IF NOT EXISTS epay_orders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  trade_no TEXT UNIQUE NOT NULL,
  out_trade_no TEXT NOT NULL,
  pid INTEGER NOT NULL,
  type TEXT NOT NULL DEFAULT 'alipay',
  name TEXT NOT NULL,
  money TEXT NOT NULL,
  status INTEGER NOT NULL DEFAULT 0,
  notify_url TEXT NOT NULL,
  return_url TEXT NOT NULL DEFAULT '',
  param TEXT NOT NULL DEFAULT '',
  alipay_trade_no TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL,
  paid_at DATETIME,
  notify_count INTEGER NOT NULL DEFAULT 0,
  notified INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_epay_orders_out_trade_no ON epay_orders(out_trade_no, pid);
CREATE INDEX IF NOT EXISTS idx_epay_orders_status ON epay_orders(status);
`
