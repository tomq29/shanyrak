import pytest

from shanyrak_scraper import db
from shanyrak_scraper.config import Local, Search
from shanyrak_scraper.feed import BlockedError
from shanyrak_scraper.scan import scan


class FakeFeed:
    """Serves the fixture as page 1 and an empty page afterwards."""

    def __init__(self, payload, pages=1, fail_on=None):
        self.payload = payload
        self.pages = pages
        self.fail_on = fail_on
        self.calls = []

    def page(self, section, filters, area="", page=1):
        self.calls.append((section, area, page))
        if self.fail_on == page:
            raise BlockedError("blocked")
        if page > self.pages:
            return {"adverts": {}, "html": "", "pager": ""}
        return {**self.payload, "pager": ""}


@pytest.fixture
def conn(tmp_path):
    connection = db.connect(tmp_path / "scan.db")
    yield connection
    connection.close()


def make_search(**kwargs):
    defaults = dict(
        name="astana",
        section="/prodazha/kvartiry/astana/",
        areas=(),
        filters={},
        local=Local(),
        max_pages=5,
    )
    return Search(**{**defaults, **kwargs})


def test_scan_stores_every_listing(conn, map_page):
    report = scan(make_search(), conn, FakeFeed(map_page))

    assert report.fetched == 20
    assert report.new == 20
    assert report.complete is True
    assert conn.execute("SELECT COUNT(*) FROM ads").fetchone()[0] == 20


def test_second_scan_reports_unchanged_listings(conn, map_page):
    scan(make_search(), conn, FakeFeed(map_page))

    report = scan(make_search(), conn, FakeFeed(map_page))

    assert report.new == 0
    assert report.seen == 20
    assert report.gone == 0


def test_scan_walks_every_configured_area(conn, map_page):
    feed = FakeFeed(map_page)
    areas = (
        "https://krisha.kz/map/prodazha/kvartiry/almaty/?areas=p43.2,76.9",
        "p51.1,71.4",
    )

    scan(make_search(areas=areas), conn, feed)

    assert [call[1] for call in feed.calls if call[2] == 1] == list(areas)
    assert feed.calls[0][0] == "/prodazha/kvartiry/almaty/"
    assert feed.calls[-1][0] == "/prodazha/kvartiry/astana/"


def test_local_filters_drop_out_of_range_listings(conn, map_page):
    local = Local(min_price_per_m2=1_000_000, max_price_per_m2=2_000_000)

    report = scan(make_search(local=local), conn, FakeFeed(map_page))

    assert report.fetched == 0
    assert report.filtered == 20


def test_rejected_listings_are_not_stored_again(conn, map_page):
    ad_id = next(iter(map_page["adverts"]))
    conn.execute("INSERT INTO statuses VALUES (?,'disliked','now')", (ad_id,))

    report = scan(make_search(), conn, FakeFeed(map_page))

    assert report.skipped == 1
    assert report.fetched == 19


def test_blocked_feed_stops_the_area_and_keeps_delisted_untouched(conn, map_page):
    scan(make_search(), conn, FakeFeed(map_page))

    report = scan(make_search(), conn, FakeFeed(map_page, fail_on=1))

    assert report.blocked is True
    assert report.complete is False
    assert report.gone == 0
    assert conn.execute("SELECT COUNT(*) FROM ads WHERE gone_at IS NULL").fetchone()[0] == 20


def test_disappeared_listings_are_marked_after_a_complete_crawl(conn, map_page):
    scan(make_search(), conn, FakeFeed(map_page))
    smaller = {**map_page, "adverts": dict(list(map_page["adverts"].items())[:5])}

    report = scan(make_search(), conn, FakeFeed(smaller))

    assert report.gone == 15


def test_max_pages_marks_the_crawl_incomplete(conn, map_page):
    report = scan(make_search(max_pages=1), conn, FakeFeed(map_page, pages=5))

    assert report.complete is False
    assert report.gone == 0
