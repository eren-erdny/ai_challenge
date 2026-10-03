"""Persistent local Qdrant; set QDRANT_URL to use a separate server."""
import argparse
import json
import os
from pathlib import Path
from tempfile import TemporaryDirectory

from qdrant_client import QdrantClient, models

ROOT = Path(__file__).resolve().parent


def open_store():
    url = os.environ.get("QDRANT_URL")
    if url:
        return QdrantClient(url=url)
    directory = Path(os.environ.get('RAG_DATA_DIR', ROOT / 'data')).resolve()
    return QdrantClient(path=str(directory / 'qdrant'))


def smoke_test():
    # Synthetic vectors test storage, not embedding quality. Never touch user data.
    with TemporaryDirectory(prefix="qdrant-check-") as directory:
        client = QdrantClient(path=directory)
        try:
            client.create_collection(
                collection_name="check",
                vectors_config=models.VectorParams(size=3, distance=models.Distance.COSINE),
            )
            payload = {
                "text": "Storage verification example",
                "source": "synthetic", "file": "check.txt", "title": "Check",
                "section": "Storage", "chunk_id": "check-1",
            }
            client.upsert(collection_name="check", wait=True, points=[
                models.PointStruct(id=1, vector=[1.0, 0.0, 0.0], payload=payload),
                models.PointStruct(id=2, vector=[0.0, 1.0, 0.0], payload={"source": "other"}),
            ])
        finally:
            client.close()
        client = QdrantClient(path=directory)
        try:
            assert client.count("check", exact=True).count == 2
            hits = client.query_points(
                collection_name="check", query=[1.0, 0.0, 0.0], limit=1,
                with_payload=True,
                query_filter=models.Filter(must=[models.FieldCondition(
                    key="source", match=models.MatchValue(value="synthetic"),
                )]),
            ).points
            assert len(hits) == 1 and hits[0].id == 1
            assert hits[0].payload == payload
            assert hits[0].score > 0.999
        finally:
            client.close()
    print("PASS: vectors, metadata, filtered search, persistence after reopening")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["status", "check"], default="status", nargs="?")
    args = parser.parse_args()
    if args.command == "check":
        smoke_test()
    else:
        store = open_store()
        try:
            print(json.dumps({
                "storage": os.environ.get("QDRANT_URL") or str(ROOT / "data" / "qdrant"),
                "collections": [c.name for c in store.get_collections().collections],
            }, indent=2))
        finally:
            store.close()
