# DeepLake Dataset Organization

**Service**: DeepLake API
**Storage**: Deep Lake 4.0 Vector Database
**API Version**: v1
**Last Updated**: 2026-01-06

## Overview

The DeepLake API organizes vector embeddings into datasets, with a hierarchical multi-tenant structure that ensures data isolation and efficient resource management. Each tenant can have multiple datasets, and each dataset contains vectors with consistent dimensions and distance metrics.

## Multi-Tenant Architecture

### Tenant Isolation Model

The DeepLake API implements **space-based multi-tenancy** aligned with the Aether platform's tenant model:

```
Keycloak User
    ↓
Aether User (id)
    ↓
Aether Space (space_id, tenant_id)
    ↓
AudiModal Tenant (id = tenant_id)
    ↓
DeepLake Datasets (tenants/{tenant_id}/{dataset_name})
```

### Storage Hierarchy

```
{storage_location}/                     # Base: ./data/vectors or /data/vectors
├── tenants/                            # Tenant-based organization
│   ├── {tenant_id_1}/                  # e.g., tenant_1756217701
│   │   ├── default/                    # Default dataset for the tenant
│   │   │   ├── dataset_metadata.json
│   │   │   ├── version_control_info.json
│   │   │   ├── dataset_info.json
│   │   │   └── tensors/
│   │   │       ├── id/
│   │   │       ├── document_id/
│   │   │       ├── embedding/
│   │   │       └── ...
│   │   ├── {space_id_1}/               # Space-specific dataset
│   │   │   └── ...
│   │   ├── {space_id_2}/               # Another space dataset
│   │   │   └── ...
│   │   └── {custom_dataset_name}/      # Custom named dataset
│   │       └── ...
│   ├── {tenant_id_2}/
│   │   ├── default/
│   │   └── ...
│   └── shared/                         # Optional shared datasets
│       └── public-knowledge-base/
│           └── ...
└── system/                             # System-level datasets
    └── embeddings-cache/
        └── ...
```

## Dataset Naming Conventions

### Standard Dataset Names

#### Default Dataset
- **Name**: `default`
- **Path**: `tenants/{tenant_id}/default`
- **Purpose**: Primary dataset for all user documents
- **Created**: Automatically during user onboarding
- **Dimensions**: 1536 (OpenAI ada-002 default)
- **Metric**: Cosine similarity

**Example**:
```
tenants/tenant_1756217701/default/
```

#### Space-Specific Datasets
- **Name**: `{space_id}` (UUID format)
- **Path**: `tenants/{tenant_id}/{space_id}`
- **Purpose**: Isolated vectors for specific workspaces
- **Created**: On-demand when space requires separate embedding storage
- **Use Case**: Large projects, confidential workspaces

**Example**:
```
tenants/tenant_1756217701/space_abc123/
```

#### Notebook-Specific Datasets
- **Name**: `notebook_{notebook_id}`
- **Path**: `tenants/{tenant_id}/notebook_{notebook_id}`
- **Purpose**: Dedicated vector storage for single notebooks
- **Created**: For notebooks with >100,000 chunks or special requirements
- **Use Case**: Large documents, specialized embeddings

**Example**:
```
tenants/tenant_1756217701/notebook_xyz789/
```

### Custom Dataset Names

Users can create custom datasets with specific names:

**Naming Rules**:
- Length: 1-100 characters
- Allowed: lowercase letters, numbers, hyphens, underscores
- Pattern: `^[a-z0-9_-]+$`
- Reserved: `default`, `system`, `shared`

**Examples**:
```
my-research-papers
client-documents-2026
embeddings-v2
knowledge-base
```

## Dataset Metadata Structure

### dataset_metadata.json

Each dataset includes a metadata file at the root:

```json
{
  "name": "default",
  "description": "Default dataset for tenant_1756217701",
  "dimensions": 1536,
  "metric_type": "cosine",
  "index_type": "hnsw",
  "tenant_id": "tenant_1756217701",
  "created_at": "2026-01-06T14:30:45.123456Z",
  "updated_at": "2026-01-06T15:45:30.654321Z",
  "custom_metadata": {
    "space_id": "space_abc123",
    "owner_user_id": "7f8e9d1c-2b3a-4c5d-6e7f-8a9b0c1d2e3f",
    "purpose": "Document embeddings for user workspace",
    "model_version": "text-embedding-ada-002",
    "last_reindex_at": "2026-01-06T12:00:00.000000Z"
  }
}
```

### Metadata Fields

#### Core Dataset Metadata

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Dataset identifier |
| `description` | string | No | Human-readable description |
| `dimensions` | integer | Yes | Vector dimension count (1-10000) |
| `metric_type` | string | Yes | Distance metric: `cosine`, `euclidean`, `manhattan`, `dot_product` |
| `index_type` | string | Yes | Index type: `default`, `flat`, `hnsw`, `ivf` |
| `tenant_id` | string | Yes | Owning tenant UUID |
| `created_at` | string | Yes | ISO 8601 creation timestamp |
| `updated_at` | string | Yes | ISO 8601 last update timestamp |

#### Custom Metadata (Optional)

| Field | Type | Description |
|-------|------|-------------|
| `space_id` | string | Associated Aether space UUID |
| `notebook_id` | string | Associated notebook UUID (for dedicated datasets) |
| `owner_user_id` | string | Creating user's Keycloak ID |
| `purpose` | string | Dataset purpose description |
| `model_version` | string | Embedding model identifier |
| `last_reindex_at` | string | Last index rebuild timestamp |
| `quota_limit` | integer | Max vectors allowed |
| `retention_days` | integer | Data retention policy (days) |
| `tags` | array[string] | Categorization tags |

## Dataset Configuration

### Distance Metrics

#### Cosine Similarity (Default)
```json
{
  "metric_type": "cosine"
}
```
- **Range**: [-1, 1] (converted to [0, 2] for distance)
- **Best For**: Text embeddings, semantic similarity
- **Normalization**: Vectors should be L2-normalized
- **Computation**: `1 - (dot(A, B) / (norm(A) * norm(B)))`

**Use Cases**:
- Document similarity search
- Semantic question answering
- Content recommendation

#### Euclidean Distance (L2)
```json
{
  "metric_type": "euclidean"
}
```
- **Range**: [0, ∞)
- **Best For**: Spatial data, feature vectors
- **Normalization**: Not required
- **Computation**: `sqrt(sum((A[i] - B[i])^2))`

**Use Cases**:
- Image similarity
- Clustering analysis
- Spatial queries

#### Manhattan Distance (L1)
```json
{
  "metric_type": "manhattan"
}
```
- **Range**: [0, ∞)
- **Best For**: High-dimensional sparse vectors
- **Normalization**: Not required
- **Computation**: `sum(abs(A[i] - B[i]))`

**Use Cases**:
- Feature matching
- Outlier detection
- Sparse embeddings

#### Dot Product (Inner Product)
```json
{
  "metric_type": "dot_product"
}
```
- **Range**: (-∞, ∞)
- **Best For**: Already normalized vectors
- **Normalization**: Required for similarity
- **Computation**: `sum(A[i] * B[i])`

**Use Cases**:
- Maximum similarity search
- Recommendation systems
- Fast approximate search

### Index Types

#### Default Index (Flat)
```json
{
  "index_type": "default"
}
```
- **Algorithm**: Brute-force linear scan
- **Build Time**: O(1) - instant
- **Search Time**: O(N) - linear
- **Memory**: Low
- **Accuracy**: 100% exact
- **Best For**: Small datasets (<10K vectors)

#### HNSW Index (Hierarchical Navigable Small World)
```json
{
  "index_type": "hnsw",
  "index_config": {
    "M": 16,
    "ef_construction": 200,
    "ef_search": 50
  }
}
```
- **Algorithm**: Graph-based approximate nearest neighbor
- **Build Time**: O(N log N)
- **Search Time**: O(log N)
- **Memory**: High (5-10x vector data)
- **Accuracy**: 95-99% with tuning
- **Best For**: Large datasets (10K-10M vectors)

**HNSW Parameters**:
- `M`: Max connections per layer (8-64, default 16)
- `ef_construction`: Build-time search width (100-500, default 200)
- `ef_search`: Query-time search width (10-500, default 50)

#### IVF Index (Inverted File)
```json
{
  "index_type": "ivf",
  "index_config": {
    "nlist": 100,
    "nprobe": 10
  }
}
```
- **Algorithm**: Clustering-based partitioning
- **Build Time**: O(N * K) where K = nlist
- **Search Time**: O(N/K * nprobe)
- **Memory**: Medium (2-3x vector data)
- **Accuracy**: 90-95%
- **Best For**: Very large datasets (>1M vectors)

**IVF Parameters**:
- `nlist`: Number of clusters (sqrt(N) to N/100, default 100)
- `nprobe`: Clusters to search (1-nlist, default 10)

## Dataset Lifecycle

### Creation Workflow

```
1. User/Service → POST /api/v1/datasets
   Request: {
     "name": "my-dataset",
     "dimensions": 1536,
     "metric_type": "cosine",
     "index_type": "hnsw",
     "description": "My custom dataset",
     "metadata": {
       "space_id": "space_abc123"
     }
   }

2. DeepLake API validates:
   - Name uniqueness within tenant
   - Dimension range (1-10000)
   - Valid metric and index types
   - Tenant authorization

3. Create dataset directory:
   {storage_location}/tenants/{tenant_id}/my-dataset/

4. Initialize Deep Lake schema:
   - Define tensor columns (id, embedding, content, metadata, etc.)
   - Set vector dimensions
   - Configure distance metric

5. Create dataset_metadata.json:
   - Store configuration
   - Set timestamps
   - Add custom metadata

6. Initialize index (if HNSW/IVF):
   - Build empty index structure
   - Set index parameters

7. Return DatasetResponse:
   {
     "id": "my-dataset",
     "name": "my-dataset",
     "dimensions": 1536,
     "metric_type": "cosine",
     "index_type": "hnsw",
     "storage_location": "{path}/tenants/{tenant_id}/my-dataset",
     "vector_count": 0,
     "storage_size": 0,
     "created_at": "2026-01-06T14:30:45.123456Z",
     "updated_at": "2026-01-06T14:30:45.123456Z",
     "tenant_id": "tenant_1756217701"
   }
```

### Update Operations

#### Metadata Update
```http
PUT /api/v1/datasets/{dataset_id}
Content-Type: application/json

{
  "description": "Updated description",
  "metadata": {
    "tags": ["updated", "active"],
    "last_reindex_at": "2026-01-06T12:00:00.000000Z"
  }
}
```

**Updatable Fields**:
- `description` - Dataset description text
- `metadata` - Custom metadata dictionary

**Non-Updatable Fields** (require recreation):
- `name` - Dataset identifier
- `dimensions` - Vector dimensions
- `metric_type` - Distance metric
- `index_type` - Index algorithm

#### Reindexing
```http
POST /api/v1/datasets/{dataset_id}/reindex
Content-Type: application/json

{
  "index_type": "hnsw",
  "index_config": {
    "M": 32,
    "ef_construction": 400
  },
  "async": true
}
```

**Reindex Process**:
1. Create new index with updated configuration
2. Rebuild index from existing vectors
3. Validate new index performance
4. Atomically swap to new index
5. Clean up old index

### Deletion Workflow

```
1. User/Service → DELETE /api/v1/datasets/{dataset_id}

2. DeepLake API validates:
   - Dataset exists
   - User has delete permission
   - No dependent resources (optional check)

3. Soft delete (recommended):
   - Rename dataset directory: my-dataset → my-dataset.deleted.{timestamp}
   - Move to trash location
   - Keep for retention period (30 days default)
   - Schedule cleanup job

4. Hard delete (immediate):
   - Remove dataset directory completely
   - Clear cache entries
   - Remove from dataset registry

5. Update metrics:
   - Decrement dataset count
   - Free storage quota

6. Return success response:
   {
     "success": true,
     "message": "Dataset 'my-dataset' deleted successfully",
     "deleted_at": "2026-01-06T15:30:00.000000Z"
   }
```

## API Operations

### Create Dataset

**Request**:
```http
POST /api/v1/datasets
Authorization: ApiKey {api_key}
Content-Type: application/json

{
  "name": "research-papers",
  "description": "Academic research papers embeddings",
  "dimensions": 1536,
  "metric_type": "cosine",
  "index_type": "hnsw",
  "metadata": {
    "space_id": "space_abc123",
    "purpose": "research",
    "tags": ["academic", "research"]
  },
  "overwrite": false
}
```

**Response** (201 Created):
```json
{
  "id": "research-papers",
  "name": "research-papers",
  "description": "Academic research papers embeddings",
  "dimensions": 1536,
  "metric_type": "cosine",
  "index_type": "hnsw",
  "metadata": {
    "space_id": "space_abc123",
    "purpose": "research",
    "tags": ["academic", "research"]
  },
  "storage_location": "/data/vectors/tenants/tenant_1756217701/research-papers",
  "vector_count": 0,
  "storage_size": 0,
  "created_at": "2026-01-06T14:30:45.123456Z",
  "updated_at": "2026-01-06T14:30:45.123456Z",
  "tenant_id": "tenant_1756217701"
}
```

### List Datasets

**Request**:
```http
GET /api/v1/datasets?limit=10&offset=0
Authorization: ApiKey {api_key}
```

**Response** (200 OK):
```json
[
  {
    "id": "default",
    "name": "default",
    "description": "Default dataset for workspace",
    "dimensions": 1536,
    "metric_type": "cosine",
    "index_type": "hnsw",
    "vector_count": 15432,
    "storage_size": 131072000,
    "created_at": "2026-01-01T00:00:00.000000Z",
    "updated_at": "2026-01-06T14:30:45.123456Z",
    "tenant_id": "tenant_1756217701"
  },
  {
    "id": "research-papers",
    "name": "research-papers",
    "description": "Academic research papers embeddings",
    "dimensions": 1536,
    "metric_type": "cosine",
    "index_type": "hnsw",
    "vector_count": 5280,
    "storage_size": 45056000,
    "created_at": "2026-01-06T14:30:45.123456Z",
    "updated_at": "2026-01-06T15:00:00.000000Z",
    "tenant_id": "tenant_1756217701"
  }
]
```

### Get Dataset Statistics

**Request**:
```http
GET /api/v1/datasets/{dataset_id}/stats
Authorization: ApiKey {api_key}
```

**Response** (200 OK):
```json
{
  "dataset": {
    "id": "default",
    "name": "default",
    "dimensions": 1536,
    "metric_type": "cosine",
    "index_type": "hnsw",
    "vector_count": 15432,
    "storage_size": 131072000,
    "tenant_id": "tenant_1756217701"
  },
  "vector_count": 15432,
  "storage_size": 131072000,
  "metadata_stats": {
    "unique_documents": 1250,
    "unique_chunks": 15432,
    "languages": {
      "en": 14500,
      "es": 750,
      "fr": 182
    },
    "content_types": {
      "text/plain": 15000,
      "text/markdown": 432
    }
  },
  "index_stats": {
    "index_type": "hnsw",
    "build_time_seconds": 45.2,
    "memory_usage_bytes": 655360000,
    "parameters": {
      "M": 16,
      "ef_construction": 200
    },
    "last_rebuild_at": "2026-01-06T12:00:00.000000Z"
  }
}
```

### Delete Dataset

**Request**:
```http
DELETE /api/v1/datasets/{dataset_id}
Authorization: ApiKey {api_key}
```

**Response** (200 OK):
```json
{
  "success": true,
  "message": "Dataset 'research-papers' deleted successfully",
  "deleted_at": "2026-01-06T15:30:00.000000Z"
}
```

## Performance Considerations

### Dataset Size Guidelines

| Vector Count | Storage Size | Index Type | Search Latency | Build Time |
|--------------|--------------|------------|----------------|------------|
| 0-1K | <10 MB | default/flat | <5ms | instant |
| 1K-10K | 10-100 MB | flat/hnsw | <20ms | <10s |
| 10K-100K | 100MB-1GB | hnsw | <50ms | 1-5min |
| 100K-1M | 1-10 GB | hnsw | <100ms | 5-30min |
| 1M-10M | 10-100 GB | hnsw/ivf | <200ms | 30min-2hr |
| 10M+ | 100GB+ | ivf | <500ms | 2hr+ |

### Dataset Partitioning Strategies

#### By Tenant (Default)
```
✓ Pros:
  - Strong isolation
  - Independent scaling
  - Easy tenant offboarding
  - Per-tenant quotas

✗ Cons:
  - Cannot search across tenants
  - Duplicate datasets for shared data
```

#### By Space
```
✓ Pros:
  - Workspace isolation
  - Project-specific optimization
  - Granular access control

✗ Cons:
  - More datasets to manage
  - Cannot search across spaces
```

#### By Time Period
```
Dataset names: documents-2026-01, documents-2026-02, ...

✓ Pros:
  - Easy archival
  - Time-based retention
  - Historical queries

✗ Cons:
  - Requires multi-dataset search
  - Complex time-range queries
```

#### By Content Type
```
Dataset names: pdfs, images, audio, code

✓ Pros:
  - Type-specific optimization
  - Different embedding models
  - Targeted search

✗ Cons:
  - Fragmented data
  - Multi-dataset search needed
```

### Caching Strategy

#### Dataset Info Cache
- **Location**: Redis
- **Key Pattern**: `dataset:info:{tenant_id}:{dataset_id}`
- **TTL**: 1 hour
- **Invalidation**: On dataset update/delete

#### Dataset List Cache
- **Location**: Redis
- **Key Pattern**: `dataset:list:{tenant_id}`
- **TTL**: 5 minutes
- **Invalidation**: On create/delete

#### Index Metadata Cache
- **Location**: Memory
- **Scope**: Per-process
- **TTL**: Until process restart
- **Use**: Avoid repeated index reads

## Multi-Dataset Search

### Search Across Datasets

```http
POST /api/v1/search/multi-dataset
Authorization: ApiKey {api_key}
Content-Type: application/json

{
  "query_vector": [0.123, -0.456, ...],
  "datasets": ["default", "research-papers", "archived-2025"],
  "options": {
    "top_k": 10,
    "filters": {
      "tenant_id": "tenant_1756217701"
    },
    "merge_strategy": "interleave"
  }
}
```

**Merge Strategies**:

#### Interleave
```
Results: [default[0], research[0], archived[0], default[1], research[1], ...]
```
- Balanced representation from each dataset

#### Score-Based
```
Results: Sorted by score across all datasets
```
- Best overall results regardless of dataset

#### Round-Robin
```
Results: [default[0], research[0], archived[0], default[1], research[1], ...]
```
- Equal representation, ignore scores

## Error Handling

### DatasetNotFoundException
```json
{
  "success": false,
  "error_code": "DATASET_NOT_FOUND",
  "message": "Dataset 'my-dataset' not found for tenant 'tenant_xyz'",
  "details": {
    "dataset_id": "my-dataset",
    "tenant_id": "tenant_xyz"
  }
}
```

### DatasetAlreadyExistsException
```json
{
  "success": false,
  "error_code": "DATASET_ALREADY_EXISTS",
  "message": "Dataset 'my-dataset' already exists",
  "details": {
    "dataset_id": "my-dataset",
    "tenant_id": "tenant_xyz",
    "action": "Use overwrite=true to replace or choose a different name"
  }
}
```

### InvalidDatasetConfigException
```json
{
  "success": false,
  "error_code": "INVALID_DATASET_CONFIG",
  "message": "Invalid dimension value: 0",
  "details": {
    "field": "dimensions",
    "value": 0,
    "allowed_range": "1-10000"
  }
}
```

## See Also

- [Vector Structure](./vector-structure.md) - Individual vector schema and fields
- [Embedding Models](./embedding-models.md) - Model configurations and selection
- [Query API](./query-api.md) - Search and retrieval operations
- [User Onboarding Flow](../cross-service/flows/user-onboarding.md) - Default dataset creation
- [Architecture Overview](../cross-service/diagrams/architecture-overview.md) - Multi-tenancy architecture
