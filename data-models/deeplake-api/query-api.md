# DeepLake Query API - Search and Retrieval Operations

## Metadata

- **Document Type**: API Documentation
- **Service**: DeepLake API
- **Component**: Search and Retrieval
- **Last Updated**: 2026-01-06
- **Owner**: TAS Platform Team
- **Status**: Active

---

## Overview

### Purpose

The DeepLake Query API provides sophisticated vector search capabilities for retrieving semantically similar vectors from datasets. It supports multiple search strategies including pure vector similarity, text-based semantic search, and hybrid search combining both approaches. The API is optimized for high-performance retrieval with advanced features like metadata filtering, result fusion, and query optimization.

### Search Capabilities

The Query API offers three primary search modes:

1. **Vector Search** - Direct similarity search using pre-computed embeddings
2. **Text Search** - Semantic search by converting text queries to embeddings
3. **Hybrid Search** - Combined vector and text search with configurable fusion methods

All search modes support:
- Advanced metadata filtering with complex expressions
- Configurable result ranking and scoring
- Performance optimization through caching
- Multi-tenancy with space-based isolation
- Comprehensive query statistics and metrics

### Key Features

- **High-Performance Retrieval**: Optimized for sub-second query times
- **Flexible Ranking**: Multiple fusion algorithms (weighted sum, RRF, Borda count, CombSUM, CombMNZ)
- **Advanced Filtering**: Complex metadata queries with boolean operators
- **Result Deduplication**: Intelligent duplicate detection and removal
- **Query Caching**: Redis-backed caching for frequent queries
- **Streaming Results**: Support for paginated and streamed search results
- **Relevance Tuning**: Configurable scoring weights and thresholds

---

## Table of Contents

1. [Vector Search API](#vector-search-api)
2. [Text Search API](#text-search-api)
3. [Hybrid Search API](#hybrid-search-api)
4. [Search Options](#search-options)
5. [Metadata Filtering](#metadata-filtering)
6. [Fusion Methods](#fusion-methods)
7. [Response Format](#response-format)
8. [Query Optimization](#query-optimization)
9. [Error Handling](#error-handling)
10. [Performance Considerations](#performance-considerations)
11. [Code Examples](#code-examples)
12. [Related Documentation](#related-documentation)

---

## Vector Search API

### Endpoint

```
POST /datasets/{dataset_id}/search
```

### Purpose

Performs vector similarity search using a pre-computed query vector. This is the most performant search method when you already have embeddings.

### Request Structure

#### Python (Pydantic)

```python
class SearchRequest(BaseModel):
    """Vector search request model."""

    query_vector: List[float] = Field(..., description="Query vector")
    options: Optional[SearchOptions] = Field(default=None)

    @field_validator('query_vector')
    @classmethod
    def validate_query_vector(cls, v: List[float]) -> List[float]:
        if not v:
            raise ValueError("Query vector cannot be empty")
        if len(v) > 10000:
            raise ValueError("Query vector dimensions cannot exceed 10000")
        return v
```

#### TypeScript

```typescript
interface SearchRequest {
  query_vector: number[];
  options?: SearchOptions;
}
```

#### Go

```go
type SearchRequest struct {
    QueryVector []float64      `json:"query_vector"`
    Options     *SearchOptions `json:"options,omitempty"`
}
```

### Request Example

```json
{
  "query_vector": [0.123, -0.456, 0.789, ...],
  "options": {
    "top_k": 10,
    "threshold": 0.7,
    "include_content": true,
    "include_metadata": true,
    "filters": {
      "document_type": "pdf",
      "language": "en"
    },
    "deduplicate": true
  }
}
```

### Response

Returns a `SearchResponse` with matching vectors ranked by similarity.

```json
{
  "results": [
    {
      "vector": {
        "id": "vec_123",
        "document_id": "doc_456",
        "chunk_id": "chunk_789",
        "values": [0.123, -0.456, ...],
        "content": "This is the matched text content...",
        "metadata": {
          "document_type": "pdf",
          "page_number": 5,
          "language": "en"
        },
        "created_at": "2026-01-06T10:30:00Z"
      },
      "score": 0.95,
      "distance": 0.05,
      "rank": 1
    }
  ],
  "total_found": 42,
  "has_more": true,
  "query_time_ms": 45.3,
  "stats": {
    "vectors_scanned": 10000,
    "index_hits": 100,
    "filtered_results": 42,
    "database_time_ms": 30.2,
    "post_processing_time_ms": 15.1
  }
}
```

### Usage Patterns

**Basic Vector Search**:
```python
import httpx

async def search_vectors(dataset_id: str, query_vector: List[float]):
    async with httpx.AsyncClient() as client:
        response = await client.post(
            f"http://localhost:8000/datasets/{dataset_id}/search",
            json={
                "query_vector": query_vector,
                "options": {
                    "top_k": 10,
                    "threshold": 0.7
                }
            },
            headers={
                "Authorization": f"Bearer {token}",
                "X-Tenant-ID": tenant_id
            }
        )
        return response.json()
```

**With Advanced Filtering**:
```python
response = await search_vectors(
    dataset_id="dataset_123",
    query_vector=embedding,
    filters={
        "AND": [
            {"field": "document_type", "operator": "eq", "value": "pdf"},
            {"field": "page_number", "operator": "gte", "value": 1},
            {"field": "page_number", "operator": "lte", "value": 10}
        ]
    }
)
```

### Performance Characteristics

- **Latency**: 10-100ms for datasets <1M vectors
- **Throughput**: 100+ queries/second with caching
- **Scalability**: Linear scaling up to 10M vectors
- **Cache Hit Rate**: 60-80% for common queries

---

## Text Search API

### Endpoint

```
POST /datasets/{dataset_id}/search/text
```

### Purpose

Performs semantic search by converting a text query into an embedding vector and then executing a vector similarity search. This is ideal for natural language queries.

### Request Structure

#### Python (Pydantic)

```python
class TextSearchRequest(BaseModel):
    """Text-based search request model."""

    query_text: str = Field(..., min_length=1, max_length=10000, description="Query text")
    options: Optional[SearchOptions] = Field(default=None)
```

#### TypeScript

```typescript
interface TextSearchRequest {
  query_text: string;
  options?: SearchOptions;
}
```

#### Go

```go
type TextSearchRequest struct {
    QueryText string         `json:"query_text"`
    Options   *SearchOptions `json:"options,omitempty"`
}
```

### Request Example

```json
{
  "query_text": "What are the key findings about climate change?",
  "options": {
    "top_k": 5,
    "threshold": 0.75,
    "include_content": true,
    "filters": {
      "document_type": "research_paper",
      "publication_year": {
        "gte": 2020
      }
    }
  }
}
```

### Workflow

1. **Text to Vector**: Query text is converted to embedding using configured embedding service
2. **Dimension Validation**: Embedding dimensions must match dataset dimensions
3. **Vector Search**: Performs standard vector similarity search
4. **Result Return**: Returns ranked results with original text context

### Embedding Service Integration

The text search endpoint automatically integrates with the embedding service:

```python
# Embedding generation
query_vector = await embedding_service.text_to_vector(search_request.query_text)

# Dimension validation
if not await embedding_service.validate_compatibility(dataset.dimensions):
    raise HTTPException(
        status_code=400,
        detail=f"Embedding dimensions ({embedding_dims}) don't match dataset dimensions ({dataset.dimensions})"
    )
```

### Caching Strategy

Text searches are cached using a hash of the query text:

```python
# Create cache key
text_hash = hashlib.sha256(search_request.query_text.encode()).hexdigest()[:16]
options_hash = hashlib.sha256(options_json.encode()).hexdigest()[:16]

# Try cache first
cached_results = await cache_manager.get_search_results(
    dataset_id, text_hash, options_hash, tenant_id
)
```

### Response

Same format as Vector Search API, with additional `embedding_time_ms` field:

```json
{
  "results": [...],
  "total_found": 15,
  "has_more": false,
  "query_time_ms": 125.7,
  "embedding_time_ms": 80.4,
  "stats": {...}
}
```

### Usage Example

```python
async def semantic_search(query: str):
    async with httpx.AsyncClient() as client:
        response = await client.post(
            f"{base_url}/datasets/{dataset_id}/search/text",
            json={
                "query_text": query,
                "options": {
                    "top_k": 10,
                    "threshold": 0.7,
                    "filters": {
                        "language": "en"
                    }
                }
            },
            headers={"Authorization": f"Bearer {token}"}
        )
        return response.json()

# Usage
results = await semantic_search("machine learning best practices")
```

### Performance Considerations

- **Embedding Generation**: Adds 50-200ms overhead
- **Cache Effectiveness**: High hit rate for common queries
- **Dimension Mismatch**: Fails fast with clear error message

---

## Hybrid Search API

### Endpoint

```
POST /datasets/{dataset_id}/search/hybrid
```

### Purpose

Combines vector similarity search and text-based search using configurable fusion algorithms. This approach leverages both exact vector matching and semantic text understanding for optimal retrieval quality.

### Request Structure

#### Python (Pydantic)

```python
class HybridSearchRequest(BaseModel):
    """Hybrid search request model."""

    query_vector: Optional[List[float]] = None
    query_text: Optional[str] = Field(None, min_length=1, max_length=10000)
    options: Optional[SearchOptions] = Field(default=None)
    vector_weight: float = Field(default=0.5, ge=0.0, le=1.0, description="Vector search weight")
    text_weight: float = Field(default=0.5, ge=0.0, le=1.0, description="Text search weight")
    fusion_method: Optional[str] = Field(
        default="weighted_sum",
        description="Result fusion method: weighted_sum, reciprocal_rank_fusion, comb_sum, comb_mnz, borda_count"
    )

    @model_validator(mode='after')
    def validate_weights(self) -> 'HybridSearchRequest':
        if abs(self.vector_weight + self.text_weight - 1.0) > 0.01:
            raise ValueError("Vector weight and text weight must sum to 1.0")
        return self
```

#### TypeScript

```typescript
interface HybridSearchRequest {
  query_vector?: number[];
  query_text?: string;
  options?: SearchOptions;
  vector_weight?: number; // 0.0 to 1.0, default 0.5
  text_weight?: number;   // 0.0 to 1.0, default 0.5
  fusion_method?: 'weighted_sum' | 'reciprocal_rank_fusion' |
                  'comb_sum' | 'comb_mnz' | 'borda_count';
}
```

### Request Examples

**Balanced Hybrid Search**:
```json
{
  "query_text": "climate change impact on agriculture",
  "vector_weight": 0.5,
  "text_weight": 0.5,
  "fusion_method": "weighted_sum",
  "options": {
    "top_k": 10,
    "threshold": 0.65
  }
}
```

**Vector-Biased Hybrid**:
```json
{
  "query_vector": [0.123, -0.456, ...],
  "query_text": "deep learning",
  "vector_weight": 0.7,
  "text_weight": 0.3,
  "fusion_method": "reciprocal_rank_fusion",
  "options": {
    "top_k": 20,
    "rerank": true
  }
}
```

**Text-Only Fallback**:
```json
{
  "query_text": "neural network architectures",
  "fusion_method": "weighted_sum",
  "options": {
    "top_k": 15
  }
}
```

### Hybrid Search Workflow

```python
async def hybrid_search(
    dataset_id: str,
    query_text: str,
    query_vector: Optional[List[float]],
    vector_weight: float,
    text_weight: float,
    fusion_method: FusionMethod,
    options: SearchOptions
) -> SearchResponse:
    """Perform hybrid search combining vector and text."""

    # 1. Validate weights
    if abs(vector_weight + text_weight - 1.0) > 0.01:
        vector_weight = vector_weight / (vector_weight + text_weight)
        text_weight = 1.0 - vector_weight

    # 2. Execute searches in parallel
    vector_results, text_results = await asyncio.gather(
        _vector_search(dataset_id, query_text, query_vector, options),
        _text_search(dataset_id, query_text, options),
        return_exceptions=True
    )

    # 3. Fuse results using specified method
    combined_results = await _fuse_results(
        vector_results,
        text_results,
        fusion_method,
        vector_weight,
        text_weight
    )

    # 4. Post-process and return
    final_results = await _post_process_results(
        combined_results,
        query_text,
        options
    )

    return SearchResponse(results=final_results, ...)
```

### Fusion Method Details

See [Fusion Methods](#fusion-methods) section below for complete algorithm descriptions.

### Response Format

```json
{
  "results": [
    {
      "vector": {...},
      "score": 0.92,
      "distance": 0.08,
      "rank": 1,
      "explanation": {
        "vector_score": 0.85,
        "text_score": 0.95,
        "fusion_method": "weighted_sum",
        "final_score": 0.92
      }
    }
  ],
  "total_found": 25,
  "has_more": true,
  "query_time_ms": 156.8,
  "embedding_time_ms": 85.2,
  "stats": {
    "vectors_scanned": 15000,
    "vector_results": 50,
    "text_results": 45,
    "fused_results": 25,
    "database_time_ms": 110.5,
    "post_processing_time_ms": 46.3
  }
}
```

### Usage Example

```python
async def hybrid_semantic_search(query: str):
    response = await client.post(
        f"{base_url}/datasets/{dataset_id}/search/hybrid",
        json={
            "query_text": query,
            "vector_weight": 0.6,
            "text_weight": 0.4,
            "fusion_method": "reciprocal_rank_fusion",
            "options": {
                "top_k": 20,
                "threshold": 0.7,
                "rerank": True,
                "filters": {
                    "document_type": ["pdf", "docx"]
                }
            }
        }
    )
    return response.json()
```

---

## Search Options

### Structure

```python
class SearchOptions(BaseModel):
    """Search options model."""

    top_k: int = Field(default=10, ge=1, le=1000, description="Number of results to return")
    threshold: Optional[float] = Field(None, ge=0.0, le=1.0, description="Similarity threshold")
    metric_type: Optional[str] = Field(None, description="Distance metric override")
    include_content: bool = Field(default=True, description="Include content in results")
    include_metadata: bool = Field(default=True, description="Include metadata in results")
    filters: Optional[Union[Dict[str, Any], str]] = Field(
        default=None,
        description="Advanced metadata filters"
    )
    deduplicate: bool = Field(default=False, description="Remove duplicate results")
    group_by_document: bool = Field(default=False, description="Group results by document")
    rerank: bool = Field(default=False, description="Apply reranking")
    ef_search: Optional[int] = Field(None, ge=1, description="HNSW ef_search parameter")
    nprobe: Optional[int] = Field(None, ge=1, description="IVF nprobe parameter")
    max_distance: Optional[float] = Field(None, ge=0.0, description="Maximum distance")
    min_score: Optional[float] = Field(None, ge=0.0, le=1.0, description="Minimum score")
```

### Option Details

#### top_k

- **Type**: Integer (1-1000)
- **Default**: 10
- **Purpose**: Maximum number of results to return
- **Usage**: Balance between recall and response time

```json
{
  "options": {
    "top_k": 20
  }
}
```

#### threshold

- **Type**: Float (0.0-1.0)
- **Default**: None (no filtering)
- **Purpose**: Minimum similarity score for results
- **Usage**: Filter low-quality matches

```json
{
  "options": {
    "top_k": 50,
    "threshold": 0.75
  }
}
```

#### metric_type

- **Type**: String
- **Options**: "cosine", "euclidean", "manhattan", "dot_product"
- **Default**: Dataset default metric
- **Purpose**: Override distance metric for this query

```json
{
  "options": {
    "metric_type": "cosine"
  }
}
```

#### include_content

- **Type**: Boolean
- **Default**: true
- **Purpose**: Include full text content in results
- **Usage**: Set to false for faster queries when only metadata needed

```json
{
  "options": {
    "include_content": false,
    "include_metadata": true
  }
}
```

#### include_metadata

- **Type**: Boolean
- **Default**: true
- **Purpose**: Include metadata fields in results
- **Usage**: Set to false to reduce response size

#### filters

- **Type**: Dictionary or String
- **Default**: None (no filtering)
- **Purpose**: Advanced metadata filtering
- **See**: [Metadata Filtering](#metadata-filtering) section

#### deduplicate

- **Type**: Boolean
- **Default**: false
- **Purpose**: Remove duplicate results based on content hash
- **Algorithm**: Uses content_hash field to detect duplicates

```json
{
  "options": {
    "deduplicate": true
  }
}
```

#### group_by_document

- **Type**: Boolean
- **Default**: false
- **Purpose**: Group results by document_id
- **Usage**: Useful for showing "top documents" instead of "top chunks"

```json
{
  "options": {
    "group_by_document": true,
    "top_k": 5
  }
}
```

#### rerank

- **Type**: Boolean
- **Default**: false
- **Purpose**: Apply post-retrieval reranking
- **Algorithm**: Token overlap and semantic similarity boosting

```json
{
  "options": {
    "top_k": 50,
    "rerank": true,
    "threshold": 0.6
  }
}
```

#### ef_search (HNSW Index)

- **Type**: Integer
- **Default**: None (uses index default)
- **Purpose**: HNSW search parameter for recall/speed tradeoff
- **Range**: Typically 100-500
- **Effect**: Higher values = better recall, slower search

```json
{
  "options": {
    "ef_search": 200
  }
}
```

#### nprobe (IVF Index)

- **Type**: Integer
- **Default**: None (uses index default)
- **Purpose**: IVF search parameter for number of clusters to probe
- **Range**: Typically 1-100
- **Effect**: Higher values = better recall, slower search

```json
{
  "options": {
    "nprobe": 10
  }
}
```

#### max_distance

- **Type**: Float
- **Default**: None (no limit)
- **Purpose**: Maximum distance threshold (distance metrics only)
- **Usage**: Alternative to min_score for distance-based filtering

```json
{
  "options": {
    "max_distance": 0.5
  }
}
```

#### min_score

- **Type**: Float (0.0-1.0)
- **Default**: None (no limit)
- **Purpose**: Minimum similarity score (same as threshold)
- **Usage**: Alias for threshold parameter

---

## Metadata Filtering

### Overview

The DeepLake Query API supports sophisticated metadata filtering with complex boolean expressions, comparison operators, and nested conditions.

### Filter Structure

```python
filters: Union[Dict[str, Any], str]
```

Filters can be specified as:
1. **Simple dictionary**: `{"field": "value"}`
2. **Complex expressions**: Nested AND/OR/NOT operators
3. **SQL-like strings**: `"document_type = 'pdf' AND page_number > 5"`

### Simple Filters

**Exact Match**:
```json
{
  "filters": {
    "document_type": "pdf"
  }
}
```

**Multiple Fields** (implicit AND):
```json
{
  "filters": {
    "document_type": "pdf",
    "language": "en",
    "page_number": 5
  }
}
```

### Complex Filter Expressions

#### AND Operator

```json
{
  "filters": {
    "AND": [
      {"field": "document_type", "operator": "eq", "value": "pdf"},
      {"field": "page_number", "operator": "gte", "value": 1},
      {"field": "page_number", "operator": "lte", "value": 10}
    ]
  }
}
```

#### OR Operator

```json
{
  "filters": {
    "OR": [
      {"field": "document_type", "operator": "eq", "value": "pdf"},
      {"field": "document_type", "operator": "eq", "value": "docx"}
    ]
  }
}
```

#### NOT Operator

```json
{
  "filters": {
    "NOT": {
      "field": "document_type",
      "operator": "eq",
      "value": "txt"
    }
  }
}
```

#### Nested Expressions

```json
{
  "filters": {
    "AND": [
      {
        "OR": [
          {"field": "document_type", "operator": "eq", "value": "pdf"},
          {"field": "document_type", "operator": "eq", "value": "docx"}
        ]
      },
      {"field": "language", "operator": "eq", "value": "en"},
      {
        "AND": [
          {"field": "page_number", "operator": "gte", "value": 1},
          {"field": "page_number", "operator": "lte", "value": 100}
        ]
      }
    ]
  }
}
```

### Comparison Operators

| Operator | Description | Example |
|----------|-------------|---------|
| `eq` | Equal to | `{"field": "status", "operator": "eq", "value": "active"}` |
| `ne` | Not equal to | `{"field": "status", "operator": "ne", "value": "deleted"}` |
| `gt` | Greater than | `{"field": "page_number", "operator": "gt", "value": 5}` |
| `gte` | Greater than or equal | `{"field": "score", "operator": "gte", "value": 0.7}` |
| `lt` | Less than | `{"field": "page_number", "operator": "lt", "value": 100}` |
| `lte` | Less than or equal | `{"field": "file_size", "operator": "lte", "value": 1000000}` |
| `in` | In list | `{"field": "category", "operator": "in", "value": ["tech", "science"]}` |
| `nin` | Not in list | `{"field": "category", "operator": "nin", "value": ["spam", "ads"]}` |
| `contains` | String contains | `{"field": "title", "operator": "contains", "value": "climate"}` |
| `startswith` | String starts with | `{"field": "filename", "operator": "startswith", "value": "report_"}` |
| `endswith` | String ends with | `{"field": "filename", "operator": "endswith", "value": ".pdf"}` |
| `exists` | Field exists | `{"field": "optional_field", "operator": "exists", "value": true}` |

### SQL-Like Filter Strings

Alternative syntax using SQL-like expressions:

```json
{
  "filters": "document_type = 'pdf' AND page_number BETWEEN 1 AND 10 AND language = 'en'"
}
```

**Supported SQL Operators**:
- `=`, `!=`, `<>` (not equal)
- `>`, `>=`, `<`, `<=`
- `BETWEEN ... AND ...`
- `IN (...)`, `NOT IN (...)`
- `LIKE '%pattern%'`
- `IS NULL`, `IS NOT NULL`
- `AND`, `OR`, `NOT`

### Filter Implementation

```python
from app.services.metadata_filter import metadata_filter_service

# Parse filter expression
filter_expr = metadata_filter_service.parse_filter_expression(options.filters)

# Apply filter to results
filtered_results = []
for result in results:
    if metadata_filter_service.apply_filter(result.vector.metadata, filter_expr):
        filtered_results.append(result)
```

### Performance Considerations

- **Index Support**: Filters on indexed fields are fastest
- **Complex Expressions**: Nested AND/OR can be expensive
- **Post-Filtering**: Filters applied after initial retrieval
- **Recommendation**: Use simple filters when possible

---

## Fusion Methods

### Overview

Fusion methods combine results from multiple search strategies (vector + text) into a unified ranking. The DeepLake API supports five fusion algorithms, each with different characteristics.

### Fusion Method Enum

```python
class FusionMethod(Enum):
    """Methods for combining vector and text search results."""
    WEIGHTED_SUM = "weighted_sum"              # Simple weighted combination
    RRF = "reciprocal_rank_fusion"             # Reciprocal Rank Fusion
    CombSUM = "comb_sum"                       # CombSUM algorithm
    CombMNZ = "comb_mnz"                       # CombMNZ algorithm
    BORDA_COUNT = "borda_count"                # Borda count voting
```

### 1. Weighted Sum

**Algorithm**: Linear combination of normalized scores

```python
async def _weighted_sum_fusion(
    vector_results: List[SearchResultItem],
    text_results: List[TextSearchResult],
    vector_weight: float,
    text_weight: float
) -> List[SearchResultItem]:
    """Combine results using weighted sum."""

    # Normalize vector scores to [0, 1]
    vector_scores = [r.score for r in vector_results]
    max_v, min_v = max(vector_scores), min(vector_scores)

    for result in vector_results:
        normalized = (result.score - min_v) / (max_v - min_v) if max_v > min_v else 1.0
        combined_scores[result.id] = vector_weight * normalized

    # Normalize text scores to [0, 1]
    text_scores = [r.score for r in text_results]
    max_t, min_t = max(text_scores), min(text_scores)

    for result in text_results:
        normalized = (result.score - min_t) / (max_t - min_t) if max_t > min_t else 1.0
        combined_scores[result.id] += text_weight * normalized

    # Sort by combined score
    return sorted(combined_scores.items(), key=lambda x: x[1], reverse=True)
```

**Use Cases**:
- Balanced vector/text weighting
- Simple, interpretable scores
- Good general-purpose default

**Example**:
```json
{
  "fusion_method": "weighted_sum",
  "vector_weight": 0.6,
  "text_weight": 0.4
}
```

### 2. Reciprocal Rank Fusion (RRF)

**Algorithm**: Reciprocal rank based scoring

```python
async def _rrf_fusion(
    vector_results: List[SearchResultItem],
    text_results: List[TextSearchResult],
    vector_weight: float,
    text_weight: float,
    k: int = 60
) -> List[SearchResultItem]:
    """Reciprocal Rank Fusion (RRF)."""

    rrf_scores = {}

    # Vector results
    for i, result in enumerate(vector_results):
        rrf_scores[result.id] = vector_weight / (k + i + 1)

    # Text results
    for i, result in enumerate(text_results):
        if result.id in rrf_scores:
            rrf_scores[result.id] += text_weight / (k + i + 1)
        else:
            rrf_scores[result.id] = text_weight / (k + i + 1)

    return sorted(rrf_scores.items(), key=lambda x: x[1], reverse=True)
```

**Formula**:
```
RRF(d) = Σ (weight_i / (k + rank_i(d)))
```

Where:
- `d` = document
- `k` = constant (typically 60)
- `rank_i(d)` = rank of document d in result set i
- `weight_i` = weight for result set i

**Use Cases**:
- Robust to outlier scores
- Position-based ranking
- Works well with heterogeneous result sets

**Example**:
```json
{
  "fusion_method": "reciprocal_rank_fusion",
  "vector_weight": 0.5,
  "text_weight": 0.5
}
```

### 3. CombSUM

**Algorithm**: Direct sum of scores without normalization

```python
async def _combsum_fusion(
    vector_results: List[SearchResultItem],
    text_results: List[TextSearchResult],
    vector_weight: float,
    text_weight: float
) -> List[SearchResultItem]:
    """CombSUM fusion algorithm."""

    combined_scores = {}

    for result in vector_results:
        combined_scores[result.id] = vector_weight * result.score

    for result in text_results:
        if result.id in combined_scores:
            combined_scores[result.id] += text_weight * result.score
        else:
            combined_scores[result.id] = text_weight * result.score

    return sorted(combined_scores.items(), key=lambda x: x[1], reverse=True)
```

**Use Cases**:
- Raw score preservation
- When scores are already normalized
- Simple aggregation

**Example**:
```json
{
  "fusion_method": "comb_sum",
  "vector_weight": 0.7,
  "text_weight": 0.3
}
```

### 4. CombMNZ

**Algorithm**: CombSUM multiplied by number of non-zero scores

```python
async def _combmnz_fusion(
    vector_results: List[SearchResultItem],
    text_results: List[TextSearchResult],
    vector_weight: float,
    text_weight: float
) -> List[SearchResultItem]:
    """CombMNZ fusion algorithm."""

    combined_scores = {}
    non_zero_counts = {}

    for result in vector_results:
        if result.score > 0:
            combined_scores[result.id] = vector_weight * result.score
            non_zero_counts[result.id] = 1

    for result in text_results:
        if result.score > 0:
            if result.id in combined_scores:
                combined_scores[result.id] += text_weight * result.score
                non_zero_counts[result.id] += 1
            else:
                combined_scores[result.id] = text_weight * result.score
                non_zero_counts[result.id] = 1

    # Multiply by number of non-zero scores
    for doc_id in combined_scores:
        combined_scores[doc_id] *= non_zero_counts[doc_id]

    return sorted(combined_scores.items(), key=lambda x: x[1], reverse=True)
```

**Use Cases**:
- Favor documents appearing in multiple result sets
- Boost consensus results
- Penalize single-source matches

**Example**:
```json
{
  "fusion_method": "comb_mnz",
  "vector_weight": 0.5,
  "text_weight": 0.5
}
```

### 5. Borda Count

**Algorithm**: Rank-based voting system

```python
async def _borda_count_fusion(
    vector_results: List[SearchResultItem],
    text_results: List[TextSearchResult],
    vector_weight: float,
    text_weight: float
) -> List[SearchResultItem]:
    """Borda count voting fusion."""

    borda_scores = {}

    # Vector results: higher rank = more points
    for i, result in enumerate(vector_results):
        points = len(vector_results) - i
        borda_scores[result.id] = vector_weight * points

    # Text results
    for i, result in enumerate(text_results):
        points = len(text_results) - i
        if result.id in borda_scores:
            borda_scores[result.id] += text_weight * points
        else:
            borda_scores[result.id] = text_weight * points

    return sorted(borda_scores.items(), key=lambda x: x[1], reverse=True)
```

**Use Cases**:
- Democratic voting approach
- Position-based aggregation
- Resistant to score manipulation

**Example**:
```json
{
  "fusion_method": "borda_count",
  "vector_weight": 0.5,
  "text_weight": 0.5
}
```

### Fusion Method Comparison

| Method | Pros | Cons | Best For |
|--------|------|------|----------|
| **Weighted Sum** | Simple, interpretable | Sensitive to score scales | General purpose |
| **RRF** | Robust, position-based | Less granular | Heterogeneous scores |
| **CombSUM** | Fast, straightforward | Requires normalized scores | Pre-normalized data |
| **CombMNZ** | Favors consensus | Penalizes single-source | Multi-source validation |
| **Borda Count** | Fair voting | Ignores score magnitude | Democratic ranking |

### Choosing a Fusion Method

**Use Weighted Sum when**:
- You want balanced control via weights
- Scores are well-calibrated
- Simplicity is preferred

**Use RRF when**:
- Dealing with heterogeneous score distributions
- Position matters more than absolute scores
- Robustness is critical

**Use CombMNZ when**:
- You want to boost documents appearing in multiple result sets
- Consensus is important
- Single-source results should be penalized

**Use Borda Count when**:
- Fairness and democratic ranking is desired
- Relative position is more important than scores

---

## Response Format

### SearchResponse Structure

```python
class SearchResponse(BaseModel):
    """Search response model."""

    results: List[SearchResultItem]
    total_found: int
    has_more: bool
    query_time_ms: float
    embedding_time_ms: float = 0.0
    stats: SearchStats
```

### SearchResultItem Structure

```python
class SearchResultItem(BaseModel):
    """Single search result item."""

    vector: VectorResponse
    score: float
    distance: float
    rank: int
    explanation: Optional[Dict[str, str]] = None
```

### SearchStats Structure

```python
class SearchStats(BaseModel):
    """Search statistics model."""

    vectors_scanned: int
    index_hits: int
    filtered_results: int
    reranking_time_ms: float = 0.0
    database_time_ms: float = 0.0
    post_processing_time_ms: float = 0.0
```

### Complete Response Example

```json
{
  "results": [
    {
      "vector": {
        "id": "vec_abc123",
        "dataset_id": "ds_456",
        "document_id": "doc_789",
        "chunk_id": "chunk_001",
        "values": [0.123, -0.456, 0.789, ...],
        "content": "This is the matched content from the document...",
        "content_hash": "sha256:abcd1234...",
        "metadata": {
          "document_type": "pdf",
          "document_name": "research_paper.pdf",
          "page_number": 5,
          "language": "en",
          "author": "John Doe",
          "publication_date": "2025-12-01"
        },
        "content_type": "text/plain",
        "language": "en",
        "chunk_index": 0,
        "chunk_count": 10,
        "model": "text-embedding-ada-002",
        "dimensions": 1536,
        "created_at": "2026-01-05T14:30:00Z",
        "updated_at": "2026-01-05T14:30:00Z",
        "tenant_id": "tenant_123"
      },
      "score": 0.95,
      "distance": 0.05,
      "rank": 1,
      "explanation": {
        "method": "hybrid_search",
        "vector_score": "0.92",
        "text_score": "0.97",
        "fusion_method": "weighted_sum",
        "vector_weight": "0.5",
        "text_weight": "0.5"
      }
    },
    {
      "vector": {...},
      "score": 0.89,
      "distance": 0.11,
      "rank": 2
    }
  ],
  "total_found": 42,
  "has_more": true,
  "query_time_ms": 156.8,
  "embedding_time_ms": 85.2,
  "stats": {
    "vectors_scanned": 15000,
    "index_hits": 200,
    "filtered_results": 42,
    "reranking_time_ms": 12.5,
    "database_time_ms": 110.3,
    "post_processing_time_ms": 46.5
  }
}
```

### Field Descriptions

#### results

Array of `SearchResultItem` objects, sorted by score (descending).

#### total_found

Total number of results matching the query (before `top_k` limit).

#### has_more

Boolean indicating if there are more results beyond `top_k`.

#### query_time_ms

Total query execution time in milliseconds.

#### embedding_time_ms

Time spent generating embeddings (for text search only).

#### stats.vectors_scanned

Number of vectors examined during search.

#### stats.index_hits

Number of vectors retrieved from index before filtering.

#### stats.filtered_results

Number of results after applying filters.

#### stats.reranking_time_ms

Time spent on reranking (if enabled).

#### stats.database_time_ms

Time spent on database operations.

#### stats.post_processing_time_ms

Time spent on filtering, deduplication, etc.

---

## Query Optimization

### Caching Strategy

The DeepLake API implements multi-level caching for query optimization:

#### Query Result Caching

```python
# Cache key generation
query_hash = hashlib.sha256(str(query_vector).encode()).hexdigest()[:16]
options_hash = hashlib.sha256(options.model_dump_json().encode()).hexdigest()[:16]

# Try cache first
cached_results = await cache_manager.get_search_results(
    dataset_id, query_hash, options_hash, tenant_id
)

if cached_results:
    metrics_service.record_cache_operation("get", "hit")
    return SearchResponse.model_validate(cached_results)

# Cache miss - perform search
results = await perform_search(...)

# Cache the results
await cache_manager.cache_search_results(
    dataset_id, query_hash, options_hash,
    [results.model_dump()], tenant_id
)
```

#### Cache Configuration

```python
# Redis cache settings
REDIS_CACHE_TTL = 3600  # 1 hour
REDIS_SEARCH_CACHE_PREFIX = "search:"
REDIS_EMBEDDING_CACHE_PREFIX = "embedding:"
```

### Index Optimization

#### HNSW Index Tuning

```json
{
  "options": {
    "ef_search": 200,
    "top_k": 10
  }
}
```

**Guidelines**:
- `ef_search >= top_k` (recommended 2-4x)
- Higher `ef_search` = better recall, slower queries
- Typical range: 100-500

#### IVF Index Tuning

```json
{
  "options": {
    "nprobe": 10,
    "top_k": 10
  }
}
```

**Guidelines**:
- `nprobe` controls recall/speed tradeoff
- More probes = better recall, slower queries
- Typical range: 1-100

### Query Batching

For multiple similar queries, use batching:

```python
async def batch_search(queries: List[str]) -> List[SearchResponse]:
    """Batch search for multiple queries."""
    tasks = [
        search_text(dataset_id, query, options)
        for query in queries
    ]
    return await asyncio.gather(*tasks)
```

### Parallel Search

Execute searches in parallel for hybrid queries:

```python
# Parallel execution
vector_results, text_results = await asyncio.gather(
    _vector_search(...),
    _text_search(...),
    return_exceptions=True
)
```

### Filter Optimization

**Pre-Filter** (faster):
```python
# Filter at database level
results = await db.search(query_vector, filters={"status": "active"})
```

**Post-Filter** (more flexible):
```python
# Filter after retrieval
all_results = await db.search(query_vector)
filtered = [r for r in all_results if r.metadata["status"] == "active"]
```

### Performance Best Practices

1. **Use Appropriate top_k**: Don't request more results than needed
2. **Enable Caching**: Leverage Redis for frequent queries
3. **Optimize Filters**: Use indexed fields in filters
4. **Batch Requests**: Group similar queries when possible
5. **Tune Index Parameters**: Adjust `ef_search`/`nprobe` based on requirements
6. **Monitor Metrics**: Track `query_time_ms` and adjust accordingly

---

## Error Handling

### Error Response Format

```json
{
  "success": false,
  "error_code": "DATASET_NOT_FOUND",
  "message": "Dataset 'dataset_123' not found",
  "details": {
    "dataset_id": "dataset_123",
    "tenant_id": "tenant_456"
  },
  "request_id": "req_789abc",
  "timestamp": "2026-01-06T10:30:00Z"
}
```

### Common Error Codes

| Error Code | HTTP Status | Description | Resolution |
|------------|-------------|-------------|------------|
| `DATASET_NOT_FOUND` | 404 | Dataset doesn't exist | Verify dataset_id and tenant access |
| `INVALID_DIMENSIONS` | 400 | Query vector dimension mismatch | Check vector dimensions match dataset |
| `INVALID_SEARCH_PARAMS` | 400 | Invalid search parameters | Validate request parameters |
| `EMBEDDING_FAILED` | 400 | Failed to generate embedding | Check embedding service health |
| `AUTHENTICATION_FAILED` | 401 | Invalid or missing token | Provide valid JWT token |
| `AUTHORIZATION_FAILED` | 403 | Insufficient permissions | Check tenant/space access |
| `RATE_LIMIT_EXCEEDED` | 429 | Too many requests | Implement backoff and retry |
| `INTERNAL_ERROR` | 500 | Internal server error | Contact support |

### Error Handling Example

```python
async def safe_search(dataset_id: str, query: str):
    """Search with comprehensive error handling."""
    try:
        response = await client.post(
            f"{base_url}/datasets/{dataset_id}/search/text",
            json={"query_text": query},
            headers={"Authorization": f"Bearer {token}"}
        )
        response.raise_for_status()
        return response.json()

    except httpx.HTTPStatusError as e:
        if e.response.status_code == 404:
            logger.error(f"Dataset not found: {dataset_id}")
            raise DatasetNotFound(dataset_id)
        elif e.response.status_code == 400:
            error_data = e.response.json()
            logger.error(f"Invalid request: {error_data['message']}")
            raise InvalidRequest(error_data['message'])
        elif e.response.status_code == 429:
            logger.warning("Rate limit exceeded, retrying...")
            await asyncio.sleep(5)
            return await safe_search(dataset_id, query)
        else:
            logger.error(f"HTTP error: {e}")
            raise

    except httpx.RequestError as e:
        logger.error(f"Request failed: {e}")
        raise ConnectionError(f"Failed to connect to DeepLake API: {e}")
```

### Retry Strategy

```python
from tenacity import retry, stop_after_attempt, wait_exponential

@retry(
    stop=stop_after_attempt(3),
    wait=wait_exponential(multiplier=1, min=4, max=10),
    retry=retry_if_exception_type(httpx.HTTPStatusError),
    before_sleep=before_sleep_log(logger, logging.WARNING)
)
async def search_with_retry(dataset_id: str, query_vector: List[float]):
    """Search with automatic retry on transient failures."""
    response = await client.post(
        f"{base_url}/datasets/{dataset_id}/search",
        json={"query_vector": query_vector}
    )
    response.raise_for_status()
    return response.json()
```

---

## Performance Considerations

### Latency Breakdown

Typical query latency for 1M vector dataset:

| Component | Latency | Percentage |
|-----------|---------|------------|
| Embedding Generation | 50-100ms | 30-40% |
| Vector Search | 20-50ms | 15-25% |
| Metadata Filtering | 10-30ms | 10-15% |
| Result Serialization | 5-15ms | 5-10% |
| Network Overhead | 10-20ms | 10-15% |
| **Total** | **95-215ms** | **100%** |

### Scalability Metrics

| Dataset Size | Query Latency | Throughput | Memory Usage |
|--------------|---------------|------------|--------------|
| 10K vectors | 10-20ms | 500 qps | 1-2 GB |
| 100K vectors | 20-40ms | 300 qps | 5-10 GB |
| 1M vectors | 40-100ms | 100 qps | 20-40 GB |
| 10M vectors | 100-300ms | 30 qps | 100-200 GB |

### Optimization Tips

#### 1. Reduce Embedding Overhead

```python
# Pre-compute embeddings for common queries
COMMON_QUERIES = {
    "climate change": [0.123, -0.456, ...],
    "machine learning": [0.789, 0.234, ...]
}

query_vector = COMMON_QUERIES.get(query_text)
if not query_vector:
    query_vector = await embedding_service.text_to_vector(query_text)
```

#### 2. Use Selective Field Inclusion

```json
{
  "options": {
    "include_content": false,
    "include_metadata": true
  }
}
```

#### 3. Implement Result Pagination

```python
# First page
response = await search(dataset_id, query, top_k=20)

# Subsequent pages (if supported)
next_response = await search(dataset_id, query, top_k=20, offset=20)
```

#### 4. Cache Frequent Queries

```python
# Application-level caching
@lru_cache(maxsize=1000)
def get_query_embedding(query: str) -> List[float]:
    return embedding_service.text_to_vector(query)
```

#### 5. Optimize Filters

```python
# Use indexed fields in filters
good_filter = {"document_type": "pdf"}  # Indexed field

# Avoid complex nested filters on non-indexed fields
bad_filter = {
    "AND": [
        {"field": "custom_field_1", "operator": "contains", "value": "xyz"},
        {"field": "custom_field_2", "operator": "gte", "value": 100}
    ]
}
```

### Monitoring and Metrics

Track these metrics for query performance:

```python
# Record query metrics
metrics_service.record_search_query(
    dataset_id=dataset_id,
    search_type="hybrid",
    query_time_ms=query_time,
    results_count=len(results),
    vectors_scanned=stats.vectors_scanned,
    tenant_id=tenant_id
)

# Track cache performance
cache_hit_ratio = cache_hits / (cache_hits + cache_misses)
```

---

## Code Examples

### Python Client

```python
import httpx
from typing import List, Optional

class DeepLakeClient:
    """DeepLake API client."""

    def __init__(self, base_url: str, api_key: str, tenant_id: str):
        self.base_url = base_url
        self.api_key = api_key
        self.tenant_id = tenant_id
        self.client = httpx.AsyncClient(
            headers={
                "Authorization": f"Bearer {api_key}",
                "X-Tenant-ID": tenant_id
            }
        )

    async def vector_search(
        self,
        dataset_id: str,
        query_vector: List[float],
        top_k: int = 10,
        threshold: Optional[float] = None,
        filters: Optional[dict] = None
    ) -> dict:
        """Perform vector similarity search."""
        response = await self.client.post(
            f"{self.base_url}/datasets/{dataset_id}/search",
            json={
                "query_vector": query_vector,
                "options": {
                    "top_k": top_k,
                    "threshold": threshold,
                    "filters": filters
                }
            }
        )
        response.raise_for_status()
        return response.json()

    async def text_search(
        self,
        dataset_id: str,
        query_text: str,
        top_k: int = 10,
        threshold: Optional[float] = None,
        filters: Optional[dict] = None
    ) -> dict:
        """Perform text-based semantic search."""
        response = await self.client.post(
            f"{self.base_url}/datasets/{dataset_id}/search/text",
            json={
                "query_text": query_text,
                "options": {
                    "top_k": top_k,
                    "threshold": threshold,
                    "filters": filters
                }
            }
        )
        response.raise_for_status()
        return response.json()

    async def hybrid_search(
        self,
        dataset_id: str,
        query_text: str,
        query_vector: Optional[List[float]] = None,
        vector_weight: float = 0.5,
        text_weight: float = 0.5,
        fusion_method: str = "weighted_sum",
        top_k: int = 10,
        filters: Optional[dict] = None
    ) -> dict:
        """Perform hybrid search."""
        response = await self.client.post(
            f"{self.base_url}/datasets/{dataset_id}/search/hybrid",
            json={
                "query_text": query_text,
                "query_vector": query_vector,
                "vector_weight": vector_weight,
                "text_weight": text_weight,
                "fusion_method": fusion_method,
                "options": {
                    "top_k": top_k,
                    "filters": filters
                }
            }
        )
        response.raise_for_status()
        return response.json()

# Usage
client = DeepLakeClient(
    base_url="http://localhost:8000",
    api_key="your-api-key",
    tenant_id="tenant_123"
)

# Vector search
results = await client.vector_search(
    dataset_id="ds_456",
    query_vector=embedding,
    top_k=10,
    threshold=0.7
)

# Text search
results = await client.text_search(
    dataset_id="ds_456",
    query_text="climate change impacts",
    top_k=15
)

# Hybrid search
results = await client.hybrid_search(
    dataset_id="ds_456",
    query_text="machine learning",
    vector_weight=0.6,
    text_weight=0.4,
    fusion_method="reciprocal_rank_fusion"
)
```

### TypeScript Client

```typescript
interface SearchOptions {
  top_k?: number;
  threshold?: number;
  filters?: Record<string, any>;
  include_content?: boolean;
  include_metadata?: boolean;
}

interface SearchResponse {
  results: SearchResultItem[];
  total_found: number;
  has_more: boolean;
  query_time_ms: number;
  embedding_time_ms?: number;
  stats: SearchStats;
}

class DeepLakeClient {
  private baseUrl: string;
  private apiKey: string;
  private tenantId: string;

  constructor(baseUrl: string, apiKey: string, tenantId: string) {
    this.baseUrl = baseUrl;
    this.apiKey = apiKey;
    this.tenantId = tenantId;
  }

  async vectorSearch(
    datasetId: string,
    queryVector: number[],
    options?: SearchOptions
  ): Promise<SearchResponse> {
    const response = await fetch(
      `${this.baseUrl}/datasets/${datasetId}/search`,
      {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${this.apiKey}`,
          'X-Tenant-ID': this.tenantId
        },
        body: JSON.stringify({
          query_vector: queryVector,
          options: options || {}
        })
      }
    );

    if (!response.ok) {
      throw new Error(`Search failed: ${response.statusText}`);
    }

    return response.json();
  }

  async textSearch(
    datasetId: string,
    queryText: string,
    options?: SearchOptions
  ): Promise<SearchResponse> {
    const response = await fetch(
      `${this.baseUrl}/datasets/${datasetId}/search/text`,
      {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${this.apiKey}`,
          'X-Tenant-ID': this.tenantId
        },
        body: JSON.stringify({
          query_text: queryText,
          options: options || {}
        })
      }
    );

    if (!response.ok) {
      throw new Error(`Text search failed: ${response.statusText}`);
    }

    return response.json();
  }

  async hybridSearch(
    datasetId: string,
    queryText: string,
    vectorWeight: number = 0.5,
    textWeight: number = 0.5,
    fusionMethod: string = 'weighted_sum',
    options?: SearchOptions
  ): Promise<SearchResponse> {
    const response = await fetch(
      `${this.baseUrl}/datasets/${datasetId}/search/hybrid`,
      {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${this.apiKey}`,
          'X-Tenant-ID': this.tenantId
        },
        body: JSON.stringify({
          query_text: queryText,
          vector_weight: vectorWeight,
          text_weight: textWeight,
          fusion_method: fusionMethod,
          options: options || {}
        })
      }
    );

    if (!response.ok) {
      throw new Error(`Hybrid search failed: ${response.statusText}`);
    }

    return response.json();
  }
}

// Usage
const client = new DeepLakeClient(
  'http://localhost:8000',
  'your-api-key',
  'tenant_123'
);

// Perform search
const results = await client.textSearch(
  'ds_456',
  'climate change',
  {
    top_k: 10,
    threshold: 0.7,
    filters: {
      document_type: 'pdf',
      language: 'en'
    }
  }
);
```

### Go Client

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
)

type SearchRequest struct {
    QueryVector []float64     `json:"query_vector,omitempty"`
    QueryText   string        `json:"query_text,omitempty"`
    Options     SearchOptions `json:"options,omitempty"`
}

type SearchOptions struct {
    TopK            int                    `json:"top_k,omitempty"`
    Threshold       *float64               `json:"threshold,omitempty"`
    Filters         map[string]interface{} `json:"filters,omitempty"`
    IncludeContent  bool                   `json:"include_content"`
    IncludeMetadata bool                   `json:"include_metadata"`
}

type SearchResponse struct {
    Results         []SearchResultItem `json:"results"`
    TotalFound      int                `json:"total_found"`
    HasMore         bool               `json:"has_more"`
    QueryTimeMs     float64            `json:"query_time_ms"`
    EmbeddingTimeMs float64            `json:"embedding_time_ms,omitempty"`
    Stats           SearchStats        `json:"stats"`
}

type DeepLakeClient struct {
    BaseURL  string
    APIKey   string
    TenantID string
    Client   *http.Client
}

func NewDeepLakeClient(baseURL, apiKey, tenantID string) *DeepLakeClient {
    return &DeepLakeClient{
        BaseURL:  baseURL,
        APIKey:   apiKey,
        TenantID: tenantID,
        Client:   &http.Client{},
    }
}

func (c *DeepLakeClient) VectorSearch(
    datasetID string,
    queryVector []float64,
    options SearchOptions,
) (*SearchResponse, error) {
    request := SearchRequest{
        QueryVector: queryVector,
        Options:     options,
    }

    body, err := json.Marshal(request)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal request: %w", err)
    }

    url := fmt.Sprintf("%s/datasets/%s/search", c.BaseURL, datasetID)
    req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }

    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+c.APIKey)
    req.Header.Set("X-Tenant-ID", c.TenantID)

    resp, err := c.Client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("request failed: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("search failed with status: %d", resp.StatusCode)
    }

    var searchResp SearchResponse
    if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
        return nil, fmt.Errorf("failed to decode response: %w", err)
    }

    return &searchResp, nil
}

func (c *DeepLakeClient) TextSearch(
    datasetID string,
    queryText string,
    options SearchOptions,
) (*SearchResponse, error) {
    request := SearchRequest{
        QueryText: queryText,
        Options:   options,
    }

    body, err := json.Marshal(request)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal request: %w", err)
    }

    url := fmt.Sprintf("%s/datasets/%s/search/text", c.BaseURL, datasetID)
    req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }

    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+c.APIKey)
    req.Header.Set("X-Tenant-ID", c.TenantID)

    resp, err := c.Client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("request failed: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("text search failed with status: %d", resp.StatusCode)
    }

    var searchResp SearchResponse
    if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
        return nil, fmt.Errorf("failed to decode response: %w", err)
    }

    return &searchResp, nil
}

// Usage
func main() {
    client := NewDeepLakeClient(
        "http://localhost:8000",
        "your-api-key",
        "tenant_123",
    )

    threshold := 0.7
    results, err := client.TextSearch(
        "ds_456",
        "machine learning",
        SearchOptions{
            TopK:      10,
            Threshold: &threshold,
            Filters: map[string]interface{}{
                "language": "en",
            },
            IncludeContent:  true,
            IncludeMetadata: true,
        },
    )
    if err != nil {
        panic(err)
    }

    fmt.Printf("Found %d results\n", results.TotalFound)
}
```

---

## Related Documentation

### Internal Documentation

- [Embedding Structure](./vectors/embedding-structure.md) - Vector format and specifications
- [Dataset Organization](./datasets/dataset-organization.md) - Dataset structure and management
- [Embedding Models](./embeddings/model-configs.md) - Supported embedding models and configurations

### Cross-Service Integration

- [Document Upload Flow](../cross-service/flows/document-upload.md) - End-to-end document processing
- [ID Mapping Chain](../cross-service/mappings/id-mapping-chain.md) - Cross-service ID relationships
- [Platform ERD](../cross-service/diagrams/platform-erd.md) - Complete data model

### API Documentation

- [DeepLake HTTP API](../../deeplake-api/README.md) - Complete API reference
- [DeepLake gRPC API](../../deeplake-api/grpc/README.md) - gRPC service definitions
- [Authentication](../keycloak/tokens/jwt-structure.md) - JWT token structure

### Service Documentation

- [Aether Backend](../../aether-be/README.md) - Document management service
- [AudiModal](../../audimodal/README.md) - Content extraction service
- [LLM Router](../../tas-llm-router/README.md) - LLM routing service

---

**Document Version**: 1.0.0
**Last Updated**: 2026-01-06
**Maintained By**: TAS Platform Team
**Next Review**: 2026-02-06
