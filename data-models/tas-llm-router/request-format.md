# TAS LLM Router - Request Format

## Metadata

- **Document Type**: API Documentation
- **Service**: TAS LLM Router
- **Component**: Request Structures
- **Last Updated**: 2026-01-06
- **Owner**: TAS Platform Team
- **Status**: Active

---

## Overview

### Purpose

The TAS LLM Router Request Format defines the structure for all LLM requests routed through the TAS platform. This unified request format supports multiple LLM providers (OpenAI, Anthropic, etc.) with intelligent routing, retry logic, fallback mechanisms, and cost optimization.

### Key Features

- **Unified Request Format**: Single API for multiple LLM providers
- **Intelligent Routing**: Automatic provider selection based on cost, performance, or features
- **Retry & Fallback**: Configurable retry with exponential backoff and provider fallback
- **Multi-Modal Support**: Text, images, function calling, structured outputs
- **Cost Controls**: Maximum cost limits and optimization hints
- **Feature Detection**: Automatic validation of provider capabilities

### Supported Providers

- **OpenAI**: GPT-4, GPT-4 Turbo, GPT-3.5 Turbo
- **Anthropic**: Claude 3 Opus, Claude 3 Sonnet, Claude 3 Haiku
- **Future**: Support for additional providers (Cohere, AI21, etc.)

---

## Table of Contents

1. [ChatRequest Structure](#chatrequest-structure)
2. [Message Format](#message-format)
3. [Model Selection](#model-selection)
4. [Configuration Parameters](#configuration-parameters)
5. [Retry Configuration](#retry-configuration)
6. [Fallback Configuration](#fallback-configuration)
7. [Function Calling](#function-calling)
8. [Structured Output](#structured-output)
9. [Multi-Modal Requests](#multi-modal-requests)
10. [Usage Examples](#usage-examples)
11. [Related Documentation](#related-documentation)

---

## ChatRequest Structure

### Go Definition

```go
package types

import (
    "time"
)

// Core request/response types
type ChatRequest struct {
    ID               string                 `json:"id"`
    Model            string                 `json:"model"`
    Messages         []Message              `json:"messages"`
    Temperature      *float32               `json:"temperature,omitempty"`
    MaxTokens        *int                   `json:"max_tokens,omitempty"`
    TopP             *float32               `json:"top_p,omitempty"`
    FrequencyPenalty *float32               `json:"frequency_penalty,omitempty"`
    PresencePenalty  *float32               `json:"presence_penalty,omitempty"`
    Stop             []string               `json:"stop,omitempty"`
    Stream           bool                   `json:"stream"`
    Functions        []Function             `json:"functions,omitempty"`
    FunctionCall     interface{}            `json:"function_call,omitempty"`
    Tools            []Tool                 `json:"tools,omitempty"`
    ToolChoice       interface{}            `json:"tool_choice,omitempty"`
    ResponseFormat   *ResponseFormat        `json:"response_format,omitempty"`
    Seed             *int                   `json:"seed,omitempty"`

    // Routing hints
    OptimizeFor      OptimizationType       `json:"optimize_for,omitempty"`
    RequiredFeatures []string               `json:"required_features,omitempty"`
    MaxCost          *float64               `json:"max_cost,omitempty"`

    // Retry and fallback controls
    RetryConfig      *RetryConfig           `json:"retry_config,omitempty"`
    FallbackConfig   *FallbackConfig        `json:"fallback_config,omitempty"`

    // Metadata
    UserID           string                 `json:"user_id"`
    ApplicationID    string                 `json:"application_id"`
    Timestamp        time.Time              `json:"timestamp"`
}
```

### TypeScript Definition

```typescript
interface ChatRequest {
  id: string;
  model: string;
  messages: Message[];
  temperature?: number;
  max_tokens?: number;
  top_p?: number;
  frequency_penalty?: number;
  presence_penalty?: number;
  stop?: string[];
  stream?: boolean;
  functions?: Function[];
  function_call?: string | { name: string };
  tools?: Tool[];
  tool_choice?: string | { type: string; function: { name: string } };
  response_format?: ResponseFormat;
  seed?: number;

  // Routing hints
  optimize_for?: 'cost' | 'performance' | 'quality';
  required_features?: string[];
  max_cost?: number;

  // Retry and fallback controls
  retry_config?: RetryConfig;
  fallback_config?: FallbackConfig;

  // Metadata
  user_id: string;
  application_id: string;
  timestamp: string;
}
```

### Python Definition

```python
from typing import Optional, List, Dict, Any, Union
from datetime import datetime
from pydantic import BaseModel, Field

class ChatRequest(BaseModel):
    """Chat request model."""

    id: str
    model: str
    messages: List[Message]
    temperature: Optional[float] = None
    max_tokens: Optional[int] = None
    top_p: Optional[float] = None
    frequency_penalty: Optional[float] = None
    presence_penalty: Optional[float] = None
    stop: Optional[List[str]] = None
    stream: bool = False
    functions: Optional[List[Function]] = None
    function_call: Optional[Union[str, Dict[str, str]]] = None
    tools: Optional[List[Tool]] = None
    tool_choice: Optional[Union[str, Dict[str, Any]]] = None
    response_format: Optional[ResponseFormat] = None
    seed: Optional[int] = None

    # Routing hints
    optimize_for: Optional[str] = None  # "cost", "performance", "quality"
    required_features: Optional[List[str]] = None
    max_cost: Optional[float] = None

    # Retry and fallback controls
    retry_config: Optional[RetryConfig] = None
    fallback_config: Optional[FallbackConfig] = None

    # Metadata
    user_id: str
    application_id: str
    timestamp: datetime = Field(default_factory=datetime.utcnow)
```

### Field Descriptions

#### id
- **Type**: String
- **Required**: Yes
- **Purpose**: Unique request identifier for tracking and correlation
- **Format**: UUID v4 recommended
- **Example**: `"req_abc123"`

#### model
- **Type**: String
- **Required**: Yes
- **Purpose**: LLM model identifier
- **Options**: `"gpt-4"`, `"gpt-3.5-turbo"`, `"claude-3-opus"`, `"claude-3-sonnet"`, etc.
- **Routing**: Model prefix determines provider routing

#### messages
- **Type**: Array of Message objects
- **Required**: Yes
- **Purpose**: Conversation history
- **See**: [Message Format](#message-format) section

#### temperature
- **Type**: Float (0.0-2.0)
- **Required**: No
- **Default**: Provider-specific (typically 1.0)
- **Purpose**: Controls randomness in responses
- **Usage**: Lower = more deterministic, Higher = more creative

#### max_tokens
- **Type**: Integer
- **Required**: No
- **Default**: Provider-specific
- **Purpose**: Maximum tokens in response
- **Note**: Includes prompt tokens in some providers

#### top_p
- **Type**: Float (0.0-1.0)
- **Required**: No
- **Default**: 1.0
- **Purpose**: Nucleus sampling parameter
- **Usage**: Alternative to temperature

#### frequency_penalty
- **Type**: Float (-2.0 to 2.0)
- **Required**: No
- **Default**: 0.0
- **Purpose**: Penalty for token frequency
- **Effect**: Positive values discourage repetition

#### presence_penalty
- **Type**: Float (-2.0 to 2.0)
- **Required**: No
- **Default**: 0.0
- **Purpose**: Penalty for token presence
- **Effect**: Positive values encourage topic diversity

#### stop
- **Type**: Array of strings
- **Required**: No
- **Purpose**: Sequences where API stops generating
- **Example**: `["END", "\n\n"]`

#### stream
- **Type**: Boolean
- **Required**: No
- **Default**: false
- **Purpose**: Enable streaming responses
- **Effect**: Server-sent events instead of single response

### Request Example

```json
{
  "id": "req_abc123",
  "model": "gpt-4",
  "messages": [
    {
      "role": "system",
      "content": "You are a helpful assistant."
    },
    {
      "role": "user",
      "content": "What is the capital of France?"
    }
  ],
  "temperature": 0.7,
  "max_tokens": 150,
  "stream": false,
  "optimize_for": "cost",
  "user_id": "user_123",
  "application_id": "app_456",
  "timestamp": "2026-01-06T10:30:00Z"
}
```

---

## Message Format

### Structure

```go
type Message struct {
    Role      string      `json:"role"`
    Content   interface{} `json:"content"` // string or []ContentPart for multimodal
    Name      string      `json:"name,omitempty"`
    ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
}
```

### TypeScript

```typescript
interface Message {
  role: 'system' | 'user' | 'assistant' | 'function' | 'tool';
  content: string | ContentPart[];
  name?: string;
  tool_calls?: ToolCall[];
}
```

### Role Types

#### system
- **Purpose**: System instructions and context
- **Placement**: Typically first message
- **Example**:
```json
{
  "role": "system",
  "content": "You are a helpful coding assistant. Provide concise, accurate code examples."
}
```

#### user
- **Purpose**: User queries and inputs
- **Placement**: Any position after system
- **Example**:
```json
{
  "role": "user",
  "content": "Write a Python function to calculate fibonacci numbers."
}
```

#### assistant
- **Purpose**: AI responses (for conversation history)
- **Placement**: After user messages
- **Example**:
```json
{
  "role": "assistant",
  "content": "Here's a Python function for fibonacci:\n\ndef fib(n):\n    if n <= 1:\n        return n\n    return fib(n-1) + fib(n-2)"
}
```

#### function
- **Purpose**: Function call results (deprecated, use tool)
- **Placement**: After assistant function call
- **Example**:
```json
{
  "role": "function",
  "name": "get_weather",
  "content": "{\"temperature\": 72, \"condition\": \"sunny\"}"
}
```

#### tool
- **Purpose**: Tool execution results
- **Placement**: After assistant tool calls
- **Example**:
```json
{
  "role": "tool",
  "content": "{\"result\": \"Operation successful\"}",
  "tool_call_id": "call_abc123"
}
```

### Multi-Turn Conversation Example

```json
{
  "messages": [
    {
      "role": "system",
      "content": "You are a helpful assistant."
    },
    {
      "role": "user",
      "content": "What is machine learning?"
    },
    {
      "role": "assistant",
      "content": "Machine learning is a subset of AI that enables computers to learn from data without being explicitly programmed."
    },
    {
      "role": "user",
      "content": "Can you give me an example?"
    }
  ]
}
```

---

## Model Selection

### Model Naming Convention

Models are identified by provider-specific prefixes:

**OpenAI Models**:
- `gpt-4` - GPT-4 (8K context)
- `gpt-4-32k` - GPT-4 (32K context)
- `gpt-4-turbo` - GPT-4 Turbo (128K context)
- `gpt-3.5-turbo` - GPT-3.5 Turbo (16K context)

**Anthropic Models**:
- `claude-3-opus` - Claude 3 Opus (200K context)
- `claude-3-sonnet` - Claude 3 Sonnet (200K context)
- `claude-3-haiku` - Claude 3 Haiku (200K context)

### Automatic Provider Routing

The router automatically determines the provider from the model name:

```go
func (r *Router) getProviderForModel(model string) (string, bool) {
    providerPrefixes := map[string]string{
        "gpt-":    "openai",
        "claude-": "anthropic",
    }

    for prefix, providerName := range providerPrefixes {
        if strings.HasPrefix(model, prefix) {
            if _, exists := r.providers[providerName]; exists {
                return providerName, true
            }
        }
    }

    return "", false
}
```

### Model Capabilities

The router validates that the selected model supports requested features:

```go
// Check if provider supports required features
for _, feature := range req.RequiredFeatures {
    switch feature {
    case "functions", "function_calling":
        if !capabilities.SupportsFunctions {
            return false
        }
    case "vision":
        if !capabilities.SupportsVision {
            return false
        }
    case "structured_output":
        if !capabilities.SupportsStructuredOutput {
            return false
        }
    }
}
```

---

## Configuration Parameters

### OptimizationType

```go
type OptimizationType string

const (
    OptimizeCost        OptimizationType = "cost"
    OptimizePerformance OptimizationType = "performance"
    OptimizeQuality     OptimizationType = "quality"
)
```

### Routing Strategies

#### Cost Optimization
```json
{
  "model": "gpt-4",
  "optimize_for": "cost",
  "messages": [...]
}
```

**Behavior**:
- Routes to cheapest provider supporting required features
- Considers input/output token costs
- Favors models with lower per-token pricing

#### Performance Optimization
```json
{
  "model": "gpt-4",
  "optimize_for": "performance",
  "messages": [...]
}
```

**Behavior**:
- Routes to fastest provider
- Prioritizes low-latency models
- Considers historical response times

#### Quality Optimization
```json
{
  "model": "claude-3-opus",
  "optimize_for": "quality",
  "messages": [...]
}
```

**Behavior**:
- Routes to highest-quality model
- Prefers newer/larger models
- May incur higher costs

### Required Features

Specify features that must be supported:

```json
{
  "model": "gpt-4",
  "required_features": [
    "function_calling",
    "vision",
    "streaming"
  ],
  "messages": [...]
}
```

**Supported Features**:
- `function_calling` - Function/tool calling
- `vision` - Image understanding
- `structured_output` - JSON schema responses
- `streaming` - Server-sent events
- `assistants` - Assistants API
- `batch` - Batch processing

### Max Cost

Set a maximum cost limit for the request:

```json
{
  "model": "gpt-4",
  "max_cost": 0.05,
  "messages": [...]
}
```

**Behavior**:
- Router estimates cost before execution
- Rejects requests exceeding limit
- Falls back to cheaper models if available

---

## Retry Configuration

### Structure

```go
type RetryConfig struct {
    MaxAttempts     int           `json:"max_attempts"`               // 0 = no retry, 1-5 allowed
    BackoffType     string        `json:"backoff_type"`               // "linear", "exponential"
    BaseDelay       time.Duration `json:"base_delay"`                 // Starting delay (e.g., 1s)
    MaxDelay        time.Duration `json:"max_delay"`                  // Cap on delay (e.g., 30s)
    RetryableErrors []string      `json:"retryable_errors,omitempty"` // Which errors to retry
}
```

### TypeScript

```typescript
interface RetryConfig {
  max_attempts: number;        // 0-5
  backoff_type: 'linear' | 'exponential';
  base_delay: number;          // milliseconds
  max_delay: number;           // milliseconds
  retryable_errors?: string[]; // Error codes to retry
}
```

### Configuration Options

#### max_attempts
- **Type**: Integer (0-5)
- **Default**: 0 (no retry)
- **Purpose**: Maximum retry attempts
- **Note**: 1 attempt = no retries, 5 = 4 retries

#### backoff_type
- **Type**: String
- **Options**: "linear", "exponential"
- **Default**: "exponential"
- **Purpose**: Delay calculation strategy

**Linear Backoff**:
```
delay = base_delay * attempt
```

**Exponential Backoff**:
```
delay = base_delay * (2 ^ attempt)
```

#### base_delay
- **Type**: Duration
- **Default**: 1 second
- **Purpose**: Initial retry delay
- **Example**: `"1s"`, `"2s"`, `"500ms"`

#### max_delay
- **Type**: Duration
- **Default**: 30 seconds
- **Purpose**: Maximum retry delay cap
- **Example**: `"30s"`, `"1m"`

#### retryable_errors
- **Type**: Array of strings
- **Default**: All transient errors
- **Purpose**: Specify which errors to retry
- **Options**:
  - `"rate_limit"` - Rate limiting errors
  - `"timeout"` - Request timeouts
  - `"server_error"` - 5xx errors
  - `"network_error"` - Network failures

### Retry Example

```json
{
  "model": "gpt-4",
  "messages": [...],
  "retry_config": {
    "max_attempts": 3,
    "backoff_type": "exponential",
    "base_delay": "1s",
    "max_delay": "30s",
    "retryable_errors": ["rate_limit", "timeout"]
  }
}
```

### Retry Implementation

```go
func (r *Router) routeWithRetry(
    ctx context.Context,
    req *types.ChatRequest,
    decision *RoutingDecision,
    metadata *types.RouterMetadata
) (*types.RouterMetadata, providers.LLMProvider, error) {
    provider := r.providers[decision.SelectedProvider]
    maxAttempts := req.RetryConfig.MaxAttempts
    var lastError error

    for attempt := 1; attempt <= maxAttempts; attempt++ {
        metadata.AttemptCount = attempt

        // Apply backoff delay for retries
        if attempt > 1 {
            delay := r.calculateBackoffDelay(req.RetryConfig, attempt-1)

            select {
            case <-time.After(delay):
                // Continue with retry
            case <-ctx.Done():
                return nil, nil, fmt.Errorf("request cancelled during retry: %w", ctx.Err())
            }
        }

        // Check provider health
        if !r.isProviderHealthy(decision.SelectedProvider) {
            lastError = fmt.Errorf("provider %s is not healthy", decision.SelectedProvider)
            continue
        }

        // Attempt succeeded
        return metadata, provider, nil
    }

    // All retry attempts exhausted
    metadata.FailedProviders = append(metadata.FailedProviders, decision.SelectedProvider)
    return metadata, nil, fmt.Errorf("all retry attempts failed: %w", lastError)
}
```

---

## Fallback Configuration

### Structure

```go
type FallbackConfig struct {
    Enabled             bool     `json:"enabled"`                          // Enable fallback to healthy providers
    PreferredChain      []string `json:"preferred_chain,omitempty"`        // Custom fallback order
    MaxCostIncrease     *float64 `json:"max_cost_increase,omitempty"`      // Max % cost increase allowed (e.g., 0.5 = 50%)
    RequireSameFeatures bool     `json:"require_same_features"`            // Must support same capabilities
}
```

### TypeScript

```typescript
interface FallbackConfig {
  enabled: boolean;
  preferred_chain?: string[];      // Provider names in order
  max_cost_increase?: number;      // 0.5 = 50% increase allowed
  require_same_features: boolean;
}
```

### Configuration Options

#### enabled
- **Type**: Boolean
- **Default**: false
- **Purpose**: Enable/disable fallback mechanism

#### preferred_chain
- **Type**: Array of provider names
- **Default**: Auto-generated based on capabilities
- **Purpose**: Custom fallback order
- **Example**: `["openai", "anthropic"]`

#### max_cost_increase
- **Type**: Float (percentage)
- **Default**: None (no limit)
- **Purpose**: Maximum allowed cost increase
- **Example**: `0.5` = 50% increase allowed

#### require_same_features
- **Type**: Boolean
- **Default**: true
- **Purpose**: Fallback must support same features
- **Effect**: Skips providers lacking required capabilities

### Fallback Example

```json
{
  "model": "gpt-4",
  "messages": [...],
  "fallback_config": {
    "enabled": true,
    "preferred_chain": ["openai", "anthropic"],
    "max_cost_increase": 0.3,
    "require_same_features": true
  },
  "retry_config": {
    "max_attempts": 2
  }
}
```

### Fallback Workflow

```
1. Primary Provider Fails
   ↓
2. Check Fallback Enabled
   ↓
3. Build Fallback Chain
   ↓
4. For Each Fallback Provider:
   - Check Health
   - Verify Feature Support
   - Check Cost Constraints
   - Attempt Request
   ↓
5. Return Success or Error
```

### Fallback Implementation

```go
func (r *Router) routeWithFallback(
    ctx context.Context,
    req *types.ChatRequest,
    originalDecision *RoutingDecision,
    metadata *types.RouterMetadata
) (*types.RouterMetadata, providers.LLMProvider, error) {
    // Build fallback chain
    var fallbackChain []string

    if len(req.FallbackConfig.PreferredChain) > 0 {
        fallbackChain = req.FallbackConfig.PreferredChain
    } else {
        fallbackChain = originalDecision.FallbackChain
    }

    // Try each fallback provider
    for _, providerName := range fallbackChain {
        // Skip if already failed
        if contains(metadata.FailedProviders, providerName) {
            continue
        }

        // Check health
        if !r.isProviderHealthy(providerName) {
            metadata.FailedProviders = append(metadata.FailedProviders, providerName)
            continue
        }

        provider := r.providers[providerName]

        // Check feature compatibility
        if req.FallbackConfig.RequireSameFeatures {
            if !r.supportsRequiredFeatures(provider, req) {
                continue
            }
        }

        // Check cost constraints
        if req.FallbackConfig.MaxCostIncrease != nil {
            costEst, _ := provider.EstimateCost(req)
            costIncrease := (costEst.TotalCost - originalDecision.EstimatedCost) /
                           originalDecision.EstimatedCost

            if costIncrease > *req.FallbackConfig.MaxCostIncrease {
                continue
            }
        }

        // Fallback provider is suitable
        metadata.Provider = providerName
        metadata.FallbackUsed = true

        return metadata, provider, nil
    }

    return metadata, nil, fmt.Errorf("all fallback providers failed")
}
```

---

## Function Calling

### Function Structure

```go
type Function struct {
    Name        string      `json:"name"`
    Description string      `json:"description,omitempty"`
    Parameters  interface{} `json:"parameters,omitempty"`
}

type Tool struct {
    Type     string   `json:"type"`
    Function Function `json:"function,omitempty"`
}
```

### Function Definition Example

```json
{
  "model": "gpt-4",
  "messages": [...],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get the current weather for a location",
        "parameters": {
          "type": "object",
          "properties": {
            "location": {
              "type": "string",
              "description": "City and state, e.g. San Francisco, CA"
            },
            "unit": {
              "type": "string",
              "enum": ["celsius", "fahrenheit"],
              "description": "Temperature unit"
            }
          },
          "required": ["location"]
        }
      }
    }
  ],
  "tool_choice": "auto"
}
```

### Tool Choice Options

```json
// Auto: Model decides whether to call functions
"tool_choice": "auto"

// None: Force no function calling
"tool_choice": "none"

// Specific function: Force specific function
"tool_choice": {
  "type": "function",
  "function": {
    "name": "get_weather"
  }
}
```

---

## Structured Output

### ResponseFormat Structure

```go
type ResponseFormat struct {
    Type       string      `json:"type"` // "text", "json_object", "json_schema"
    JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

type JSONSchema struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description,omitempty"`
    Schema      map[string]interface{} `json:"schema"`
    Strict      bool                   `json:"strict,omitempty"` // OpenAI specific
}
```

### JSON Object Mode

```json
{
  "model": "gpt-4",
  "messages": [
    {
      "role": "system",
      "content": "You are a helpful assistant. Always respond with valid JSON."
    },
    {
      "role": "user",
      "content": "List the top 3 programming languages"
    }
  ],
  "response_format": {
    "type": "json_object"
  }
}
```

### JSON Schema Mode

```json
{
  "model": "gpt-4",
  "messages": [...],
  "response_format": {
    "type": "json_schema",
    "json_schema": {
      "name": "programming_languages",
      "description": "List of programming languages with details",
      "schema": {
        "type": "object",
        "properties": {
          "languages": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "name": {"type": "string"},
                "year_created": {"type": "integer"},
                "paradigm": {"type": "string"}
              },
              "required": ["name"]
            }
          }
        },
        "required": ["languages"]
      },
      "strict": true
    }
  }
}
```

---

## Multi-Modal Requests

### ContentPart Structure

```go
type ContentPart struct {
    Type     string    `json:"type"` // "text" or "image_url"
    Text     string    `json:"text,omitempty"`
    ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
    URL    string `json:"url"`
    Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}
```

### Vision Request Example

```json
{
  "model": "gpt-4-vision",
  "messages": [
    {
      "role": "user",
      "content": [
        {
          "type": "text",
          "text": "What's in this image?"
        },
        {
          "type": "image_url",
          "image_url": {
            "url": "https://example.com/image.jpg",
            "detail": "high"
          }
        }
      ]
    }
  ],
  "max_tokens": 300
}
```

### Image Detail Levels

- **auto**: Model chooses detail level
- **low**: Low resolution (faster, cheaper)
- **high**: High resolution (better quality, more expensive)

---

## Usage Examples

### Basic Request

```typescript
const request: ChatRequest = {
  id: 'req_' + Date.now(),
  model: 'gpt-4',
  messages: [
    {
      role: 'system',
      content: 'You are a helpful assistant.'
    },
    {
      role: 'user',
      content: 'Explain quantum computing in simple terms.'
    }
  ],
  temperature: 0.7,
  max_tokens: 500,
  user_id: 'user_123',
  application_id: 'app_456',
  timestamp: new Date().toISOString()
};
```

### Request with Retry and Fallback

```typescript
const request: ChatRequest = {
  id: 'req_' + Date.now(),
  model: 'gpt-4',
  messages: [...],
  optimize_for: 'cost',
  retry_config: {
    max_attempts: 3,
    backoff_type: 'exponential',
    base_delay: 1000,
    max_delay: 30000,
    retryable_errors: ['rate_limit', 'timeout']
  },
  fallback_config: {
    enabled: true,
    preferred_chain: ['openai', 'anthropic'],
    max_cost_increase: 0.5,
    require_same_features: true
  },
  user_id: 'user_123',
  application_id: 'app_456',
  timestamp: new Date().toISOString()
};
```

### Function Calling Request

```python
request = ChatRequest(
    id="req_abc123",
    model="gpt-4",
    messages=[
        Message(
            role="user",
            content="What's the weather like in San Francisco?"
        )
    ],
    tools=[
        Tool(
            type="function",
            function=Function(
                name="get_weather",
                description="Get weather for a location",
                parameters={
                    "type": "object",
                    "properties": {
                        "location": {"type": "string"}
                    },
                    "required": ["location"]
                }
            )
        )
    ],
    tool_choice="auto",
    user_id="user_123",
    application_id="app_456"
)
```

---

## Related Documentation

- [Response Format](./response-format.md) - Response structures
- [Model Configurations](./model-configurations.md) - Supported models and capabilities
- [Router Architecture](../architecture/router-design.md) - Routing logic
- [Error Handling](../errors/error-codes.md) - Error codes and handling

---

**Document Version**: 1.0.0
**Last Updated**: 2026-01-06
**Maintained By**: TAS Platform Team
