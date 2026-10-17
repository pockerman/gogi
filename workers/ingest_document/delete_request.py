from pydantic import BaseModel


class DocumentDeleteRequest(BaseModel):
    index_name: str
    document_id: str
