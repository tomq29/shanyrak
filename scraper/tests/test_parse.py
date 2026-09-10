import pytest

from shanyrak_scraper.config import Local
from shanyrak_scraper.parse import (
    Listing,
    card_dates,
    city_for_section,
    max_page,
    parse_page,
    parse_title,
    passes_local,
    with_flags,
)


def test_parse_page_reads_every_advert(map_page):
    listings = parse_page(map_page, city="Астана")

    assert len(listings) == 20
    assert all(l.lat and l.lon for l in listings)
    assert all(l.price and l.area for l in listings)
    assert all(l.link.startswith("https://krisha.kz/a/show/") for l in listings)
    assert {l.city for l in listings} == {"Астана"}


def test_parse_page_maps_fields(map_page):
    listing = next(l for l in parse_page(map_page) if l.id == "1015617521")

    assert listing.price == 28_500_000
    assert listing.area == 66.6
    assert listing.rooms == 2
    assert listing.floor == 11
    assert listing.total_floors == 15
    assert listing.address == "Бейбарыс Султан 12"
    assert listing.seller == "owner"
    assert listing.date_posted == "11 сентября"
    assert listing.photo.startswith("https://")
    assert listing.price_per_m2 == pytest.approx(427_927, rel=1e-3)


def test_parse_page_skips_adverts_without_price():
    payload = {"adverts": {"1": {"id": 1, "title": "x", "price": 0}}, "html": ""}
    assert parse_page(payload) == []


@pytest.mark.parametrize(
    "title, expected",
    [
        ("2-комнатная квартира · 66.6 м² · 11/15 этаж", (2, 66.6, 11, 15)),
        ("1-комнатная квартира · 28 м² · 4/14 этаж", (1, 28.0, 4, 14)),
        ("3-комн. квартира · 80,5 м² · 10 этаж", (3, 80.5, 10, None)),
        ("квартира", (None, None, None, None)),
    ],
)
def test_parse_title(title, expected):
    assert parse_title(title) == expected


def test_card_dates_are_keyed_by_listing_id(map_page):
    dates = card_dates(map_page["html"])

    assert dates["1015617521"] == "11 сентября"
    assert len(dates) == len(map_page["adverts"])


def test_max_page_reads_the_paginator(map_page):
    assert max_page(map_page["pager"]) == 15
    assert max_page("") is None


@pytest.mark.parametrize(
    "section, city",
    [
        ("/prodazha/kvartiry/astana/", "Астана"),
        ("/prodazha/kvartiry/almaty-bostandykskij/", "Алматы"),
        ("/prodazha/kvartiry/unknown-town/", ""),
    ],
)
def test_city_for_section(section, city):
    assert city_for_section(section) == city


def test_with_flags_marks_matching_words():
    listing = Listing(id="1", title="Квартира в залоге", address="ул. Абая 1")

    assert with_flags(listing, ("залог", "доля")).flags == "залог"
    assert with_flags(listing, ()).flags == ""


@pytest.mark.parametrize(
    "price, area, expected",
    [(30_000_000, 60, True), (6_000_000, 60, False), (90_000_000, 60, False)],
)
def test_passes_local_price_per_m2_bounds(price, area, expected):
    listing = Listing(id="1", price=price, area=area)
    local = Local(min_price_per_m2=400_000, max_price_per_m2=1_000_000)

    assert passes_local(listing, local) is expected
