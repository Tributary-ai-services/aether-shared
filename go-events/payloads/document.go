// Package payloads defines typed payload structs for each TAS event family.
// These are marshaled into the CloudEvents "data" field.
package payloads

// CE types for document activity events.
const (
	TypeDocumentUploaded  = "com.tas.activity.document.uploaded"
	TypeDocumentProcessed = "com.tas.activity.document.processed"
	TypeDocumentFailed    = "com.tas.activity.document.failed"
)

type DocumentUploaded struct {
	FileID    string `json:"file_id"`
	FileName  string `json:"file_name"`
	SizeBytes int64  `json:"size_bytes"`
	MimeType  string `json:"mime_type,omitempty"`
	Source    string `json:"source,omitempty"`
}

type DocumentProcessed struct {
	FileID     string `json:"file_id"`
	FileName   string `json:"file_name,omitempty"`
	ChunkCount int    `json:"chunk_count,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

type DocumentFailed struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	Stage    string `json:"stage,omitempty"`
	Error    string `json:"error"`
}
