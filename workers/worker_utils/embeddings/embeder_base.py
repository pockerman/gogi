from abc import ABC, abstractmethod
import numpy as np
from io import BytesIO
from PIL import Image
from pathlib import Path
from typing import List
from enum import StrEnum

from workers.worker_utils.embeddings.embedding_response import EmbeddingResponse
from workers.worker_utils.chunking.text_chunk_model import TextChunk

class EmbedderClientType(StrEnum):
    SENTENCE_TRANSFORMER = "sentence-transformer"

class EmbedderModelType(StrEnum):
    CLIP = "clip"

class EmbeddingBase(ABC):

    @staticmethod
    def normalize(v):
        v = np.array(v, dtype=np.float32)
        return v / (np.linalg.norm(v) + 1e-10)

    @staticmethod
    def load_image(img: Path | bytes) -> Image.Image:
        if isinstance(img, Path):
            image = Image.open(img)
        elif isinstance(img, bytes):
            image = Image.open(BytesIO(img))
        else:
            raise ValueError("img should be either a Path to an image or bytes")
        return image

    def __init__(self):
        self._model = None

    @property
    @abstractmethod
    def embedder_id(self) -> str:
        pass

    @abstractmethod
    def embed_image(self, path: Path | bytes) -> EmbeddingResponse:
        pass

    @abstractmethod
    def embed_text(self, text: str) -> EmbeddingResponse:
        pass

    
    def embed_chunks(self, chunks: List[TextChunk]) -> List[EmbeddingResponse]:
        embeddings = []

        for chunk in chunks:
            embeddings.append(self.embed_text(text=chunk.text))
        return embeddings