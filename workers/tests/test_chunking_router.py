"""
Run from the repository root: PYTHONPATH=. pytest workers/tests
"""
import pytest

from workers.worker_utils.chunking.chunking_router import ChunkingRouter


class Strategy:
    def __init__(self, name):
        self.name = name

    def chunk(self, document, chunk_overlap, chunk_size):
        return [self.name]


def test_a_request_without_a_strategy_is_chunked_with_the_default():
    router = ChunkingRouter({"fixed": Strategy("fixed")})

    assert router.chunk(document=None, chunking_strategy="") == ["fixed"]


def test_an_unknown_strategy_is_refused_with_the_known_ones():
    router = ChunkingRouter({"fixed": Strategy("fixed")})

    with pytest.raises(ValueError, match="unknown chunking strategy 'semantic': use one of fixed"):
        router.chunk(document=None, chunking_strategy="semantic")
