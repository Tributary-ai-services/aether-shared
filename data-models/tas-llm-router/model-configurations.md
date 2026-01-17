# TAS LLM Router - Model Configurations

## Metadata

- **Document Type**: Configuration Documentation
- **Service**: TAS LLM Router  
- **Component**: Model Management
- **Last Updated**: 2026-01-06
- **Owner**: TAS Platform Team
- **Status**: Active

---

## Overview

### Purpose

This document provides comprehensive configuration details for all LLM models supported by the TAS LLM Router. It includes model specifications, capabilities, pricing, context windows, and provider-specific features to enable intelligent routing decisions.

### Supported Providers

- **OpenAI**: GPT-4 family, GPT-3.5 family
- **Anthropic**: Claude 3 family (Opus, Sonnet, Haiku)
- **Future**: Cohere, AI21, Mistral, local models

---

## Table of Contents

1. [Model Information Structure](#model-information-structure)
2. [Provider Capabilities](#provider-capabilities)
3. [OpenAI Models](#openai-models)
4. [Anthropic Models](#anthropic-models)
5. [Model Selection Logic](#model-selection-logic)
6. [Cost Comparison](#cost-comparison)
7. [Performance Benchmarks](#performance-benchmarks)
8. [Related Documentation](#related-documentation)

---

## Model Information Structure

### Go Definition

```go
type ModelInfo struct {
    Name                 string   `json:"name"`
    DisplayName          string   `json:"display_name"`
    MaxContextWindow     int      `json:"max_context_window"`
    MaxOutputTokens      int      `json:"max_output_tokens"`
    SupportsFunctions    bool     `json:"supports_functions"`
    SupportsVision       bool     `json:"supports_vision"`
    SupportsStructured   bool     `json:"supports_structured_output"`
    InputCostPer1K       float64  `json:"input_cost_per_1k"`
    OutputCostPer1K      float64  `json:"output_cost_per_1k"`

    // Provider-specific model info
    ProviderModelID      string   `json:"provider_model_id,omitempty"`
    Tags                 []string `json:"tags,omitempty"`
}
```

### Example Model Configuration

```json
{
  "name": "gpt-4",
  "display_name": "GPT-4",
  "max_context_window": 8192,
  "max_output_tokens": 4096,
  "supports_functions": true,
  "supports_vision": false,
  "supports_structured_output": true,
  "input_cost_per_1k": 0.03,
  "output_cost_per_1k": 0.06,
  "provider_model_id": "gpt-4-0613",
  "tags": ["general", "reasoning", "code"]
}
```

---

## Provider Capabilities

### ProviderCapabilities Structure

```go
type ProviderCapabilities struct {
    ProviderName              string                     `json:"provider_name"`
    SupportedModels           []ModelInfo                `json:"supported_models"`
    SupportsFunctions         bool                       `json:"supports_functions"`
    SupportsParallelFunctions bool                       `json:"supports_parallel_functions"`
    SupportsVision            bool                       `json:"supports_vision"`
    SupportsStructuredOutput  bool                       `json:"supports_structured_output"`
    SupportsStreaming         bool                       `json:"supports_streaming"`
    SupportsAssistants        bool                       `json:"supports_assistants"`
    SupportsBatch             bool                       `json:"supports_batch"`
    MaxContextWindow          int                        `json:"max_context_window"`
    SupportedImageFormats     []string                   `json:"supported_image_formats"`
    CostPer1KTokens           CostStructure              `json:"cost_per_1k_tokens"`

    // Provider-specific capabilities
    OpenAISpecific            *OpenAICapabilities        `json:"openai_specific,omitempty"`
    AnthropicSpecific         *AnthropicCapabilities     `json:"anthropic_specific,omitempty"`
}
```

### OpenAI-Specific Capabilities

```go
type OpenAICapabilities struct {
    SupportsJSONSchema        bool     `json:"supports_json_schema"`
    SupportsStrictMode        bool     `json:"supports_strict_mode"`
    SupportsLogProbs          bool     `json:"supports_log_probs"`
    SupportsSeed              bool     `json:"supports_seed"`
    SupportsSystemFingerprint bool     `json:"supports_system_fingerprint"`
    SupportsParallelFunctions bool     `json:"supports_parallel_functions"`
    MaxFunctionCalls          int      `json:"max_function_calls"`
    SupportedResponseFormats  []string `json:"supported_response_formats"`
}
```

### Anthropic-Specific Capabilities

```go
type AnthropicCapabilities struct {
    SupportsSystemMessages    bool     `json:"supports_system_messages"`
    MaxSystemMessageLength    int      `json:"max_system_message_length"`
    SupportsStopSequences     bool     `json:"supports_stop_sequences"`
    SupportsToolUse           bool     `json:"supports_tool_use"`
    MaxToolCalls              int      `json:"max_tool_calls"`
    SupportedStopSequences    []string `json:"supported_stop_sequences"`
}
```

---

## OpenAI Models

### GPT-4 Family

#### GPT-4 (8K)
```json
{
  "name": "gpt-4",
  "display_name": "GPT-4",
  "max_context_window": 8192,
  "max_output_tokens": 4096,
  "supports_functions": true,
  "supports_vision": false,
  "supports_structured_output": true,
  "input_cost_per_1k": 0.03,
  "output_cost_per_1k": 0.06,
  "provider_model_id": "gpt-4-0613",
  "tags": ["reasoning", "code", "analysis"]
}
```

**Capabilities**:
- Advanced reasoning and problem-solving
- Strong code generation
- Function calling with parallel execution
- Structured JSON output
- System fingerprinting

**Use Cases**:
- Complex reasoning tasks
- Code generation and debugging
- Data analysis
- Multi-step workflows

#### GPT-4 Turbo (128K)
```json
{
  "name": "gpt-4-turbo",
  "display_name": "GPT-4 Turbo",
  "max_context_window": 128000,
  "max_output_tokens": 4096,
  "supports_functions": true,
  "supports_vision": true,
  "supports_structured_output": true,
  "input_cost_per_1k": 0.01,
  "output_cost_per_1k": 0.03,
  "provider_model_id": "gpt-4-turbo-2024-04-09",
  "tags": ["reasoning", "code", "vision", "long-context"]
}
```

**Capabilities**:
- Massive 128K context window
- Vision capabilities (images)
- JSON schema mode with strict validation
- Parallel function calling
- Reduced cost vs GPT-4

**Use Cases**:
- Long document analysis
- Multi-modal tasks (text + images)
- Large codebase understanding
- Complex multi-step workflows

#### GPT-4 Vision
```json
{
  "name": "gpt-4-vision",
  "display_name": "GPT-4 Vision",
  "max_context_window": 128000,
  "max_output_tokens": 4096,
  "supports_functions": true,
  "supports_vision": true,
  "supports_structured_output": true,
  "input_cost_per_1k": 0.01,
  "output_cost_per_1k": 0.03,
  "provider_model_id": "gpt-4-vision-preview",
  "tags": ["vision", "multimodal", "analysis"]
}
```

**Capabilities**:
- Image understanding and analysis
- Chart/graph interpretation
- OCR and text extraction
- Visual reasoning

**Use Cases**:
- Document analysis with images
- Chart/diagram interpretation
- UI/UX analysis
- Visual content description

### GPT-3.5 Family

#### GPT-3.5 Turbo (16K)
```json
{
  "name": "gpt-3.5-turbo",
  "display_name": "GPT-3.5 Turbo",
  "max_context_window": 16385,
  "max_output_tokens": 4096,
  "supports_functions": true,
  "supports_vision": false,
  "supports_structured_output": true,
  "input_cost_per_1k": 0.0015,
  "output_cost_per_1k": 0.002,
  "provider_model_id": "gpt-3.5-turbo-0125",
  "tags": ["fast", "cost-effective", "general"]
}
```

**Capabilities**:
- Fast response times
- Cost-effective
- Function calling
- Good general performance

**Use Cases**:
- Simple queries
- High-throughput applications
- Cost-sensitive workloads
- Real-time chat

---

## Anthropic Models

### Claude 3 Family

#### Claude 3 Opus
```json
{
  "name": "claude-3-opus",
  "display_name": "Claude 3 Opus",
  "max_context_window": 200000,
  "max_output_tokens": 4096,
  "supports_functions": true,
  "supports_vision": true,
  "supports_structured_output": true,
  "input_cost_per_1k": 0.015,
  "output_cost_per_1k": 0.075,
  "provider_model_id": "claude-3-opus-20240229",
  "tags": ["reasoning", "creative", "long-context", "vision"]
}
```

**Capabilities**:
- Massive 200K context window
- Superior reasoning and analysis
- Vision capabilities
- Tool use support
- Excellent creative writing

**Use Cases**:
- Complex research tasks
- Long document analysis
- Creative writing
- Advanced reasoning

#### Claude 3 Sonnet
```json
{
  "name": "claude-3-sonnet",
  "display_name": "Claude 3 Sonnet",
  "max_context_window": 200000,
  "max_output_tokens": 4096,
  "supports_functions": true,
  "supports_vision": true,
  "supports_structured_output": true,
  "input_cost_per_1k": 0.003,
  "output_cost_per_1k": 0.015,
  "provider_model_id": "claude-3-sonnet-20240229",
  "tags": ["balanced", "cost-effective", "vision"]
}
```

**Capabilities**:
- Excellent cost/performance balance
- 200K context window
- Vision support
- Strong reasoning
- Fast responses

**Use Cases**:
- General-purpose applications
- Cost-optimized workflows
- Mixed workloads
- Production systems

#### Claude 3 Haiku
```json
{
  "name": "claude-3-haiku",
  "display_name": "Claude 3 Haiku",
  "max_context_window": 200000,
  "max_output_tokens": 4096,
  "supports_functions": true,
  "supports_vision": false,
  "supports_structured_output": true,
  "input_cost_per_1k": 0.00025,
  "output_cost_per_1k": 0.00125,
  "provider_model_id": "claude-3-haiku-20240307",
  "tags": ["fast", "cost-effective", "simple"]
}
```

**Capabilities**:
- Fastest response times
- Most cost-effective
- Good for simple tasks
- Large context window

**Use Cases**:
- Simple queries
- High-throughput applications
- Real-time chat
- Cost-sensitive workloads

---

## Model Selection Logic

### Routing Decision Flow

```go
func (r *Router) routeByCost(ctx context.Context, req *types.ChatRequest) (*RoutingDecision, providers.LLMProvider, error) {
    // Get healthy providers
    candidates := r.getHealthyProviders()

    // Filter by feature requirements
    candidates = r.filterByFeatures(candidates, req)

    // Get cost estimates
    var costsAndProviders []candidateWithCost

    for _, name := range candidates {
        provider := r.providers[name]
        costEst, err := provider.EstimateCost(req)
        if err != nil {
            continue
        }

        costsAndProviders = append(costsAndProviders, candidateWithCost{
            name:     name,
            provider: provider,
            cost:     costEst.TotalCost,
            estimate: costEst,
        })
    }

    // Sort by cost (ascending)
    sort.Slice(costsAndProviders, func(i, j int) bool {
        return costsAndProviders[i].cost < costsAndProviders[j].cost
    })

    // Select cheapest
    selected := costsAndProviders[0]

    return &RoutingDecision{
        SelectedProvider: selected.name,
        EstimatedCost:    selected.cost,
        RoutingContext:   buildRoutingContext("cost_optimized", req, candidates),
    }, selected.provider, nil
}
```

### Feature Compatibility Check

```go
func (r *Router) supportsRequiredFeatures(provider providers.LLMProvider, req *types.ChatRequest) bool {
    capabilities := provider.GetCapabilities()

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
        case "streaming":
            if !capabilities.SupportsStreaming {
                return false
            }
        }
    }

    return true
}
```

---

## Cost Comparison

### Cost per 1M Tokens (Input/Output)

| Model | Input Cost | Output Cost | Total (1M in + 1M out) |
|-------|-----------|-------------|------------------------|
| GPT-4 | $30 | $60 | $90 |
| GPT-4 Turbo | $10 | $30 | $40 |
| GPT-3.5 Turbo | $1.50 | $2.00 | $3.50 |
| Claude 3 Opus | $15 | $75 | $90 |
| Claude 3 Sonnet | $3 | $15 | $18 |
| Claude 3 Haiku | $0.25 | $1.25 | $1.50 |

### Cost Optimization Examples

**Example 1: Simple Query**
```
Input: 100 tokens
Output: 50 tokens

GPT-3.5 Turbo: $0.00030
Claude 3 Haiku: $0.00009
Savings: 70%
```

**Example 2: Long Document**
```
Input: 50,000 tokens
Output: 1,000 tokens

GPT-4 Turbo: $0.530
Claude 3 Sonnet: $0.165
Savings: 69%
```

---

## Performance Benchmarks

### Latency Comparison

| Model | Avg Latency | P95 Latency | P99 Latency |
|-------|-------------|-------------|-------------|
| GPT-4 | 800ms | 1200ms | 1800ms |
| GPT-4 Turbo | 600ms | 900ms | 1400ms |
| GPT-3.5 Turbo | 400ms | 600ms | 900ms |
| Claude 3 Opus | 1200ms | 1800ms | 2500ms |
| Claude 3 Sonnet | 800ms | 1200ms | 1600ms |
| Claude 3 Haiku | 500ms | 750ms | 1100ms |

### Throughput (requests/second)

| Model | Max Throughput |
|-------|---------------|
| GPT-4 | 50 rps |
| GPT-4 Turbo | 100 rps |
| GPT-3.5 Turbo | 500 rps |
| Claude 3 Opus | 30 rps |
| Claude 3 Sonnet | 100 rps |
| Claude 3 Haiku | 300 rps |

---

## Related Documentation

- [Request Format](./request-format.md) - Request structures
- [Response Format](./response-format.md) - Response structures
- [Router Architecture](../architecture/router-design.md) - Routing logic

---

**Document Version**: 1.0.0
**Last Updated**: 2026-01-06
**Maintained By**: TAS Platform Team
