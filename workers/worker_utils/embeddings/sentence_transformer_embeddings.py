from sentence_transformers import SentenceTransformer
from typing import override 

from workers.worker_utils.embeddings.embedding_response import EmbeddingResponse
from workers.worker_utils.embeddings.embeder_base import EmbeddingBase

class SentenceTransformerEmbeddings(EmbeddingBase):

    def __init__(self, model_name: str = "clip-ViT-L-14"):
        super().__init__()
        self.model_name = model_name
        self.model = SentenceTransformer(model_name)

    @property
    def embedder_id(self) -> str:
        return "clip"

    @override
    def embed_image(self, img: Path | bytes) -> EmbeddingResponse:
        image = self.load_image(img)
        image = image.convert('RGB')
        img_embedding = self.model.encode(image, convert_to_numpy=True,
                                          normalize_embeddings=True)
        img_embedding = img_embedding.tolist()
        return EmbeddingResponse(embeddings=img_embedding,
                                model_name=self.model_name,)

    @override
    def embed_text(self, text: str) -> EmbeddingResponse:
        emb = self.model.encode(text, convert_to_numpy=True, normalize_embeddings=True)
        return EmbeddingResponse(embeddings=emb.tolist(),
                                 model_name=self.model_name,)