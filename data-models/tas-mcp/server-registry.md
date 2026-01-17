# TAS-MCP - Server Registry

## Metadata

- **Document Type**: System Documentation
- **Service**: TAS-MCP
- **Component**: Server Federation
- **Last Updated**: 2026-01-06
- **Owner**: TAS Platform Team
- **Status**: Active

---

## Overview

### Purpose

The TAS-MCP Server Registry manages the federation of external MCP servers, enabling service discovery, health monitoring, and dynamic routing of requests across multiple MCP server instances.

### Key Features

- **Dynamic Registration**: Servers can register/unregister at runtime
- **Health Monitoring**: Continuous health checks with automatic failover
- **Service Discovery**: Multiple discovery sources (static, Kubernetes, Consul)
- **Load Balancing**: Intelligent request distribution
- **Protocol Translation**: Support for HTTP, gRPC, SSE, and stdio protocols

---

## Table of Contents

1. [Server Model](#server-model)
2. [Registration Process](#registration-process)
3. [Service Discovery](#service-discovery)
4. [Health Monitoring](#health-monitoring)
5. [Load Balancing](#load-balancing)
6. [Protocol Bridge](#protocol-bridge)
7. [Usage Examples](#usage-examples)
8. [Related Documentation](#related-documentation)

---

## Server Model

### MCPServer Structure

```go
type MCPServer struct {
    ID           string            `json:"id"`
    Name         string            `json:"name"`
    Description  string            `json:"description"`
    Version      string            `json:"version"`
    Category     string            `json:"category"`
    Endpoint     string            `json:"endpoint"`
    Protocol     Protocol          `json:"protocol"`
    Auth         AuthConfig        `json:"auth"`
    Capabilities []string          `json:"capabilities"`
    Tags         []string          `json:"tags"`
    Metadata     map[string]string `json:"metadata"`
    Status       ServerStatus      `json:"status"`
    HealthCheck  HealthCheckConfig `json:"health_check"`
    CreatedAt    time.Time         `json:"created_at"`
    UpdatedAt    time.Time         `json:"updated_at"`
}
```

### Protocol Types

```go
type Protocol string

const (
    ProtocolHTTP  Protocol = "http"
    ProtocolGRPC  Protocol = "grpc"
    ProtocolSSE   Protocol = "sse"
    ProtocolStdIO Protocol = "stdio"
)
```

### Server Status

```go
type ServerStatus string

const (
    StatusUnknown     ServerStatus = "unknown"
    StatusHealthy     ServerStatus = "healthy"
    StatusUnhealthy   ServerStatus = "unhealthy"
    StatusMaintenance ServerStatus = "maintenance"
    StatusDeprecated  ServerStatus = "deprecated"
)
```

---

## Registration Process

### Registration Flow

```
1. Server Starts
   ↓
2. Register with TAS-MCP
   ↓
3. Health Check
   ↓
4. Add to Active Pool
   ↓
5. Begin Serving Requests
```

### Registration API

```go
func (m *Manager) RegisterServer(server *MCPServer) error {
    if err := m.validateServer(server); err != nil {
        return err
    }

    m.mu.Lock()
    defer m.mu.Unlock()

    m.servers[server.ID] = server
    m.logger.Info("Server registered", "id", server.ID, "name", server.Name)

    return nil
}
```

### Example Registration

```go
server := &MCPServer{
    ID:          "mcp_git_001",
    Name:        "Git MCP Server",
    Description: "Git operations MCP server",
    Version:     "1.0.0",
    Category:    "version-control",
    Endpoint:    "http://localhost:3000",
    Protocol:    ProtocolHTTP,
    Auth: AuthConfig{
        Type: AuthAPIKey,
        Config: map[string]string{
            "api_key": "secret-key",
        },
    },
    Capabilities: []string{"git.clone", "git.commit", "git.push"},
    Tags:        []string{"git", "vcs"},
    HealthCheck: HealthCheckConfig{
        Enabled:  true,
        Interval: 30 * time.Second,
        Timeout:  5 * time.Second,
        Path:     "/health",
    },
}

manager.RegisterServer(server)
```

---

## Service Discovery

### Discovery Sources

```go
type SourceType string

const (
    SourceStatic     SourceType = "static"
    SourceKubernetes SourceType = "kubernetes"
    SourceConsul     SourceType = "consul"
    SourceEtcd       SourceType = "etcd"
    SourceRegistry   SourceType = "registry"
    SourceDNS        SourceType = "dns"
)
```

### Static Discovery

```yaml
servers:
  - id: mcp_git_001
    name: Git MCP Server
    endpoint: http://git-mcp:3000
    protocol: http
    capabilities:
      - git.clone
      - git.commit
```

### Kubernetes Discovery

```yaml
apiVersion: v1
kind: Service
metadata:
  name: git-mcp
  labels:
    mcp-server: "true"
    mcp-category: "version-control"
spec:
  selector:
    app: git-mcp
  ports:
    - port: 3000
```

### Consul Discovery

```hcl
service {
  name = "git-mcp"
  port = 3000
  tags = ["mcp-server", "version-control"]
  check {
    http     = "http://localhost:3000/health"
    interval = "30s"
  }
}
```

---

## Health Monitoring

### Health Check Configuration

```go
type HealthCheckConfig struct {
    Enabled            bool          `json:"enabled"`
    Interval           time.Duration `json:"interval"`
    Timeout            time.Duration `json:"timeout"`
    HealthyThreshold   int           `json:"healthy_threshold"`
    UnhealthyThreshold int           `json:"unhealthy_threshold"`
    Path               string        `json:"path,omitempty"`
}
```

### Health Check Implementation

```go
func (m *Manager) performHealthCheck(server *MCPServer) error {
    ctx, cancel := context.WithTimeout(context.Background(), server.HealthCheck.Timeout)
    defer cancel()

    switch server.Protocol {
    case ProtocolHTTP:
        return m.httpHealthCheck(ctx, server)
    case ProtocolGRPC:
        return m.grpcHealthCheck(ctx, server)
    default:
        return fmt.Errorf("unsupported protocol: %s", server.Protocol)
    }
}

func (m *Manager) httpHealthCheck(ctx context.Context, server *MCPServer) error {
    url := server.Endpoint + server.HealthCheck.Path
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return err
    }

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("unhealthy: status code %d", resp.StatusCode)
    }

    return nil
}
```

---

## Load Balancing

### Load Balancing Strategies

**Round Robin**:
```go
func (m *Manager) selectServerRoundRobin(category string) (*MCPServer, error) {
    servers := m.getServersByCategory(category)
    if len(servers) == 0 {
        return nil, ErrNoServersAvailable
    }

    index := atomic.AddInt64(&m.roundRobinIndex, 1)
    return servers[index%int64(len(servers))], nil
}
```

**Least Connections**:
```go
func (m *Manager) selectServerLeastConn(category string) (*MCPServer, error) {
    servers := m.getServersByCategory(category)
    if len(servers) == 0 {
        return nil, ErrNoServersAvailable
    }

    var selected *MCPServer
    minConns := int64(math.MaxInt64)

    for _, server := range servers {
        conns := atomic.LoadInt64(&server.ActiveConnections)
        if conns < minConns {
            minConns = conns
            selected = server
        }
    }

    return selected, nil
}
```

---

## Protocol Bridge

### Protocol Translation

```go
type ProtocolBridge interface {
    TranslateRequest(ctx context.Context, from Protocol, to Protocol, request *MCPRequest) (*MCPRequest, error)
    TranslateResponse(ctx context.Context, from Protocol, to Protocol, response *MCPResponse) (*MCPResponse, error)
    SupportsProtocol(protocol Protocol) bool
    SupportedProtocols() []Protocol
}
```

### HTTP to gRPC Translation

```go
func (b *Bridge) TranslateHTTPToGRPC(req *MCPRequest) (*grpc.Request, error) {
    return &grpc.Request{
        Method: req.Method,
        Params: req.Params,
        Metadata: req.Metadata,
    }, nil
}
```

---

## Usage Examples

### Server Registration

```go
manager := NewManager()

server := &MCPServer{
    ID:       "git-001",
    Name:     "Git Server",
    Endpoint: "http://localhost:3000",
    Protocol: ProtocolHTTP,
}

manager.RegisterServer(server)
```

### Server Invocation

```go
response, err := manager.InvokeServer(ctx, "git-001", &MCPRequest{
    Method: "git.clone",
    Params: map[string]interface{}{
        "url": "https://github.com/example/repo.git",
    },
})
```

### Health Monitoring

```go
status, err := manager.GetHealthStatus()
for serverID, health := range status {
    fmt.Printf("Server %s: %s\n", serverID, health)
}
```

---

## Related Documentation

- [Protocol Buffers](./protocol-buffers.md) - gRPC definitions
- [Event Structure](./event-structure.md) - Event models
- [Federation Architecture](../architecture/federation-design.md) - System design

---

**Document Version**: 1.0.0
**Last Updated**: 2026-01-06
**Maintained By**: TAS Platform Team
