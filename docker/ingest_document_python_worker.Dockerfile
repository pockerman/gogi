# syntax=docker/dockerfile:1.7
# docker/ingest_document_python_worker.Dockerfile

FROM python:3.12-slim

# Prevent Python from buffering stdout/stderr
ENV PYTHONUNBUFFERED=1

# Set working directory
WORKDIR /app

# Install system dependencies if needed later
RUN apt-get update && apt-get install -y \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# Copy the worker_utils requirements
COPY workers/worker_utils/requirements.txt .

RUN --mount=type=cache,target=/root/.cache/pip \
    pip install -r requirements.txt

# Copy worker requirements first for better Docker layer caching
COPY workers/ingest_document/requirements.txt .

# Install Python dependencies
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install -r requirements.txt

# The worker calls the model service to embed text: generate its client from the protos, with the protobuf the
# worker runs with
COPY third_party/protos/gogi /protos
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install grpcio-tools \
    && mkdir -p /app/generated \
    && python -m grpc_tools.protoc -I /protos --python_out=/app/generated --grpc_python_out=/app/generated \
        $(find /protos/gogi/v1 -name '*.proto')

# Copy worker source code
COPY workers ./workers

# Make worker utilities, and the generated client, importable
ENV PYTHONPATH=/app:/app/generated

# Run the Temporal ingestion worker
CMD ["python", "workers/ingest_document/ingestion_pipeline_worker.py"]