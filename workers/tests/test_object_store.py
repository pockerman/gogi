"""
Run from the repository root: PYTHONPATH=. pytest workers/tests

The tests of reading run against an S3-compatible store and Azure Blob Storage, where they're given:
GOGI_TEST_S3_ENDPOINT=localhost:9000 GOGI_TEST_S3_ACCESS_KEY=admin GOGI_TEST_S3_SECRET_KEY=password
GOGI_TEST_AZURE_BLOB_URL=http://127.0.0.1:10000/devstoreaccount1 (Azurite)
"""
import io
import os
import uuid

import pytest

from workers.worker_utils.storage.objects.object_store import (
    AzureBlobObjectStore,
    S3ObjectStore,
    object_store_from_env,
)

# Azurite's own account, the same for everyone
AZURITE_KEY = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="


@pytest.fixture
def bucket():
    return f"gogi-test-{uuid.uuid4().hex[:8]}"


@pytest.mark.skipif(not os.getenv("GOGI_TEST_S3_ENDPOINT"), reason="GOGI_TEST_S3_ENDPOINT is not set")
def test_an_upload_is_read_from_s3(bucket):
    store = S3ObjectStore(
        endpoint=os.environ["GOGI_TEST_S3_ENDPOINT"],
        access_key=os.environ["GOGI_TEST_S3_ACCESS_KEY"],
        secret_key=os.environ["GOGI_TEST_S3_SECRET_KEY"],
        secure=False,
    )
    store._client.make_bucket(bucket)
    store._client.put_object(bucket, "maria/lift.md", io.BytesIO(b"lift and drag"), 13)

    assert store.read(bucket, "maria/lift.md") == b"lift and drag"


@pytest.mark.skipif(not os.getenv("GOGI_TEST_AZURE_BLOB_URL"), reason="GOGI_TEST_AZURE_BLOB_URL is not set")
def test_an_upload_is_read_from_azure_blob_storage(bucket):
    from azure.storage.blob import BlobServiceClient

    client = BlobServiceClient(
        os.environ["GOGI_TEST_AZURE_BLOB_URL"],
        credential={"account_name": "devstoreaccount1", "account_key": AZURITE_KEY},
    )
    client.create_container(bucket)
    client.get_blob_client(bucket, "maria/lift.md").upload_blob(b"lift and drag")

    assert AzureBlobObjectStore(client).read(bucket, "maria/lift.md") == b"lift and drag"


SETTINGS = ["GOGI_OBJECT_STORE", "GOGI_S3_ENDPOINT", "GOGI_S3_ACCESS_KEY", "GOGI_S3_SECRET_KEY", "GOGI_S3_SECURE",
            "GOGI_S3_REGION", "AWS_REGION", "MINIO_HOST", "MINIO_ROOT_USER", "MINIO_ROOT_PASSWORD",
            "AZURE_STORAGE_ACCOUNT_URL"]


@pytest.fixture
def env(monkeypatch):
    for name in SETTINGS:
        monkeypatch.delenv(name, raising=False)
    return monkeypatch


def test_development_reads_from_minio(env):
    # as the development compose file sets it: MinIO, over plain HTTP
    env.setenv("MINIO_HOST", "minio:9000")
    env.setenv("MINIO_ROOT_USER", "admin")
    env.setenv("MINIO_ROOT_PASSWORD", "password")

    store = object_store_from_env()

    assert isinstance(store, S3ObjectStore)
    assert store.options == {"endpoint": "minio:9000", "access_key": "admin", "secret_key": "password",
                             "secure": False, "region": None}


def test_aws_reads_from_s3_as_the_servers_role(env):
    env.setenv("GOGI_S3_ENDPOINT", "s3.eu-west-2.amazonaws.com")
    env.setenv("AWS_REGION", "eu-west-2")

    store = object_store_from_env()

    assert isinstance(store, S3ObjectStore)
    assert store.options == {"endpoint": "s3.eu-west-2.amazonaws.com", "access_key": None, "secret_key": None,
                             "secure": True, "region": "eu-west-2"}


def test_azure_reads_from_blob_storage(env):
    env.setenv("GOGI_OBJECT_STORE", "azure")
    with pytest.raises(ValueError):
        object_store_from_env()

    env.setenv("AZURE_STORAGE_ACCOUNT_URL", "https://gogi.blob.core.windows.net/")

    assert isinstance(object_store_from_env(), AzureBlobObjectStore)


def test_an_unknown_store_is_refused(env):
    env.setenv("GOGI_OBJECT_STORE", "gcs")

    with pytest.raises(ValueError):
        object_store_from_env()
