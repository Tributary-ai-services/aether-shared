# AIQG Policy Pattern

---

**Metadata**

```yaml
service: aiqg-dashboard-be (catalog + read-only endpoint)
model: PolicyPattern
database: none (static catalog, compiled into the service)
version: 1.0.0
last_updated: 2026-06-15
status: implemented (read-only reference catalog; non-breaking addition)
spec_refs: source-spec-v0.2.md §3.5.1, §3.10
related: policy-rule.md, policy-bundle.md, tag-set.md
```

---

## 1. Overview

### Purpose
A `Policy Pattern` is one **Gatekeeper matcher** that a [[policy-rule]] can react to via its `pattern_id`. The catalog is the canonical, human-facing description of the matcher set: the closed list of `pattern_id` values the production pipeline (`tas-llm-router` + Gatekeeper) can emit, each enriched with a label, description, category, a suggested default severity, and the NIST AI RMF characteristic the finding maps to.

It exists so the dashboard's policy-bundle wizard can present `pattern_id` as a curated, explained picker instead of a free-text box. `pattern_id` remains a free-form string on [[policy-rule]] for forward-compatibility — the catalog describes the *known* set, it does not constrain the column.

### Ownership
- **Owning service**: `aiqg-dashboard-be` — owns the catalog (`internal/handlers/policy_patterns.go`) and serves it read-only. Tenant-agnostic; the catalog is global.
- **Source of truth for ids**: this catalog. `metrics.go`'s `knownTagPatterns` (the `/metrics/tags` Loki query) derives its id list from `PolicyPatternIDs()` so the tag query and the wizard can't drift.
- **Severity/NIST alignment**: mirrors `tas-llm-router/cmd/demo-traffic/catalog.go` where an entry exists there.
- **Read-only consumers**: `aiqg-ui` (policy-bundle wizard pattern picker).

### Storage
Static Go data compiled into `aiqg-dashboard-be`. No database row, no migration. When Gatekeeper gains a matcher, add it to `policyPatternCatalog`.

---

## 2. Schema

```go
type PolicyPattern struct {
    PatternID       string `json:"pattern_id"`
    Label           string `json:"label"`
    Description     string `json:"description"`
    Category        string `json:"category"`             // pii | credential | injection | quality | safety
    DefaultSeverity string `json:"default_severity"`     // low | medium | high | critical
    NIST            string `json:"nist_characteristic"`  // valid_reliable | privacy_enhanced | secure_resilient | safe
}
```

### Field reference
| Field | Meaning |
|---|---|
| `pattern_id` | Gatekeeper matcher id (hyphenated, as emitted). The value a rule's `pattern_id` matches against. |
| `label` | Short human name for the picker (e.g. "Social Security number"). |
| `description` | One-line "what it detects", shown in the wizard. |
| `category` | Grouping for the picker. One of `pii`, `credential`, `injection`, `quality`, `safety`. |
| `default_severity` | Suggested severity the wizard pre-fills; the rule may override. Drives CLEAR **Assurance** scoring (`critical=0, high=50, medium=80, low=95`) and audit/alert prominence — it does **not** by itself gate enforcement. |
| `nist_characteristic` | NIST AI RMF characteristic the finding maps to (drives the Trustworthiness breakdown). |

---

## 3. Categories

| Category | NIST characteristic | Risk framing |
|---|---|---|
| `pii` | `privacy_enhanced` | Personal data exposure. |
| `credential` | `secure_resilient` | Secret / credential leakage. |
| `injection` | `secure_resilient` | Adversarial input (prompt / SQL / XSS). |
| `quality` | `valid_reliable` | Cost / utility defects, not safety breaches (low–medium). |
| `safety` | `safe` | Harmful, jailbreak, or solicitation content. |

---

## 4. Catalog (36 patterns)

### PII (`privacy_enhanced`)
| pattern_id | label | default severity |
|---|---|---|
| `pii-ssn` | Social Security number | critical |
| `pii-credit-card` | Credit card number | high |
| `pii-bank-account` | Bank account number | high |
| `pii-passport` | Passport number | high |
| `pii-drivers-license` | Driver's license | high |
| `pii-mrn` | Medical record number | critical |
| `pii-dob` | Date of birth | medium |
| `pii-email` | Email address | medium |
| `pii-phone` | Phone number | medium |
| `pii-address` | Postal address | medium |
| `pii-name` | Personal name | low |
| `pii-ip-address` | IP address | low |

### Credentials (`secure_resilient`)
| pattern_id | label | default severity |
|---|---|---|
| `cred-api-key` | API key | critical |
| `cred-aws-secret-key` | AWS secret key | critical |
| `cred-aws-access-key` | AWS access key ID | high |
| `cred-azure-key` | Azure key | critical |
| `cred-gcp-key` | GCP service account key | critical |
| `cred-private-key` | Private key | critical |
| `cred-connection-string` | Connection string | critical |
| `cred-jwt-token` | JWT token | high |
| `cred-oauth-token` | OAuth token | high |

### Injection (`secure_resilient`)
| pattern_id | label | default severity |
|---|---|---|
| `injection-prompt` | Prompt injection | high |
| `injection-sql` | SQL injection | high |
| `injection-xss` | Cross-site scripting | high |

### Quality (`valid_reliable`)
| pattern_id | label | default severity |
|---|---|---|
| `aiqg-bloated-context` | Bloated context | medium |
| `aiqg-unbounded-loop` | Unbounded loop | medium |
| `aiqg-refusal` | Model refusal | medium |
| `aiqg-hallucination-hedge` | Hallucination hedge | low |
| `aiqg-repetition` | Repetition | low |
| `aiqg-vague-prompt` | Vague prompt | low |
| `aiqg-instruction-stuffing` | Instruction stuffing | low |
| `aiqg-malformed-output` | Malformed output | low |
| `aiqg-role-claim` | Role claim | low |

### Safety (`safe`)
| pattern_id | label | default severity |
|---|---|---|
| `aiqg-harm-request` | Harmful request | critical |
| `aiqg-explicit-jailbreak` | Explicit jailbreak | critical |
| `aiqg-credential-solicitation` | Credential solicitation | high |

---

## 5. API

### `GET /api/v1/policy-patterns`
Read-only, authenticated (any tenant member). Returns the full catalog.

```json
{
  "patterns": [
    {
      "pattern_id": "pii-ssn",
      "label": "Social Security number",
      "description": "Detects U.S. Social Security numbers in prompts or completions.",
      "category": "pii",
      "default_severity": "critical",
      "nist_characteristic": "privacy_enhanced"
    }
  ]
}
```

---

## 6. Relationships

- A [[policy-rule]]'s `pattern_id` references a `PolicyPattern.pattern_id`. The rule's own `severity` defaults from `PolicyPattern.default_severity` (wizard pre-fill) and may be overridden.
- A [[policy-bundle]] is an ordered composition of rules.
- Tags emitted when a pattern fires are documented in [[tag-set]] (`tag_<sanitized>`); the `/metrics/tags` endpoint counts them using the same id list.

---

## 7. Action semantics (context for the wizard)

A rule pairs a `pattern_id` with an `action`:
- **log** — record the finding; no mutation, no block.
- **redact** — mask/tokenize the matched span before the request reaches the vendor.
- **block** — refuse the request (HTTP 403); it never reaches the vendor.

> AIQG is **observe-only (Phase 4.0)** today. `redact`/`block` are stored but not enforced until the `tas-llm-router` evaluator ships (Phase 4.1). The wizard surfaces this so configured actions aren't mistaken for active enforcement.
