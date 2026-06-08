from pathlib import Path 
from typing import List
from workers.worker_utils.embeddings.embeder_base import EmbeddingBase
from workers.worker_utils.embeddings.embedding_response import EmbeddingResponse
from workers.worker_utils.chunking.text_chunk_model import TextChunk

EmbeddingModel = str
EmbeddingClient  =str

class EmbeddingsRouter:
    def __init__(self, embedders: dict[(EmbeddingModel, EmbeddingClient), EmbeddingBase]):
        self._embedders = embedders


    def embed_image(self, path: Path | bytes, model: EmbeddingModel, client: EmbeddingClient) -> EmbeddingResponse:
        return self._embedders[(model, client)].embed_image(path=path)

    
    def embed_text(self, text: str, model: EmbeddingModel, client: EmbeddingClient) -> EmbeddingResponse:
        return self._embedders[(model, client)].embed_text(text=text)

    
    def embed_chunks(self, chunks: List[TextChunk], model: EmbeddingModel, client: EmbeddingClient) -> List[EmbeddingResponse]:
        return self._embedders[(model, client)].embed_chunks(chunks=chunks)