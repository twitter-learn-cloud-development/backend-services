from pathlib import Path
from typing import Any, cast

import yaml


def test_openapi_contract_exposes_only_unified_business_endpoints() -> None:
    contract_path = Path("contracts/support-agent-v1.openapi.yaml")
    document = cast(dict[str, Any], yaml.safe_load(contract_path.read_text(encoding="utf-8")))

    assert document["openapi"] == "3.1.0"
    assert set(document["paths"]) == {
        "/api/v1/support/chat",
        "/internal/v1/support/query",
    }
