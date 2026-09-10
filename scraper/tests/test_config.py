import pytest

from shanyrak_scraper import config

BASE = """
data_dir = "db"

[defaults]
max_pages = 10

[defaults.filters]
has_photo = true

[defaults.local]
min_price_per_m2 = 400_000
flag_words = ["залог"]

[searches.astana]
section = "prodazha/kvartiry/astana"

[searches.astana.filters]
price_to = 40_000_000

[searches.almaty]
section = "/prodazha/kvartiry/almaty/"
max_pages = 3

[searches.almaty.local]
min_price_per_m2 = 500_000
"""


def write(tmp_path, text=BASE):
    path = tmp_path / "shanyrak.toml"
    path.write_text(text, encoding="utf-8")
    return path


def test_load_merges_defaults_into_each_search(tmp_path):
    conf = config.load(write(tmp_path))

    astana = conf.get("astana")
    almaty = conf.get("almaty")

    assert [s.name for s in conf.searches] == ["almaty", "astana"]
    assert astana.filters == {"has_photo": True, "price_to": 40_000_000}
    assert astana.max_pages == 10
    assert almaty.max_pages == 3
    assert astana.local.min_price_per_m2 == 400_000
    assert almaty.local.min_price_per_m2 == 500_000
    assert almaty.local.flag_words == ("залог",)


def test_section_is_normalised(tmp_path):
    conf = config.load(write(tmp_path))

    assert conf.get("astana").section == "/prodazha/kvartiry/astana/"


def test_data_dir_and_db_paths_are_relative_to_the_config(tmp_path):
    conf = config.load(write(tmp_path))

    assert conf.data_dir == tmp_path / "db"
    assert conf.db_path(conf.get("astana")) == tmp_path / "db" / "astana.db"


def test_select_defaults_to_every_search(tmp_path):
    conf = config.load(write(tmp_path))

    assert len(conf.select([])) == 2
    assert [s.name for s in conf.select(["astana"])] == ["astana"]


def test_unknown_search_is_reported(tmp_path):
    conf = config.load(write(tmp_path))

    with pytest.raises(config.ConfigError, match="unknown search"):
        conf.get("karaganda")


def test_unknown_filter_key_is_rejected(tmp_path):
    text = BASE.replace("price_to = 40_000_000", "price_two = 40_000_000")

    with pytest.raises(config.ConfigError, match="unknown keys"):
        config.load(write(tmp_path, text))


def test_search_without_section_or_areas_is_rejected(tmp_path):
    text = """
[searches.astana]
max_pages = 2
"""

    with pytest.raises(config.ConfigError, match="section"):
        config.load(write(tmp_path, text))


def test_areas_only_search_is_allowed(tmp_path):
    text = """
[searches.astana]
areas = ["https://krisha.kz/map/prodazha/kvartiry/astana/?areas=p51.1,71.4"]
"""

    conf = config.load(write(tmp_path, text))

    assert conf.get("astana").areas


def test_missing_file_explains_how_to_create_one(tmp_path):
    with pytest.raises(config.ConfigError, match="shanyrak.example.toml"):
        config.load(tmp_path / "absent.toml")


def test_empty_config_is_rejected(tmp_path):
    with pytest.raises(config.ConfigError, match="no \\[searches"):
        config.load(write(tmp_path, "data_dir = 'x'\n"))
