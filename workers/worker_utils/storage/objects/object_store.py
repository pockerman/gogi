"""
Reads the documents' uploaded files from the object store (MinIO). The documents service
stores each upload there and the ingestion workflow carries only the object's key, because
Temporal limits workflow inputs to a few MB.
"""
from minio import Minio


class ObjectStore:

    def __init__(self, host: str, user: str, password: str, secure: bool = False):
        self._client = Minio(host, access_key=user, secret_key=password, secure=secure)

    def read(self, bucket: str, object_key: str) -> bytes:
        response = self._client.get_object(bucket, object_key)
        try:
            return response.read()
        finally:
            response.close()
            response.release_conn()
