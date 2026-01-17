# DeepLake Vector Structure

**Service**: DeepLake API
**Storage**: Deep Lake 4.0 Vector Database
**API Version**: v1
**Last Updated**: 2026-01-06

## Overview

The DeepLake API uses Deep Lake 4.0 as the underlying vector database for storing document embeddings. Each vector represents a chunk of text extracted from documents, enabling semantic search and AI-powered querying across the Aether platform.

## Schema Definition

### Deep Lake 4.0 Schema

The vector database uses a comprehensive schema defined at dataset creation time:

```python
schema = {
    'id': deeplake.types.Text(),                      # Unique vector identifier (UUID)
    'document_id': deeplake.types.Text(),             # Parent document ID from Aether
    'embedding': deeplake.types.Array(
        deeplake.types.Float32(),
        shape=[dimensions]                             # Typically 1536 for OpenAI ada-002
    ),
    'content': deeplake.types.Text(),                 # Original text content
    'chunk_count': deeplake.types.Int32(),            # Total chunks in document
    'metadata': deeplake.types.Text(),                # JSON string for flexible metadata
    'chunk_id': deeplake.types.Text(),                # AudiModal chunk identifier
    'content_hash': deeplake.types.Text(),            # SHA-256 hash of content
    'content_type': deeplake.types.Text(),            # MIME type (text/plain, etc.)
    'language': deeplake.types.Text(),                # ISO language code (en, es, fr)
    'chunk_index': deeplake.types.Int32(),            # Position in document (0-based)
    'model': deeplake.types.Text(),                   # Embedding model identifier
    'created_at': deeplake.types.Text(),              # ISO 8601 timestamp
    'updated_at': deeplake.types.Text()               # ISO 8601 timestamp
}
```

## Vector Fields

### Core Identification Fields

#### `id` (Text, Required)
- **Type**: UUID v4 string
- **Generated**: By DeepLake API on insertion
- **Example**: `"a3f2c8d1-4b7e-9f1a-2c5d-8e9f1a2b3c4d"`
- **Purpose**: Unique identifier for the vector within the dataset
- **Indexed**: Yes (primary key)

#### `document_id` (Text, Required)
- **Type**: UUID v4 string
- **Generated**: By Aether Backend
- **Example**: `"7f8e9d1c-2b3a-4c5d-6e7f-8a9b0c1d2e3f"`
- **Purpose**: References the parent Document node in Neo4j
- **Cross-Service**: Maps to `Document.id` in Aether Backend
- **Indexed**: Yes (for filtering by document)

#### `chunk_id` (Text, Optional)
- **Type**: UUID v4 string
- **Generated**: By AudiModal during text chunking
- **Example**: `"c5d6e7f8-9a0b-1c2d-3e4f-5a6b7c8d9e0f"`
- **Purpose**: References the Chunk entity in AudiModal PostgreSQL
- **Cross-Service**: Maps to `Chunk.id` in AudiModal
- **Indexed**: Yes (for chunk-level operations)

### Embedding Field

#### `embedding` (Array[Float32], Required)
- **Type**: Fixed-size array of 32-bit floats
- **Dimensions**: Configurable per dataset (typically 1536 for OpenAI)
- **Range**: Each value typically in [-1.0, 1.0] range (normalized)
- **Example**: `[0.0123, -0.0456, 0.0789, ..., -0.0234]` (1536 values)
- **Purpose**: Vector representation of text chunk for semantic search
- **Generation**:
  - Created by OpenAI `text-embedding-ada-002` model (default)
  - Alternative models: `text-embedding-3-small`, `text-embedding-3-large`
- **Indexed**: Yes (vector index for similarity search)
- **Storage Size**: 1536 dimensions × 4 bytes = 6,144 bytes per vector

**Embedding Model Configurations**:

| Model | Dimensions | Cost per 1M tokens | Performance | Use Case |
|-------|------------|-------------------|-------------|----------|
| ada-002 | 1536 | $0.10 | Excellent | Default choice, best balance |
| 3-small | 512 / 1536 | $0.02 | Good | Cost-optimized scenarios |
| 3-large | 256-3072 | $0.13 | Best | High-accuracy requirements |

### Content Fields

#### `content` (Text, Optional)
- **Type**: UTF-8 text string
- **Max Length**: Typically 1000-2000 characters (configurable)
- **Example**: `"The quick brown fox jumps over the lazy dog. This is a sample text chunk from a larger document about animals."`
- **Purpose**: Original text that was embedded
- **Storage**: Full text stored for result display and re-ranking
- **Searchable**: Yes (for text-based and hybrid search)

#### `content_hash` (Text, Optional)
- **Type**: SHA-256 hex digest
- **Format**: 64 hexadecimal characters
- **Example**: `"2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"`
- **Purpose**: Deduplication and integrity verification
- **Calculation**: `hashlib.sha256(content.encode('utf-8')).hexdigest()`
- **Use Cases**:
  - Detect duplicate content across documents
  - Verify content hasn't been corrupted
  - Skip re-embedding identical chunks

#### `content_type` (Text, Optional)
- **Type**: MIME type string
- **Default**: `"text/plain"`
- **Examples**:
  - `"text/plain"` - Plain text
  - `"text/markdown"` - Markdown formatted text
  - `"text/html"` - HTML content
  - `"application/json"` - JSON data
- **Purpose**: Indicates content format for proper rendering

#### `language` (Text, Optional)
- **Type**: ISO 639-1 language code
- **Default**: `"en"`
- **Examples**: `"en"`, `"es"`, `"fr"`, `"de"`, `"zh"`, `"ja"`
- **Purpose**: Language-specific search and filtering
- **Detection**: Automatic via `langdetect` library in AudiModal

### Chunking Context Fields

#### `chunk_index` (Int32, Optional)
- **Type**: 32-bit integer
- **Range**: 0 to N-1 (zero-based indexing)
- **Example**: `15` (16th chunk in the document)
- **Purpose**: Preserves sequential order of chunks within document
- **Use Cases**:
  - Reconstruct document order
  - Display surrounding context
  - Pagination through document chunks

#### `chunk_count` (Int32, Optional)
- **Type**: 32-bit integer
- **Range**: 1 to 10,000+ (depends on document size)
- **Example**: `42` (document has 42 total chunks)
- **Purpose**: Indicates total number of chunks in the document
- **Calculation**: Set by AudiModal after chunking completes
- **Use Cases**:
  - Calculate document coverage percentage
  - Estimate document length
  - Pagination controls

### Model Tracking Field

#### `model` (Text, Optional)
- **Type**: Model identifier string
- **Default**: `"text-embedding-ada-002"`
- **Examples**:
  - `"text-embedding-ada-002"` - OpenAI Ada v2
  - `"text-embedding-3-small"` - OpenAI Embedding v3 Small
  - `"text-embedding-3-large"` - OpenAI Embedding v3 Large
  - `"all-MiniLM-L6-v2"` - Sentence Transformers model
  - `"instructor-xl"` - Instructor embeddings
- **Purpose**: Track which model generated the embedding
- **Use Cases**:
  - Model version migration
  - A/B testing different models
  - Ensure consistent search across same-model vectors

### Metadata Field

#### `metadata` (Text, Optional)
- **Type**: JSON string (stringified JSON object)
- **Format**: Any valid JSON object
- **Example**:
```json
{
  "tenant_id": "tenant_1756217701",
  "space_id": "space_abc123",
  "notebook_id": "notebook_xyz789",
  "author": "john@example.com",
  "tags": ["machine-learning", "nlp", "embeddings"],
  "classification": "public",
  "page_number": 5,
  "paragraph_index": 3,
  "custom_field": "custom_value"
}
```
- **Purpose**: Flexible storage for application-specific metadata
- **Queryable**: Yes, using Deep Lake's metadata filtering
- **Size Limit**: Recommended < 10KB per vector
- **Common Fields**:
  - `tenant_id`: Multi-tenancy isolation
  - `space_id`: Workspace identifier
  - `notebook_id`: Parent notebook
  - `tags`: Categorization tags
  - `classification`: Security classification
  - `page_number`: PDF page reference
  - `section`: Document section name

**Metadata Filtering Examples**:

```python
# Filter by tenant_id
filters = {"tenant_id": "tenant_1756217701"}

# Filter by tags (array contains)
filters = {"tags": {"$in": ["machine-learning"]}}

# Complex filter (AND/OR conditions)
filters = {
    "$and": [
        {"tenant_id": "tenant_1756217701"},
        {"$or": [
            {"classification": "public"},
            {"author": "john@example.com"}
        ]}
    ]
}
```

### Timestamp Fields

#### `created_at` (Text, Required)
- **Type**: ISO 8601 formatted datetime string
- **Format**: `YYYY-MM-DDTHH:MM:SS.ffffffZ`
- **Timezone**: Always UTC
- **Example**: `"2026-01-06T14:30:45.123456Z"`
- **Generated**: By DeepLake API on vector insertion
- **Purpose**: Track when vector was created
- **Indexed**: Yes (for time-range queries)

#### `updated_at` (Text, Required)
- **Type**: ISO 8601 formatted datetime string
- **Format**: Same as `created_at`
- **Example**: `"2026-01-06T15:45:30.654321Z"`
- **Updated**: On any vector modification (content, metadata, embedding)
- **Purpose**: Track last modification time
- **Use Cases**:
  - Incremental updates
  - Change detection
  - Cache invalidation

## Pydantic Models

### VectorCreate (Request)

```python
class VectorCreate(BaseModel):
    """Vector creation request model."""

    id: Optional[str] = None                          # Auto-generated if not provided
    document_id: str                                  # Required
    chunk_id: Optional[str] = None
    values: List[float]                               # Required, embedding vector
    content: Optional[str] = None
    content_hash: Optional[str] = None
    metadata: Optional[Dict[str, Any]] = None
    content_type: Optional[str] = None
    language: Optional[str] = None
    chunk_index: Optional[int] = None
    chunk_count: Optional[int] = None
    model: Optional[str] = None
```

**Example Request**:

```json
{
  "document_id": "7f8e9d1c-2b3a-4c5d-6e7f-8a9b0c1d2e3f",
  "chunk_id": "c5d6e7f8-9a0b-1c2d-3e4f-5a6b7c8d9e0f",
  "values": [0.0123, -0.0456, 0.0789, ...],
  "content": "This is the chunk text content.",
  "content_hash": "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
  "metadata": {
    "tenant_id": "tenant_1756217701",
    "space_id": "space_abc123",
    "notebook_id": "notebook_xyz789",
    "tags": ["example", "documentation"]
  },
  "content_type": "text/plain",
  "language": "en",
  "chunk_index": 0,
  "chunk_count": 5,
  "model": "text-embedding-ada-002"
}
```

### VectorResponse (Response)

```python
class VectorResponse(BaseModel):
    """Vector response model."""

    id: str                                           # UUID generated by DeepLake
    dataset_id: str                                   # Dataset name/ID
    document_id: str
    chunk_id: Optional[str] = None
    values: List[float]                               # Embedding vector
    content: Optional[str] = None
    content_hash: Optional[str] = None
    metadata: Dict[str, Any]                          # Parsed from JSON string
    content_type: Optional[str] = None
    language: Optional[str] = None
    chunk_index: Optional[int] = None
    chunk_count: Optional[int] = None
    model: Optional[str] = None
    dimensions: int                                   # Calculated from values length
    created_at: datetime
    updated_at: datetime
    tenant_id: Optional[str] = None                   # Extracted from metadata
```

### VectorBatchInsert (Batch Request)

```python
class VectorBatchInsert(BaseModel):
    """Batch vector insertion request model."""

    vectors: List[VectorCreate]                       # 1-1000 vectors
    skip_existing: bool = False                       # Skip if ID exists
    overwrite: bool = False                           # Overwrite if ID exists
    batch_size: Optional[int] = None                  # Internal batch size (1-1000)
```

**Example Batch Request**:

```json
{
  "vectors": [
    {
      "document_id": "doc1",
      "chunk_id": "chunk1",
      "values": [0.1, 0.2, ...],
      "content": "First chunk",
      "chunk_index": 0,
      "chunk_count": 3
    },
    {
      "document_id": "doc1",
      "chunk_id": "chunk2",
      "values": [0.3, 0.4, ...],
      "content": "Second chunk",
      "chunk_index": 1,
      "chunk_count": 3
    },
    {
      "document_id": "doc1",
      "chunk_id": "chunk3",
      "values": [0.5, 0.6, ...],
      "content": "Third chunk",
      "chunk_index": 2,
      "chunk_count": 3
    }
  ],
  "skip_existing": false,
  "overwrite": false,
  "batch_size": 100
}
```

### VectorBatchResponse

```python
class VectorBatchResponse(BaseModel):
    """Batch vector operation response model."""

    inserted_count: int                               # Successfully inserted
    skipped_count: int = 0                            # Skipped (already exist)
    failed_count: int = 0                             # Failed to insert
    error_messages: List[str] = []                    # Error details
    processing_time_ms: float                         # Total processing time
```

**Example Response**:

```json
{
  "inserted_count": 2,
  "skipped_count": 1,
  "failed_count": 0,
  "error_messages": [],
  "processing_time_ms": 125.5
}
```

## Vector Storage Format

### Internal Deep Lake Structure

Deep Lake 4.0 stores vectors in columnar format optimized for:
- Fast vector similarity search
- Efficient metadata filtering
- Batch operations
- Memory-mapped access

**Storage Layout**:

```
{storage_location}/{tenant_id}/{dataset_name}/
├── dataset_metadata.json          # Dataset configuration
├── version_control_info.json      # Deep Lake versioning
├── dataset_info.json               # Deep Lake metadata
└── tensors/                        # Column-oriented tensor storage
    ├── id/                         # Text tensor
    ├── document_id/                # Text tensor
    ├── embedding/                  # Float32 array tensor
    ├── content/                    # Text tensor
    ├── chunk_count/                # Int32 tensor
    ├── metadata/                   # Text tensor (JSON strings)
    ├── chunk_id/                   # Text tensor
    ├── content_hash/               # Text tensor
    ├── content_type/               # Text tensor
    ├── language/                   # Text tensor
    ├── chunk_index/                # Int32 tensor
    ├── model/                      # Text tensor
    ├── created_at/                 # Text tensor (ISO timestamps)
    └── updated_at/                 # Text tensor (ISO timestamps)
```

### Storage Efficiency

**Per-Vector Storage Calculation**:

```
Embedding (1536 dims):    6,144 bytes (1536 × 4 bytes)
Text Fields (avg):        2,000 bytes (content, ids, metadata)
Metadata JSON (avg):      500 bytes
Integer Fields:           16 bytes (4 int32 fields)
Timestamps:               50 bytes (2 ISO timestamps)
-----------------------------------------------------------
Total per vector:         ~8,710 bytes (~8.5 KB)
```

**Dataset Size Examples**:

| Vectors | Storage Size | Notes |
|---------|--------------|-------|
| 1,000 | ~8.5 MB | Small document collection |
| 10,000 | ~85 MB | Medium notebook |
| 100,000 | ~850 MB | Large workspace |
| 1,000,000 | ~8.5 GB | Enterprise dataset |

## Cross-Service Integration

### Document Upload Flow

```
1. User uploads PDF → Aether Frontend
2. Frontend → Aether Backend: Upload request
3. Backend creates Document node in Neo4j
4. Backend → MinIO: Store file
5. Backend → AudiModal: Initiate processing
6. AudiModal extracts text, creates Chunk entities
7. AudiModal → DeepLake API: Batch insert vectors
   Request: POST /api/v1/datasets/{dataset_id}/vectors/batch
   Body: {
     "vectors": [
       {
         "document_id": "{neo4j_document_id}",
         "chunk_id": "{audimodal_chunk_id}",
         "values": [0.123, ...],
         "content": "chunk text",
         "metadata": {
           "tenant_id": "{tenant_id}",
           "space_id": "{space_id}",
           "notebook_id": "{notebook_id}"
         },
         "chunk_index": 0,
         "chunk_count": 10
       },
       ...
     ]
   }
8. DeepLake API stores vectors in Deep Lake
9. DeepLake API → AudiModal: Success response
10. AudiModal → Kafka: Publish document.processed event
11. Backend consumes event, updates Document status
12. Frontend polls backend, displays completion
```

### Search Flow

```
1. User enters search query → Aether Frontend
2. Frontend → Aether Backend: Search request
3. Backend → LLM Router: Generate query embedding
4. Backend → DeepLake API: Vector search
   Request: POST /api/v1/datasets/{dataset_id}/search
   Body: {
     "query_vector": [0.123, ...],
     "options": {
       "top_k": 10,
       "filters": {"tenant_id": "tenant_xyz"},
       "include_content": true,
       "include_metadata": true
     }
   }
5. DeepLake performs similarity search with filtering
6. DeepLake → Backend: Search results with vectors
7. Backend enriches with Document metadata from Neo4j
8. Backend → Frontend: Formatted search results
9. Frontend displays results with context
```

## Validation Rules

### Field Constraints

- **id**: Must be valid UUID v4 format
- **document_id**: Must exist in Neo4j Document nodes
- **chunk_id**: Must exist in AudiModal Chunk table (if provided)
- **values**:
  - Length must match dataset dimensions
  - All values must be finite numbers
  - Recommended range: [-1, 1] for normalized embeddings
- **content**:
  - Max length: 10,000 characters
  - Must be valid UTF-8
- **content_hash**: Must be 64-character hex string (SHA-256)
- **content_type**: Must be valid MIME type
- **language**: Must be valid ISO 639-1 code
- **chunk_index**: Must be >= 0 and < chunk_count
- **chunk_count**: Must be >= 1
- **model**: Must match known embedding model identifiers
- **metadata**:
  - Must be valid JSON when stringified
  - Recommended size: < 10KB
  - Required fields: `tenant_id` for multi-tenancy

### Business Rules

1. **Dimension Matching**: Vector dimensions must match dataset configuration
2. **Content Deduplication**: Vectors with identical `content_hash` may be skipped
3. **Tenant Isolation**: `metadata.tenant_id` must match authenticated user's tenant
4. **Sequential Chunks**: `chunk_index` should be sequential within a document
5. **Model Consistency**: All vectors in a dataset should use the same embedding model

## Performance Characteristics

### Insertion Performance

| Operation | Throughput | Latency | Notes |
|-----------|------------|---------|-------|
| Single Insert | ~100 ops/sec | 10ms | HTTP API overhead |
| Batch Insert (100) | ~10,000 vectors/sec | 10ms/vector | Optimal batch size |
| Batch Insert (1000) | ~15,000 vectors/sec | 67ms total | Max batch size |

### Search Performance

| Dataset Size | Search Latency | Notes |
|--------------|----------------|-------|
| 1K vectors | <10ms | In-memory search |
| 10K vectors | <50ms | Cached index |
| 100K vectors | <100ms | HNSW index |
| 1M vectors | <200ms | HNSW with metadata filtering |

### Storage I/O

- **Read IOPS**: ~5,000 IOPS for random vector access
- **Write IOPS**: ~2,000 IOPS for batch insertions
- **Sequential Read**: ~500 MB/s for full dataset scans
- **Compression**: ~30% reduction with Deep Lake compression

## Error Handling

### Common Errors

#### InvalidVectorDimensionsException
```json
{
  "success": false,
  "error_code": "INVALID_VECTOR_DIMENSIONS",
  "message": "Vector dimensions (512) do not match dataset dimensions (1536)",
  "details": {
    "provided_dimensions": 512,
    "expected_dimensions": 1536,
    "dataset_id": "my-dataset"
  }
}
```

#### VectorNotFoundException
```json
{
  "success": false,
  "error_code": "VECTOR_NOT_FOUND",
  "message": "Vector 'a3f2c8d1-4b7e-9f1a-2c5d-8e9f1a2b3c4d' not found",
  "details": {
    "vector_id": "a3f2c8d1-4b7e-9f1a-2c5d-8e9f1a2b3c4d",
    "dataset_id": "my-dataset"
  }
}
```

#### DuplicateVectorException
```json
{
  "success": false,
  "error_code": "DUPLICATE_VECTOR",
  "message": "Vector with ID already exists",
  "details": {
    "vector_id": "a3f2c8d1-4b7e-9f1a-2c5d-8e9f1a2b3c4d",
    "action": "Use overwrite=true to replace or skip_existing=true to skip"
  }
}
```

## Migration Considerations

### Model Version Upgrades

When upgrading embedding models (e.g., ada-002 → embedding-3):

1. **Create New Dataset**: New dimensions or model characteristics
2. **Parallel Population**: Insert new vectors alongside old ones
3. **Gradual Migration**: Update application to query new dataset
4. **Validation**: Compare search results between datasets
5. **Cutover**: Switch all traffic to new dataset
6. **Cleanup**: Delete old dataset after validation period

**Migration Script Pattern**:

```python
# Read vectors from old dataset
old_vectors = await deeplake_service.list_vectors(
    dataset_name="old-dataset",
    tenant_id=tenant_id
)

# Re-embed content with new model
new_embeddings = await embedding_service.embed_batch(
    texts=[v.content for v in old_vectors],
    model="text-embedding-3-small"
)

# Insert into new dataset
new_vectors = [
    VectorCreate(
        document_id=old.document_id,
        chunk_id=old.chunk_id,
        values=new_emb,
        content=old.content,
        metadata=old.metadata,
        chunk_index=old.chunk_index,
        chunk_count=old.chunk_count,
        model="text-embedding-3-small"
    )
    for old, new_emb in zip(old_vectors, new_embeddings)
]

await deeplake_service.insert_vectors_batch(
    dataset_name="new-dataset",
    vectors=new_vectors,
    tenant_id=tenant_id
)
```

## See Also

- [Dataset Organization](./dataset-organization.md) - Multi-tenant dataset structure
- [Embedding Models](./embedding-models.md) - Model configurations and selection
- [Query API](./query-api.md) - Search and retrieval operations
- [AudiModal Chunk Entity](../audimodal/entities/chunk.md) - Source chunks
- [Aether Document Node](../aether-be/nodes/document.md) - Parent documents
- [Cross-Service Flows: Document Upload](../cross-service/flows/document-upload.md)
