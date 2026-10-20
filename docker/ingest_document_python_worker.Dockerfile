# syntax=docker/dockerfile:1.7
# docker/ingest_document_python_worker.Dockerfile
#
# The ingestion worker as a process polling gogi's ingestion queue table in Postgres (GOGI_INGESTION_QUEUE=postgres),
# e.g. in development. In the cloud the same code runs as a function: docker/ingest_document_lambda.Dockerfile on AWS,
# workers/azure_function on Azure.

FROM python:3.12-slim

# Prevent Python from buffering stdout/stderr
ENV PYTHONUNBUFFERED=1

WORKDIR /app

COPY workers/worker_utils/requirements.txt worker_utils-requirements.txt
COPY workers/ingest_document/requirements.txt ingest_document-requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install -r worker_utils-requirements.txt -r ingest_document-requirements.txt

# The worker calls the model service to embed text: generate its client from the protos, with the protobuf the
# worker runs with
COPY third_party/protos/gogi /protos
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install grpcio-tools \
    && mkdir -p /app/generated \
    && python -m grpc_tools.protoc -I /protos --python_out=/app/generated --grpc_python_out=/app/generated \
        $(find /protos/gogi/v1 -name '*.proto')

COPY workers ./workers

# Make the worker's code, and the generated client, importable
ENV PYTHONPATH=/app:/app/generated

CMD ["python", "workers/ingest_document/local_worker.py"]
