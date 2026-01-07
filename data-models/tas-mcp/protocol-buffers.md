# TAS-MCP - Protocol Buffers Specification

## Metadata

- **Document Type**: Protocol Documentation
- **Service**: TAS-MCP
- **Component**: gRPC Protocol Buffers
- **Last Updated**: 2026-01-06
- **Owner**: TAS Platform Team
- **Status**: Active

---

## Overview

### Purpose

This document provides comprehensive documentation for the TAS-MCP (Model Context Protocol) gRPC service definitions. The MCP protocol enables federated communication between multiple MCP servers, event ingestion, event streaming, and health monitoring across the TAS platform.

### Protocol Features

- **Event Ingestion**: Single-event and batch ingestion
- **Event Streaming**: Server-side and bidirectional streaming
- **Health Monitoring**: Standard health check protocol
- **Metrics Collection**: Server metrics and statistics
- **Multi-Tenancy**: Tenant-aware event routing
- **Error Handling**: Standardized error responses

---

## Table of Contents

1. [Proto File Definition](#proto-file-definition)
2. [Message Definitions](#message-definitions)
3. [Service Interface](#service-interface)
4. [Generated Code](#generated-code)
5. [Usage Examples](#usage-examples)
6. [Related Documentation](#related-documentation)

---

## Proto File Definition

### Complete Proto Specification

```protobuf
syntax = "proto3";

package mcp.v1;

option go_package = "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1";

// Event represents a structured event in the MCP system
message Event {
  string event_id = 1;
  string event_type = 2;
  string source = 3;
  int64 timestamp = 4;
  string data = 5; // JSON data payload
  map<string, string> metadata = 6;
}

// IngestEventRequest represents a request to ingest an event
message IngestEventRequest {
  string event_id = 1;
  string event_type = 2;
  string source = 3;
  int64 timestamp = 4;
  string data = 5; // JSON data payload
  map<string, string> metadata = 6;
}

// IngestEventResponse represents the response to an event ingestion
message IngestEventResponse {
  string event_id = 1;
  bool success = 2;
  string message = 3;
  int64 timestamp = 4;
  string status = 5;
}

// StreamEventsRequest represents a request to stream events
message StreamEventsRequest {
  repeated string event_types = 1; // Filter by event types
  int64 start_timestamp = 2; // Start streaming from this timestamp
  bool follow = 3; // Continue streaming new events
}

// HealthCheckRequest represents a health check request
message HealthCheckRequest {}

// HealthCheckResponse represents a health check response
message HealthCheckResponse {
  bool healthy = 1;
  string status = 2;
  map<string, string> details = 3;
  int64 uptime = 4;
}

// MetricsRequest represents a request for server metrics
message MetricsRequest {}

// MetricsResponse represents server metrics
message MetricsResponse {
  int64 total_events = 1;
  int64 stream_events = 2;
  int64 forwarded_events = 3;
  int64 error_events = 4;
  int32 active_streams = 5;
  int64 uptime = 6;
}

// MCPService defines the main service interface
service MCPService {
  // Ingest a single event
  rpc IngestEvent(IngestEventRequest) returns (IngestEventResponse);

  // Stream events (server-side streaming)
  rpc StreamEvents(StreamEventsRequest) returns (stream Event);

  // Bidirectional event streaming
  rpc EventStream(stream Event) returns (stream Event);

  // Health check
  rpc GetHealth(HealthCheckRequest) returns (HealthCheckResponse);

  // Get server metrics
  rpc GetMetrics(MetricsRequest) returns (MetricsResponse);
}
```

---

## Message Definitions

### Event Message

```protobuf
message Event {
  string event_id = 1;
  string event_type = 2;
  string source = 3;
  int64 timestamp = 4;
  string data = 5;
  map<string, string> metadata = 6;
}
```

**Field Descriptions**:

- **event_id** (string): Unique event identifier (UUID)
- **event_type** (string): Event classification (e.g., "document.uploaded", "user.created")
- **source** (string): Event source service (e.g., "aether-be", "audimodal")
- **timestamp** (int64): Unix timestamp in milliseconds
- **data** (string): JSON-encoded event payload
- **metadata** (map<string, string>): Key-value metadata (tenant_id, space_id, user_id, etc.)

**Example JSON Representation**:
```json
{
  "event_id": "evt_abc123",
  "event_type": "document.uploaded",
  "source": "aether-be",
  "timestamp": 1704537600000,
  "data": "{\"document_id\":\"doc_456\",\"name\":\"report.pdf\"}",
  "metadata": {
    "tenant_id": "tenant_789",
    "space_id": "space_123",
    "user_id": "user_456"
  }
}
```

### IngestEventRequest/Response

**Request**:
```protobuf
message IngestEventRequest {
  string event_id = 1;
  string event_type = 2;
  string source = 3;
  int64 timestamp = 4;
  string data = 5;
  map<string, string> metadata = 6;
}
```

**Response**:
```protobuf
message IngestEventResponse {
  string event_id = 1;
  bool success = 2;
  string message = 3;
  int64 timestamp = 4;
  string status = 5;
}
```

**Status Values**:
- `"accepted"` - Event accepted for processing
- `"processed"` - Event processed immediately
- `"queued"` - Event queued for async processing
- `"rejected"` - Event rejected (validation failed)

### StreamEventsRequest

```protobuf
message StreamEventsRequest {
  repeated string event_types = 1;
  int64 start_timestamp = 2;
  bool follow = 3;
}
```

**Field Descriptions**:
- **event_types**: Filter events by type (empty = all types)
- **start_timestamp**: Historical playback starting point (0 = from beginning)
- **follow**: Continue streaming new events (true) or stop after historical (false)

**Example**:
```json
{
  "event_types": ["document.uploaded", "document.processed"],
  "start_timestamp": 1704537600000,
  "follow": true
}
```

### HealthCheckRequest/Response

**Request**:
```protobuf
message HealthCheckRequest {}
```

**Response**:
```protobuf
message HealthCheckResponse {
  bool healthy = 1;
  string status = 2;
  map<string, string> details = 3;
  int64 uptime = 4;
}
```

**Status Values**:
- `"healthy"` - Service operational
- `"degraded"` - Service operational with issues
- `"unhealthy"` - Service not operational

**Example**:
```json
{
  "healthy": true,
  "status": "healthy",
  "details": {
    "grpc_server": "running",
    "http_server": "running",
    "event_queue": "healthy",
    "connected_servers": "5"
  },
  "uptime": 86400
}
```

### MetricsRequest/Response

**Request**:
```protobuf
message MetricsRequest {}
```

**Response**:
```protobuf
message MetricsResponse {
  int64 total_events = 1;
  int64 stream_events = 2;
  int64 forwarded_events = 3;
  int64 error_events = 4;
  int32 active_streams = 5;
  int64 uptime = 6;
}
```

**Example**:
```json
{
  "total_events": 1234567,
  "stream_events": 987654,
  "forwarded_events": 456789,
  "error_events": 123,
  "active_streams": 42,
  "uptime": 86400
}
```

---

## Service Interface

### MCPService

```protobuf
service MCPService {
  rpc IngestEvent(IngestEventRequest) returns (IngestEventResponse);
  rpc StreamEvents(StreamEventsRequest) returns (stream Event);
  rpc EventStream(stream Event) returns (stream Event);
  rpc GetHealth(HealthCheckRequest) returns (HealthCheckResponse);
  rpc GetMetrics(MetricsRequest) returns (MetricsResponse);
}
```

### RPC Methods

#### IngestEvent (Unary)

**Purpose**: Ingest a single event

**Pattern**: Request-Response

**Usage**:
```go
response, err := client.IngestEvent(ctx, &IngestEventRequest{
    EventId:   "evt_abc123",
    EventType: "document.uploaded",
    Source:    "aether-be",
    Timestamp: time.Now().UnixMilli(),
    Data:      jsonData,
    Metadata:  metadata,
})
```

#### StreamEvents (Server Streaming)

**Purpose**: Stream events from server to client

**Pattern**: Request-Stream Response

**Usage**:
```go
stream, err := client.StreamEvents(ctx, &StreamEventsRequest{
    EventTypes:     []string{"document.uploaded"},
    StartTimestamp: 0,
    Follow:         true,
})

for {
    event, err := stream.Recv()
    if err == io.EOF {
        break
    }
    // Process event
}
```

#### EventStream (Bidirectional Streaming)

**Purpose**: Bidirectional event streaming

**Pattern**: Stream Request-Stream Response

**Usage**:
```go
stream, err := client.EventStream(ctx)

// Send events
go func() {
    for event := range eventChan {
        stream.Send(event)
    }
    stream.CloseSend()
}()

// Receive events
for {
    event, err := stream.Recv()
    if err == io.EOF {
        break
    }
    // Process event
}
```

#### GetHealth (Unary)

**Purpose**: Check server health

**Pattern**: Request-Response

**Usage**:
```go
response, err := client.GetHealth(ctx, &HealthCheckRequest{})
fmt.Printf("Healthy: %v, Status: %s\n", response.Healthy, response.Status)
```

#### GetMetrics (Unary)

**Purpose**: Retrieve server metrics

**Pattern**: Request-Response

**Usage**:
```go
response, err := client.GetMetrics(ctx, &MetricsRequest{})
fmt.Printf("Total Events: %d, Active Streams: %d\n",
    response.TotalEvents, response.ActiveStreams)
```

---

## Generated Code

### Go Server Implementation

```go
package server

import (
    "context"
    "io"
    "sync"
    "time"

    pb "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
    "google.golang.org/grpc"
)

type MCPServer struct {
    pb.UnimplementedMCPServiceServer
    eventCount   int64
    streamCount  int32
    startTime    time.Time
    mu           sync.RWMutex
}

func NewMCPServer() *MCPServer {
    return &MCPServer{
        startTime: time.Now(),
    }
}

func (s *MCPServer) IngestEvent(
    ctx context.Context,
    req *pb.IngestEventRequest,
) (*pb.IngestEventResponse, error) {
    s.mu.Lock()
    s.eventCount++
    s.mu.Unlock()

    return &pb.IngestEventResponse{
        EventId:   req.EventId,
        Success:   true,
        Message:   "Event ingested successfully",
        Timestamp: time.Now().UnixMilli(),
        Status:    "accepted",
    }, nil
}

func (s *MCPServer) StreamEvents(
    req *pb.StreamEventsRequest,
    stream pb.MCPService_StreamEventsServer,
) error {
    s.mu.Lock()
    s.streamCount++
    s.mu.Unlock()

    defer func() {
        s.mu.Lock()
        s.streamCount--
        s.mu.Unlock()
    }()

    // Stream events to client
    for {
        select {
        case <-stream.Context().Done():
            return stream.Context().Err()
        case event := <-s.eventQueue:
            if err := stream.Send(event); err != nil {
                return err
            }
        }
    }
}

func (s *MCPServer) GetHealth(
    ctx context.Context,
    req *pb.HealthCheckRequest,
) (*pb.HealthCheckResponse, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    return &pb.HealthCheckResponse{
        Healthy: true,
        Status:  "healthy",
        Details: map[string]string{
            "uptime": time.Since(s.startTime).String(),
        },
        Uptime: int64(time.Since(s.startTime).Seconds()),
    }, nil
}

func (s *MCPServer) GetMetrics(
    ctx context.Context,
    req *pb.MetricsRequest,
) (*pb.MetricsResponse, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    return &pb.MetricsResponse{
        TotalEvents:   s.eventCount,
        ActiveStreams: s.streamCount,
        Uptime:        int64(time.Since(s.startTime).Seconds()),
    }, nil
}
```

### Go Client Implementation

```go
package client

import (
    "context"
    "io"

    pb "github.com/tributary-ai-services/tas-mcp/gen/mcp/v1"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

type MCPClient struct {
    conn   *grpc.ClientConn
    client pb.MCPServiceClient
}

func NewMCPClient(address string) (*MCPClient, error) {
    conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        return nil, err
    }

    return &MCPClient{
        conn:   conn,
        client: pb.NewMCPServiceClient(conn),
    }, nil
}

func (c *MCPClient) IngestEvent(
    ctx context.Context,
    event *pb.IngestEventRequest,
) (*pb.IngestEventResponse, error) {
    return c.client.IngestEvent(ctx, event)
}

func (c *MCPClient) StreamEvents(
    ctx context.Context,
    req *pb.StreamEventsRequest,
    handler func(*pb.Event) error,
) error {
    stream, err := c.client.StreamEvents(ctx, req)
    if err != nil {
        return err
    }

    for {
        event, err := stream.Recv()
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }

        if err := handler(event); err != nil {
            return err
        }
    }

    return nil
}

func (c *MCPClient) Close() error {
    return c.conn.Close()
}
```

---

## Usage Examples

### Event Ingestion

```go
client, _ := NewMCPClient("localhost:50052")
defer client.Close()

response, err := client.IngestEvent(context.Background(), &pb.IngestEventRequest{
    EventId:   "evt_123",
    EventType: "document.uploaded",
    Source:    "aether-be",
    Timestamp: time.Now().UnixMilli(),
    Data:      `{"document_id":"doc_456"}`,
    Metadata: map[string]string{
        "tenant_id": "tenant_789",
        "space_id":  "space_123",
    },
})
```

### Event Streaming

```go
err := client.StreamEvents(
    context.Background(),
    &pb.StreamEventsRequest{
        EventTypes:     []string{"document.uploaded"},
        StartTimestamp: 0,
        Follow:         true,
    },
    func(event *pb.Event) error {
        fmt.Printf("Received event: %s\n", event.EventId)
        return nil
    },
)
```

---

## Related Documentation

- [Event Structure](./event-structure.md) - Event data models
- [Server Registry](./server-registry.md) - MCP server federation
- [Federation Architecture](../architecture/federation-design.md) - System design

---

**Document Version**: 1.0.0
**Last Updated**: 2026-01-06
**Maintained By**: TAS Platform Team
