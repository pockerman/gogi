# syntax=docker/dockerfile:1.7
# docker/ingest_document_lambda.Dockerfile
#
# The ingestion worker as an AWS Lambda that SQS triggers (GOGI_INGESTION_QUEUE=sqs); see
# workers/ingest_document/aws_lambda.py. Build it for the Lambda's architecture and push it to the ECR repository
# deploy/aws makes:
#
#   docker build --platform linux/amd64 --provenance=false -f docker/ingest_document_lambda.Dockerfile -t <repository>:<tag> .

FROM public.ecr.aws/lambda/python:3.12

COPY workers/worker_utils/requirements.txt worker_utils-requirements.txt
COPY workers/ingest_document/requirements.txt ingest_document-requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install -r worker_utils-requirements.txt -r ingest_document-requirements.txt

# the model service's client, generated with the protobuf the worker runs with; grpcio-tools is only needed to
# generate it
COPY third_party/protos/gogi /protos
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install grpcio-tools \
    && python -m grpc_tools.protoc -I /protos --python_out=${LAMBDA_TASK_ROOT} --grpc_python_out=${LAMBDA_TASK_ROOT} \
        /protos/gogi/v1/*.proto /protos/gogi/v1/*/*.proto \
    && pip uninstall -y grpcio-tools \
    && rm -rf /protos

COPY workers/ingest_document ${LAMBDA_TASK_ROOT}/workers/ingest_document
COPY workers/worker_utils ${LAMBDA_TASK_ROOT}/workers/worker_utils

CMD ["workers.ingest_document.aws_lambda.handler"]
