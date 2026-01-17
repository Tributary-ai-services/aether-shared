# TAS LLM Router - Response Format

## Metadata

- **Document Type**: API Documentation
- **Service**: TAS LLM Router
- **Component**: Response Structures
- **Last Updated**: 2026-01-06
- **Owner**: TAS Platform Team
- **Status**: Active

---

## Overview

### Purpose

The TAS LLM Router Response Format defines the unified response structure returned from all LLM providers through the TAS platform. This standardized format ensures consistency across OpenAI, Anthropic, and other providers while adding valuable routing metadata and cost tracking.

### Key Features

- **Unified Response Format**: Consistent structure across all providers
- **Routing Metadata**: Detailed information about provider selection and retry attempts
- **Cost Tracking**: Actual cost calculations with estimated vs actual comparison
- **Usage Statistics**: Token counts and performance metrics
- **Streaming Support**: Server-sent events for real-time responses
- **Error Standardization**: Consistent error format across providers

---

## Table of Contents

1. [ChatResponse Structure](#chatresponse-structure)
2. [Choice Structure](#choice-structure)
3. [Usage Tracking](#usage-tracking)
4. [Router Metadata](#router-metadata)
5. [Cost Estimation](#cost-estimation)
6. [Streaming Responses](#streaming-responses)
7. [Error Responses](#error-responses)
8. [Usage Examples](#usage-examples)
9. [Related Documentation](#related-documentation)

---

## ChatResponse Structure

### Go Definition

```go
package types

import (
    "time"
)

// Response types
type ChatResponse struct {
    ID                string             `json:"id"`
    Object            string             `json:"object"`
    Created           int64              `json:"created"`
    Model             string             `json:"model"`
    Choices           []Choice           `json:"choices"`
    Usage             *Usage             `json:"usage,omitempty"`
    SystemFingerprint string             `json:"system_fingerprint,omitempty"`

    // Routing metadata (added by router)
    RouterMetadata    *RouterMetadata    `json:"router_metadata,omitempty"`
}
```

### TypeScript Definition

```typescript
interface ChatResponse {
  id: string;
  object: string;
  created: number;
  model: string;
  choices: Choice[];
  usage?: Usage;
  system_fingerprint?: string;

  // Routing metadata
  router_metadata?: RouterMetadata;
}
```

### Python Definition

```python
from typing import Optional, List
from pydantic import BaseModel

class ChatResponse(BaseModel):
    """Chat response model."""

    id: str
    object: str
    created: int
    model: str
    choices: List[Choice]
    usage: Optional[Usage] = None
    system_fingerprint: Optional[str] = None

    # Routing metadata
    router_metadata: Optional[RouterMetadata] = None
```

### Field Descriptions

#### id
- **Type**: String
- **Purpose**: Unique response identifier
- **Format**: Provider-specific (e.g., `"chatcmpl-123"`)
- **Example**: `"chatcmpl-abc123xyz"`

#### object
- **Type**: String
- **Value**: `"chat.completion"` or `"chat.completion.chunk"`
- **Purpose**: Response type identifier

#### created
- **Type**: Integer (Unix timestamp)
- **Purpose**: Response creation time
- **Example**: `1704537600`

#### model
- **Type**: String
- **Purpose**: Actual model used (may differ from requested)
- **Example**: `"gpt-4-0613"`

#### choices
- **Type**: Array of Choice objects
- **Purpose**: Generated completions
- **See**: [Choice Structure](#choice-structure)

#### usage
- **Type**: Usage object
- **Purpose**: Token consumption statistics
- **See**: [Usage Tracking](#usage-tracking)

#### system_fingerprint
- **Type**: String (optional)
- **Purpose**: OpenAI-specific backend configuration identifier
- **Example**: `"fp_44709d6fcb"`

#### router_metadata
- **Type**: RouterMetadata object
- **Purpose**: TAS routing information
- **See**: [Router Metadata](#router-metadata)

### Complete Response Example

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "created": 1704537600,
  "model": "gpt-4-0613",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Paris is the capital of France."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 15,
    "completion_tokens": 7,
    "total_tokens": 22
  },
  "system_fingerprint": "fp_44709d6fcb",
  "router_metadata": {
    "provider": "openai",
    "model": "gpt-4-0613",
    "routing_reason": [
      "Specific model requested: gpt-4",
      "Provider selected: openai"
    ],
    "estimated_cost": 0.00066,
    "actual_cost": 0.00066,
    "processing_time": "125ms",
    "request_id": "req_abc123",
    "provider_latency": "95ms",
    "attempt_count": 1,
    "fallback_used": false
  }
}
```

---

## Choice Structure

### Go Definition

```go
type Choice struct {
    Index        int          `json:"index"`
    Message      Message      `json:"message,omitempty"`
    Delta        *Message     `json:"delta,omitempty"`
    FinishReason string       `json:"finish_reason,omitempty"`
    Logprobs     *Logprobs    `json:"logprobs,omitempty"`
}
```

### TypeScript Definition

```typescript
interface Choice {
  index: number;
  message?: Message;
  delta?: Message;
  finish_reason?: string;
  logprobs?: Logprobs;
}
```

### Field Descriptions

#### index
- **Type**: Integer
- **Purpose**: Choice position in array
- **Example**: `0`

#### message
- **Type**: Message object
- **Purpose**: Complete message (non-streaming)
- **Contains**: `role`, `content`, optional `tool_calls`

#### delta
- **Type**: Message object
- **Purpose**: Incremental message chunk (streaming)
- **Contains**: Partial `content` or `tool_calls`

#### finish_reason
- **Type**: String
- **Purpose**: Why generation stopped
- **Values**:
  - `"stop"` - Natural completion
  - `"length"` - Max tokens reached
  - `"function_call"` - Function called (deprecated)
  - `"tool_calls"` - Tool calls generated
  - `"content_filter"` - Content filtered
  - `null` - Still generating (streaming)

#### logprobs
- **Type**: Logprobs object (optional)
- **Purpose**: Token log probabilities
- **Availability**: OpenAI only, when requested

### Choice Examples

**Standard Completion**:
```json
{
  "index": 0,
  "message": {
    "role": "assistant",
    "content": "The capital of France is Paris."
  },
  "finish_reason": "stop"
}
```

**Function Call**:
```json
{
  "index": 0,
  "message": {
    "role": "assistant",
    "content": null,
    "tool_calls": [
      {
        "id": "call_abc123",
        "type": "function",
        "function": {
          "name": "get_weather",
          "arguments": "{\"location\": \"San Francisco\"}"
        }
      }
    ]
  },
  "finish_reason": "tool_calls"
}
```

**Streaming Delta**:
```json
{
  "index": 0,
  "delta": {
    "content": " Paris"
  },
  "finish_reason": null
}
```

---

## Usage Tracking

### Structure

```go
type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}
```

### TypeScript

```typescript
interface Usage {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}
```

### Field Descriptions

#### prompt_tokens
- **Type**: Integer
- **Purpose**: Number of tokens in the prompt
- **Includes**: All messages, system instructions, function definitions
- **Example**: `150`

#### completion_tokens
- **Type**: Integer
- **Purpose**: Number of tokens in the completion
- **Includes**: Generated response content
- **Example**: `75`

#### total_tokens
- **Type**: Integer
- **Purpose**: Sum of prompt_tokens and completion_tokens
- **Formula**: `prompt_tokens + completion_tokens`
- **Example**: `225`

### Usage Example

```json
{
  "usage": {
    "prompt_tokens": 150,
    "completion_tokens": 75,
    "total_tokens": 225
  }
}
```

### Cost Calculation

```go
// Calculate cost from usage
func CalculateCost(usage *Usage, model string) float64 {
    rates := getModelRates(model)

    inputCost := float64(usage.PromptTokens) / 1000.0 * rates.InputCostPer1K
    outputCost := float64(usage.CompletionTokens) / 1000.0 * rates.OutputCostPer1K

    return inputCost + outputCost
}
```

---

## Router Metadata

### Structure

```go
// Router-specific types
type RouterMetadata struct {
    Provider         string        `json:"provider"`
    Model            string        `json:"model"`
    RoutingReason    []string      `json:"routing_reason"`
    EstimatedCost    float64       `json:"estimated_cost"`
    ActualCost       float64       `json:"actual_cost,omitempty"`
    ProcessingTime   time.Duration `json:"processing_time"`
    RequestID        string        `json:"request_id"`
    ProviderLatency  time.Duration `json:"provider_latency"`

    // Retry and fallback metadata
    AttemptCount     int      `json:"attempt_count"`
    FailedProviders  []string `json:"failed_providers,omitempty"`
    FallbackUsed     bool     `json:"fallback_used"`
    RetryDelays      []int64  `json:"retry_delays,omitempty"`
    TotalRetryTime   int64    `json:"total_retry_time,omitempty"`
}
```

### TypeScript

```typescript
interface RouterMetadata {
  provider: string;
  model: string;
  routing_reason: string[];
  estimated_cost: number;
  actual_cost?: number;
  processing_time: number; // milliseconds
  request_id: string;
  provider_latency: number; // milliseconds

  // Retry and fallback
  attempt_count: number;
  failed_providers?: string[];
  fallback_used: boolean;
  retry_delays?: number[];
  total_retry_time?: number;
}
```

### Field Descriptions

#### provider
- **Type**: String
- **Purpose**: Provider that handled the request
- **Examples**: `"openai"`, `"anthropic"`

#### model
- **Type**: String
- **Purpose**: Exact model version used
- **Example**: `"gpt-4-0613"`

#### routing_reason
- **Type**: Array of strings
- **Purpose**: Explanation of routing decision
- **Example**: `["Cost-optimized routing", "Selected cheapest provider"]`

#### estimated_cost
- **Type**: Float
- **Purpose**: Pre-request cost estimate
- **Unit**: USD
- **Example**: `0.00066`

#### actual_cost
- **Type**: Float
- **Purpose**: Actual cost based on usage
- **Unit**: USD
- **Example**: `0.00068`

#### processing_time
- **Type**: Duration (milliseconds)
- **Purpose**: Total request processing time
- **Includes**: Routing + provider call + processing
- **Example**: `125`

#### request_id
- **Type**: String
- **Purpose**: Original request identifier
- **Example**: `"req_abc123"`

#### provider_latency
- **Type**: Duration (milliseconds)
- **Purpose**: Provider API response time
- **Excludes**: Routing and preprocessing
- **Example**: `95`

#### attempt_count
- **Type**: Integer
- **Purpose**: Number of attempts made
- **Note**: 1 = no retries, 3 = 2 retries
- **Example**: `1`

#### failed_providers
- **Type**: Array of strings
- **Purpose**: Providers that failed before success
- **Example**: `["openai"]`

#### fallback_used
- **Type**: Boolean
- **Purpose**: Whether fallback mechanism was triggered
- **Example**: `false`

#### retry_delays
- **Type**: Array of integers (milliseconds)
- **Purpose**: Delays between retry attempts
- **Example**: `[1000, 2000]`

#### total_retry_time
- **Type**: Integer (milliseconds)
- **Purpose**: Total time spent on retries
- **Example**: `3000`

### Router Metadata Examples

**Simple Request**:
```json
{
  "router_metadata": {
    "provider": "openai",
    "model": "gpt-4-0613",
    "routing_reason": ["Specific model requested: gpt-4"],
    "estimated_cost": 0.00066,
    "actual_cost": 0.00066,
    "processing_time": "125ms",
    "request_id": "req_abc123",
    "provider_latency": "95ms",
    "attempt_count": 1,
    "fallback_used": false
  }
}
```

**With Retries**:
```json
{
  "router_metadata": {
    "provider": "openai",
    "model": "gpt-4-0613",
    "routing_reason": ["Cost-optimized routing", "Retry successful on attempt 2"],
    "estimated_cost": 0.00066,
    "actual_cost": 0.00066,
    "processing_time": "3250ms",
    "request_id": "req_abc123",
    "provider_latency": "95ms",
    "attempt_count": 2,
    "fallback_used": false,
    "retry_delays": [1000],
    "total_retry_time": 1150
  }
}
```

**With Fallback**:
```json
{
  "router_metadata": {
    "provider": "anthropic",
    "model": "claude-3-sonnet",
    "routing_reason": [
      "Primary provider failed",
      "Fallback to anthropic",
      "Cost increase: 15%"
    ],
    "estimated_cost": 0.00066,
    "actual_cost": 0.00076,
    "processing_time": "4500ms",
    "request_id": "req_abc123",
    "provider_latency": "120ms",
    "attempt_count": 3,
    "failed_providers": ["openai"],
    "fallback_used": true,
    "retry_delays": [1000, 2000],
    "total_retry_time": 3200
  }
}
```

---

## Cost Estimation

### Structure

```go
type CostEstimate struct {
    InputTokens      int     `json:"input_tokens"`
    OutputTokens     int     `json:"output_tokens,omitempty"`
    TotalTokens      int     `json:"total_tokens"`
    InputCost        float64 `json:"input_cost"`
    OutputCost       float64 `json:"output_cost"`
    TotalCost        float64 `json:"total_cost"`
    CostPer1KTokens  float64 `json:"cost_per_1k_tokens"`
}
```

### TypeScript

```typescript
interface CostEstimate {
  input_tokens: number;
  output_tokens?: number;
  total_tokens: number;
  input_cost: number;
  output_cost: number;
  total_cost: number;
  cost_per_1k_tokens: number;
}
```

### Cost Calculation Example

```python
def estimate_cost(
    prompt_tokens: int,
    max_tokens: int,
    model: str
) -> CostEstimate:
    """Estimate request cost."""

    rates = get_model_rates(model)

    input_cost = (prompt_tokens / 1000.0) * rates.input_cost_per_1k
    output_cost = (max_tokens / 1000.0) * rates.output_cost_per_1k
    total_cost = input_cost + output_cost

    return CostEstimate(
        input_tokens=prompt_tokens,
        output_tokens=max_tokens,
        total_tokens=prompt_tokens + max_tokens,
        input_cost=input_cost,
        output_cost=output_cost,
        total_cost=total_cost,
        cost_per_1k_tokens=rates.avg_cost_per_1k
    )
```

### Model Pricing (as of 2026-01-06)

**GPT-4**:
- Input: $0.03 per 1K tokens
- Output: $0.06 per 1K tokens

**GPT-3.5 Turbo**:
- Input: $0.0015 per 1K tokens
- Output: $0.002 per 1K tokens

**Claude 3 Opus**:
- Input: $0.015 per 1K tokens
- Output: $0.075 per 1K tokens

**Claude 3 Sonnet**:
- Input: $0.003 per 1K tokens
- Output: $0.015 per 1K tokens

---

## Streaming Responses

### Structure

```go
type ChatChunk struct {
    ID                string             `json:"id"`
    Object            string             `json:"object"`
    Created           int64              `json:"created"`
    Model             string             `json:"model"`
    Choices           []ChoiceChunk      `json:"choices"`
    Usage             *Usage             `json:"usage,omitempty"`
    SystemFingerprint string             `json:"system_fingerprint,omitempty"`

    // Routing metadata
    RouterMetadata    *RouterMetadata    `json:"router_metadata,omitempty"`
}

type ChoiceChunk struct {
    Index        int          `json:"index"`
    Delta        *Message     `json:"delta,omitempty"`
    FinishReason string       `json:"finish_reason,omitempty"`
    Logprobs     *Logprobs    `json:"logprobs,omitempty"`
}
```

### Streaming Example

**First Chunk**:
```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion.chunk",
  "created": 1704537600,
  "model": "gpt-4-0613",
  "choices": [
    {
      "index": 0,
      "delta": {
        "role": "assistant",
        "content": ""
      },
      "finish_reason": null
    }
  ]
}
```

**Content Chunks**:
```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion.chunk",
  "created": 1704537600,
  "model": "gpt-4-0613",
  "choices": [
    {
      "index": 0,
      "delta": {
        "content": "Paris"
      },
      "finish_reason": null
    }
  ]
}
```

**Final Chunk**:
```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion.chunk",
  "created": 1704537600,
  "model": "gpt-4-0613",
  "choices": [
    {
      "index": 0,
      "delta": {},
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 15,
    "completion_tokens": 7,
    "total_tokens": 22
  },
  "router_metadata": {
    "provider": "openai",
    "actual_cost": 0.00066,
    "processing_time": "2500ms"
  }
}
```

---

## Error Responses

### Structure

```go
// Error response
type ErrorResponse struct {
    Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
    Message string `json:"message"`
    Type    string `json:"type"`
    Param   string `json:"param,omitempty"`
    Code    string `json:"code,omitempty"`
}
```

### TypeScript

```typescript
interface ErrorResponse {
  error: ErrorDetail;
}

interface ErrorDetail {
  message: string;
  type: string;
  param?: string;
  code?: string;
}
```

### Error Types

| Type | Description | HTTP Status |
|------|-------------|-------------|
| `invalid_request_error` | Malformed request | 400 |
| `authentication_error` | Invalid API key | 401 |
| `permission_error` | Insufficient permissions | 403 |
| `not_found_error` | Resource not found | 404 |
| `rate_limit_error` | Rate limit exceeded | 429 |
| `provider_error` | Provider API error | 502 |
| `server_error` | Internal server error | 500 |

### Error Examples

**Invalid Request**:
```json
{
  "error": {
    "message": "Invalid model specified",
    "type": "invalid_request_error",
    "param": "model",
    "code": "invalid_model"
  }
}
```

**Rate Limit**:
```json
{
  "error": {
    "message": "Rate limit exceeded. Retry after 60 seconds.",
    "type": "rate_limit_error",
    "code": "rate_limit_exceeded"
  }
}
```

**Provider Error**:
```json
{
  "error": {
    "message": "Provider API error: Service temporarily unavailable",
    "type": "provider_error",
    "code": "provider_unavailable"
  }
}
```

---

## Usage Examples

### TypeScript Client

```typescript
interface LLMClient {
  async chat(request: ChatRequest): Promise<ChatResponse> {
    const response = await fetch('http://localhost:8085/v1/chat/completions', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${apiKey}`
      },
      body: JSON.stringify(request)
    });

    if (!response.ok) {
      const error: ErrorResponse = await response.json();
      throw new Error(error.error.message);
    }

    const result: ChatResponse = await response.json();

    // Access router metadata
    console.log('Provider:', result.router_metadata?.provider);
    console.log('Cost:', result.router_metadata?.actual_cost);
    console.log('Latency:', result.router_metadata?.provider_latency);

    return result;
  }

  async *chatStream(request: ChatRequest): AsyncGenerator<ChatChunk> {
    const response = await fetch('http://localhost:8085/v1/chat/completions', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${apiKey}`
      },
      body: JSON.stringify({ ...request, stream: true })
    });

    const reader = response.body!.getReader();
    const decoder = new TextDecoder();

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      const chunk = decoder.decode(value);
      const lines = chunk.split('\n');

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          const data = line.slice(6);
          if (data === '[DONE]') return;

          const parsed: ChatChunk = JSON.parse(data);
          yield parsed;
        }
      }
    }
  }
}
```

### Python Client

```python
from typing import Iterator
import httpx

class LLMClient:
    def __init__(self, base_url: str, api_key: str):
        self.base_url = base_url
        self.api_key = api_key

    async def chat(self, request: ChatRequest) -> ChatResponse:
        """Send chat request."""
        async with httpx.AsyncClient() as client:
            response = await client.post(
                f"{self.base_url}/v1/chat/completions",
                json=request.dict(),
                headers={"Authorization": f"Bearer {self.api_key}"}
            )

            if response.status_code != 200:
                error = response.json()
                raise Exception(error['error']['message'])

            result = ChatResponse(**response.json())

            # Log metadata
            if result.router_metadata:
                print(f"Provider: {result.router_metadata.provider}")
                print(f"Cost: ${result.router_metadata.actual_cost:.6f}")
                print(f"Latency: {result.router_metadata.provider_latency}ms")

            return result

    async def chat_stream(
        self,
        request: ChatRequest
    ) -> Iterator[ChatChunk]:
        """Stream chat response."""
        request.stream = True

        async with httpx.AsyncClient() as client:
            async with client.stream(
                'POST',
                f"{self.base_url}/v1/chat/completions",
                json=request.dict(),
                headers={"Authorization": f"Bearer {self.api_key}"}
            ) as response:
                async for line in response.aiter_lines():
                    if line.startswith('data: '):
                        data = line[6:]
                        if data == '[DONE]':
                            break

                        chunk = ChatChunk(**json.loads(data))
                        yield chunk
```

---

## Related Documentation

- [Request Format](./request-format.md) - Request structures
- [Model Configurations](./model-configurations.md) - Supported models
- [Router Architecture](../architecture/router-design.md) - Routing logic
- [Cost Optimization](../guides/cost-optimization.md) - Cost management

---

**Document Version**: 1.0.0
**Last Updated**: 2026-01-06
**Maintained By**: TAS Platform Team
