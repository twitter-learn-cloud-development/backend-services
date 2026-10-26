"""Review locally by default; --publish explicitly updates only owned document ids."""

import argparse
import asyncio
import json
from pathlib import Path

from app.core.config import get_settings
from app.integrations.knowledge import build_qdrant_knowledge_store
from app.services.user_guide import load_user_guide


async def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, default=Path("knowledge/user-guide/manifest.json"))
    parser.add_argument("--publish", action="store_true")
    parser.add_argument("--document-id", action="append", default=[])
    args = parser.parse_args()
    documents = load_user_guide(args.manifest)
    if args.document_id:
        requested = set(args.document_id)
        if not requested <= {doc.document_id for doc in documents}:
            raise ValueError("unknown owned guide document id")
        documents = [doc for doc in documents if doc.document_id in requested]
    result: dict[str, object] = {
        "mode": "publish" if args.publish else "dry-run",
        "documents": [
            {"id": doc.document_id, "version": doc.version, "published": doc.published}
            for doc in documents
        ],
    }
    if args.publish:
        store = build_qdrant_knowledge_store(get_settings())
        try:
            for document in documents:
                await store.upsert(document)
            matches = await store.search("Workflow 是什么, 如何创建、保存和运行?", limit=5)
            ids = sorted({match.document_id for match in matches})
            result["retrieved_document_ids"] = ids
            if not any(doc_id.startswith("user-guide-workflow") for doc_id in ids):
                raise RuntimeError("published guide retrieval verification failed")
            social = await store.search("如何添加书签和发送私信", limit=5)
            social_ids = sorted({match.document_id for match in social})
            result["social_retrieved_document_ids"] = social_ids
            if "user-guide-social-actions" not in social_ids:
                raise RuntimeError("published social guide retrieval verification failed")
        finally:
            await store.aclose()
    print(json.dumps(result, ensure_ascii=False))


if __name__ == "__main__":
    asyncio.run(main())
