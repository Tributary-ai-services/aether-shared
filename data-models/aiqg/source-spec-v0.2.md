**AI Quality Gateway**

A Methodology for Measuring Enterprise AI Stack Performance

*Stakeholder Review Draft \| v0.2*

# Executive Summary

Enterprise AI projects fail at unprecedented rates. MIT\'s 2025 study
found that **95% of AI pilots deliver zero measurable P&L impact**, and
S&P Global reports that the share of companies abandoning AI projects
jumped from 17% to 42% in a single year. The root cause is consistent:
organizations cannot see what is actually happening inside their AI
stack, lack the engineering depth to diagnose it, or both.

The **AI Quality Gateway** is a new product line --- built on existing
TAS Gatekeeper infrastructure --- that gives enterprises a self-service
way to measure the cost, latency, quality, and operational health of
their AI stack. Customers set two environment variables and add one line
of SDK configuration. Within 24 hours, they receive a diagnostic report
identifying where their AI investment is leaking value.

The methodology extends the published **CLEAR framework** (Cost,
Latency, Efficacy, Assurance, Reliability) --- a peer-validated
five-dimensional evaluation framework for enterprise AI --- adapted for
inline gateway measurement. The gateway position uniquely enables
measurement of CLEAR dimensions on production traffic and adds
input-side analysis that controlled benchmarks cannot capture.

## Why This Product, Why Now

-   **The diagnostic gap is universal.** Enterprises know their AI
    projects are underperforming but cannot diagnose why. CLEAR\'s
    empirical validation demonstrates that multi-dimensional measurement
    predicts production success substantially better than accuracy-only
    evaluation (ρ=0.83 vs ρ=0.41 against expert deployment-readiness
    judgment).

-   **Existing tools observe; they don\'t diagnose or intervene.** LLM
    observability vendors provide dashboards and traces. None produce a
    prescriptive plain-English diagnosis, and none can transparently
    reduce token waste in-flight.

-   **The gateway position is structurally advantaged.** A proxy sees
    the full request --- including the context, retrieval, and tool
    definitions injected before the model call --- and can intervene on
    it. Observability vendors only see what hits the model API. This
    visibility into and control over inputs is the architectural moat.

-   **It reinforces, not competes with, current TAS offerings.** The
    Gateway reuses Gatekeeper\'s pattern-matching and tagging
    infrastructure with new rule packs. It is a new product line
    targeting a different buyer (the AI program owner, not the security
    architect) and creates a natural upsell path into the broader TAS
    platform.

## What This Document Is Not

This is a **methodology and product definition**. Pricing, detailed
engineering specifications, GTM plans, and business-outcome integration
are intentionally deferred to subsequent documents. Section 5 defines
what ships in the MVP and what comes later. Section 6 lists open
questions for stakeholder input.

# 1. Methodology

## 1.1 The Problem We Are Measuring

Enterprise AI projects fail for predictable reasons. Every failed
deployment falls into one of five categories:

1.  **Bad inputs** --- retrieval returns wrong, stale, or fragmented
    content; context bloat; ambiguous queries; poor prompt design.

2.  **Bad reasoning** --- model hallucination, logical errors, tool
    misuse, confidence miscalibration.

3.  **Bad grounding** --- output doesn\'t match business need; wrong
    format, wrong action, right answer to wrong question.

4.  **Bad economics** --- works, but cost-per-outcome is unsustainable;
    retry waste; over-provisioned context.

5.  **Silent degradation** --- system worked at launch, slowly degrading
    from drift, data staleness, model updates, or API changes.

The methodology and the metrics in Section 2 are designed to diagnose
which category dominates for any given customer. The architecture in
Section 3 is designed to address most of these categories transparently
--- without requiring developer involvement --- once a customer commits
to the platform.

## 1.2 Methodological Foundations

The methodology rests on three published, validated foundations: the
CLEAR framework for the metric structure, the NIST AI Risk Management
Framework for the governance overlay, and the DORA precedent for the
structural pattern of how a small number of balanced metrics drives
improvement.

+-----------------------------------------------------------------------+
| **Sidebar: What is DORA, and why does it matter here?**               |
|                                                                       |
| The DevOps Research and Assessment (DORA) team at Google Cloud spent  |
| over a decade studying what distinguishes elite software delivery     |
| organizations from low performers. Their finding, published in the    |
| Accelerate book and refined across annual State of DevOps reports,    |
| was that a small number of carefully chosen metrics --- known as the  |
| Four Keys --- could predict organizational performance better than    |
| any larger dashboard.                                                 |
|                                                                       |
| The original Four Keys measured the speed and stability of software   |
| delivery: Deployment Frequency, Lead Time for Changes, Change Failure |
| Rate, and Mean Time to Recovery (since renamed Failed Deployment      |
| Recovery Time). The framework\'s power came not from the specific     |
| metrics, but from the structural insight: a small number of balanced, |
| hard-to-game metrics, organized around productive tension (velocity   |
| vs. stability), produces more actionable diagnosis than comprehensive |
| observability dashboards.                                             |
|                                                                       |
| This document does not propose AI versions of the DORA four keys. The |
| AI-specific evaluation framework we use is CLEAR, introduced below.   |
| What we borrow from DORA is the methodological pattern: a small       |
| number of balanced metrics, derived from the inline gateway position, |
| that an enterprise can act on.                                        |
+-----------------------------------------------------------------------+

### 1.2.1 The CLEAR Framework

CLEAR (Cost, Latency, Efficacy, Assurance, Reliability) is a
five-dimensional evaluation framework for enterprise agentic AI,
published in November 2025. It addresses three documented gaps in prior
AI evaluation work:

-   **Cost was unmeasured** despite agents exhibiting 50x cost
    variations for similar accuracy levels. Optimizing accuracy alone
    yielded agents 4.4--10.8x more expensive than Pareto-efficient
    alternatives.

-   **Reliability was untested** despite agent performance dropping from
    60% on single runs (pass@1) to 25% on 8-run consistency tests
    (pass@8) --- an unacceptable drop for production deployment.

-   **Operational dimensions were absent** --- security, policy
    compliance, latency under SLA constraints. A 37% performance gap
    between lab tests and production was documented across the field.

CLEAR\'s most important empirical finding: expert evaluation across N=15
enterprise AI deployment leads showed that CLEAR predictions correlate
with production-deployment-readiness judgment at **ρ=0.83**, versus
**ρ=0.41** for accuracy-only evaluation. This is the validation that
justifies using CLEAR as the methodological backbone of this document.
It is not a framework we invented; it is a framework with measured
predictive power that we adapt for the inline gateway position.

### 1.2.2 NIST AI Risk Management Framework

Where CLEAR provides the metric structure, the NIST AI RMF provides the
governance overlay. The framework defines seven trustworthy AI
characteristics: valid and reliable; safe; secure and resilient;
accountable and transparent; explainable and interpretable;
privacy-enhanced; and fair with harmful bias managed. NIST\'s four core
functions --- Govern, Map, Measure, Manage --- define what organizations
should achieve at each stage of the AI lifecycle.

The Gateway primarily implements the **Measure** function: it observes
production AI traffic and quantifies the trustworthiness characteristics
across the seven NIST dimensions. When active features are enabled
(payload reduction, policy enforcement), the Gateway also implements
parts of the **Manage** function. Mapping CLEAR\'s Assurance dimension
to NIST\'s specific trustworthiness characteristics is what gives
regulated-industry buyers the audit-trail framing they need.

## 1.3 What the Gateway Position Adds to CLEAR

CLEAR, as published, is designed primarily for *controlled-benchmark
evaluation* --- running agents against curated task suites with
ground-truth annotations. This is appropriate for academic research and
pre-deployment validation. It is *not sufficient* for continuous
measurement of production traffic, which is what enterprises actually
need to manage live AI systems.

The inline gateway position enables three measurement capabilities that
benchmark-style evaluation cannot provide:

-   **Production-traffic CLEAR measurement.** Every dimension of CLEAR
    can be measured on real customer requests in real time, not on
    synthetic benchmark tasks. This eliminates the documented 37%
    lab-to-production performance gap by measuring production directly.

-   **Input-side visibility.** The gateway sees the full request ---
    system prompt, retrieved context, tool definitions, conversation
    history --- before it reaches the model. This enables analysis of
    input quality, which is the dominant root cause of CLEAR dimension
    failures (see Section 1.4) and which no benchmark can capture
    because benchmarks bypass the customer\'s actual data pipeline.

-   **Latency decomposition.** CLEAR\'s published Latency dimension is
    end-to-end. The gateway position lets us decompose latency into its
    causal components --- network, vendor think-time, generation rate,
    gateway overhead --- each of which is independently actionable. See
    Section 2.2.

## 1.4 Cost Destruction as One Problem, Not Two

A central insight that shapes this methodology: most enterprise AI cost
destruction comes from **noise**, and noise destroys cost at two layers
of the same pipeline.

Noise *before* the model --- bloated context, redundant chunks,
conversation history beyond useful horizon, system prompt sections that
don\'t apply, tool definitions for tools that won\'t be called ---
drives **direct payload waste**. The customer pays input tokens for
content the model didn\'t need to produce its answer.

Noise *after* the model manifests as retries, abandonment, and rejected
outputs. Most of this is actually **induced output waste** --- a
downstream consequence of pre-model noise. The retry happened because
the bloated context produced a confidently wrong answer; the abandonment
happened because the user\'s query got lost in the noise of the context
window; the malformed output happened because verbose tool definitions
confused the model into the wrong call.

A smaller residual is **genuine post-model waste** --- failures where
the input was clean and the model still failed. Real hallucination from
training-data limits; fundamental task difficulty; model regression. The
gateway can detect this but cannot fix it through payload reduction; the
customer addresses it with prompt engineering, model swap, or task
redesign.

This decomposition matters because **most cost destruction is
addressable by the gateway transparently**, without developer
involvement. Direct payload waste is removed in-flight. Configurable
post-model waste (retries, fallbacks, model swaps on degradation) is
removed by route-attached policy. Only genuine post-model waste requires
customer-side remediation. Section 2.1 makes this concrete; Section 3
explains the architecture; Section 5 defines what ships in which phase.

## 1.5 Scope and Boundaries

Explicit out-of-scope items for this product:

-   **Subscription-priced AI usage.** The Gateway measures API-priced
    traffic. Subscription products (Claude Pro/Max, ChatGPT Plus/Pro)
    authenticate via OAuth tokens that vendors restrict to official
    clients; routing them through a third-party proxy violates vendor
    terms of service. The Gateway\'s buyer is the enterprise platform
    team running API-priced workloads in their applications, not the
    individual developer on a personal subscription.

-   **Pre-deployment evaluation.** The Gateway measures production
    traffic. Golden-set evaluation, red-teaming, and pre-deployment
    validation remain necessary and complementary; the Gateway does not
    replace them.

-   **Model selection or marketplace.** The Gateway is model-agnostic
    and provider-agnostic. It measures and governs traffic to whatever
    models customers choose; it does not recommend models.

-   **Compliance certification.** The Gateway produces audit-quality
    data that supports compliance programs. It does not itself certify
    compliance with regulatory frameworks.

# 2. The Five CLEAR Dimensions

Each dimension is defined first by CLEAR\'s published specification,
then by how the gateway measures it from production traffic, and finally
by the diagnostic story it enables. Sub-metrics are computed from wire
traffic alone in the default measurement mode; some sub-metrics are
sharpened by optional enrichment.

Where CLEAR defines a formula, the formula is cited as published. Where
the gateway position enables extension of CLEAR, the extension is called
out explicitly.

## 2.1 Cost (C) --- Cost as the Lead Metric

**CLEAR\'s published definition:** Cost measures economic efficiency,
including API token consumption, inference costs, and infrastructure
overhead. CLEAR introduces two primary metrics:

**Cost-Normalized Accuracy (CNA) = (Accuracy / Cost) × 100**

CNA enables fair comparison between expensive high-accuracy systems and
cost-effective alternatives. A system with 70% efficacy at \$0.30/task
(CNA = 233) is more enterprise-deployable than one with 75% efficacy at
\$5.00/task (CNA = 15) despite the higher headline accuracy.

**Cost Per Success (CPS) = Total Cost / Number of Successful Tasks**

CPS accounts for the fact that failed attempts still incur cost. A
system with 50% success rate has CPS twice its per-call cost. This is
the metric that makes reliability economically meaningful.

**Gateway extension --- three-category cost decomposition:** Beyond CNA
and CPS, the inline gateway position lets us decompose where cost
destruction occurs:

  -----------------------------------------------------------------------
  **Category**       **What It Captures**         **How It\'s Addressed**
  ------------------ ---------------------------- -----------------------
  Direct payload     Input tokens spent on        Gateway payload
  waste              context the model didn\'t    reduction (active
                     need to produce its output.  feature, Phase 2)
                     Measured by sampling         
                     output-to-context            
                     attribution.                 

  Induced output     Retries, abandonment, and    Reduced indirectly by
  waste              rejected outputs caused by   addressing direct
                     pre-model noise. Measured by payload waste;
                     retry-similarity detection   configurable retries /
                     and session abandonment      fallback handled by
                     signals.                     route policy (Phase 2)

  Genuine post-model Failures where input was     Customer-side
  waste              clean and model still        remediation (prompt
                     failed. True hallucination,  engineering, model
                     task difficulty, model       swap, task redesign).
                     regression.                  Gateway measures; does
                                                  not fix.
  -----------------------------------------------------------------------

**The headline report number:** Total cost destruction this period,
decomposed by category, with the gateway-addressable portion called out
explicitly. Customers see what the gateway can fix (typically 70-90% of
the destruction) separated from what they have to fix themselves.

**Sub-metrics:** Cost per request, cost per inferred-successful-request,
waste percentage by category, cost concentration (top 5% of
users/endpoints/workflows), cost trend slope over rolling 7/30 day
windows, context efficiency ratio (tokens-used vs. tokens-provided).

**Scoring:** Healthy ≥ 75, Marginal 50--74, Failing \< 50, weighted
heaviest on the gateway-addressable categories. The composite reflects
how much of the customer\'s AI spend produces value-bearing outcomes
versus noise that the gateway and customer can remove.

## 2.2 Latency (L) --- End-to-End and Decomposed

**CLEAR\'s published definition:** Latency evaluates response time
throughout the planning, execution, and reflection phases. The primary
metric is SLA Compliance Rate (SCR) = (Tasks Completed Within SLA /
Total Tasks) × 100%, with domain-specific thresholds (e.g., 3 seconds
for customer support, 30 seconds for code generation).

**Gateway extension --- latency decomposition.** CLEAR\'s published SCR
is end-to-end. The gateway position lets us decompose latency into
independent components, each separately diagnosable and actionable:

  --------------------------------------------------------------------------
  **Component**         **What It Measures**         **What It Tells You**
  --------------------- ---------------------------- -----------------------
  Network round-trip    DNS, TLS handshake, TCP      Network or regional
                        connection, and round-trip   issue; vendor endpoint
                        to vendor endpoint           outage; CDN provider
                                                     change

  Vendor                Time from request received   Prompt complexity,
  time-to-first-token   at vendor to first generated model reasoning depth,
                        token returned               vendor queue depth

  Vendor inter-token    Tokens-per-second once the   Model generation speed;
  latency               stream starts                vendor capacity issues

  Vendor total          Total generation time on     Output length;
  think-time            vendor side, from first      prompt-induced
                        token to last                verbosity; reasoning
                                                     depth

  Gateway overhead      Time added by the TAS        Gateway performance
                        gateway on ingress + egress  (target: \<50ms total)
  --------------------------------------------------------------------------

**The diagnostic value:** A customer seeing \"P95 latency is 12
seconds\" cannot act. A customer seeing \"P95 latency is 12 seconds ---
9.4s of which is vendor time-to-first-token, 2.1s is inter-token
generation, 0.4s is network, 0.1s is gateway overhead\" knows
immediately: prompts are causing the model to think long, switch to a
smaller model or simplify the prompt. Each component is independently
actionable.

**Sub-metrics:** SLA Compliance Rate per workflow type, P50/P95/P99 for
each latency component, latency trend over rolling windows,
latency-by-vendor comparison (high-value side-effect data: cross-vendor
benchmarking from real customer traffic).

**Scoring:** Healthy ≥ 75, Marginal 50--74, Failing \< 50. Composite
weights SCR heaviest because it most directly reflects user experience;
component diagnostics surface in the drill-down rather than the
headline.

## 2.3 Efficacy (E) --- Task Completion Quality

**CLEAR\'s published definition:** Efficacy captures task completion
quality through traditional accuracy metrics augmented with
domain-specific measurements. CLEAR recognizes that efficacy
requirements vary by domain: functional correctness for software tasks;
result accuracy for data analysis; intent classification accuracy for
customer support.

**Gateway measurement:** Without customer-supplied ground truth, the
gateway measures efficacy through observable proxies that correlate with
task success:

-   **Structural validity rate** --- for structured outputs (JSON, code,
    tool calls, function arguments), the percentage that parse
    correctly. Direct, deterministic, computed on every request.

-   **Implicit acceptance rate** --- percentage of outputs not followed
    by a retry, regeneration, or abandonment within session. Inferred
    from conversation threading and request-similarity detection in
    60-second windows (deferred to Phase 2 for full implementation).

-   **Schema conformance** for classification and extraction workflows
    --- does the output match the requested schema, both structurally
    and semantically.

-   **Groundedness rate** --- for workflows with provided context (RAG,
    tool use), the percentage of factual claims in the output that trace
    to claims in the context. Computed via claim-level Natural Language
    Inference checks on a stratified sample (Phase 2).

**With optional enrichment:** Customers who add an outcome webhook
upgrade implicit acceptance to true task-success measurement. Customers
who pass user-feedback headers (thumbs-up/down) sharpen calibration.

**Sub-metrics:** Per-workflow efficacy scores, structural validity rate,
implicit acceptance rate, groundedness rate (RAG workflows), tool-call
accuracy (agentic workflows). Hedge-phrase trend as a leading indicator
of efficacy degradation.

**Scoring:** Healthy ≥ 75, Marginal 50--74, Failing \< 50. The composite
weights structural validity and implicit acceptance heaviest as the most
reliable production proxies for task success.

## 2.4 Assurance (A) --- Safety, Security, Policy Compliance

**CLEAR\'s published definition:** Assurance evaluates safety, security,
and policy compliance. The primary metric is Policy Adherence Score
(PAS) = 1 − (Policy Violations / Total Policy-Critical Actions). Policy
violations represent hard failures in enterprise contexts: a single
unauthorized data disclosure can invalidate otherwise perfect
performance.

**Mapping to NIST AI RMF:** CLEAR\'s Assurance dimension maps directly
to the trustworthiness characteristics in NIST AI RMF. The gateway
produces per-request tags that align with NIST\'s framework:

  -----------------------------------------------------------------------
  **NIST Trustworthiness **Gateway Measurement**
  Characteristic**       
  ---------------------- ------------------------------------------------
  Safe                   Output content scanning for prohibited
                         categories; harm-reduction tag set

  Secure and resilient   Prompt injection detection; adversarial input
                         flagging; jailbreak attempt classification

  Privacy-enhanced       PII detection in inputs and outputs;
                         tokenization compliance; jurisdictional
                         residency

  Accountable and        Per-request audit trail; tag attribution; policy
  transparent            bundle versioning

  Valid and reliable     See Efficacy (2.3) and Reliability (2.5)
                         dimensions
  -----------------------------------------------------------------------

**This is the dimension where Gatekeeper\'s existing infrastructure
transfers most cleanly.** Gatekeeper\'s \"match once, tag many\"
pattern-matching framework already produces compliance tags against
HIPAA, GDPR, SOX, PCI-DSS. The same infrastructure runs new rule packs
for AI-specific assurance checks (prompt injection, output filtering,
agentic tool-use governance).

**Sub-metrics:** Policy Adherence Score (PAS) as published in CLEAR,
per-NIST-characteristic violation rates, prompt-injection resistance
rate (when active sampling enabled), structured-output policy
compliance, jurisdictional / residency compliance.

**Scoring:** Healthy ≥ 90, Marginal 75--89, Failing \< 75. Thresholds
are stricter than other dimensions because assurance failures have
asymmetric consequences --- one PII leak can invalidate the value of an
entire deployment.

## 2.5 Reliability (R) --- Consistency Across Runs

**CLEAR\'s published definition:** Reliability assesses consistency
through the pass@k metric: the probability of achieving k consecutive
successes. Production deployment for mission-critical applications
requires pass@8 ≥ 80%. Single-run evaluation masks brittleness: an agent
with 70% pass@1 might achieve only 30% pass@8.

**Gateway measurement:** Pass@k cannot be measured by passive
observation of one-off requests. The gateway approximates it through two
mechanisms:

-   **Organic retry observation.** When users retry the same or similar
    query within a session, the gateway observes the consistency of
    model behavior across those retries. This is a real-traffic
    approximation of pass@k that doesn\'t require active sampling.
    Available in Phase 2 once conversation threading is shipped.

-   **Active sampling on enrichment.** For customers who upload an eval
    set (Phase 3 enrichment), the gateway periodically runs the eval set
    against production traffic and computes true pass@k. This is the
    rigorous measurement; organic observation is the always-on
    approximation.

**Sub-metrics:** Observed pass@k from organic retries (per workflow
type), structural consistency rate (does the same prompt produce
consistently parseable output), behavioral drift detection (statistical
change in retry rate, conversation length, output length distributions
over rolling windows).

**Scoring:** Healthy ≥ 75, Marginal 50--74, Failing \< 50. In MVP,
scoring is partially heuristic (based on structural consistency
proxies); full pass@k scoring requires Phase 2.

## 2.6 Input Quality as Leading Indicator

CLEAR\'s five dimensions are output-side measurements: they describe the
result of an AI invocation. Most enterprise AI failures originate
*before* the model runs --- in the data, retrieval, or context layer.
The gateway\'s unique position lets us measure the upstream causes of
CLEAR dimension failures.

Rather than introducing a sixth CLEAR dimension (which would extend the
framework non-trivially and weaken our claim to anchor on published
research), we treat Input Quality as a **leading indicator** --- a set
of sub-metrics that predict which CLEAR dimensions will fail next. The
Input Quality measurements feed into Cost, Efficacy, and Assurance
scoring; they are not a separate composite. The doc returns to this
insight repeatedly: most cost destruction is caused by input noise, and
most output-side failures are downstream of input quality.

Input Quality sub-metrics computed by the gateway:

-   **Context utilization ratio** --- for RAG workflows, the fraction of
    provided context tokens attributed to the output. Low ratios
    indicate over-fetching.

-   **Context groundedness fit** --- does the provided context contain
    the information the query is asking about? Measured by sampling
    retrieval-query semantic alignment.

-   **Chunk integrity score** --- are retrieved chunks well-formed
    (proper sentence boundaries, complete sections, no orphan
    fragments)?

-   **Context staleness** --- date markers in retrieved content vs.
    temporal markers in the query.

-   **Context contradiction rate** --- fraction of contexts containing
    conflicting claims.

-   **Prompt structure anti-patterns** --- detected via rule packs
    targeting known-bad system prompt patterns.

-   **Tool definition quality** --- for agentic workflows, schema
    completeness and disambiguation quality of tool definitions.

These signals are what differentiate a TAS Gateway diagnostic from a
competitor\'s observability report. Observability tools see what reached
the model; the gateway sees how it reached the model and can diagnose
the failure mode upstream of the symptom.

**Future research direction:** We believe Input Quality merits inclusion
as a sixth dimension in a future version of CLEAR. The methodology
adopted in this document treats it as a leading indicator to remain
faithful to the published framework, while collecting the production
data that would support a formal extension proposal. This positions us
as contributors to CLEAR rather than competitors with it.

# 3. How We Capture the Metrics

All metrics derive from one of three data sources: wire traffic flowing
through the proxy, inferred signals computed by the gateway, and
optional enrichment from customer-side signals. Section 3 walks through
the architecture from onboarding through capture mechanics.

## 3.1 Onboarding: Environment Variable Redirection

All major LLM vendor SDKs read base URLs from environment variables.
OpenAI\'s SDKs read OPENAI_BASE_URL; Anthropic\'s SDKs read
ANTHROPIC_BASE_URL. This means customers redirect traffic to the gateway
without modifying application code:

  -----------------------------------------------------------------------
  \# Two environment variables in deployment config:\
  OPENAI_BASE_URL=https://gateway.tas.io/openai/v1\
  ANTHROPIC_BASE_URL=https://gateway.tas.io/anthropic/v1\
  \
  \# Plus one config line per SDK init to inject TAS-Auth header:\
  \# (Python OpenAI example)\
  client = OpenAI(default_headers={\"TAS-Auth\": tas_token})\
  \
  \# Customer\'s vendor API key stays in the standard env var,\
  \# unchanged, and flows through to the vendor in the standard\
  \# Authorization header. TAS never sees or stores it.

  -----------------------------------------------------------------------

**This is configuration, not code.** The customer\'s application code
does not change. The two environment variables are set in deployment
configuration (Kubernetes ConfigMap, ECS task definition, Helm values,
Terraform). The one SDK config line is a small wrapper or init
parameter, set in one place per service.

**Scope: server-side and container-based deployments.** Desktop AI
applications (Claude Desktop, GUI tools launched from a dock or finder)
do not inherit shell environment variables and require in-app
configuration. This is outside the Gateway\'s target audience;
enterprise platform teams running API-priced workloads in their
applications are the buyer.

## 3.2 Authentication: Path A as Default

Three credentials flow through the system: the customer\'s gateway
authentication token (TAS-Auth header), their vendor API key
(Authorization header, unchanged), and an optional per-request override
(TAS-Upstream-Authorization header, for sophisticated multi-tenant
cases).

**The Gateway never holds vendor API keys in the default path.** This is
a deliberate security position. The vendor key stays in the customer\'s
existing environment variable, goes out in the standard Authorization
header, transits through one request at a time, and is never persisted
by the Gateway. This is a sharper security position than any
broad-coverage gateway competitor offers, and it pre-empts the
supply-chain compromise category that affected LiteLLM in early 2026.

Resolution order for upstream authentication:

6.  Per-request TAS-Upstream-Authorization header (if present) → use
    this raw key for this request only

7.  Standard Authorization header from customer\'s SDK → pass through to
    vendor as-is

**Stored vendor keys (Path B) as future enterprise option.** For
customers who explicitly prefer dashboard credential management ---
encrypted at rest, accessed only by the forwarding hot path,
customer-rotatable --- this is a Phase 2+ capability. Path A remains the
default and recommended approach.

## 3.3 Streaming-Native Architecture

Production AI traffic is predominantly streamed. Non-streaming requests
time out or cause retries on any prompt complex enough to require
significant model reasoning. The Gateway is designed streaming-native:
it accepts and proxies streamed responses chunk-by-chunk with sub-50ms
overhead, observing the stream as it unfolds and computing metrics from
chunk timing.

Implications for metric computation:

-   **Latency measurement is decomposed naturally.** Time-to-first-byte,
    time-to-first-token, inter-token latency, and total stream duration
    are each captured as the stream progresses. The decomposition in
    Section 2.2 is a property of streaming, not an add-on.

-   **Output-side quality checks are post-hoc.** Groundedness,
    structural validity, and policy compliance on output are computed on
    stream completion, not in flight. This means output-side policy
    enforcement is detect-and-tag, not block-and-modify. The gateway can
    identify problematic outputs but cannot transparently rewrite them
    mid-stream.

-   **Input-side intervention works normally.** Payload reduction,
    policy enforcement on inputs, and routing all happen before the
    request is forwarded. Streaming does not affect these capabilities.

-   **Request-response is a degenerate case.** Non-streaming responses
    are handled as streams with a single chunk plus end-of-stream
    marker. No separate code path.

## 3.4 Multi-Endpoint and Traffic Pattern Coverage

The Gateway is not limited to chat completions. Different endpoint types
have different cost profiles, different quality measurement
implications, and different relevance to each CLEAR dimension:

  -----------------------------------------------------------------------
  **Endpoint Type**  **Traffic Characteristics**  **CLEAR Coverage**
  ------------------ ---------------------------- -----------------------
  Chat / messages /  Streaming default; variable  Full CLEAR coverage in
  completions        cost; conversation context;  Phase 2
                     tool-use loops               

  Embeddings         High-volume, small-payload;  Cost, Latency,
                     cost driven by request       Assurance (input PII
                     count; no streaming          scanning)

  Image generation   Single-request expensive;    Cost, Latency,
                     per-call pricing; long       Assurance (prompt
                     latency                      content scanning)

  Audio (TTS / STT)  Binary payloads; different   Cost, Latency in MVP;
                     cost models; streaming for   quality measurement
                     TTS                          deferred

  Files /            Long-lived async; different  Cost tracking;
  fine-tuning        lifecycle from real-time     Assurance for uploaded
                     inference                    content

  Tool-use loops     Multi-request task;          Per-request CLEAR in
                     conversation threading       MVP; task-level
                     required for task-level      attribution in Phase 2
                     attribution                  

  Batch APIs         Async submit-retrieve        Deferred --- see
                     lifecycle; results hours     Section 5
                     later                        
  -----------------------------------------------------------------------

**Honest disclosure in customer reports:** When a workflow\'s CLEAR
coverage is incomplete in the current product phase, the Day-1 report
says so explicitly rather than reporting partial metrics as if they were
complete. A streaming-heavy customer in the MVP gets a report that says
\"we measured cost and latency for your streaming traffic in full;
output-quality measurement on streamed responses is post-hoc and partial
in this release.\"

## 3.5 Policy as Headers, Policy as Routes

By default, the Gateway is a transparent measurement proxy. Active
features --- payload reduction, policy enforcement, retry handling,
model fallback --- engage opt-in via two mechanisms: per-request headers
and route-attached policy.

### 3.5.1 Per-Request Policy via Headers

All TAS-controlled headers use the TAS- namespace. The gateway strips
these before forwarding upstream so vendors never see them. Reserved
headers:

  -----------------------------------------------------------------------------
  **Header**                   **Purpose**
  ---------------------------- ------------------------------------------------
  TAS-Auth                     Gateway authentication token. Required.

  TAS-Policy                   Comma-separated policy names to apply for this
                               request. Overrides any route-attached or
                               account-default policy. Example: TAS-Policy:
                               payload_reduce,compliance_strict

  TAS-Policy-Bundle            Named policy bundle. Mutually exclusive with
                               TAS-Policy. Bundles are managed in the
                               dashboard.

  TAS-Workflow                 Customer-supplied workflow name. Overrides
                               auto-detected workflow classification for this
                               request.

  TAS-Upstream-Authorization   Per-request override of the vendor API key. Used
                               for multi-tenant routing.

  TAS-Trace                    When set to 1, returns the captured measurement
                               event as a response header (TAS-Trace-Result,
                               base64-encoded). For developer debugging.

  TAS-Dry-Run                  When set, the gateway computes what active
                               policies would do without actually applying
                               them. Reports actions in TAS-Trace-Result. Used
                               for safe policy evaluation before enforcement.
  -----------------------------------------------------------------------------

### 3.5.2 Route-Attached Policy (Dashboard)

Most production deployments should not require developers to construct
policy headers in application code. Platform teams configure policy in
the dashboard, bound to request route patterns. The application code
stays unchanged; the gateway applies the configured policy automatically
based on the route the request matches.

Routes are matched on combinations of:

-   URL path (vendor and endpoint type)

-   Source identifier (which TAS-Auth token sent the request ---
    typically corresponds to which application or service)

-   Header values from the customer\'s application (e.g., X-Environment
    for prod/staging distinction)

-   Workflow type (gateway-detected, can itself be a matcher)

-   Time of day / day of week (apply stricter compliance during
    off-hours)

**Policy resolution order:** (1) Per-request TAS-Policy header if
present, (2) most-specific matching route policy, (3) account default
policy, (4) pure pass-through. The header override is intentionally
highest priority --- it gives developers a documented escape hatch when
route configuration breaks a specific use case, and that override
appears in the audit log for the platform team to review.

**Dry-run mode for safe rollout.** Policy changes can be staged in
audit-only mode, where the gateway evaluates the new policy against live
traffic and reports what would have happened, without actually applying
the policy. When metrics confirm the change is safe, flip to enforce.
This is the same pattern as a WAF in detection vs. blocking mode and is
the right safety net for inline traffic transformation.

**Policy bundles as named, versioned objects.** Bundles like
production_strict, cost_aggressive, pci_compliance are defined in the
dashboard with versioning and audit trail. Headers reference bundles by
name; route configuration assigns bundles to matchers; bundle
definitions can be updated centrally without changing any references.
The starter bundle taxonomy is one of the open questions in Section 6.

## 3.6 The Thin Client SDK (Optional)

For developers building net-new AI applications, an optional TAS-native
SDK provides a richer programming surface than vendor SDKs allow. The
SDK is opt-in: customers can use the Gateway purely through env-var
redirection of vendor SDKs without ever installing it.

**Design philosophy: thin client, smart gateway.** The SDK translates
TAS-native calls into HTTP requests to the gateway and parses responses.
All actual capabilities --- routing, policy enforcement, payload
reduction, evaluation --- live in the gateway. This is a deliberate
response to the supply-chain compromise category that affected LiteLLM
in March 2026: a thin library has minimal attack surface, no third-party
plugin loading, no local credential handling, and no business logic that
could be compromised by a malicious dependency update.

What the SDK does:

-   OpenAI-shaped surface with TAS extensions (familiar entry point for
    any developer who has used the OpenAI SDK)

-   Multi-vendor abstraction --- write to the SDK once, route to
    OpenAI/Anthropic/Bedrock/Vertex based on policy

-   Optional TAS-native programming surface for advanced configuration
    (eval set integration, A/B testing, conditional routing)

-   Local test mode for development (mock or replay-buffer mode that
    doesn\'t make real API calls)

What the SDK explicitly does not do:

-   Cache user-content responses locally (privacy risk; cache belongs in
    gateway)

-   Hold or process vendor API keys client-side beyond passing through

-   Implement local rate-limiting, retries, or other policy behaviors
    (these belong in the gateway for consistency across all customers)

-   Have a plugin or extension architecture that loads third-party code
    (this is the LiteLLM attack vector)

**Phasing:** SDK is a Phase 3 deliverable, not in MVP. Python first,
TypeScript within 90 days of SDK launch.

## 3.7 What the Gateway Captures Per Request

  ------------------------------------------------------------------------
  **Field Category** **Captured Data**              **CLEAR Dimension
                                                    Served**
  ------------------ ------------------------------ ----------------------
  Request envelope   Account ID, endpoint, vendor,  All
                     model, source app identifier,  
                     region, request ID             

  Timestamps         Request received, DNS          Latency (decomposed)
                     resolved, TLS handshake,       
                     request forwarded, first byte  
                     received, first chunk to       
                     client, last chunk, request    
                     complete (microsecond          
                     precision)                     

  Token accounting   Input tokens, output tokens,   Cost
                     tool-call tokens, cached       
                     tokens, vendor pricing applied 

  Request structure  System prompt, user message,   Input Quality,
                     conversation history, tool     Assurance
                     definitions, retrieved context 
                     blocks                         

  Response structure Full response, tool calls,     Efficacy, Assurance
                     finish reason, logprobs (when  
                     available), structural         
                     validity check                 

  Inferred labels    Workflow type,                 Efficacy, Reliability,
                     retry-of-previous flag,        Input Quality
                     session abandonment flag,      
                     validity flags, hedge-phrase   
                     signals                        

  Tag set            Output of Gatekeeper-style     All (especially
                     rule packs: quality tags,      Assurance)
                     policy tags, NIST              
                     trustworthiness tags,          
                     anti-pattern tags              

  Active mode        When active mode engaged:      Cost (intervention
  actions            payload reduction bytes/tokens attribution)
                     removed, policy decisions      
                     made, retries triggered,       
                     fallbacks engaged              
  ------------------------------------------------------------------------

## 3.8 Sampling Strategy

Computation cost varies by metric type:

  ------------------------------------------------------------------------
  **Computation      **Sample Rate**  **Rationale**
  Type**                              
  ------------------ ---------------- ------------------------------------
  Deterministic      100%             Token counting, schema validation,
  checks                              regex/Hyperscan tagging ---
                                      negligible cost

  Embedding-based    100% (cached)    Similarity calculations for retry
  checks                              detection, context-output alignment

  LLM-as-judge       5--10%           Groundedness, tool accuracy, context
  (small)            stratified       attribution. Stratified by workflow,
                                      customer, anomaly history.

  LLM-as-judge       1% + triggered   Deeper analysis when small-judge is
  (large)                             ambiguous or when an anomaly is
                                      detected
  ------------------------------------------------------------------------

**Stratified sampling.** Sampling is stratified by workflow type,
customer, and recent anomaly history. Rare workflows are still measured
at meaningful confidence; customers with degrading quality automatically
get more thorough sampling.

## 3.9 Workflow Classification

Auto-classification runs per request and determines which sub-metrics
apply. Six workflow types in v1, identified by request shape:

  ------------------------------------------------------------------------
  **Workflow Type**  **Detection Signals**                **Key
                                                          Sub-Metrics**
  ------------------ ------------------------------------ ----------------
  Single-turn Q&A    Short input, no conversation         Cost, validity,
                     history, no tool definitions, short  hedge tracking
                     output                               

  RAG                Input contains structured context    Full Input
                     blocks (delimiters, chunk markers)   Quality,
                                                          groundedness

  Agentic / tool-use Tool definitions present; responses  Tool accuracy,
                     include tool_calls; multi-turn with  trajectory
                     tool_results                         validity

  Long-context       Very large input (\>10K tokens),     Context
  summarization      short output, single-turn            utilization,
                                                          coverage

  Code generation    Code patterns in input/output,       Parse validity,
                     code-block delimiters, language      language
                     hints                                detection

  Classification /   Structured-output schemas, short     Schema
  extraction         responses, repetitive request shape  conformance,
                                                          label drift
  ------------------------------------------------------------------------

Classification accuracy is itself surfaced --- when the gateway is
uncertain about a request\'s workflow type, the diagnostic report calls
that out, and customers can manually correct via TAS-Workflow header.

## 3.10 Gatekeeper Reuse: Match Once, Tag Many

The Gateway reuses Gatekeeper\'s existing pattern-matching
infrastructure with new rule packs. Hyperscan-based pattern matching
evaluates each request and response against all enabled rule packs in a
single pass; each rule applies one or more tags to the captured event.
The aggregation produces the dimensional scores.

Rule pack categories new for the AI Quality Gateway product:

-   **Workflow detection** --- identifies workflow type from request
    shape

-   **Context anti-patterns** --- fragmented chunks, stale date markers,
    context bloat, contradictory injection

-   **Prompt anti-patterns** --- known-bad system prompt structures
    (conflicting instructions, missing escape conditions,
    prompt-injection vulnerabilities)

-   **Output anti-patterns** --- hedge-phrase clusters, refusal
    patterns, structural violations, leakage signals

-   **Behavioral signals** --- retry patterns, session continuation,
    abandonment markers

-   **NIST trustworthiness mapping** --- tags align Gatekeeper
    compliance categories to NIST AI RMF characteristics

**Engineering implication:** The Gateway product is, in implementation
terms, primarily a new ingress layer + new rule packs + a new analytics
layer on top of existing TAS infrastructure. This dramatically reduces
time-to-market and de-risks the build. The competitive briefing\'s claim
of 18-24 months of architectural lead is grounded substantially in this
reuse.

## 3.11 Privacy and Data Handling

-   **Default: no payload retention.** Raw request and response payloads
    are processed in-memory, scored, and discarded. Only computed
    metrics, tags, and metadata are persisted.

-   **Optional: sampled payload retention.** Customers can opt in to
    retain payloads for the LLM-judged sample (5-10%) to enable
    explanation-rich diagnostics. Retention period and access controls
    are customer-configurable.

-   **PII tokenization.** Where payloads are retained, Gatekeeper\'s
    existing Databunker integration tokenizes PII before storage.

-   **Regional residency.** Customers select processing region at
    signup. Cross-region data movement is blocked by default.

-   **Customer-owned exports.** All collected metrics and retained
    payloads are exportable on demand. No lock-in.

-   **Vendor credentials: never stored.** Path A default keeps customer
    vendor API keys out of TAS persistence entirely. The Gateway never
    has standing access to customer model credentials.

# 4. User Experience and UI Flow

The UI serves three audiences: the developer doing initial integration,
the platform team configuring policy and managing the deployment, and
the AI program owner / CFO consuming reports. The flow has four primary
states: sign-up, first 24 hours, ongoing dashboard use, and policy
configuration.

## 4.1 Sign-Up and Quickstart

**Design principle:** Time from landing on homepage to first request
flowing through the gateway should be under 5 minutes. The customer\'s
application code does not change.

**Screen 1 --- Landing**

  --------------------------------------------------------------------------
  ┌─────────────────────────────────────────────────────────────────────┐\
  │ TAS AI Quality Gateway \[Sign In\] \[Sign Up\]│\
  ├─────────────────────────────────────────────────────────────────────┤\
  │ │\
  │ Find out what\'s wrong with your AI stack. │\
  │ In 24 hours. Without changing a line of code. │\
  │ │\
  │ Point your LLM traffic at our gateway via two environment │\
  │ variables. We\'ll measure cost, latency, quality, and trust │\
  │ --- and tell you what\'s leaking value. │\
  │ │\
  │ \[ Get started --- 5 minutes \] │\
  │ │\
  │ ─── what you\'ll see in your first report ─── │\
  │ │\
  │ ✓ Where your AI spend is being destroyed (and what we\'d │\
  │ remove if you let us) │\
  │ ✓ Whether your data pipeline is feeding the model noise │\
  │ ✓ Whether outputs are trustworthy across NIST categories │\
  │ ✓ Whether your system is silently degrading │\
  │ │\
  └─────────────────────────────────────────────────────────────────────┘

  --------------------------------------------------------------------------

**Screen 2 --- Account Creation**

  --------------------------------------------------------------------------
  ┌─────────────────────────────────────────────────────────────────────┐\
  │ Create your account │\
  ├─────────────────────────────────────────────────────────────────────┤\
  │ │\
  │ Email \[\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\]
  │\
  │ Company (optional)
  \[\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\] │\
  │ Password
  \[\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\_\] │\
  │ Processing region ( ) US-East ( ) US-West ( ) EU │\
  │ │\
  │ \[ \] I agree to the terms │\
  │ │\
  │ \[ Create account & continue \] │\
  │ │\
  │ Or sign up with: \[ Google \] \[ GitHub \] \[ SSO \] │\
  │ │\
  └─────────────────────────────────────────────────────────────────────┘

  --------------------------------------------------------------------------

**Screen 3 --- Quickstart (one step, no code changes)**

  --------------------------------------------------------------------------
  ┌─────────────────────────────────────────────────────────────────────┐\
  │ Quickstart --- Connect your AI traffic Step 1 of 1 │\
  ├─────────────────────────────────────────────────────────────────────┤\
  │ │\
  │ Your TAS-Auth token: │\
  │ ┌─────────────────────────────────────────────────────────────┐ │\
  │ │ tas_qg_live_a8f29e3b4c1d9\... \[Copy\] │ │\
  │ └─────────────────────────────────────────────────────────────┘ │\
  │ │\
  │ Pick your primary provider for tailored instructions: │\
  │ \[● OpenAI\] \[ Anthropic \] \[ Bedrock \] \[ Vertex \] \[ Azure OAI \]
  │\
  │ │\
  │ Step A: Set two environment variables in your deployment config │\
  │ │\
  │ OPENAI_BASE_URL=https://gateway.tas.io/openai/v1 │\
  │ ANTHROPIC_BASE_URL=https://gateway.tas.io/anthropic/v1 │\
  │ │\
  │ Step B: Add the TAS-Auth header via your SDK config (one line): │\
  │ │\
  │ Python: │\
  │ client = OpenAI(default_headers={ │\
  │ \"TAS-Auth\": \"tas_qg_live_a8f29e3b4c1d9\...\" │\
  │ }) │\
  │ │\
  │ Node: │\
  │ new OpenAI({ │\
  │ defaultHeaders: { \'TAS-Auth\': \'tas_qg_live\_\...\' } │\
  │ }); │\
  │ │\
  │ Your vendor API key stays where it is. We never see or store it. │\
  │ │\
  │ \[ Send a test request from this page \] │\
  │ │\
  │ ───────────────────────────────────────────────────────────────── │\
  │ │\
  │ Once traffic flows, your first report will be ready in \~24 hours. │\
  │ We\'ll email you when it is. │\
  │ │\
  │ \[ Done \] │\
  └─────────────────────────────────────────────────────────────────────┘

  --------------------------------------------------------------------------

## 4.2 The First 24 Hours

After quickstart, the dashboard shows progress toward the first report
and surfaces early signals as they arrive.

**Screen 4 --- Dashboard, Pre-Report State**

  --------------------------------------------------------------------------
  ┌─────────────────────────────────────────────────────────────────────┐\
  │ AI Quality Gateway \[Dashboard\] \[Policy\] \[Settings\] ⚙ │\
  ├─────────────────────────────────────────────────────────────────────┤\
  │ │\
  │ Your first report │\
  │ ┌─────────────────────────────────────────────────────────────┐ │\
  │ │ │ │\
  │ │ We\'re watching your traffic. │ │\
  │ │ │ │\
  │ │ Requests received so far: 12,847 │ │\
  │ │ Streaming traffic detected: 94% │ │\
  │ │ Vendors detected: OpenAI, Anthropic │ │\
  │ │ Workflows detected: 3 (RAG, Q&A, Agentic) │ │\
  │ │ Time until first report: \~ 18 hours │ │\
  │ │ │ │\
  │ │ ▓▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░ 26% │ │\
  │ │ │ │\
  │ │ We need \~24 hours of typical traffic to establish a │ │\
  │ │ meaningful baseline. Keep using the gateway normally. │ │\
  │ │ │ │\
  │ └─────────────────────────────────────────────────────────────┘ │\
  │ │\
  │ Early signals │\
  │ ┌──────────────────┐ ┌──────────────────┐ ┌─────────────────┐ │\
  │ │ Spend (live) │ │ Latency (P50) │ │ Streaming TTFT │ │\
  │ │ │ │ │ │ │ │\
  │ │ \$284.20 │ │ 1.8s │ │ 840ms │ │\
  │ │ today │ │ end-to-end │ │ median │ │\
  │ └──────────────────┘ └──────────────────┘ └─────────────────┘ │\
  │ │\
  └─────────────────────────────────────────────────────────────────────┘

  --------------------------------------------------------------------------

## 4.3 The First Report

The first report is the artifact customers screenshot and share with
their CFO. It is designed to be shareable, opinionated, and grounded in
CLEAR-anchored measurement.

**Screen 5 --- Day-1 Report (CLEAR-anchored)**

  --------------------------------------------------------------------------
  ┌─────────────────────────────────────────────────────────────────────┐\
  │ Your AI Health Report (CLEAR-anchored) \[Share\] \[Export\] │\
  │ Acme Corp · 7 days · 47,283 requests · generated Nov 14 2025 │\
  ├─────────────────────────────────────────────────────────────────────┤\
  │ │\
  │ ─── 1. What we observed ──────────────────────────────────────── │\
  │ │\
  │ ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐ │\
  │ │ Cost (C) │ │ Latency(L) │ │ Efficacy(E)│ │ Assur. (A) │ │\
  │ │ │ │ │ │ │ │ │ │\
  │ │ 42 / 100 │ │ 61 / 100 │ │ 53 / 100 │ │ 74 / 100 │ │\
  │ │ ✗ Failing │ │ ⚠ Marginal │ │ ✗ Failing │ │ ⚠ Marginal │ │\
  │ └────────────┘ └────────────┘ └────────────┘ └────────────┘ │\
  │ ┌────────────┐ │\
  │ │ Reliab.(R) │ Composite CLEAR score: 55 / 100 │\
  │ │ partial │ Status: ⚠ Significant issues detected │\
  │ │ │ │\
  │ └────────────┘ │\
  │ │\
  │ ─── 2. Where cost is being destroyed ─────────────────────────── │\
  │ │\
  │ You spent \$8,420 last week on 47,283 requests. │\
  │ │\
  │ Cost destruction breakdown: │\
  │ │\
  │ Direct payload waste: \$3,180 / week ◄ we\'d fix │\
  │ Induced output waste: \$1,710 / week ◄ we\'d fix │\
  │ Genuine post-model waste: \$410 / week │\
  │ ───── │\
  │ Total destruction: \$5,300 / week │\
  │ Annualized: \$275,600 │\
  │ │\
  │ What active mode would address: 92% (\$253,300 annualized) │\
  │ │\
  │ Largest sources: │\
  │ • RAG context bloat: 18K avg tokens injected, \~5K used by model │\
  │ • /api/customer-query retry rate: 61% --- confident wrong answers │\
  │ • Top 4 users drive 31% of spend │\
  │ │\
  │ ─── 3. Latency decomposition ─────────────────────────────────── │\
  │ │\
  │ Where your P95 latency of 8.2s is actually spent: │\
  │ │\
  │ Network round-trip to vendor: 0.4s (5%) │\
  │ Gateway overhead: 0.04s (\<1%) ✓ healthy │\
  │ Vendor time-to-first-token: 5.8s (71%) ◄ dominant │\
  │ Vendor inter-token generation: 2.0s (24%) │\
  │ │\
  │ What this means: your prompts are causing extended model think │\
  │ time. Either prompts are bloated (see Input Quality below) or │\
  │ the model is doing more reasoning than the task requires. │\
  │ │\
  │ ─── 4. Input quality (root cause analysis) ───────────────────── │\
  │ │\
  │ We detected RAG-style traffic on 64% of your requests. │\
  │ │\
  │ Of those: │\
  │ • 71% of provided context goes unused by the model │\
  │ • 43% of responses contain claims unsupported by context │\
  │ (groundedness failure) │\
  │ • 28% of retrieved chunks appear fragmented │\
  │ │\
  │ This is the upstream cause of most other findings on this page. │\
  │ │\
  │ ─── 5. Trustworthiness (NIST AI RMF mapping) ─────────────────── │\
  │ │\
  │ Secure and resilient: 0 prompt-injection signals detected ✓ │\
  │ Privacy-enhanced: PII in outputs: 0.8% (within tolerance) │\
  │ Valid and reliable: Structural validity: 94% ✓ │\
  │ Safe: Policy violations: 0.4% ✓ │\
  │ │\
  │ ─── 6. What\'s drifting ───────────────────────────────────────── │\
  │ │\
  │ ⚠ Your cost-per-request has risen 23% over the last 30 days. │\
  │ Conversation length up from 2.1 turns to 3.4 turns. System is │\
  │ degrading without an obvious incident. │\
  │ │\
  │ ─── What to do next ──────────────────────────────────────────── │\
  │ │\
  │ ┌─────────────────────────────────────────────────────────────┐ │\
  │ │ │ │\
  │ │ Want active mode to capture the \$253K of addressable │ │\
  │ │ destruction we identified? │ │\
  │ │ │ │\
  │ │ Talk to us about your remediation options and what your │ │\
  │ │ team can fix vs. what we handle transparently. │ │\
  │ │ │ │\
  │ │ \[ Talk to us about active mode \] │ │\
  │ │ │ │\
  │ └─────────────────────────────────────────────────────────────┘ │\
  │ │\
  └─────────────────────────────────────────────────────────────────────┘

  --------------------------------------------------------------------------

**Design notes on the Day-1 report:**

-   **CLEAR scores as headline.** Five-dimension grid, with composite.
    This is the score system stakeholders can compare across customers
    and over time.

-   **Dollar-denominated waste with addressable callout.** The customer
    immediately sees what they\'re losing and what fraction the gateway
    can recover for them.

-   **Latency decomposition as visual evidence.** Hard for competitors
    to produce; differentiates the report from observability dashboards.

-   **Input quality as root cause section.** Frames why the other
    findings are what they are; positions the gateway as diagnostic, not
    just observational.

-   **NIST mapping for the security architect audience.** This section
    is what gets the security/compliance buyer to forward the report.

-   **Single CTA.** Not five buttons. One conversation invitation.
    Routes into Phase 2+ tier sales motion.

## 4.4 Ongoing Dashboard

**Screen 6 --- Ongoing Dashboard**

  --------------------------------------------------------------------------
  ┌─────────────────────────────────────────────────────────────────────┐\
  │ Dashboard Acme Corp · last 7 days ▾ \[Generate Report\] │\
  ├─────────────────────────────────────────────────────────────────────┤\
  │ │\
  │ CLEAR composite: 55 ↓ 3 Status: ⚠ Marginal │\
  │ │\
  │ ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐ │\
  │ │ Cost 42│ │ Lat. 61│ │ Effic. 53│ │ Assur. 74│ │\
  │ │ ↓ 3 │ │ → 0 │ │ ↓ 4 │ │ ↑ 2 │ │\
  │ │ ✗ Failing │ │ ⚠ Marginal │ │ ✗ Failing │ │ ⚠ Marginal │ │\
  │ └────────────┘ └────────────┘ └────────────┘ └────────────┘ │\
  │ │\
  │ Spend over time │\
  │ \$1.5K┤ ╭──╮ │\
  │ │ ╭────╯ ╰──╮ │\
  │ \$1.0K┤ ╭───────╯ ╰─ │\
  │ │ ╭───────────╯ │\
  │ \$0.5K┤ ╭──────────╯ │\
  │ └───┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴── │\
  │ Nov 1 Nov 14 │\
  │ │\
  │ By workflow type Requests Cost CLEAR │\
  │ ────────────────────────────────────────────────────────── │\
  │ RAG 30,261 \$5,840 42 ✗ │\
  │ Single-turn Q&A 12,142 \$1,920 72 ✓ │\
  │ Agentic / tool-use 4,880 \$ 660 58 ⚠ │\
  │ │\
  │ Recent activity │\
  │ • Drift alert: cost/req rose 4% in last 24h │\
  │ • New workflow type detected: classification (small sample) │\
  │ • Hedge-phrase rate trending up on /api/customer-query │\
  │ │\
  │ \[ Generate full report \] \[ Configure policy \] │\
  │ │\
  └─────────────────────────────────────────────────────────────────────┘

  --------------------------------------------------------------------------

## 4.5 Route Policy Editor

The route policy editor is the platform team\'s primary interface. It
lets a customer\'s platform engineers bind policy bundles to traffic
patterns without involving developers or changing application code.

**Screen 7 --- Route Policy Editor**

  --------------------------------------------------------------------------
  ┌─────────────────────────────────────────────────────────────────────┐\
  │ Policy Configuration \[+ New Route\] │\
  │ Acme Corp · 4 routes configured · 2 policy bundles active │\
  ├─────────────────────────────────────────────────────────────────────┤\
  │ │\
  │ Route rules (evaluated top-to-bottom; first match applies) │\
  │ │\
  │ ┌─────────────────────────────────────────────────────────────┐ │\
  │ │ 1. Production billing inquiry pipeline │ │\
  │ │ │ │\
  │ │ Matches: │ │\
  │ │ Source app: billing-api-prod │ │\
  │ │ Path: /openai/v1/chat/completions │ │\
  │ │ Header: X-Environment = production │ │\
  │ │ │ │\
  │ │ Policy bundle: production_strict \[ Edit \] │ │\
  │ │ ↳ payload_reduce_v2 │ │\
  │ │ ↳ pii_strip_aggressive │ │\
  │ │ ↳ retry_on_5xx_max_3 │ │\
  │ │ ↳ fallback_on_degraded_to: claude-haiku-4-5 │ │\
  │ │ │ │\
  │ │ Mode: ● Enforce ○ Dry-run ○ Disabled │ │\
  │ │ Last change: 4 days ago · Active \[ Audit log \] │ │\
  │ └─────────────────────────────────────────────────────────────┘ │\
  │ │\
  │ ┌─────────────────────────────────────────────────────────────┐ │\
  │ │ 2. Compliance-sensitive workflows │ │\
  │ │ │ │\
  │ │ Matches: │ │\
  │ │ Workflow type: RAG │ │\
  │ │ Source app: billing-api-prod, claims-api-prod │ │\
  │ │ │ │\
  │ │ Policy bundle: pci_compliance \[ Edit \] │ │\
  │ │ ↳ pci_dss_full_scanning │ │\
  │ │ ↳ payload_reduce_conservative │ │\
  │ │ ↳ audit_full_retention_90d │ │\
  │ │ │ │\
  │ │ Mode: ○ Enforce ● Dry-run ○ Disabled │ │\
  │ │ Last change: 14 hours ago · Evaluating \[ Audit log \] │ │\
  │ │ Dry-run findings: 0 blocked, 14 would-have-tagged │ │\
  │ └─────────────────────────────────────────────────────────────┘ │\
  │ │\
  │ ┌─────────────────────────────────────────────────────────────┐ │\
  │ │ 3. Staging traffic (all) │ │\
  │ │ │ │\
  │ │ Matches: │ │\
  │ │ Header: X-Environment = staging │ │\
  │ │ │ │\
  │ │ Policy bundle: development_lenient \[ Edit \] │ │\
  │ │ │ │\
  │ │ Mode: ● Enforce ○ Dry-run ○ Disabled │ │\
  │ └─────────────────────────────────────────────────────────────┘ │\
  │ │\
  │ ┌─────────────────────────────────────────────────────────────┐ │\
  │ │ Default: pass-through (measurement only) │ │\
  │ │ │ │\
  │ │ Applied when no rule above matches. │ │\
  │ │ Mode: ● Enforce │ │\
  │ └─────────────────────────────────────────────────────────────┘ │\
  │ │\
  │ ──────────────────────────────────────────────────────────── │\
  │ │\
  │ Policy bundles \[+ New bundle\] │\
  │ │\
  │ • production_strict (used by 1 route) \[ Edit \] │\
  │ • pci_compliance (used by 1 route) \[ Edit \] │\
  │ • development_lenient (used by 1 route) \[ Edit \] │\
  │ • cost_aggressive (unused) \[ Edit \] │\
  │ │\
  └─────────────────────────────────────────────────────────────────────┘

  --------------------------------------------------------------------------

**Note on bundle names.** The specific bundle names shown
(production_strict, pci_compliance, development_lenient,
cost_aggressive) are illustrative. The starter bundle taxonomy is one of
the open questions in Section 6.

## 4.6 Settings

Settings are intentionally minimal in v1. No vendor key registration
(Path A default does not require it); no per-account policy editor
(that\'s the route policy editor); just account, team, retention
preferences.

**Screen 8 --- Settings**

  --------------------------------------------------------------------------
  ┌─────────────────────────────────────────────────────────────────────┐\
  │ Settings │\
  ├─────────────────────────────────────────────────────────────────────┤\
  │ │\
  │ ┌─ Sharpen your diagnosis (optional) ──────────────────────────┐ │\
  │ │ Outcome webhook \[Configure\]│ │\
  │ │ User feedback headers \[Configure\]│ │\
  │ │ Workflow naming override \[Configure\]│ │\
  │ └──────────────────────────────────────────────────────────────┘ │\
  │ │\
  │ ┌─ Data handling ──────────────────────────────────────────────┐ │\
  │ │ Payload retention ( ) Off (●) Sampled ( ) Full │ │\
  │ │ Retention period \[ 7 days ▾ \] │ │\
  │ │ PII tokenization (●) On │ │\
  │ │ Processing region US-East │ │\
  │ └──────────────────────────────────────────────────────────────┘ │\
  │ │\
  │ ┌─ Team ───────────────────────────────────────────────────────┐ │\
  │ │ jane@acmecorp.com Admin \[Remove\] │ │\
  │ │ \[ + Invite teammate \] │ │\
  │ └──────────────────────────────────────────────────────────────┘ │\
  │ │\
  └─────────────────────────────────────────────────────────────────────┘

  --------------------------------------------------------------------------

# 5. Phased Implementation Plan

## 5.1 Phasing Principles

The phasing is structured to maximize learning between phases. Each
phase ships a coherent product, generates data that resolves uncertainty
about the next phase, and commits the minimum engineering investment
needed to validate the next decision.

Three criteria filter what qualifies for MVP scope:

8.  **It produces the artifact that hooks customers** --- the Day-1
    diagnostic report.

9.  **It can be built largely on existing TAS Gatekeeper
    infrastructure** --- every new system added delays the ship date and
    adds operational surface.

10. **It generates the data we need to validate the next phase** --- MVP
    is also a market test.

**Phasing uses relative ordering, not calendar dates.** Implementation
timelines depend on staffing, prioritization, and learnings from each
phase. Committing to specific months commits us to a timeline that will
drift; the more honest framing is \"this ships before that ships, in
this order.\"

## 5.2 MVP (Phase 1)

**What ships:**

-   **Streaming-native gateway** accepting native OpenAI and Anthropic
    chat/messages endpoints. Streaming as default; request-response
    handled as degenerate case.

-   **Path A authentication** --- vendor key in standard Authorization
    header, gateway auth via TAS-Auth header. No dashboard credential
    storage.

-   **Three CLEAR dimensions measured fully**: Cost (with three-category
    decomposition and projected payload-reduction opportunity), Latency
    (with full decomposition), Assurance (via existing Gatekeeper rule
    packs).

-   **Two CLEAR dimensions measured via heuristics**: Efficacy
    (structural validity, hedge-phrase trend), Reliability (consistency
    proxies from organic retries when threading is partial).

-   **Coarse workflow classification** --- chat completions vs
    embeddings vs images vs audio. Fine-grained six-type taxonomy
    deferred.

-   **Day-1 report** generated automatically from 24 hours of traffic.
    CLEAR-anchored. Payload reduction shown as projected (not yet
    active).

-   **Basic dashboard** --- account, team, retention. No route policy
    editor in MVP.

-   **No SDK** --- env-var redirection only.

-   **No active features** --- measurement only. Payload reduction,
    route policy, retry handling all in Phase 2.

**Why this scope:**

This is the minimum viable measurement product. It produces the
diagnostic that creates demand for Phase 2. It deliberately defers
transformation (payload reduction, policy enforcement) because the first
version will get edge cases wrong, and we want customers to trust our
measurement before we transform their traffic. It defers the SDK because
the platform-team buyer doesn\'t need it; the developer-power-user use
case is Phase 3.

**What we learn from the MVP that informs Phase 2:**

-   Which workflows are most common in real enterprise traffic (informs
    the Phase 2 fine-grained workflow taxonomy)

-   Where cost waste actually concentrates (informs payload reduction
    implementation priorities)

-   What policy questions customers ask first (informs which policy
    bundles ship in Phase 2\'s route editor)

-   Whether the diagnostic report is genuinely better than competitors\'
    observability output --- the existential question

## 5.3 Phase 2 (Active Platform)

Phase 2 unlocks the gateway\'s transformation capabilities and brings
full CLEAR coverage online. This is what turns measurement into platform
infrastructure.

**What ships:**

-   **Payload reduction in production** --- the single biggest
    cost-recovery feature. Customer enables via dashboard toggle;
    gateway removes direct payload waste in-flight.

-   **Route-attached policy** --- dashboard policy editor with route
    matchers, named bundles, dry-run mode, audit log.

-   **Full CLEAR coverage** --- Efficacy via implicit acceptance signals
    and conversation threading; Reliability via observed pass@k from
    organic retries; groundedness measurement on RAG workflows.

-   **Fine-grained workflow classification** --- the six-type taxonomy
    (Q&A, RAG, agentic, summarization, code generation, classification).

-   **Tool-use loop awareness** --- conversation threading attributes
    cost and quality at the task level, not just the request level.

-   **Multi-endpoint expansion** --- embeddings, images, audio with
    appropriate per-endpoint metrics.

-   **Bedrock and Vertex support** --- proxying SigV4-signed and
    Google-auth requests is non-trivial; this is real engineering work
    but expands the addressable market significantly.

-   **Continuous monitoring and drift alerting** --- moves the customer
    from snapshot diagnostics to live observability with statistical
    drift detection.

-   **Stored vendor keys (Path B) as enterprise convenience option** ---
    customers who want dashboard credential storage can opt in. Path A
    remains default.

## 5.4 Phase 3 (Platform Depth)

Phase 3 capabilities that make TAS a true platform investment for
customers committed to AI as core infrastructure:

-   **Thin-client SDK** --- Python first, TypeScript second. Native TAS
    programming surface for advanced developers.

-   **Outcome webhook integration** --- customer-reported success
    signals upgrade implicit acceptance to true task-success
    measurement. Enables business-outcome correlation.

-   **Custom rule packs** --- customers can write their own quality
    detectors. Turns TAS into an extensible platform.

-   **Eval set management** --- customers upload golden datasets;
    gateway runs them periodically for active pass@k measurement.

-   **Advanced policy primitives** --- conditional payload reduction by
    workflow type, multi-stage refinement, model-swap-on-degradation,
    automated A/B testing of prompt variants.

-   **Multi-tenant policy management** --- RBAC and approval workflows
    for the policy editor; different teams in the same enterprise get
    scoped access.

-   **Compliance-vertical bundles** --- PCI DSS, HIPAA, SOC 2,
    GDPR-tuned starter bundles with dedicated reporting views.

## 5.5 What is Deferred Indefinitely

Items considered and deprioritized. Including these in the doc
demonstrates scope discipline and prevents scope creep in stakeholder
reviews:

-   **Batch APIs** --- async submit/retrieve lifecycle is structurally
    different from streaming inference. Customers who care about batch
    are a specific subset; serving them well requires dedicated effort
    that competes with higher-leverage features. Will be revisited if
    customer demand warrants.

-   **Model marketplace / selection recommendations** --- the Gateway is
    provider-agnostic by design; we measure and govern, we do not
    recommend models.

-   **Outcome-prediction ML** --- building proprietary models to predict
    business outcomes from AI traffic is a research project, not a
    product. We rely on customer-supplied outcome signals via webhook
    integration.

-   **Eval-as-a-service** --- running customer evals on demand is
    adjacent to the gateway\'s role; the eval landscape (Patronus,
    Galileo, etc.) is well-served by specialists. The Gateway integrates
    evals customers have rather than replacing eval vendors.

-   **Subscription-based AI usage measurement** --- as covered in
    Section 1.5; structurally precluded by vendor TOS.

## 5.6 Known Unknowns

Categories of issues we expect to encounter as we ship but have not yet
designed solutions for. Listed here explicitly to set expectations
honestly and to provide context when these issues surface in customer
engagements:

-   **Multi-modal inputs at scale** --- vision models with image inputs,
    audio transcription with binary payloads. Token-counting models
    don\'t apply uniformly; cost models differ by vendor; policy
    applicability changes.

-   **Vendor-specific feature support** --- Anthropic\'s prompt caching,
    OpenAI\'s structured outputs with JSON schemas, vendor-specific
    tool-use formats. Some features pass through transparently; others
    require explicit modeling.

-   **Long-running connections** --- WebSocket connections for realtime
    APIs, persistent SSE streams. The connection-pooling and resource
    model differs from short-lived HTTPS.

-   **Vendor SDK quirks** --- not all vendor SDKs behave identically
    when redirected. Some send extra telemetry headers; some validate
    response shapes strictly; some have hardcoded fallback logic.

-   **High-cardinality customers** --- a customer with thousands of
    distinct workflows produces dashboards that don\'t fit. Metric
    aggregation has to handle this without becoming unreadable.

-   **Adversarial customers** --- someone routing prompt-injection test
    traffic through the gateway to evaluate detection. We need to handle
    these without polluting the customer\'s production metrics.

-   **Metrics pipeline scale** --- the per-request event payload
    (timestamps, tokens, tags, classifications, decompositions) is
    substantial. At 100K+ requests/minute per customer, this is a
    ClickHouse-scale problem, not a logging problem.

-   **Idempotency and vendor-side caching** --- vendor retries, response
    caching, and idempotency keys can produce duplicate gateway
    observations that double-count cost or quality.

# 6. Open Questions for Stakeholders

Decisions surfaced during methodology development that warrant
stakeholder input. Several questions from v0.1 have been resolved by
decisions in this version; the remaining open questions are listed here.

## 6.1 Product and Positioning

11. **Pricing model for the self-service tier.** Free with request
    volume cap (most viral, has LLM-judge sampling cost implications) or
    30-day free trial then paid (more conservative)?

12. **How aggressive should the upsell into the broader TAS platform
    be?** If a diagnostic reveals data-pipeline issues, the natural next
    step is TAS-MCP-Services and Audimodal. Soft-recommend or stay
    hands-off?

13. **The \'we never hold your keys\' positioning.** Path A makes this
    true. Should this be a marketing headline or a quiet engineering
    detail? Stronger as a headline; ties us to that posture if we lead
    with it.

14. **OSS for the thin client SDK.** Deferred decision per stakeholder
    guidance --- revisit when SDK design is closer to ship and we can
    clearly assess what IP would be exposed. Thin-client design
    minimizes the IP question because most capabilities live in the
    gateway.

## 6.2 Methodology and Metrics

15. **CLEAR composite weighting.** CLEAR\'s default is equal weighting
    (each dimension at 0.2). The paper notes domain-specific weighting
    (e.g., financial services: Reliability 0.4, Assurance 0.3;
    customer-facing: Latency 0.35). Should the Day-1 report use equal
    weighting (CLEAR default) or customer-selectable weighting?

16. **Threshold calibration.** Score thresholds (Healthy / Marginal /
    Failing) in this document are reasonable starting points. They
    should be calibrated against actual customer data once we have it.
    Should v1 thresholds be conservative (overcall failures) or
    permissive (undercall failures)?

17. **Input Quality as future CLEAR extension.** This methodology treats
    Input Quality as a leading indicator rather than a sixth CLEAR
    dimension. Should we explicitly engage with the CLEAR authors about
    formal extension? Position: contributors to CLEAR rather than
    competitors.

## 6.3 Architecture

18. **Policy bundle taxonomy for v1.** What is the starter set of named
    policy bundles? Likely candidates: production_strict,
    development_lenient, cost_aggressive, pii_strip, pci_compliance,
    hipaa_compliance, audit_full. The bundle names become customer
    vocabulary and affect sales conversations.

19. **Route matcher expressiveness for v1.** URL + headers + source
    identifier is enough for most cases. Workflow-type and time-window
    matchers add power but require more UI work. Where do we draw the
    line?

20. **Multi-tenant policy management for v1.** Some enterprises will
    want different teams to have scoped access to different parts of the
    route configuration. Likely needed eventually; not required for MVP.
    When?

21. **Stored-key (Path B) introduction timing.** Phase 2 vs. on-demand
    for specific enterprise prospects. Adds dashboard UX complexity and
    security surface; should not ship until customers explicitly want
    it.

## 6.4 Go-to-Market

22. **Customer acquisition motion.** Self-service products live on
    developer-led discovery (content marketing, comparison tools,
    conference presence). Different motion than enterprise TAS sales.
    Does this product report to a different GTM owner than core TAS?

23. **Conversion mechanism from self-service to higher tiers.** The
    single CTA in the Day-1 report is the obvious one. Conversion likely
    also requires outbound from a CSM watching for high-engagement
    accounts.

24. **Cannibalization risk.** A prospect evaluating TAS for AI
    governance might land on the Gateway free tier instead of buying the
    broader platform. Worth thinking through positioning to avoid the
    cheaper product eating the more strategic one.

# Appendix A --- Supporting Research and Industry Validation

This appendix documents the primary sources behind the methodology. The
intellectual lineage is triangulated across peer-reviewed academic
research, convergent industry frameworks, government standards, and
methodological precedent. The combination is intended to insulate the
methodology from being dismissed as ad-hoc.

## A.1 CLEAR Framework --- Primary Source

**Mehta, S. (November 2025). \"Beyond Accuracy: A Multi-Dimensional
Framework for Evaluating Enterprise Agentic AI Systems.\"
arXiv:2511.14136.**

Key empirical findings:

-   Three documented limitations in existing agent evaluation: (1)
    absence of cost-controlled evaluation, leading to 50x cost
    variations for similar accuracy; (2) inadequate reliability
    assessment, where performance drops from 60% pass@1 to 25%
    pass@8; (3) missing multi-dimensional metrics for security, latency,
    and policy compliance.

-   Expert validation (N=15 enterprise AI deployment leads) showed CLEAR
    correlates with production-deployment-readiness at ρ=0.83 (p\<0.001)
    versus ρ=0.41 for accuracy-only evaluation.

-   Empirical evaluation of six leading agents on 300 enterprise tasks
    across six domains demonstrated that accuracy-optimal configurations
    cost 4.4-10.8x more than Pareto-efficient alternatives.

-   The framework defines specific metrics: Cost-Normalized Accuracy
    (CNA), Cost Per Success (CPS), SLA Compliance Rate (SCR), Policy
    Adherence Score (PAS), pass@k.

## A.2 Convergent Industry Frameworks

**Aisera Research Team (2024). \"CLASSic: A Holistic Framework for
Evaluating Enterprise AI Agents.\" Aisera Technical Report.**

CLASSic proposes five dimensions --- Cost, Latency, Accuracy, Stability,
Security --- that map cleanly onto CLEAR\'s structure with slightly
different naming. Empirical evidence: domain-specific agents achieve
82.7% accuracy versus 59-63% for general LLMs at 4.4-10.8x lower cost.
Independent convergence on a five-dimensional structure validates
CLEAR\'s choices.

**Salesforce AI Research (2024). \"CRMArena: First-Of-Its-Kind LLM
Benchmark Ranks Generative AI Against Real-World Business Tasks.\"
Salesforce Blog.**

Independent enterprise-task benchmark with cost annotations. Reinforces
the case for cost-controlled evaluation in production-relevant contexts.

**Samsung Research (2024). \"TRUEBench: Trustworthy Real-world Usage
Evaluation Benchmark for Enterprise LLMs.\" Samsung AI Research.**

Trust-focused enterprise evaluation benchmark. Validates the Assurance
dimension\'s importance and provides additional grounding for the
trustworthiness sub-metrics.

**Liu et al. (2024). \"Towards Effective GenAI Multi-Agent
Collaboration: Design and Evaluation for Enterprise Applications.\"
arXiv:2412.05449 (AWS Research).**

Documents the 37% performance gap between lab tests and production
deployment. Strengthens the case for production-traffic measurement
(Section 1.3) over benchmark-only evaluation.

## A.3 Government Standards Alignment

**NIST AI 100-1: Artificial Intelligence Risk Management Framework (AI
RMF 1.0). National Institute of Standards and Technology, January
2023.**

Defines seven trustworthy AI characteristics: validity and reliability;
safety; security and resilience; accountability and transparency;
explainability and interpretability; privacy enhancement; fairness with
harmful bias managed. Four core functions: Govern, Map, Measure, Manage.
Voluntary but widely adopted enterprise governance standard. The
Gateway\'s Assurance dimension maps explicitly to these characteristics
(Section 2.4).

**NIST AI 600-1: Artificial Intelligence Risk Management Framework:
Generative Artificial Intelligence Profile. July 2024.**

Maps 12 risk categories specific to generative AI to the broader RMF,
with 200+ specific actions. Provides the generative-AI-specific framing
for enterprise governance overlays.

## A.4 Methodological Precedent (DORA)

**Forsgren, N., Humble, J., and Kim, G. (2018). \"Accelerate: The
Science of Lean Software and DevOps.\" IT Revolution Press.**

Original publication of the DORA Four Keys methodology. Documents the
research demonstrating that a small number of carefully chosen, balanced
metrics predict organizational performance better than larger
dashboards. The structural pattern (small number of balanced metrics,
organized around productive tension) is what this document adopts;
DORA\'s specific metrics are not.

**DORA (2024-2026). Annual State of DevOps reports. dora.dev.**

Recent evolution of the framework from four keys to five-metric model,
including renaming MTTR to Failed Deployment Recovery Time. Worth
noting: even the metric definitions evolve as a framework matures;
methodology should be expected to refine over time with new evidence.

## A.5 Economic and Demand-Side Case

**MIT (2025). State of AI in Business Report.**

Documents 95% of AI pilots delivering zero measurable P&L impact.
Foundational evidence for why enterprises need better measurement, not
more models.

**Chiu, O. (2025). \"The Key to Production AI Agents: Evaluations.\"
Databricks Blog.**

Industry-side evidence that inadequate evaluation frameworks are cited
as the main failure factor in production AI deployment. Only 10% of
enterprises successfully implement generative AI in production.

**S&P Global (2025). AI project abandonment rate research.**

Share of companies abandoning AI projects jumped from 17% to 42%
year-over-year. Strengthens the case that measurement is the missing
layer in enterprise AI deployment.

## A.6 Thin Client Design and the LiteLLM Lesson

**Supply-chain compromise of LiteLLM PyPI packages, March 2026.**

TeamPCP compromised two LiteLLM PyPI packages in late March 2026,
deploying credential harvesters, Kubernetes lateral-movement payloads,
and persistent backdoors. The compromise exploited the substantial
business logic in LiteLLM\'s client library, which includes plugin
loading, local credential handling, retry logic, and provider
integration code. The attack surface was large because the library does
substantial work locally.

This event directly informs the Gateway\'s thin-client SDK design
philosophy (Section 3.6). Our SDK is deliberately minimal: HTTP request
construction, response parsing, no local credential handling, no plugin
architecture. All capabilities live in the gateway, where supply-chain
risks are controlled by TAS\'s existing security practices rather than
distributed across customer machines.

*Document version: v0.2. Author: TAS Platform. Status: Stakeholder
review. Comments welcome on any section; numbered questions in Section 6
are highest priority for feedback.*
