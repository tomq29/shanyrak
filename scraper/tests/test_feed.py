import json

import pytest

from shanyrak_scraper.feed import (
    BlockedError,
    Feed,
    FeedError,
    area_params,
    build_query,
    section_from_area,
)

MAP_URL = (
    "https://krisha.kz/map/prodazha/kvartiry/astana/?das[price][to]=40000000"
    "&zoom=13&lat=51.09&lon=71.41&areas=p51.11,71.46,51.12,71.45,51.11,71.45"
)


class FakeResponse:
    def __init__(self, status_code=200, payload=None, text=""):
        self.status_code = status_code
        self._payload = payload
        self.text = text or json.dumps(payload or {})

    def json(self):
        if self._payload is None:
            raise ValueError("not json")
        return self._payload


class FakeSession:
    def __init__(self, responses):
        self.responses = list(responses)
        self.urls = []
        self.headers = {}

    def get(self, url, **kwargs):
        self.urls.append(url)
        response = self.responses.pop(0)
        if isinstance(response, Exception):
            raise response
        return response


def make_feed(responses, **kwargs):
    return Feed(
        session=FakeSession(responses), sleep=lambda _: None, delay=(0, 0), **kwargs
    )


def test_area_params_from_map_url():
    assert area_params(MAP_URL) == {
        "areas": "p51.11,71.46,51.12,71.45,51.11,71.45",
        "lat": "51.09",
        "lon": "71.41",
        "zoom": "13",
    }


def test_area_params_from_bare_polygon():
    assert area_params("p51.11,71.46,51.12,71.45") == {
        "areas": "p51.11,71.46,51.12,71.45"
    }
    assert area_params("") == {}


def test_section_from_area_strips_the_map_prefix():
    assert section_from_area(MAP_URL) == "/prodazha/kvartiry/astana/"
    assert section_from_area("https://krisha.kz/") == ""


def test_build_query_maps_filters_to_site_parameters():
    query = build_query(
        {
            "price_to": 40_000_000,
            "square_from": 30,
            "rooms": [2, 3],
            "not_first_floor": True,
            "has_photo": True,
            "who": 1,
            "text": "панорамные окна",
        },
        page=2,
    )

    assert "das%5Bprice%5D%5Bto%5D=40000000" in query
    assert "das%5Blive.square%5D%5Bfrom%5D=30" in query
    assert query.count("das%5Blive.rooms%5D%5B%5D=") == 2
    assert "das%5Bfloor_not_first%5D=1" in query
    assert "das%5B_sys.hasphoto%5D=1" in query
    assert "das%5Bwho%5D=1" in query
    assert query.endswith("page=2")


def test_build_query_omits_absent_filters():
    assert build_query({}) == "page=1"


def test_page_requests_the_feed_url(map_page):
    feed = make_feed([FakeResponse(payload=map_page)])

    payload = feed.page("/prodazha/kvartiry/astana/", {"price_to": 1}, MAP_URL, 3)

    assert payload["page"] == 1
    url = feed.session.urls[0]
    assert url.startswith("https://krisha.kz/a/ajax-map-list/map/prodazha/kvartiry/astana/?")
    assert "page=3" in url
    assert "areas=p51.11" in url


def test_challenge_response_raises_blocked():
    feed = make_feed([FakeResponse(status_code=468, text="<title id=slg-title>")])

    with pytest.raises(BlockedError, match="bot-protection"):
        feed.page("/prodazha/kvartiry/astana/", {})


def test_retries_then_succeeds(map_page):
    feed = make_feed(
        [FakeResponse(status_code=500, text="oops"), FakeResponse(payload=map_page)]
    )

    assert feed.page("/prodazha/kvartiry/astana/", {})["page"] == 1
    assert len(feed.session.urls) == 2


def test_gives_up_after_retries():
    feed = make_feed([FakeResponse(status_code=500, text="oops")] * 3, retries=3)

    with pytest.raises(FeedError, match="giving up"):
        feed.page("/prodazha/kvartiry/astana/", {})
