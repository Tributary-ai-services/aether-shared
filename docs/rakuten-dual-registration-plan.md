# TAS: Rakuten AI 3.0 Dual-Registration Implementation Plan
# For use with Claude Code CLI

## Context
Integrate Rakuten AI 3.0 (700B MoE, Apache 2.0) into the TAS platform in two roles:
1. As a **Router Provider** in TAS-LLM-Router (primary inference for Japanese-language and cost-optimized requests)
2. As an **MCP Skill** in TAS-MCP-Services (post-processing tool callable by any agent/LLM)

Both registrations share a single Neo4j capability registry node to prevent version drift.

---

## Phase 1: Infrastructure — Rakuten AI 3.0 Endpoint

### 1.1 Hugging Face Model Pull
- Pull `Rakuten/RakutenAI-3.0-instruct` from Hugging Face (Apache 2.0, open-weight)
- Model specs: ~671B total params, ~37B active (MoE), 128K context
- Confirm GPU cluster availability for inference (vLLM recommended for MoE)

### 1.2 vLLM Inference Server
- Deploy vLLM with MoE support targeting Rakuten AI 3.0
- Expose OpenAI-compatible `/v1/chat/completions` endpoint
- Configure:
  - `--max-model-len 131072` (128K context)
  - `--tensor-parallel-size` appropriate to GPU count
  - `--served-model-name rakuten-ai-3.0`
- Health check endpoint: `GET /health`
- Target: internal cluster URL, e.g. `http://rakuten-inference.tas.internal:8000`

### 1.3 Inference Wrapper Service (tas-rakuten-adapter)
- Thin Go or Python service that:
  - Accepts TAS-normalized LLMRequest
  - Translates to vLLM OpenAI-compatible format
  - Handles auth, retries, timeout (default 30s)
  - Returns TAS-normalized LLMResponse with token counts
- Exposes two endpoints:
  - `POST /v1/infer` — full inference (router path)
  - `POST /v1/postprocess` — constrained translation/localization (MCP path)
- Deploy as Kubernetes Deployment in `tas-inference` namespace

---

## Phase 2: Neo4j Capability Registry — Dual-Role Node

### 2.1 Node Schema
Create a single node representing Rakuten AI 3.0 with dual role properties:

```cypher
CREATE (r:AICapability {
  id: 'rakuten-ai-3.0',
  name: 'Rakuten AI 3.0',
  version: '3.0',
  type: ['LLMProvider', 'MCPSkill'],
  languages: ['ja', 'en'],
  parameter_count: '671B',
  active_params: '37B',
  context_window: 131072,
  architecture: 'MoE',
  license: 'Apache-2.0',

  // Router provider contract
  router_endpoint: 'http://tas-rakuten-adapter.tas-inference.svc/v1/infer',
  router_model_id: 'rakuten-ai-3.0',
  cost_per_1k_input_tokens: <tbd_after_benchmarking>,
  cost_per_1k_output_tokens: <tbd_after_benchmarking>,
  avg_latency_ms: <tbd_after_benchmarking>,
  max_concurrent_requests: 50,

  // MCP skill contract
  mcp_tool_name: 'japanese_postprocess',
  mcp_endpoint: 'http://tas-rakuten-adapter.tas-inference.svc/v1/postprocess',
  mcp_input_schema: 'PostprocessRequest',
  mcp_output_schema: 'PostprocessResponse',

  // Metadata
  registered_at: datetime(),
  last_health_check: null,
  status: 'initializing'
})
```

### 2.2 Routing Relationships
```cypher
// Connect to language capability nodes
MATCH (lang:Language {code: 'ja'})
MATCH (r:AICapability {id: 'rakuten-ai-3.0'})
CREATE (r)-[:OPTIMIZED_FOR]->(lang)

// Connect to cost tier
MATCH (tier:CostTier {level: 'low'})
CREATE (r)-[:BELONGS_TO]->(tier)

// Connect to existing providers for fallback chain
MATCH (claude:AICapability {id: 'claude-sonnet-4-5'})
CREATE (r)-[:FALLBACK_TO]->(claude)
```

### 2.3 Version Update Procedure
When upgrading to a future Rakuten AI version:
```cypher
MATCH (r:AICapability {id: 'rakuten-ai-3.0'})
SET r.version = '3.1',
    r.router_endpoint = '<new_endpoint>',
    r.mcp_endpoint = '<new_endpoint>',
    r.last_updated = datetime()
```
Single node update propagates to both router and MCP contexts automatically.

---

## Phase 3: TAS-LLM-Router Integration

### 3.1 Provider Registration
File: `tas-llm-router/providers/rakuten.go` (or equivalent)

```go
type RakutenProvider struct {
    Name        string
    Endpoint    string
    ModelID     string
    MaxTokens   int
    Languages   []string
}

func (p *RakutenProvider) Invoke(ctx context.Context, req LLMRequest) (LLMResponse, error) {
    // Translate to OpenAI-compat format
    // POST to tas-rakuten-adapter /v1/infer
    // Return normalized LLMResponse
}
```

### 3.2 Routing Rules
Add to router decision logic:

**Rule 1 — Language Detection**
```yaml
- name: japanese_language_route
  condition:
    detected_language: ja
    confidence_threshold: 0.85
  action:
    provider: rakuten-ai-3.0
    priority: 1
    fallback: claude-sonnet-4-5
```

**Rule 2 — Cost Optimization**
```yaml
- name: cost_optimized_route
  condition:
    cost_tier: low
    task_complexity: simple_or_medium
    language: [ja, en]
  action:
    provider: rakuten-ai-3.0
    priority: 2
    fallback: claude-haiku-4-5
```

**Rule 3 — Direct Japanese Prompt**
```yaml
- name: japanese_prompt_route
  condition:
    prompt_script: [hiragana, katakana, kanji]
    min_japanese_char_ratio: 0.3
  action:
    provider: rakuten-ai-3.0
    priority: 1
```

### 3.3 Language Detection Utility
If not already present in the router, add lightweight language detection:
- Use `lingua-go` or `whatlanggo` for fast script/language detection
- Run pre-routing on the first 512 tokens of the prompt
- Cache detection result in request context for downstream use

### 3.4 Router Config Update
```yaml
# tas-llm-router/config/providers.yaml
providers:
  - id: rakuten-ai-3.0
    type: openai_compat
    endpoint: http://tas-rakuten-adapter.tas-inference.svc/v1/infer
    model_id: rakuten-ai-3.0
    context_window: 131072
    languages: [ja, en]
    enabled: true
    health_check_interval: 30s
```

---

## Phase 4: TAS-MCP-Services Integration

### 4.1 MCP Tool Definition
File: `tas-mcp-services/tools/japanese_postprocess.json`

```json
{
  "name": "japanese_postprocess",
  "description": "Translate or localize text to Japanese using Rakuten AI 3.0. Optimized for Japanese language fidelity including cultural context, formality levels, and domain-specific terminology.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "text": {
        "type": "string",
        "description": "Text to translate or localize to Japanese"
      },
      "formality": {
        "type": "string",
        "enum": ["casual", "polite", "formal", "business"],
        "default": "polite",
        "description": "Japanese formality register (keigo level)"
      },
      "domain": {
        "type": "string",
        "enum": ["general", "medical", "legal", "technical", "ecommerce"],
        "default": "general"
      },
      "preserve_structure": {
        "type": "boolean",
        "default": true,
        "description": "Preserve markdown/HTML structure in output"
      }
    },
    "required": ["text"]
  },
  "outputSchema": {
    "type": "object",
    "properties": {
      "translated_text": { "type": "string" },
      "detected_source_language": { "type": "string" },
      "confidence": { "type": "number" },
      "tokens_used": { "type": "integer" }
    }
  }
}
```

### 4.2 MCP Tool Handler
File: `tas-mcp-services/handlers/japanese_postprocess.go`

```go
func HandleJapanesePostprocess(ctx context.Context, params PostprocessRequest) (PostprocessResponse, error) {
    // Build constrained prompt for Rakuten adapter
    prompt := buildTranslationPrompt(params.Text, params.Formality, params.Domain)

    resp, err := rakutenClient.Postprocess(ctx, PostprocessCall{
        Prompt:    prompt,
        MaxTokens: estimateOutputTokens(params.Text),
    })

    return PostprocessResponse{
        TranslatedText:          resp.Text,
        DetectedSourceLanguage:  resp.SourceLang,
        Confidence:              resp.Confidence,
        TokensUsed:              resp.Usage.TotalTokens,
    }, err
}
```

### 4.3 MCP Server Registration
Register the new tool in TAS-MCP-Services federation:

```yaml
# tas-mcp-services/registry/tools.yaml
- tool_id: japanese_postprocess
  handler: handlers.HandleJapanesePostprocess
  backend: rakuten-ai-3.0
  backend_role: mcp_skill
  rate_limit: 100/min
  timeout: 15s
  cache_ttl: 300s  # Cache identical translation requests
  tags: [translation, japanese, postprocessing, localization]
```

---

## Phase 5: Argo Workflows — Hybrid Path

### 5.1 Japanese-Aware Agent Workflow
Add a workflow template for the hybrid pattern (Claude reasons + Rakuten localizes):

```yaml
# argo/workflows/japanese-aware-agent.yaml
apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: japanese-aware-agent
spec:
  templates:
  - name: main
    dag:
      tasks:
      - name: detect-language
        template: language-detect
        arguments:
          parameters:
          - name: text
            value: "{{workflow.parameters.input}}"

      - name: route-decision
        dependencies: [detect-language]
        template: routing-branch
        arguments:
          parameters:
          - name: detected_lang
            value: "{{tasks.detect-language.outputs.parameters.language}}"
          - name: cost_tier
            value: "{{workflow.parameters.cost_tier}}"

      # Path A: Direct to Rakuten (Japanese prompt or low cost)
      - name: rakuten-direct
        dependencies: [route-decision]
        when: "{{tasks.route-decision.outputs.parameters.path}} == direct_rakuten"
        template: llm-invoke
        arguments:
          parameters:
          - name: provider
            value: rakuten-ai-3.0
          - name: prompt
            value: "{{workflow.parameters.input}}"

      # Path B: Claude reasons, Rakuten translates
      - name: claude-reason
        dependencies: [route-decision]
        when: "{{tasks.route-decision.outputs.parameters.path}} == hybrid"
        template: llm-invoke
        arguments:
          parameters:
          - name: provider
            value: claude-sonnet-4-5
          - name: prompt
            value: "{{workflow.parameters.input}}"

      - name: rakuten-localize
        dependencies: [claude-reason]
        when: "{{tasks.route-decision.outputs.parameters.path}} == hybrid"
        template: mcp-tool-invoke
        arguments:
          parameters:
          - name: tool
            value: japanese_postprocess
          - name: text
            value: "{{tasks.claude-reason.outputs.parameters.response}}"
          - name: formality
            value: "{{workflow.parameters.formality}}"
```

---

## Phase 6: Observability

### 6.1 Metrics to Track
Add to existing TAS observability stack:

```
# Router provider metrics
tas_router_requests_total{provider="rakuten-ai-3.0", path="direct"}
tas_router_latency_p99{provider="rakuten-ai-3.0"}
tas_router_cost_per_request{provider="rakuten-ai-3.0"}
tas_router_language_detection_accuracy

# MCP skill metrics
tas_mcp_tool_invocations_total{tool="japanese_postprocess"}
tas_mcp_tool_latency_p99{tool="japanese_postprocess"}
tas_mcp_translation_confidence_avg{tool="japanese_postprocess"}
```

### 6.2 Neo4j Health Update Job
Argo CronWorkflow to update capability registry health:

```yaml
schedule: "*/5 * * * *"
# Ping rakuten adapter health endpoint
# Update Neo4j node: last_health_check, status, avg_latency_ms
```

---

## Phase 7: Testing

### 7.1 Router Path Tests
```
- Japanese prompt → assert provider == rakuten-ai-3.0
- English prompt, low cost tier → assert provider == rakuten-ai-3.0
- English prompt, high complexity → assert provider != rakuten-ai-3.0
- Rakuten unavailable → assert fallback to claude-sonnet-4-5
```

### 7.2 MCP Skill Tests
```
- POST japanese_postprocess with English text → assert valid Japanese output
- Test formality levels (casual vs business keigo)
- Test domain-specific vocabulary (medical, legal)
- Test preserve_structure flag with markdown input
```

### 7.3 Hybrid Path Test
```
- Complex reasoning prompt in English with Japanese output requirement
- Assert Claude used for reasoning step
- Assert Rakuten used for localization step
- Assert final output is valid Japanese
- Compare translation quality vs. Claude-only Japanese output
```

---

## Implementation Order

1. [ ] Phase 1: Deploy inference infrastructure (vLLM + adapter)
2. [ ] Phase 2: Neo4j dual-role node + relationships
3. [ ] Phase 3: Router provider registration + routing rules
4. [ ] Phase 4: MCP tool definition + handler
5. [ ] Phase 5: Argo hybrid workflow template
6. [ ] Phase 6: Observability metrics
7. [ ] Phase 7: Tests

## Open Questions / Decisions Needed
- GPU cluster capacity for 37B active param MoE inference — confirm with infra team
- Cost per token benchmarking needed to set router cost-tier thresholds
- Formality level mapping — align with existing TAS request schema or add new field
- Cache strategy for MCP postprocess tool — Redis TTL vs. in-memory
- Rakuten AI 3.0 rate limits when self-hosted vs. any Rakuten-managed API offering

