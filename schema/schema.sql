CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);

CREATE TABLE IF NOT EXISTS ads (
    id           TEXT PRIMARY KEY,
    title        TEXT,
    price        REAL,
    area         REAL,
    rooms        INTEGER,
    floor        INTEGER,
    total_floors INTEGER,
    year_built   INTEGER,
    address      TEXT,
    city         TEXT,
    district     TEXT,
    complex      TEXT,
    complex_id   TEXT,
    seller       TEXT,
    owner_name   TEXT,
    photo        TEXT,
    lat          REAL,
    lon          REAL,
    flags        TEXT,
    date_posted  TEXT,
    link         TEXT,
    first_seen   TEXT,
    last_seen    TEXT,
    gone_at      TEXT
);

CREATE TABLE IF NOT EXISTS price_history (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    ad_id   TEXT NOT NULL REFERENCES ads(id) ON DELETE CASCADE,
    seen_at TEXT NOT NULL,
    price   REAL NOT NULL
);

CREATE TABLE IF NOT EXISTS statuses (
    ad_id      TEXT PRIMARY KEY,
    status     TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ads_gone ON ads(gone_at);
CREATE INDEX IF NOT EXISTS idx_price_history_ad ON price_history(ad_id);
