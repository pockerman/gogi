"""
Ingest a document pipeline worker. The pipeline has the following steps

// 1. extract the document from the bytes
extract_document := p.ExtractDocument(config.Filename, config.Content)

// 2. Create the chunks.
chunkGenerator := p.chunkRegistar.GetChunkStrategy(config.ChunkStrategy)
chunksList := chunkGenerator.GenerateChunks(extract_document)

// 3. Generate embeddings for the chunks
embeddings := p.embeddingGenerator.EmbedChunks(chunksList, config.EmbeddingsModel, config.EmbeddingsClient, config.BatchSize)

// 4. Store the chunks and embeddings in the vector database
_, err := (*p.vectorStore).Insert(indexName, documentID, chunks, embeddings, metadata)

"""
import os
from temporalio import activity

from workers.worker_utils.documents.parsers.parser_router import ParseRouter
from workers.worker_utils.documents.parsers.text_parser import TextParser
from workers.worker_utils.chunking.chunking_router import ChunkingRouter
from workers.worker_utils.storage.vector_storage_router import VectorStorageRouter
from workers.worker_utils.embeddings.embbeddings_router import EmbeddingsRouter


PARSERS = {

    "txt": TextParser()
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

    # 1. Parse the document 
    extracted_document = parse_router.parse(file_bytes=req.content, filename=req.filename)

    # 2. Create chunks
    chunking_strategy = req.chunk_strategy
    chunks = chunk_router[chunking_strategy].chunk(extracted_document)

    # 3. Create the embeddings for the chunks
    embeddings = embeddings_router.embed_chunks(chunks=chunks, model=req.embeddings_model,
                                                client=req.embeddings_client)
    
    
    # 4. Store vectors
    vector_storage_router.insert(index_name=req.index_name, document_id=req.document_id,
                                 chunks=chunks, embeddings=embeddings, metadata=req.metadata)
    return "done"



