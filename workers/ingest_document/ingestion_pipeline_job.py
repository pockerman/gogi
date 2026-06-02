"""
Ingest a document pipeline worker. The pipeline has the following steps

1. extract the document from the bytes
2. Create the chunks.
3. Generate embeddings for the chunks
4. Store the chunks and embeddings in the vector database
"""
import os
from temporalio import activity

from workers.worker_utils.documents.parsers.parser_router import ParseRouter
from workers.worker_utils.documents.parsers.document_parser_base import ParserType
from workers.worker_utils.documents.parsers.text_parser import TextParser
from workers.worker_utils.chunking.chunking_router import ChunkingRouter
from workers.worker_utils.storage.vector_storage_router import VectorStorageRouter
from workers.worker_utils.embeddings.embbeddings_router import EmbeddingsRouter
from workers.ingest_document.ingestion_request import DocumentIngestionRequest


PARSERS = {

   ParserType.TEXT: TextParser()
}

parse_router = ParseRouter(parsers=PARSERS)


CHUNKING_STRATEGIES = {}

chunk_router = ChunkingRouter(chunking_strategies=CHUNKING_STRATEGIES)

EMBEDDERS = {}
embeddings_router = EmbeddingsRouter(embedders=EMBEDDERS)

vector_storage_router = VectorStorageRouter(name=os.getenv("VECTOR_STORAGE_TYPE"),
                                            host=os.getenv("VECTOR_STORAGE_HOST"),
                                            port=os.getenv("VECTOR_STORAGE_PORT"))

@activity.defn(name="ingest_document")
async def ingest_document(req):
    print(f"ingest_document received request: {req}")

    request = DocumentIngestionRequest(**req)

    # 1. Parse the document 
    extracted_document = parse_router.parse(file_bytes=request.content, filename=request.filename)

    # 2. Create chunks
    chunking_strategy = request.chunk_strategy
    chunks = chunk_router[chunking_strategy].chunk(extracted_document)

    # 3. Create the embeddings for the chunks
    embeddings = embeddings_router.embed_chunks(chunks=chunks, model=request.embeddings_model,
                                                client=request.embeddings_client)
    
    
    # 4. Store vectors
    vector_storage_router.insert(index_name=request.index_name, document_id=request.document_id,
                                 chunks=chunks, embeddings=embeddings, metadata=request.metadata)
    return "done"



