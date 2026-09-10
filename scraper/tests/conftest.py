import json
from pathlib import Path

import pytest

FIXTURES = Path(__file__).parent / "fixtures"


@pytest.fixture
def map_page():
    return json.loads((FIXTURES / "map_list_page1.json").read_text(encoding="utf-8"))
