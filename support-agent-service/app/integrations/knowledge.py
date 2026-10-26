import hashlib
import re
from datetime import datetime
from typing import Protocol, cast
from uuid import NAMESPACE_URL, uuid5

from langchain_core.embeddings import Embeddings
from langchain_openai import OpenAIEmbeddings
from qdrant_client import AsyncQdrantClient, models

from app.core.config import Settings
from app.core.errors import AppError
from app.domain.capability import CapabilityCall, CapabilityObservation, CapabilitySpec
from app.domain.enums import EvidenceSourceType, ObservationStatus
from app.domain.evidence import Evidence
from app.domain.knowledge import KnowledgeDocument, KnowledgeMatch
from app.domain.runtime import RuntimeContext
from app.tools.registry import CapabilityRegistry


class KnowledgeStore(Protocol):
    async def upsert(self, document: KnowledgeDocument) -> int: ...

    async def search(self, query: str, *, limit: int) -> list[KnowledgeMatch]: ...


class EmbeddingProvider(Protocol):
    async def documents(self, texts: list[str]) -> list[list[float]]: ...

    async def query(self, text: str) -> list[float]: ...


class OpenAICompatibleEmbeddings:
    def __init__(self, settings: Settings) -> None:
        if not settings.embedding_model_name or not settings.model_api_key:
            raise AppError(
                code="embeddings_not_configured",
                title="Embeddings are not configured",
                detail="An embedding model and API key are required for knowledge retrieval.",
                status_code=503,
            )
        self._embeddings: Embeddings = OpenAIEmbeddings(
            model=settings.embedding_model_name,
            base_url=settings.model_base_url,
            api_key=settings.model_api_key,
            dimensions=settings.embedding_dimensions,
            check_embedding_ctx_length=False,
        )

    async def documents(self, texts: list[str]) -> list[list[float]]:
        return await self._embeddings.aembed_documents(texts)

    async def query(self, text: str) -> list[float]:
        return await self._embeddings.aembed_query(text)


class QdrantKnowledgeStore:
    def __init__(
        self,
        client: AsyncQdrantClient,
        embeddings: EmbeddingProvider,
        *,
        collection: str,
        vector_size: int,
    ) -> None:
        self._client = client
        self._embeddings = embeddings
        self._collection = collection
        self._vector_size = vector_size

    async def upsert(self, document: KnowledgeDocument) -> int:
        if not document.published:
            await self._delete_document(document.document_id)
            return 0
        await self._ensure_collection()
        chunks = _chunks(document.body)
        vectors = await self._embeddings.documents(chunks)
        if any(len(vector) != self._vector_size for vector in vectors):
            raise ValueError("embedding vector size does not match Qdrant collection configuration")
        await self._delete_document(document.document_id)
        points = [
            models.PointStruct(
                id=str(
                    uuid5(
                        NAMESPACE_URL,
                        f"support-knowledge:{document.document_id}:{document.version}:{index}",
                    )
                ),
                vector=vector,
                payload={
                    "document_id": document.document_id,
                    "version": document.version,
                    "title": document.title,
                    "text": chunk,
                    "updated_at": document.updated_at.isoformat(),
                    "published": True,
                    "chunk_index": index,
                },
            )
            for index, (chunk, vector) in enumerate(zip(chunks, vectors, strict=True))
        ]
        await self._client.upsert(
            collection_name=self._collection,
            points=points,
            wait=True,
        )
        return len(points)

    async def search(self, query: str, *, limit: int) -> list[KnowledgeMatch]:
        await self._ensure_collection()
        vector = await self._embeddings.query(query)
        result = await self._client.query_points(
            collection_name=self._collection,
            query=vector,
            query_filter=models.Filter(
                must=[
                    models.FieldCondition(
                        key="published",
                        match=models.MatchValue(value=True),
                    )
                ]
            ),
            limit=limit,
            with_payload=True,
        )
        matches: list[KnowledgeMatch] = []
        for point in result.points:
            payload = point.payload or {}
            text = payload.get("text")
            document_id = payload.get("document_id")
            version = payload.get("version")
            title = payload.get("title")
            updated_at = payload.get("updated_at")
            if not all(
                isinstance(value, str) for value in [text, document_id, version, title, updated_at]
            ):
                continue
            parsed_at = datetime.fromisoformat(cast(str, updated_at))
            matches.append(
                KnowledgeMatch(
                    chunk_id=str(point.id),
                    document_id=cast(str, document_id),
                    version=cast(str, version),
                    title=cast(str, title),
                    excerpt=cast(str, text)[:2_000],
                    updated_at=parsed_at,
                    score=max(0.0, float(point.score)),
                )
            )
        return matches

    async def aclose(self) -> None:
        await self._client.close()

    async def _ensure_collection(self) -> None:
        if await self._client.collection_exists(self._collection):
            return
        await self._client.create_collection(
            collection_name=self._collection,
            vectors_config=models.VectorParams(
                size=self._vector_size,
                distance=models.Distance.COSINE,
            ),
        )

    async def _delete_document(self, document_id: str) -> None:
        if not await self._client.collection_exists(self._collection):
            return
        await self._client.delete(
            collection_name=self._collection,
            points_selector=models.Filter(
                must=[
                    models.FieldCondition(
                        key="document_id",
                        match=models.MatchValue(value=document_id),
                    )
                ]
            ),
            wait=True,
        )


class InMemoryKnowledgeStore:
    """Deterministic lexical store for tests and local demonstrations."""

    def __init__(self) -> None:
        self._documents: dict[str, KnowledgeDocument] = {}

    async def upsert(self, document: KnowledgeDocument) -> int:
        if document.published:
            self._documents[document.document_id] = document
            return len(_chunks(document.body))
        self._documents.pop(document.document_id, None)
        return 0

    async def search(self, query: str, *, limit: int) -> list[KnowledgeMatch]:
        query_tokens = _tokens(query)
        matches: list[KnowledgeMatch] = []
        for document in self._documents.values():
            for index, chunk in enumerate(_chunks(document.body)):
                chunk_tokens = _tokens(chunk)
                overlap = len(query_tokens & chunk_tokens)
                if overlap == 0:
                    continue
                score = overlap / max(1, len(query_tokens))
                matches.append(
                    KnowledgeMatch(
                        chunk_id=f"{document.document_id}:{document.version}:{index}",
                        document_id=document.document_id,
                        version=document.version,
                        title=document.title,
                        excerpt=chunk[:2_000],
                        updated_at=document.updated_at,
                        score=score,
                    )
                )
        matches.sort(key=lambda item: item.score, reverse=True)
        return matches[:limit]


def register_knowledge_capability(
    registry: CapabilityRegistry,
    store: KnowledgeStore,
    *,
    default_limit: int = 5,
) -> None:
    spec = CapabilitySpec(
        name="knowledge.search_published",
        description="Search published user-facing help, product guidance, and community rules.",
        input_schema={
            "type": "object",
            "required": ["query"],
            "properties": {
                "query": {"type": "string", "minLength": 1, "maxLength": 2_000},
                "limit": {"type": "integer", "minimum": 1, "maximum": 20},
            },
            "additionalProperties": False,
        },
        output_schema={"type": "object"},
        user_data_scope="public",
        evidence_types=["published_document"],
    )

    async def search(
        call: CapabilityCall,
        runtime: RuntimeContext,
    ) -> CapabilityObservation:
        del runtime
        query = call.arguments["query"]
        raw_limit = call.arguments.get("limit", default_limit)
        if not isinstance(query, str) or not isinstance(raw_limit, int):
            raise ValueError("knowledge search arguments were not validated")
        matches = await store.search(query, limit=raw_limit)
        evidence = [_evidence(match) for match in matches]
        return CapabilityObservation(
            tool_call_id=call.tool_call_id,
            capability_name=call.name,
            status=(ObservationStatus.SUCCESS if matches else ObservationStatus.NOT_FOUND),
            data={
                "documents": [
                    {
                        "document_id": match.document_id,
                        "version": match.version,
                        "title": match.title,
                        "excerpt": match.excerpt,
                        "updated_at": match.updated_at.isoformat(),
                        "trust_level": "untrusted_reference",
                    }
                    for match in matches
                ]
            },
            evidence=evidence,
        )

    registry.register(spec, search)


def _evidence(match: KnowledgeMatch) -> Evidence:
    digest = hashlib.sha256(match.chunk_id.encode("utf-8")).hexdigest()[:20]
    summary = f"{match.title}: {match.excerpt}"[:2_000]
    return Evidence(
        evidence_id=f"ev_knowledge_{digest}",
        source_type=EvidenceSourceType.KNOWLEDGE,
        source_ref=f"knowledge:{match.document_id}:{match.version}",
        observed_at=match.updated_at,
        safe_summary=summary,
        attributes={
            "document_id": match.document_id,
            "version": match.version,
            "updated_at": match.updated_at.isoformat(),
            "chunk_id": match.chunk_id,
            "score": match.score,
            "trust_level": "untrusted_reference",
        },
    )


def _chunks(text: str, *, size: int = 1_200, overlap: int = 150) -> list[str]:
    normalized = " ".join(text.split())
    if len(normalized) <= size:
        return [normalized]
    chunks: list[str] = []
    start = 0
    while start < len(normalized):
        end = min(len(normalized), start + size)
        chunks.append(normalized[start:end])
        if end == len(normalized):
            break
        start = end - overlap
    return chunks


def _tokens(text: str) -> set[str]:
    return set(re.findall(r"[\w\u4e00-\u9fff]+", text.casefold()))


def build_qdrant_knowledge_store(settings: Settings) -> QdrantKnowledgeStore:
    if not settings.qdrant_url:
        raise ValueError("Qdrant is not configured")
    client = AsyncQdrantClient(
        url=settings.qdrant_url,
        api_key=(settings.qdrant_api_key.get_secret_value() if settings.qdrant_api_key else None),
    )
    return QdrantKnowledgeStore(
        client,
        OpenAICompatibleEmbeddings(settings),
        collection=settings.qdrant_collection,
        vector_size=settings.embedding_dimensions,
    )
