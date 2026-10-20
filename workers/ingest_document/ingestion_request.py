from pydantic import BaseModel, Field
from typing import Optional, Dict, List


class EmbeddingModelConfig(BaseModel):
    """how a document is chunked for a model, and the index's collection for the model"""
    model: str
    collection: str
    # in words, so a chunk is no longer than the model reads
    chunk_size: int
    chunk_overlap: int


class DocumentIngestionRequest(BaseModel):
    job_id: str
    index_name: str
    filename: str
    document_id: str
    # the application whose document it is: the models are its own, and called with its keys
    application: str
    batch_size: int = 0
    # the models that embed the document, each into a collection of its own
    embedding_models: List[EmbeddingModelConfig]
    chunk_strategy: str = ""
    metadata: Optional[Dict[str, str]] = Field(default_factory=dict)
    # the document's content is in the object store
    bucket: str
    object_key: str
