"""
Reads the documents' uploaded files from the object store: MinIO in development, S3 on AWS, Blob Storage on Azure.
The documents service stores each upload there and the ingestion workflow carries only the object's key, because
Temporal limits workflow inputs to a few MB.
"""
import os
from typing import Protocol

from minio import Minio
from minio.credentials import ChainedProvider, EnvAWSProvider, IamAwsProvider


class ObjectStore(Protocol):

    def read(self, bucket: str, object_key: str) -> bytes: ...


class S3ObjectStore:
    """S3, or an S3-compatible store such as MinIO. Without keys, it signs in with AWS' own variables, or else as the
    server's role, e.g. an EC2 instance's."""

    def __init__(self, endpoint: str, access_key: str | None = None, secret_key: str | None = None,
                 secure: bool = True, region: str | None = None):
        self.options = {"endpoint": endpoint, "access_key": access_key, "secret_key": secret_key,
                        "secure": secure, "region": region}
        credentials = None if access_key else ChainedProvider([EnvAWSProvider(), IamAwsProvider()])
        self._client = Minio(endpoint, access_key=access_key, secret_key=secret_key, secure=secure, region=region,
                             credentials=credentials)

    def read(self, bucket: str, object_key: str) -> bytes:
        response = self._client.get_object(bucket, object_key)
        try:
            return response.read()
        finally:
            response.close()
            response.release_conn()


class AzureBlobObjectStore:
    """Azure Blob Storage: a bucket is a container of the storage account, an object a blob in it."""

    def __init__(self, client):
        self._client = client

    @classmethod
    def from_defaults(cls, account_url: str) -> "AzureBlobObjectStore":
        """Signed in as Azure's tools are: on an Azure VM, its managed identity (AZURE_CLIENT_ID chooses one of
        several); elsewhere, e.g. AZURE_TENANT_ID, AZURE_CLIENT_ID and AZURE_CLIENT_SECRET."""
        from azure.identity import DefaultAzureCredential
        from azure.storage.blob import BlobServiceClient

        return cls(BlobServiceClient(account_url, credential=DefaultAzureCredential()))

    def read(self, bucket: str, object_key: str) -> bytes:
        return self._client.get_blob_client(bucket, object_key).download_blob().readall()


def object_store_from_env() -> ObjectStore:
    """The store of GOGI_OBJECT_STORE, "s3" (the default) or "azure", with the settings the Go services take
    (gogi/storage/objects). Without GOGI_S3_ENDPOINT, the development settings, MINIO_HOST, MINIO_ROOT_USER and
    MINIO_ROOT_PASSWORD, are MinIO's, over plain HTTP."""
    backend = os.getenv("GOGI_OBJECT_STORE") or "s3"
    if backend == "s3":
        endpoint = os.getenv("GOGI_S3_ENDPOINT")
        if not endpoint:
            return S3ObjectStore(endpoint=os.getenv("MINIO_HOST") or "minio:9000",
                                 access_key=os.getenv("MINIO_ROOT_USER") or "admin",
                                 secret_key=os.getenv("MINIO_ROOT_PASSWORD") or "password",
                                 secure=False)
        return S3ObjectStore(endpoint=endpoint,
                             access_key=os.getenv("GOGI_S3_ACCESS_KEY") or None,
                             secret_key=os.getenv("GOGI_S3_SECRET_KEY") or None,
                             # anything but a clear false keeps TLS
                             secure=(os.getenv("GOGI_S3_SECURE") or "true").lower() not in ("false", "0", "no"),
                             region=os.getenv("GOGI_S3_REGION") or os.getenv("AWS_REGION") or None)
    if backend == "azure":
        account_url = os.getenv("AZURE_STORAGE_ACCOUNT_URL")
        if not account_url:
            raise ValueError('GOGI_OBJECT_STORE is "azure", but AZURE_STORAGE_ACCOUNT_URL is not set')
        return AzureBlobObjectStore.from_defaults(account_url)
    raise ValueError(f'unknown GOGI_OBJECT_STORE "{backend}", expected "s3" or "azure"')
