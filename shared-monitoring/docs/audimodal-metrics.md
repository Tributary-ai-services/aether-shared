# Audimodal Application Metrics
# Auto-generated from audimodal codebase

## Storage Metrics
audimodal_storage_total_bytes{tier="hot|warm|cold",type="document|chunk|embedding"} - Total storage usage in bytes
audimodal_storage_total_files{tier="hot|warm|cold",type="document|chunk|embedding"} - Total number of files
audimodal_storage_throughput_mbps - Storage throughput in MB/s
audimodal_storage_iops - Storage I/O operations per second
audimodal_storage_usage_percent - Storage usage percentage
audimodal_storage_monthly_cost_change_percent - Monthly cost change percentage

## Processing Metrics
audimodal_processing_queue_size{processor="tier|embedding|analysis"} - Current queue size
audimodal_processing_items_processed_total{processor="tier|embedding|analysis"} - Total items processed
audimodal_processing_errors_total{processor="tier|embedding|analysis"} - Total processing errors
audimodal_processing_duration_seconds{processor="tier|embedding|analysis"} - Processing duration histogram

## Document Metrics
audimodal_documents_total{status="active|deleted|archived"} - Total documents by status
audimodal_documents_size_bytes - Document size distribution
audimodal_documents_chunks_total - Total chunks per document
audimodal_documents_processing_time_seconds - Document processing time

## Embedding Metrics
audimodal_embeddings_generated_total{model="openai|cohere|local"} - Total embeddings generated
audimodal_embeddings_cache_hits_total - Embedding cache hit count
audimodal_embeddings_cache_misses_total - Embedding cache miss count
audimodal_embeddings_generation_duration_seconds - Embedding generation time
audimodal_embeddings_batch_size - Embedding batch size distribution

## ML Analysis Metrics
audimodal_ml_analysis_requests_total{type="classification|extraction|summarization"} - ML analysis requests
audimodal_ml_analysis_success_total - Successful ML analyses
audimodal_ml_analysis_failure_total - Failed ML analyses
audimodal_ml_analysis_duration_seconds - ML analysis duration

## Sync Metrics
audimodal_sync_operations_total{source="sharepoint|onedrive|gdrive",status="success|failure"} - Sync operations
audimodal_sync_files_synced_total - Total files synced
audimodal_sync_bytes_synced_total - Total bytes synced
audimodal_sync_duration_seconds - Sync duration
audimodal_sync_errors_total{error_type="auth|network|rate_limit"} - Sync errors by type

## DLP Metrics
audimodal_dlp_violations_total{severity="critical|high|medium|low"} - DLP violations by severity
audimodal_dlp_policies_evaluated_total - Total DLP policies evaluated
audimodal_dlp_scan_duration_seconds - DLP scan duration
audimodal_dlp_false_positives_total - DLP false positives

## API Metrics
audimodal_api_requests_total{method="GET|POST|PUT|DELETE",endpoint="/api/v1/*"} - API requests
audimodal_api_request_duration_seconds{method="GET|POST|PUT|DELETE"} - API request duration
audimodal_api_errors_total{status="4xx|5xx"} - API errors by status code
audimodal_api_auth_failures_total - Authentication failures

## System Metrics
audimodal_database_connections{status="active|idle|waiting"} - Database connection pool
audimodal_database_query_duration_seconds{query_type="select|insert|update|delete"} - Query duration
audimodal_cache_hits_total{cache="document|embedding|metadata"} - Cache hits
audimodal_cache_misses_total{cache="document|embedding|metadata"} - Cache misses
audimodal_memory_usage_bytes - Memory usage
audimodal_goroutines_total - Active goroutines
