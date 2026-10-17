"""
Run from the repository root: PYTHONPATH=. pytest workers/tests (skipped without chromadb installed)
"""
import pytest

chromadb_errors = pytest.importorskip("chromadb.errors")

from workers.worker_utils.storage.chromadb_wrapper import ChromaDBHttpWrapper


class NoCollections:
    """A Chroma client with no collections: nothing has been ingested into any index."""

    def get_collection(self, name):
        raise chromadb_errors.NotFoundError(f"Collection [{name}] does not exist")


def test_searching_an_index_with_nothing_ingested_finds_nothing():
    storage = object.__new__(ChromaDBHttpWrapper)
    storage._chroma_client = NoCollections()

    assert storage.search(index_name="maria--aeronautics", query_embedding=[0.1, 0.2], top_k=3) == []
