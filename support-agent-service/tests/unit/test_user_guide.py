import json
from pathlib import Path

import pytest

from app.services.user_guide import load_user_guide


def test_reviewed_guide_documents_load_without_internal_source_notes() -> None:
    documents = load_user_guide(Path("knowledge/user-guide/manifest.json"))
    assert len(documents) == 6
    assert all(document.published for document in documents)
    assert all("## 来源" not in document.body for document in documents)
    assert any("先保存" in document.body and "真实运行" in document.body for document in documents)
    assert any(document.document_id == "user-guide-social-actions" for document in documents)


@pytest.mark.parametrize("relative", ["../outside.md", "/absolute.md", "doc.txt"])
def test_loader_rejects_outside_or_non_markdown_paths(tmp_path: Path, relative: str) -> None:
    manifest = tmp_path / "manifest.json"
    manifest.write_text(json.dumps([{"path": relative}]), encoding="utf-8")
    with pytest.raises(ValueError):
        load_user_guide(manifest)


def test_loader_rejects_duplicate_document_ids(tmp_path: Path) -> None:
    (tmp_path / "doc.md").write_text("用户指南", encoding="utf-8")
    entry = {
        "path": "doc.md",
        "document_id": "user-guide-test",
        "version": "v1",
        "title": "指南",
        "updated_at": "2026-10-02T00:00:00Z",
        "published": True,
    }
    manifest = tmp_path / "manifest.json"
    manifest.write_text(json.dumps([entry, entry]), encoding="utf-8")
    with pytest.raises(ValueError, match="unique"):
        load_user_guide(manifest)
