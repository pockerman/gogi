"""
Embeddings made by gogi's model service. The workers run no embedding model: the service calls each model, whether
gogi runs it, it's OpenAI's or an application hosts it, with the keys of the application whose document it is.
"""
from typing import List, Sequence

import grpc


class EmbeddingRefused(Exception):
    """The model service won't embed the texts, e.g. the model isn't one the application can use or its key was
    refused; asking again won't change that."""


# what the service answers when asking again won't help
REFUSED = {grpc.StatusCode.NOT_FOUND, grpc.StatusCode.FAILED_PRECONDITION, grpc.StatusCode.PERMISSION_DENIED,
           grpc.StatusCode.INVALID_ARGUMENT, grpc.StatusCode.UNAUTHENTICATED}

# the header that says which application a call is for
APPLICATION_HEADER = "x-gogi-application"


class ModelServiceEmbeddings:

    def __init__(self, address: str, timeout_seconds: float = 300.0):
        self._address = address
        self._timeout = timeout_seconds
        self._stub = None

    @property
    def stub(self):
        # made when first used, so the activities that embed nothing need no connection
        if self._stub is None:
            from gogi.v1 import llm_model_service_pb2_grpc
            self._stub = llm_model_service_pb2_grpc.LLMModelServerStub(grpc.insecure_channel(self._address))
        return self._stub

    def embed(self, application: str, model: str, texts: Sequence[str]) -> List[List[float]]:
        """the texts' vectors, in their order"""
        from gogi.v1 import llm_model_service_pb2

        try:
            response = self.stub.Embed(llm_model_service_pb2.EmbedRequest(model=model, inputs=list(texts)),
                                       metadata=[(APPLICATION_HEADER, application)], timeout=self._timeout)
        except grpc.RpcError as e:
            if e.code() in REFUSED:
                raise EmbeddingRefused(f"{model}: {e.details()}") from e
            raise
        return [list(embedding.values) for embedding in response.embeddings]
