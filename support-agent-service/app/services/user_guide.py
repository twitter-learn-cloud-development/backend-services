"""Bounded, explicit ingestion of reviewed user documentation."""

import json
from pathlib import Path
from typing import Annotated

from pydantic import Field, TypeAdapter

from app.domain.knowledge import KnowledgeDocument


def load_user_guide(manifest: Path) -> list[KnowledgeDocument]:
    root = manifest.resolve().parent
    if manifest.stat().st_size > 1_000_000:
        raise ValueError("guide manifest is too large")
    raw = json.loads(manifest.read_text(encoding="utf-8"))
    entries: TypeAdapter[list[dict[str, object]]] = TypeAdapter(
        Annotated[list[dict[str, object]], Field(min_length=1, max_length=50)]
    )
    validated = entries.validate_python(raw)
    documents: list[KnowledgeDocument] = []
    seen: set[str] = set()
    for entry in validated:
        values = dict(entry)
        relative = values.pop("path", None)
        if not isinstance(relative, str) or not relative or Path(relative).is_absolute():
            raise ValueError("guide path must be relative")
        path = (root / relative).resolve()
        if not path.is_relative_to(root) or path.suffix != ".md":
            raise ValueError("guide path must remain inside the documentation directory")
        # Bound read size before loading the document; do not ingest internal source notes.
        if path.stat().st_size > 400_000:
            raise ValueError("guide document is too large")
        body = path.read_text(encoding="utf-8")
        body = body.split("\n## 来源", 1)[0].strip()
        document = KnowledgeDocument.model_validate({**values, "body": body})
        if not document.document_id.startswith("user-guide-"):
            raise ValueError("guide document id must use the user-guide namespace")
        if document.document_id in seen:
            raise ValueError("guide document ids must be unique")
        seen.add(document.document_id)
        documents.append(document)
    return documents
