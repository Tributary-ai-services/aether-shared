# DeepLake Embedding Models

**Service**: DeepLake API
**Component**: Embedding Service
**API Version**: v1
**Last Updated**: 2026-01-06

## Overview

The DeepLake API supports multiple embedding model providers for converting text into vector representations. The embedding service follows a provider pattern allowing flexible integration with OpenAI, Sentence Transformers, and other embedding models.

## Supported Providers

### OpenAI Embeddings (Default)

**Provider Class**: `OpenAIEmbeddingProvider`
**Authentication**: API Key required
**Default Model**: `text-embedding-3-small`

**Configuration**:
```python
from app.services.embedding_service import OpenAIEmbeddingProvider

provider = OpenAIEmbeddingProvider(
    api_key="sk-...",  # or set OPENAI_API_KEY env var
    model="text-embedding-3-small"
)
```

**Environment Variables**:
```bash
OPENAI_API_KEY=sk-...
EMBEDDING_OPENAI_MODEL=text-embedding-3-small
```

#### Available OpenAI Models

| Model | Dimensions | Cost (per 1M tokens) | Max Input | Performance | Use Case |
|-------|------------|---------------------|-----------|-------------|----------|
| `text-embedding-3-small` | 1536 | $0.02 | 8191 tokens | Fast | Cost-optimized, general purpose |
| `text-embedding-3-large` | 3072 | $0.13 | 8191 tokens | Best | High accuracy requirements |
| `text-embedding-ada-002` | 1536 | $0.10 | 8191 tokens | Good | Legacy model, stable |

**Model Selection Guidelines**:

- **text-embedding-3-small**: Best default choice (87.5% cost savings vs ada-002)
  - Use for: General document embeddings, Q&A systems, standard similarity search
  - Dimensions: 1536 (compatible with existing ada-002 datasets)

- **text-embedding-3-large**: Maximum accuracy
  - Use for: Critical search applications, research, high-precision matching
  - Dimensions: 3072 (requires new dataset configuration)

- **text-embedding-ada-002**: Legacy compatibility
  - Use for: Existing applications, proven stability, no migration needed
  - Dimensions: 1536 (same as 3-small)

#### OpenAI API Integration

**Single Text Embedding**:
```python
async def embed_text(self, text: str) -> List[float]:
    import openai

    client = openai.AsyncOpenAI(api_key=self.api_key)

    response = await client.embeddings.create(
        model=self.model,
        input=text
    )

    return response.data[0].embedding  # Returns 1536-dim or 3072-dim vector
```

**Batch Embedding** (Recommended for multiple texts):
```python
async def embed_texts(self, texts: List[str]) -> List[List[float]]:
    import openai

    client = openai.AsyncOpenAI(api_key=self.api_key)

    response = await client.embeddings.create(
        model=self.model,
        input=texts  # OpenAI supports up to 2048 texts per request
    )

    return [item.embedding for item in response.data]
```

**Performance Characteristics**:
- **Latency**: 100-300ms for single text, 200-500ms for batch (10-100 texts)
- **Throughput**: ~10,000 tokens/sec per API key
- **Rate Limits**:
  - Free tier: 3 RPM, 150,000 TPM
  - Pay-as-you-go: 3,000 RPM, 1,000,000 TPM
  - Tier 4+: 5,000 RPM, 5,000,000 TPM

**Error Handling**:
```python
try:
    embeddings = await provider.embed_texts(texts)
except openai.RateLimitError as e:
    # Implement exponential backoff
    await asyncio.sleep(2 ** retry_count)
except openai.APIError as e:
    # Handle API errors
    logger.error("OpenAI API error", error=str(e))
except Exception as e:
    # Handle other errors
    logger.error("Embedding failed", error=str(e))
```

### Sentence Transformers (Local)

**Provider Class**: `SentenceTransformersProvider`
**Authentication**: None (local models)
**Default Model**: `all-MiniLM-L6-v2`

**Configuration**:
```python
from app.services.embedding_service import SentenceTransformersProvider

provider = SentenceTransformersProvider(
    model_name="all-MiniLM-L6-v2"
)
```

**Environment Variables**:
```bash
EMBEDDING_SENTENCE_TRANSFORMERS_MODEL=all-MiniLM-L6-v2
```

#### Available Sentence Transformer Models

| Model | Dimensions | Size (MB) | Speed | Performance | Use Case |
|-------|------------|-----------|-------|-------------|----------|
| `all-MiniLM-L6-v2` | 384 | 80 | Fast | Good | Default, offline, resource-constrained |
| `all-mpnet-base-v2` | 768 | 420 | Medium | Better | Balanced accuracy/speed |
| `all-MiniLM-L12-v2` | 384 | 120 | Medium | Good+ | Better than L6, still fast |
| `paraphrase-multilingual-MiniLM-L12-v2` | 384 | 420 | Medium | Good | 50+ languages |
| `multi-qa-MiniLM-L6-cos-v1` | 384 | 80 | Fast | Good | Question answering |
| `msmarco-distilbert-base-v4` | 768 | 250 | Medium | Better | Information retrieval |

**Model Selection Guidelines**:

- **all-MiniLM-L6-v2**: Best default for offline/local deployment
  - Pros: Fast, small, good quality
  - Cons: Lower accuracy than larger models

- **all-mpnet-base-v2**: Best quality for local models
  - Pros: Higher accuracy, still reasonable speed
  - Cons: 5x larger model size

- **paraphrase-multilingual-MiniLM-L12-v2**: Multilingual support
  - Pros: Supports 50+ languages
  - Cons: Lower accuracy per language than specialized models

#### Sentence Transformers API Integration

**Model Loading** (lazy-loaded on first use):
```python
async def _load_model(self):
    from sentence_transformers import SentenceTransformer

    loop = asyncio.get_event_loop()
    self._model = await loop.run_in_executor(
        None, SentenceTransformer, self.model_name
    )

    # Get dimensions by encoding test string
    test_embedding = await loop.run_in_executor(
        None, self._model.encode, "test"
    )
    self._dimensions = len(test_embedding)
```

**Embedding Generation**:
```python
async def embed_text(self, text: str) -> List[float]:
    await self._load_model()

    loop = asyncio.get_event_loop()
    embedding = await loop.run_in_executor(None, self._model.encode, text)
    return embedding.tolist()
```

**Batch Embedding**:
```python
async def embed_texts(self, texts: List[str]) -> List[List[float]]:
    await self._load_model()

    loop = asyncio.get_event_loop()
    embeddings = await loop.run_in_executor(None, self._model.encode, texts)
    return [emb.tolist() for emb in embeddings]
```

**Performance Characteristics**:
- **Latency**: 10-50ms for single text (after model load), 20-200ms for batch (10-100 texts)
- **Throughput**: ~1,000-5,000 texts/sec (depends on hardware)
- **Startup Time**: 1-5 seconds for model loading
- **Memory**: 100MB-500MB depending on model

**Hardware Recommendations**:
- **CPU**: 4+ cores for production workloads
- **RAM**: 2GB+ available (model + batch processing)
- **GPU**: Optional, 2-5x speedup with CUDA-enabled GPUs

## Embedding Service Architecture

### Provider Pattern

```python
class EmbeddingProvider(ABC):
    """Abstract base class for embedding providers."""

    @abstractmethod
    async def embed_text(self, text: str) -> List[float]:
        """Convert text to embedding vector."""
        pass

    @abstractmethod
    async def embed_texts(self, texts: List[str]) -> List[List[float]]:
        """Convert multiple texts to embedding vectors."""
        pass

    @abstractmethod
    def get_dimensions(self) -> int:
        """Get the dimensions of the embeddings produced."""
        pass
```

### Service Initialization

```python
class EmbeddingService:
    """Service for text-to-vector embedding conversion."""

    def __init__(self, provider: Optional[EmbeddingProvider] = None):
        self.provider = provider or self._create_default_provider()

    def _create_default_provider(self) -> EmbeddingProvider:
        # Priority: OpenAI (if API key available) → Sentence Transformers
        openai_key = os.getenv("OPENAI_API_KEY")
        if openai_key:
            return OpenAIEmbeddingProvider(api_key=openai_key)
        else:
            return SentenceTransformersProvider()
```

### Fallback Strategy

The embedding service implements an automatic fallback:

```
1. Check for OPENAI_API_KEY environment variable
   ├─ Found → Use OpenAIEmbeddingProvider
   └─ Not found → Fall back to SentenceTransformersProvider

2. Initialize chosen provider
   ├─ Success → Use provider
   └─ Failure → Raise RuntimeError (no embedding provider available)
```

## Model Comparison

### Accuracy Comparison (MTEB Benchmark)

| Model | Avg Score | Classification | Clustering | Reranking | Retrieval | STS | Summarization |
|-------|-----------|----------------|------------|-----------|-----------|-----|---------------|
| text-embedding-3-large | 64.6 | 75.3 | 49.0 | 60.4 | 54.0 | 81.4 | 30.2 |
| text-embedding-3-small | 62.3 | 73.0 | 47.2 | 59.6 | 53.0 | 80.1 | 29.3 |
| text-embedding-ada-002 | 61.0 | 70.9 | 45.9 | 59.0 | 49.2 | 80.9 | 30.8 |
| all-mpnet-base-v2 | 57.8 | 68.2 | 42.3 | 57.0 | 43.8 | 78.0 | 29.4 |
| all-MiniLM-L6-v2 | 56.3 | 66.8 | 41.8 | 55.3 | 41.9 | 76.5 | 28.9 |

### Cost-Performance Trade-offs

**Scenario 1: High-Volume General Purpose** (10M tokens/month)
- **Recommended**: text-embedding-3-small
- **Cost**: $200/month
- **Quality**: Excellent (62.3 MTEB)
- **Latency**: 200-300ms

**Scenario 2: Low-Volume High-Accuracy** (1M tokens/month)
- **Recommended**: text-embedding-3-large
- **Cost**: $130/month
- **Quality**: Best (64.6 MTEB)
- **Latency**: 200-400ms

**Scenario 3: Offline/Air-Gapped** (unlimited)
- **Recommended**: all-mpnet-base-v2 (local)
- **Cost**: $0 (hardware only)
- **Quality**: Good (57.8 MTEB)
- **Latency**: 20-100ms

**Scenario 4: Cost-Optimized Offline** (unlimited)
- **Recommended**: all-MiniLM-L6-v2 (local)
- **Cost**: $0 (hardware only)
- **Quality**: Decent (56.3 MTEB)
- **Latency**: 10-50ms

## Integration with DeepLake

### Dataset Creation with Specific Model

```python
from app.services.embedding_service import EmbeddingService, OpenAIEmbeddingProvider
from app.services.deeplake_service import DeepLakeService
from app.models.schemas import DatasetCreate

# Initialize embedding provider
embedding_provider = OpenAIEmbeddingProvider(model="text-embedding-3-large")
embedding_service = EmbeddingService(provider=embedding_provider)

# Get model dimensions
dimensions = embedding_provider.get_dimensions()  # 3072 for 3-large

# Create dataset with matching dimensions
dataset_create = DatasetCreate(
    name="high-accuracy-embeddings",
    dimensions=dimensions,
    metric_type="cosine",
    index_type="hnsw",
    metadata={"model": "text-embedding-3-large"}
)

deeplake_service = DeepLakeService()
dataset = await deeplake_service.create_dataset(dataset_create, tenant_id)
```

### Embedding and Inserting Vectors

```python
from app.models.schemas import VectorCreate

# Generate embeddings
texts = [
    "First document chunk",
    "Second document chunk",
    "Third document chunk"
]

embeddings = await embedding_service.provider.embed_texts(texts)

# Create vector objects
vectors = [
    VectorCreate(
        document_id=doc_id,
        chunk_id=chunk_ids[i],
        values=embeddings[i],
        content=texts[i],
        chunk_index=i,
        chunk_count=len(texts),
        model="text-embedding-3-large"
    )
    for i in range(len(texts))
]

# Batch insert
result = await deeplake_service.insert_vectors_batch(
    dataset_name="high-accuracy-embeddings",
    vectors=vectors,
    tenant_id=tenant_id
)
```

## Model Migration Strategies

### Upgrading from ada-002 to embedding-3-small

**Strategy**: Side-by-side migration (zero downtime)

```python
# Step 1: Create new dataset with 3-small
new_dataset = await deeplake_service.create_dataset(
    DatasetCreate(
        name="documents-3-small",
        dimensions=1536,  # Same as ada-002
        metric_type="cosine",
        metadata={"model": "text-embedding-3-small"}
    ),
    tenant_id
)

# Step 2: Re-embed all documents
old_vectors = await deeplake_service.list_vectors("documents-ada-002", tenant_id)

for batch in chunk_list(old_vectors, batch_size=100):
    texts = [v.content for v in batch]
    new_embeddings = await embedding_provider.embed_texts(texts)

    new_vectors = [
        VectorCreate(
            document_id=old_vectors[i].document_id,
            chunk_id=old_vectors[i].chunk_id,
            values=new_embeddings[i],
            content=texts[i],
            metadata=old_vectors[i].metadata,
            chunk_index=old_vectors[i].chunk_index,
            chunk_count=old_vectors[i].chunk_count,
            model="text-embedding-3-small"
        )
        for i in range(len(batch))
    ]

    await deeplake_service.insert_vectors_batch(
        "documents-3-small", new_vectors, tenant_id
    )

# Step 3: Update application to use new dataset

# Step 4: Verify quality with A/B testing

# Step 5: Delete old dataset after validation period
await deeplake_service.delete_dataset("documents-ada-002", tenant_id)
```

### Upgrading to 3-large (Higher Dimensions)

**Strategy**: Requires new dataset (dimensions change)

```python
# Step 1: Create new dataset with 3-large (3072 dimensions)
new_dataset = await deeplake_service.create_dataset(
    DatasetCreate(
        name="documents-3-large",
        dimensions=3072,  # Different from ada-002/3-small
        metric_type="cosine",
        metadata={"model": "text-embedding-3-large"}
    ),
    tenant_id
)

# Step 2: Re-embed (same as above but with 3-large model)
# Step 3-5: Same as above
```

## Embedding Quality Optimization

### Text Preprocessing

**Best Practices**:
```python
def preprocess_text_for_embedding(text: str) -> str:
    """Preprocess text for optimal embedding quality."""

    # Remove excessive whitespace
    text = " ".join(text.split())

    # Normalize unicode characters
    text = unicodedata.normalize("NFKC", text)

    # Truncate to model limits (8191 tokens for OpenAI)
    text = truncate_to_token_limit(text, max_tokens=8000)

    # Optionally lowercase (not recommended for OpenAI models)
    # text = text.lower()  # Skip for OpenAI, they handle casing

    return text
```

### Chunk Size Optimization

**Recommended Chunk Sizes by Model**:

| Model | Optimal Chunk Size | Max Tokens | Reasoning |
|-------|-------------------|------------|-----------|
| OpenAI 3-small/large | 500-1000 chars | 8191 | Balance context and granularity |
| OpenAI ada-002 | 500-1000 chars | 8191 | Same as 3-small |
| Sentence Transformers | 200-500 chars | 256-512 | Smaller context windows |

**Chunking Strategy**:
```python
def chunk_text_for_embedding(text: str, model_type: str) -> List[str]:
    """Chunk text optimally for embedding model."""

    if model_type.startswith("text-embedding"):  # OpenAI
        chunk_size = 1000  # characters
        overlap = 200       # character overlap
    else:  # Sentence Transformers
        chunk_size = 400
        overlap = 100

    chunks = []
    start = 0
    while start < len(text):
        end = start + chunk_size
        chunks.append(text[start:end])
        start = end - overlap  # Overlap to preserve context

    return chunks
```

## Monitoring and Metrics

### Embedding Performance Metrics

**Track These Metrics**:
- **Embedding Latency**: Time to generate embeddings
- **Batch Size**: Number of texts per embedding request
- **Token Usage**: OpenAI API token consumption
- **Error Rate**: Failed embedding requests
- **Model Version**: Track which models are in use

**Prometheus Metrics**:
```python
# app/services/metrics_service.py

from prometheus_client import Histogram, Counter, Gauge

embedding_latency = Histogram(
    'embedding_generation_seconds',
    'Time to generate embeddings',
    ['provider', 'model', 'batch_size']
)

embedding_tokens = Counter(
    'embedding_tokens_total',
    'Total tokens processed for embeddings',
    ['provider', 'model']
)

embedding_errors = Counter(
    'embedding_errors_total',
    'Total embedding errors',
    ['provider', 'model', 'error_type']
)
```

## Configuration Reference

### Environment Variables

```bash
# OpenAI Provider
OPENAI_API_KEY=sk-...
EMBEDDING_OPENAI_MODEL=text-embedding-3-small

# Sentence Transformers Provider
EMBEDDING_SENTENCE_TRANSFORMERS_MODEL=all-MiniLM-L6-v2

# Embedding Service
EMBEDDING_PROVIDER=openai  # or sentence-transformers
EMBEDDING_BATCH_SIZE=100
EMBEDDING_MAX_RETRIES=3
EMBEDDING_TIMEOUT_SECONDS=30
```

### Configuration File (.env)

```env
# Embedding Configuration
EMBEDDING_PROVIDER=openai
EMBEDDING_OPENAI_MODEL=text-embedding-3-small
EMBEDDING_SENTENCE_TRANSFORMERS_MODEL=all-MiniLM-L6-v2

# OpenAI API
OPENAI_API_KEY=sk-proj-...
OPENAI_ORG_ID=org-...

# Performance
EMBEDDING_BATCH_SIZE=100
EMBEDDING_MAX_CONCURRENT=10
EMBEDDING_TIMEOUT_SECONDS=30

# Retry Configuration
EMBEDDING_MAX_RETRIES=3
EMBEDDING_RETRY_BACKOFF_MULTIPLIER=2
EMBEDDING_RETRY_MAX_WAIT_SECONDS=60
```

## See Also

- [Vector Structure](./vector-structure.md) - Vector schema and storage format
- [Dataset Organization](./dataset-organization.md) - Multi-tenant dataset structure
- [Query API](./query-api.md) - Search and retrieval operations
- [AudiModal Processing](../audimodal/entities/processing-session.md) - Text extraction and chunking
- [Document Upload Flow](../cross-service/flows/document-upload.md) - End-to-end embedding generation
