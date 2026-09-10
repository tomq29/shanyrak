import sqlite3

import pytest

from shanyrak_scraper import db
from shanyrak_scraper.parse import Listing


@pytest.fixture
def conn(tmp_path):
    connection = db.connect(tmp_path / "test.db")
    yield connection
    connection.close()


def listing(ad_id="1", price=30_000_000, **kwargs):
    return Listing(id=ad_id, price=price, area=60, title="2-комн", **kwargs)


def test_first_insert_is_new_and_records_price(conn):
    assert db.upsert(conn, listing()) == "new"

    row = conn.execute("SELECT * FROM ads WHERE id='1'").fetchone()
    assert row["price"] == 30_000_000
    assert row["first_seen"] and row["last_seen"] and row["gone_at"] is None
    assert conn.execute("SELECT COUNT(*) FROM price_history").fetchone()[0] == 1


def test_same_price_is_seen_without_new_history(conn):
    db.upsert(conn, listing())

    assert db.upsert(conn, listing()) == "seen"
    assert conn.execute("SELECT COUNT(*) FROM price_history").fetchone()[0] == 1


@pytest.mark.parametrize(
    "new_price, outcome", [(28_000_000, "price_down"), (32_000_000, "price_up")]
)
def test_price_change_is_recorded(conn, new_price, outcome):
    db.upsert(conn, listing())

    assert db.upsert(conn, listing(price=new_price)) == outcome
    prices = [r["price"] for r in conn.execute("SELECT price FROM price_history ORDER BY id")]
    assert prices == [30_000_000, new_price]


def test_update_keeps_existing_values_when_new_ones_are_empty(conn):
    db.upsert(conn, listing(address="Абая 1"))

    db.upsert(conn, listing(address=""))

    assert conn.execute("SELECT address FROM ads WHERE id='1'").fetchone()[0] == "Абая 1"


def test_mark_gone_flags_listings_that_disappeared(conn):
    db.upsert(conn, listing("1"))
    db.upsert(conn, listing("2"))

    assert db.mark_gone(conn, {"1"}) == 1
    assert conn.execute("SELECT gone_at FROM ads WHERE id='2'").fetchone()[0]
    assert conn.execute("SELECT gone_at FROM ads WHERE id='1'").fetchone()[0] is None


def test_mark_gone_ignores_rejected_listings(conn):
    db.upsert(conn, listing("1"))
    conn.execute("INSERT INTO statuses VALUES ('1','disliked','now')")

    assert db.mark_gone(conn, set()) == 0


def test_returning_listing_clears_gone_at(conn):
    db.upsert(conn, listing("1"))
    db.mark_gone(conn, set())

    db.upsert(conn, listing("1"))

    assert conn.execute("SELECT gone_at FROM ads WHERE id='1'").fetchone()[0] is None


def test_skip_ids_covers_both_status_spellings(conn):
    conn.executemany(
        "INSERT INTO statuses VALUES (?,?,?)",
        [("1", "disliked", "now"), ("2", "liked", "now")],
    )

    assert db.skip_ids(conn) == {"1"}


def test_connect_upgrades_a_database_from_an_older_version(tmp_path):
    path = tmp_path / "old.db"
    old = sqlite3.connect(path)
    old.executescript(
        """
        CREATE TABLE ads (id TEXT PRIMARY KEY, title TEXT, price REAL, area REAL,
                          lat REAL, lon REAL, first_seen TEXT, last_seen TEXT,
                          gone_at TEXT);
        CREATE TABLE statuses (ad_id TEXT PRIMARY KEY, status TEXT, updated_at TEXT);
        INSERT INTO ads (id, title, price) VALUES ('1', 'old', 1000);
        INSERT INTO statuses VALUES ('1', 'hidden', 'now');
        """
    )
    old.commit()
    old.close()

    conn = db.connect(path)

    columns = {row["name"] for row in conn.execute("PRAGMA table_info(ads)")}
    assert {"owner_name", "photo", "district", "complex_id"} <= columns
    assert conn.execute("SELECT status FROM statuses").fetchone()[0] == "disliked"
    assert db.upsert(conn, listing("2")) == "new"
    conn.close()


def test_schema_can_be_pointed_somewhere_else(tmp_path, monkeypatch):
    custom = tmp_path / "custom.sql"
    custom.write_text("CREATE TABLE IF NOT EXISTS marker (id TEXT);", encoding="utf-8")
    monkeypatch.setenv("SHANYRAK_SCHEMA", str(custom))

    assert "marker" in db.schema_sql()


def test_missing_schema_says_where_it_looked(tmp_path, monkeypatch):
    monkeypatch.setenv("SHANYRAK_SCHEMA", str(tmp_path / "absent.sql"))

    with pytest.raises(FileNotFoundError, match="absent.sql"):
        db.schema_sql()


def test_counts_summarise_the_database(conn):
    db.upsert(conn, listing("1", lat=51.1, lon=71.4))
    db.upsert(conn, listing("2"))
    db.mark_gone(conn, {"1"})

    values = db.counts(conn)

    assert values["listings"] == 2
    assert values["active"] == 1
    assert values["delisted"] == 1
    assert values["with_coordinates"] == 1
