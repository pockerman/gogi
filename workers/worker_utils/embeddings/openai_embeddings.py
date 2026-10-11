import os
from pathlib import Path
from typing import List, override

from openai import OpenAI

from workers.worker_utils.embeddings.embedding_response import EmbeddingResponse
from workers.worker_utils.embeddings.embeder_base import EmbeddingBase
from workers.worker_utils.chunking.text_chunk_model import TextChunk

# OpenAI embeddings accept up to 2048 inputs per request; keep comfortably under that.
MAX_BATCH_SIZE = 256


class OpenAIEmbeddings(EmbeddingBase):
    """Text embeddings backed by the OpenAI embeddings API."""

    def __init__(self, model_name: str = "text-embedding-3-small"):
        super().__init__()
        self.model_name = model_name
        self._client = OpenAI(api_key=os.getenv("OPENAI_API_KEY"))

    @property
    def embedder_id(self) -> str:
        return "openai"

    @override
    def embed_image(self, img: Path | bytes) -> EmbeddingResponse:
        raise NotImplementedError("OpenAI text embedding models do not support images")

    @override
    def embed_text(self, text: str) -> EmbeddingResponse:
        response = self._client.embeddings.create(model=self.model_name, input=text)
        return EmbeddingResponse(embeddings=response.data[0].embedding, model_name=self.model_name)

    @override
    def embed_chunks(self, chunks: List[TextChunk]) -> List[EmbeddingResponse]:
        embeddings: List[EmbeddingResponse] = []
        for start in range(0, len(chunks), MAX_BATCH_SIZE):
            batch = chunks[start : start + MAX_BATCH_SIZE]
            response = self._client.embeddings.create(
                model=self.model_name, input=[chunk.text for chunk in batch]
            )
            embeddings.extend(
                EmbeddingResponse(embeddings=item.embedding, model_name=self.model_name) for item in response.data
            )
        return embeddings
