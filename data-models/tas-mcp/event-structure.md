# TAS-MCP - Event Structure

## Metadata

- **Document Type**: Data Model Documentation
- **Service**: TAS-MCP
- **Component**: Event System
- **Last Updated**: 2026-01-06
- **Owner**: TAS Platform Team
- **Status**: Active

---

## Overview

### Purpose

This document defines the event structure used throughout the TAS-MCP system for inter-service communication, event routing, and subscription patterns. Events are the primary mechanism for asynchronous communication between federated MCP servers.

### Event Characteristics

- **Immutable**: Events are write-once, read-many
- **Timestamped**: All events have precise timestamps
- **Typed**: Events are categorized by type for filtering
- **Sourced**: Events track their originating service
- **Metadata-Rich**: Events carry contextual metadata
- **JSON Payload**: Event data encoded as JSON

---

## Table of Contents

1. [Event Structure](#event-structure)
2. [Event Types](#event-types)
3. [Event Metadata](#event-metadata)
4. [Event Payload](#event-payload)
5. [Event Routing](#event-routing)
6. [Event Subscriptions](#event-subscriptions)
7. [Event Persistence](#event-persistence)
8. [Usage Examples](#usage-examples)
9. [Related Documentation](#related-documentation)

---

## Event Structure

### Core Event Model

```go
type Event struct {
    EventID   string            `json:"event_id"`
    EventType string            `json:"event_type"`
    Source    string            `json:"source"`
    Timestamp int64             `json:"timestamp"`
    Data      string            `json:"data"`
    Metadata  map[string]string `json:"metadata"`
}
```

### TypeScript Definition

```typescript
interface Event {
  event_id: string;
  event_type: string;
  source: string;
  timestamp: number;
  data: string; // JSON-encoded
  metadata: Record<string, string>;
}
```

### Python Definition

```python
from typing import Dict
from dataclasses import dataclass

@dataclass
class Event:
    event_id: str
    event_type: str
    source: str
    timestamp: int
    data: str  # JSON-encoded
    metadata: Dict[str, str]
```

### Field Descriptions

#### event_id
- **Type**: String (UUID)
- **Purpose**: Unique event identifier
- **Format**: `evt_[random]` or UUID v4
- **Example**: `"evt_abc123def456"`

#### event_type
- **Type**: String
- **Purpose**: Event classification
- **Format**: `{domain}.{action}` (dot notation)
- **Examples**:
  - `"document.uploaded"`
  - `"user.created"`
  - `"space.updated"`
  - `"processing.completed"`

#### source
- **Type**: String
- **Purpose**: Originating service identifier
- **Examples**:
  - `"aether-be"`
  - `"audimodal"`
  - `"deeplake-api"`
  - `"tas-agent-builder"`

#### timestamp
- **Type**: Integer (int64)
- **Purpose**: Event creation time
- **Unit**: Milliseconds since Unix epoch
- **Example**: `1704537600000`

#### data
- **Type**: String (JSON)
- **Purpose**: Event payload data
- **Format**: JSON-encoded object
- **Example**: `"{\"document_id\":\"doc_456\",\"name\":\"report.pdf\"}"`

#### metadata
- **Type**: Map<string, string>
- **Purpose**: Contextual metadata
- **Common Keys**:
  - `tenant_id` - Multi-tenancy identifier
  - `space_id` - Space identifier
  - `user_id` - User identifier
  - `correlation_id` - Request correlation
  - `trace_id` - Distributed tracing

---

## Event Types

### Document Events

```
document.uploaded
document.processed
document.updated
document.deleted
document.shared
document.chunked
document.embedded
```

**Example**:
```json
{
  "event_type": "document.uploaded",
  "data": {
    "document_id": "doc_456",
    "name": "report.pdf",
    "size": 1024000,
    "mime_type": "application/pdf"
  }
}
```

### User Events

```
user.created
user.updated
user.deleted
user.authenticated
user.onboarded
```

**Example**:
```json
{
  "event_type": "user.created",
  "data": {
    "user_id": "user_789",
    "email": "user@example.com",
    "keycloak_id": "kc_123"
  }
}
```

### Space Events

```
space.created
space.updated
space.deleted
space.member_added
space.member_removed
```

**Example**:
```json
{
  "event_type": "space.created",
  "data": {
    "space_id": "space_123",
    "name": "My Workspace",
    "type": "personal",
    "tenant_id": "tenant_789"
  }
}
```

### Processing Events

```
processing.started
processing.completed
processing.failed
processing.chunk_created
processing.embedding_created
```

**Example**:
```json
{
  "event_type": "processing.completed",
  "data": {
    "document_id": "doc_456",
    "chunk_count": 42,
    "processing_time_ms": 15000
  }
}
```

### Agent Events

```
agent.created
agent.updated
agent.deleted
agent.executed
agent.execution_completed
agent.execution_failed
```

**Example**:
```json
{
  "event_type": "agent.executed",
  "data": {
    "agent_id": "agent_123",
    "execution_id": "exec_456",
    "input": "Analyze this document",
    "started_at": 1704537600000
  }
}
```

---

## Event Metadata

### Standard Metadata Fields

```go
type EventMetadata struct {
    TenantID      string `json:"tenant_id"`
    SpaceID       string `json:"space_id"`
    UserID        string `json:"user_id"`
    CorrelationID string `json:"correlation_id"`
    TraceID       string `json:"trace_id"`
    Priority      string `json:"priority"`
    Version       string `json:"version"`
}
```

### Metadata Usage

**Multi-Tenancy**:
```json
{
  "metadata": {
    "tenant_id": "tenant_789",
    "space_id": "space_123"
  }
}
```

**Request Correlation**:
```json
{
  "metadata": {
    "correlation_id": "req_abc123",
    "trace_id": "trace_xyz789"
  }
}
```

**Priority Handling**:
```json
{
  "metadata": {
    "priority": "high"
  }
}
```

---

## Event Payload

### Payload Structure

Event payloads are JSON-encoded objects stored in the `data` field.

**Document Upload Event**:
```json
{
  "event_id": "evt_123",
  "event_type": "document.uploaded",
  "source": "aether-be",
  "timestamp": 1704537600000,
  "data": "{\"document_id\":\"doc_456\",\"name\":\"report.pdf\",\"notebook_id\":\"nb_789\",\"size\":1024000,\"mime_type\":\"application/pdf\",\"storage_path\":\"s3://bucket/path/doc_456.pdf\"}",
  "metadata": {
    "tenant_id": "tenant_123",
    "space_id": "space_456",
    "user_id": "user_789"
  }
}
```

**Parsed Data**:
```json
{
  "document_id": "doc_456",
  "name": "report.pdf",
  "notebook_id": "nb_789",
  "size": 1024000,
  "mime_type": "application/pdf",
  "storage_path": "s3://bucket/path/doc_456.pdf"
}
```

---

## Event Routing

### Routing by Event Type

```go
func (s *Server) routeEvent(event *Event) error {
    subscribers := s.getSubscribersForEventType(event.EventType)

    for _, subscriber := range subscribers {
        if err := subscriber.Deliver(event); err != nil {
            log.Printf("Failed to deliver event to subscriber: %v", err)
        }
    }

    return nil
}
```

### Routing by Metadata

```go
func (s *Server) routeByTenant(event *Event) error {
    tenantID := event.Metadata["tenant_id"]
    if tenantID == "" {
        return fmt.Errorf("event missing tenant_id")
    }

    subscribers := s.getTenantSubscribers(tenantID)
    return s.deliverToSubscribers(event, subscribers)
}
```

---

## Event Subscriptions

### Subscription Model

```go
type Subscription struct {
    ID         string
    EventTypes []string
    TenantID   string
    Callback   func(*Event) error
    CreatedAt  time.Time
}
```

### Creating Subscriptions

```go
subscription := &Subscription{
    ID:         "sub_123",
    EventTypes: []string{"document.uploaded", "document.processed"},
    TenantID:   "tenant_789",
    Callback: func(event *Event) error {
        fmt.Printf("Received event: %s\n", event.EventID)
        return nil
    },
}

server.Subscribe(subscription)
```

---

## Event Persistence

Events are persisted to enable:
- Historical playback
- Audit trails
- Event sourcing
- Disaster recovery

### Storage Format

```sql
CREATE TABLE events (
    event_id VARCHAR(255) PRIMARY KEY,
    event_type VARCHAR(255) NOT NULL,
    source VARCHAR(255) NOT NULL,
    timestamp BIGINT NOT NULL,
    data TEXT NOT NULL,
    metadata JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_events_type ON events(event_type);
CREATE INDEX idx_events_timestamp ON events(timestamp);
CREATE INDEX idx_events_tenant ON events((metadata->>'tenant_id'));
```

---

## Usage Examples

### Publishing Events

```go
event := &Event{
    EventID:   uuid.New().String(),
    EventType: "document.uploaded",
    Source:    "aether-be",
    Timestamp: time.Now().UnixMilli(),
    Data:      string(jsonData),
    Metadata: map[string]string{
        "tenant_id": "tenant_123",
        "space_id":  "space_456",
        "user_id":   "user_789",
    },
}

client.IngestEvent(ctx, event)
```

### Subscribing to Events

```go
client.StreamEvents(
    ctx,
    &StreamEventsRequest{
        EventTypes:     []string{"document.*"},
        StartTimestamp: 0,
        Follow:         true,
    },
    func(event *Event) error {
        // Handle event
        return nil
    },
)
```

---

## Related Documentation

- [Protocol Buffers](./protocol-buffers.md) - gRPC definitions
- [Server Registry](./server-registry.md) - Server federation
- [Federation Architecture](../architecture/federation-design.md) - System design

---

**Document Version**: 1.0.0
**Last Updated**: 2026-01-06
**Maintained By**: TAS Platform Team
