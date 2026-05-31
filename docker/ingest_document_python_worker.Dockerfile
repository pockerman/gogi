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

# Copy worker requirements first for better Docker layer caching
COPY workers/ingest_document/requirements.txt .

# Install Python dependencies
RUN pip install --no-cache-dir -r requirements.txt

# Copy worker source code
COPY workers ./workers

# Make worker utilities importable
ENV PYTHONPATH=/app

# Run the Temporal ingestion worker
CMD ["python", "workers/ingest_document/ingestion_pipeline_worker.py"]