from pydantic import BaseModel



class EmbeddingResponse(BaseModel):
    embeddings: list[float]
    model_name: str
    device: str | None = None
    pretrained: str | None = None
    tokenizer: str | None = None
