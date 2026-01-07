# API Response Types - Aether Frontend

---
service: aether
model: API Response Types
database: HTTP/JSON (Aether Backend API)
version: 1.0
last_updated: 2026-01-05
author: TAS Platform Team
---

## 1. Overview

**Purpose**: Documents all API response type definitions returned by the Aether Backend API (`/api/v1`) and consumed by the Aether Frontend. These types define the contract between frontend and backend services.

**Lifecycle**: API responses are generated on-demand by backend handlers and consumed by Redux thunks via the `aetherApi` service.

**Ownership**: Aether Backend (Go) defines response structures, Aether Frontend (TypeScript/JavaScript) consumes them.

**Key Characteristics**:
- **JSON Serialization**: All responses use JSON format with camelCase field names in responses
- **Envelope Pattern**: Most responses wrap data in `{ data, success }` structure
- **Pagination**: List endpoints return pagination metadata (`total`, `limit`, `offset`, `hasMore`)
- **Error Handling**: Failed responses include error messages and HTTP status codes
- **Space Isolation**: All responses respect space context from request headers

---

## 2. Base Response Structure

### Generic API Response Wrapper

All API requests through `aetherApi.request()` return this structure:

```typescript
interface ApiResponse<T> {
  data: T | null;           // Response payload (null for 204 No Content)
  success: boolean;         // Request success status
}
```

**Example**:
```javascript
// Success response
{
  "data": { "id": "uuid", "name": "My Notebook" },
  "success": true
}

// Error response (handled as thrown exception)
throw new Error("HTTP 400: Invalid request")
```

---

## 3. Authentication & User Responses

### 3.1 Keycloak Token Response

**Endpoint**: `POST /realms/{realm}/protocol/openid-connect/token` (Keycloak)

```typescript
interface KeycloakTokenResponse {
  access_token: string;          // JWT access token
  expires_in: number;            // Expiration in seconds (300 = 5 minutes)
  refresh_expires_in: number;    // Refresh token expiration (1800 = 30 minutes)
  refresh_token: string;         // JWT refresh token
  token_type: "Bearer";
  not_before_policy: number;     // Policy timestamp
  session_state: string;         // Session identifier
  scope: string;                 // Granted scopes ("profile email")
}
```

**Example**:
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_in": 300,
  "refresh_expires_in": 1800,
  "refresh_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "not_before_policy": 0,
  "session_state": "a1b2c3d4-...",
  "scope": "profile email"
}
```

### 3.2 User Response

**Endpoints**:
- `GET /api/v1/users/me`
- `GET /api/v1/users/{id}`

```typescript
interface UserResponse {
  id: string;                    // UUID
  email: string;
  username: string;
  fullName: string;
  avatarUrl?: string;
  status: "active" | "inactive" | "suspended";
  createdAt: string;             // ISO 8601 timestamp
  updatedAt: string;             // ISO 8601 timestamp
}
```

**Example**:
```json
{
  "id": "90d5d23e-ac23-430c-844d-4f7c2a0dcb06",
  "email": "john@scharber.com",
  "username": "john",
  "fullName": "John Scharber",
  "avatarUrl": null,
  "status": "active",
  "createdAt": "2026-01-05T12:00:00Z",
  "updatedAt": "2026-01-05T12:00:00Z"
}
```

### 3.3 Public User Response

**Context**: Used in nested responses (notebook owner, team members)

```typescript
interface PublicUserResponse {
  id: string;
  username: string;
  fullName: string;
  avatarUrl?: string;
}
```

**Example**:
```json
{
  "id": "90d5d23e-ac23-430c-844d-4f7c2a0dcb06",
  "username": "john",
  "fullName": "John Scharber",
  "avatarUrl": null
}
```

---

## 4. Notebook Responses

### 4.1 Notebook Response

**Endpoints**:
- `GET /api/v1/notebooks/{id}`
- `POST /api/v1/notebooks` (create)
- `PUT /api/v1/notebooks/{id}` (update)

```typescript
interface NotebookResponse {
  id: string;                    // UUID
  name: string;
  description?: string;
  visibility: "private" | "shared" | "public";
  status: "active" | "archived" | "deleted";
  ownerId: string;               // UUID
  parentId?: string;             // UUID (hierarchical notebooks)
  complianceSettings?: object;   // JSONB compliance configuration
  documentCount: number;         // Number of documents
  totalSizeBytes: number;        // Total size in bytes
  tags?: string[];
  createdAt: string;             // ISO 8601 timestamp
  updatedAt: string;             // ISO 8601 timestamp

  // Optional nested data (in detail view)
  owner?: PublicUserResponse;
  children?: NotebookResponse[];
  parent?: NotebookResponse;
}
```

**Example (basic)**:
```json
{
  "id": "81600038-1262-407b-a45a-9aeac648ead2",
  "name": "Getting Started",
  "description": "Your first notebook",
  "visibility": "private",
  "status": "active",
  "ownerId": "90d5d23e-ac23-430c-844d-4f7c2a0dcb06",
  "parentId": null,
  "complianceSettings": null,
  "documentCount": 0,
  "totalSizeBytes": 0,
  "tags": [],
  "createdAt": "2026-01-05T12:00:00Z",
  "updatedAt": "2026-01-05T12:00:00Z"
}
```

**Example (with nested owner)**:
```json
{
  "id": "81600038-1262-407b-a45a-9aeac648ead2",
  "name": "Getting Started",
  "visibility": "private",
  "status": "active",
  "ownerId": "90d5d23e-ac23-430c-844d-4f7c2a0dcb06",
  "documentCount": 3,
  "totalSizeBytes": 1048576,
  "tags": ["tutorial", "sample"],
  "createdAt": "2026-01-05T12:00:00Z",
  "updatedAt": "2026-01-05T12:00:00Z",
  "owner": {
    "id": "90d5d23e-ac23-430c-844d-4f7c2a0dcb06",
    "username": "john",
    "fullName": "John Scharber"
  }
}
```

### 4.2 Notebook List Response

**Endpoint**: `GET /api/v1/notebooks`

```typescript
interface NotebookListResponse {
  notebooks: NotebookResponse[];
  total: number;                 // Total count across all pages
  limit: number;                 // Page size
  offset: number;                // Current offset
  hasMore: boolean;              // More notebooks available
}
```

**Example**:
```json
{
  "notebooks": [
    {
      "id": "81600038-1262-407b-a45a-9aeac648ead2",
      "name": "Getting Started",
      "visibility": "private",
      "status": "active",
      "ownerId": "90d5d23e-ac23-430c-844d-4f7c2a0dcb06",
      "documentCount": 0,
      "totalSizeBytes": 0,
      "tags": [],
      "createdAt": "2026-01-05T12:00:00Z",
      "updatedAt": "2026-01-05T12:00:00Z"
    }
  ],
  "total": 1,
  "limit": 20,
  "offset": 0,
  "hasMore": false
}
```

---

## 5. Document Responses

### 5.1 Document Response

**Endpoints**:
- `GET /api/v1/documents/{id}`
- `POST /api/v1/documents` (create)
- `PUT /api/v1/documents/{id}` (update)

```typescript
interface DocumentResponse {
  id: string;                    // UUID
  name: string;
  description?: string;
  type: string;                  // File type category
  status: "uploading" | "processing" | "processed" | "failed" | "archived" | "deleted";
  original_name: string;         // Original filename
  mime_type: string;             // MIME type (e.g., "application/pdf")
  size_bytes: number;            // File size in bytes
  extracted_text?: string;       // Text extracted by AudiModal
  processing_result?: object;    // JSONB processing metadata
  processingTime?: number;       // Duration in milliseconds
  confidenceScore?: number;      // AI confidence (0.0-1.0)
  metadata?: object;             // JSONB custom metadata
  notebook_id: string;           // UUID
  owner_id: string;              // UUID
  tags?: string[];
  processed_at?: string;         // ISO 8601 timestamp
  chunking_strategy?: string;    // Strategy used for chunking
  chunk_count: number;           // Number of chunks created
  average_chunk_size?: number;   // Average chunk size in bytes
  chunk_quality_score?: number;  // Average quality (0.0-1.0)
  createdAt: string;             // ISO 8601 timestamp
  updatedAt: string;             // ISO 8601 timestamp
}
```

**Example**:
```json
{
  "id": "d4e5f6a7-b8c9-4d3e-a2f1-0b1c2d3e4f5a",
  "name": "sample-document.pdf",
  "description": "Sample PDF document",
  "type": "pdf",
  "status": "processed",
  "original_name": "sample-document.pdf",
  "mime_type": "application/pdf",
  "size_bytes": 524288,
  "extracted_text": "This is a sample PDF document...",
  "processing_result": {
    "pages": 5,
    "language": "en"
  },
  "processingTime": 2345,
  "confidenceScore": 0.95,
  "metadata": {},
  "notebook_id": "81600038-1262-407b-a45a-9aeac648ead2",
  "owner_id": "90d5d23e-ac23-430c-844d-4f7c2a0dcb06",
  "tags": ["sample"],
  "processed_at": "2026-01-05T12:05:00Z",
  "chunking_strategy": "semantic",
  "chunk_count": 12,
  "average_chunk_size": 512,
  "chunk_quality_score": 0.92,
  "createdAt": "2026-01-05T12:00:00Z",
  "updatedAt": "2026-01-05T12:05:00Z"
}
```

### 5.2 Document List Response

**Endpoint**: `GET /api/v1/documents`

```typescript
interface DocumentListResponse {
  documents: DocumentResponse[];
  total: number;
  limit: number;
  offset: number;
  hasMore: boolean;
}
```

---

## 6. Space Responses

### 6.1 Space Response

**Context**: Spaces are returned as part of user onboarding or space switching

```typescript
interface SpaceResponse {
  space_id: string;              // UUID (tenant_id equivalent)
  space_type: "personal" | "organization";
  name: string;
  permissions: string[];         // User permissions in this space
}
```

**Example (personal space)**:
```json
{
  "space_id": "9855e094-36a6-4d3a-a4f5-d77da4614439",
  "space_type": "personal",
  "name": "Personal Workspace",
  "permissions": ["read", "write", "delete"]
}
```

**Example (organization space)**:
```json
{
  "space_id": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
  "space_type": "organization",
  "name": "Acme Corporation",
  "permissions": ["read", "write"]
}
```

### 6.2 Available Spaces Response

**Endpoint**: `GET /api/v1/users/me/spaces`

```typescript
interface AvailableSpacesResponse {
  personalSpace: SpaceResponse;
  organizationSpaces: SpaceResponse[];
}
```

**Example**:
```json
{
  "personalSpace": {
    "space_id": "9855e094-36a6-4d3a-a4f5-d77da4614439",
    "space_type": "personal",
    "name": "Personal Workspace",
    "permissions": ["read", "write", "delete"]
  },
  "organizationSpaces": [
    {
      "space_id": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
      "space_type": "organization",
      "name": "Acme Corporation",
      "permissions": ["read", "write"]
    }
  ]
}
```

---

## 7. Team & Organization Responses

### 7.1 Team Response

**Endpoints**:
- `GET /api/v1/teams/{id}`
- `POST /api/v1/teams` (create)

```typescript
interface TeamResponse {
  id: string;                    // UUID
  name: string;
  description?: string;
  spaceId: string;               // Organization space UUID
  organizationId?: string;       // Parent organization UUID
  createdAt: string;
  updatedAt: string;
  members?: TeamMemberResponse[];
}

interface TeamMemberResponse {
  userId: string;
  role: "owner" | "admin" | "member";
  joinedAt: string;
  invitedBy: string;
}
```

**Example**:
```json
{
  "id": "t1e2a3m4-5i6d-7890-abcd-ef1234567890",
  "name": "Engineering Team",
  "description": "Core engineering team",
  "spaceId": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
  "organizationId": "o1r2g3a4-n5i6-z7890-abcd-ef1234567890",
  "createdAt": "2026-01-05T10:00:00Z",
  "updatedAt": "2026-01-05T10:00:00Z",
  "members": [
    {
      "userId": "90d5d23e-ac23-430c-844d-4f7c2a0dcb06",
      "role": "owner",
      "joinedAt": "2026-01-05T10:00:00Z",
      "invitedBy": "system"
    }
  ]
}
```

### 7.2 Organization Response

**Endpoints**:
- `GET /api/v1/organizations/{id}`
- `POST /api/v1/organizations` (create)

```typescript
interface OrganizationResponse {
  id: string;                    // UUID
  name: string;
  description?: string;
  spaceId: string;               // Organization space UUID (1:1 mapping)
  createdAt: string;
  updatedAt: string;
  members?: OrganizationMemberResponse[];
  teams?: TeamResponse[];
}

interface OrganizationMemberResponse {
  userId: string;
  role: "owner" | "admin" | "member";
  title?: string;                // Job title
  department?: string;           // Department name
  joinedAt: string;
  invitedBy: string;
}
```

**Example**:
```json
{
  "id": "o1r2g3a4-n5i6-z7890-abcd-ef1234567890",
  "name": "Acme Corporation",
  "description": "Enterprise organization",
  "spaceId": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d",
  "createdAt": "2026-01-05T09:00:00Z",
  "updatedAt": "2026-01-05T09:00:00Z",
  "members": [
    {
      "userId": "90d5d23e-ac23-430c-844d-4f7c2a0dcb06",
      "role": "owner",
      "title": "Software Engineer",
      "department": "Engineering",
      "joinedAt": "2026-01-05T09:00:00Z",
      "invitedBy": "system"
    }
  ],
  "teams": []
}
```

---

## 8. Agent Builder Responses

### 8.1 Agent Response

**Endpoints**:
- `GET /api/v1/agents/{id}`
- `POST /api/v1/agents` (create)
- `PUT /api/v1/agents/{id}` (update)

```typescript
interface AgentResponse {
  id: string;                    // UUID
  name: string;
  description?: string;
  space_id: string;              // Space isolation UUID
  status: "draft" | "active" | "inactive";
  is_public: boolean;            // Public/private visibility
  is_template: boolean;          // Template agent
  configuration: object;         // JSONB agent configuration
  tags?: string[];
  createdAt: string;
  updatedAt: string;
}
```

**Example**:
```json
{
  "id": "a1g2e3n4-t5i6-d7890-abcd-ef1234567890",
  "name": "Document Analyzer",
  "description": "Analyzes documents for key insights",
  "space_id": "9855e094-36a6-4d3a-a4f5-d77da4614439",
  "status": "active",
  "is_public": false,
  "is_template": false,
  "configuration": {
    "model": "gpt-4",
    "temperature": 0.7
  },
  "tags": ["analysis", "documents"],
  "createdAt": "2026-01-05T11:00:00Z",
  "updatedAt": "2026-01-05T11:00:00Z"
}
```

### 8.2 Agent List Response

**Endpoint**: `GET /api/v1/agents`

```typescript
interface AgentListResponse {
  agents: AgentResponse[];
  total: number;
  page: number;
  size: number;
  hasMore: boolean;
}
```

### 8.3 Agent Execution Response

**Endpoint**: `POST /api/v1/agents/{id}/execute`

```typescript
interface AgentExecutionResponse {
  execution_id: string;          // UUID
  agent_id: string;
  status: "pending" | "running" | "completed" | "failed";
  result?: object;               // JSONB execution result
  error?: string;
  startedAt?: string;
  completedAt?: string;
  createdAt: string;
}
```

**Example**:
```json
{
  "execution_id": "e1x2e3c4-u5t6-i7890-abcd-ef1234567890",
  "agent_id": "a1g2e3n4-t5i6-d7890-abcd-ef1234567890",
  "status": "completed",
  "result": {
    "summary": "Document contains 5 key insights..."
  },
  "error": null,
  "startedAt": "2026-01-05T12:00:00Z",
  "completedAt": "2026-01-05T12:00:05Z",
  "createdAt": "2026-01-05T12:00:00Z"
}
```

---

## 9. Onboarding Responses

### 9.1 Onboarding Status Response

**Endpoint**: `GET /api/v1/onboarding/status`

```typescript
interface OnboardingStatusResponse {
  tutorial_completed: boolean;
  should_auto_trigger: boolean;
}
```

**Example**:
```json
{
  "tutorial_completed": false,
  "should_auto_trigger": true
}
```

### 9.2 Onboarding Complete Response

**Endpoint**: `POST /api/v1/onboarding/complete`

```typescript
interface OnboardingCompleteResponse {
  success: boolean;
  message: string;
}
```

**Example**:
```json
{
  "success": true,
  "message": "Tutorial marked as complete"
}
```

---

## 10. Error Responses

### HTTP Error Response

All failed requests throw errors with this structure:

```typescript
interface ErrorResponse {
  message: string;               // Error message
  response: {
    status: number;              // HTTP status code
  };
}
```

**Example Error Handling**:
```javascript
try {
  const response = await aetherApi.request('/notebooks/invalid-id');
} catch (error) {
  console.error(error.message);   // "HTTP 404: Notebook not found"
  console.error(error.response.status); // 404
}
```

### Common HTTP Status Codes

| Status | Meaning | Example |
|--------|---------|---------|
| 200 | OK | Successful GET/PUT request |
| 201 | Created | Successful POST request |
| 204 | No Content | Successful DELETE request (no body) |
| 400 | Bad Request | Invalid request data |
| 401 | Unauthorized | Missing or invalid token |
| 403 | Forbidden | Insufficient permissions |
| 404 | Not Found | Resource doesn't exist |
| 409 | Conflict | Resource already exists |
| 500 | Internal Server Error | Backend error |

---

## 11. Field Name Convention Differences

### Backend (Go) vs Frontend (JavaScript)

**Backend Go Structs** use `snake_case` internally and `camelCase` in JSON tags:

```go
type NotebookResponse struct {
    ID          string `json:"id"`
    OwnerID     string `json:"ownerId"`
    DocumentCount int  `json:"documentCount"`
}
```

**Frontend Redux State** preserves the camelCase from JSON:

```javascript
{
  id: "uuid",
  ownerId: "uuid",
  documentCount: 5
}
```

**Special Case**: `complianceSettings` returned as string by backend, parsed by frontend:

```javascript
// Backend returns (bug)
{ "complianceSettings": "{\"key\":\"value\"}" }

// Frontend parses
const parsedSettings = JSON.parse(response.complianceSettings);
```

---

## 12. Pagination Patterns

### Standard Pagination Parameters

**Query Parameters**:
- `limit`: Page size (default: 20)
- `offset`: Number of items to skip (default: 0)

**Response Structure**:
```typescript
{
  data: T[];           // Array of items
  total: number;       // Total count across all pages
  limit: number;       // Page size used
  offset: number;      // Offset used
  hasMore: boolean;    // More pages available
}
```

**Frontend Usage**:
```javascript
// Fetch first page
dispatch(fetchNotebooks({ limit: 20, offset: 0 }));

// Fetch next page
dispatch(fetchNotebooks({ limit: 20, offset: 20 }));

// Check if more available
if (response.hasMore) {
  // Load more button enabled
}
```

---

## 13. Request Headers

All API requests include these headers:

### Authentication Header

```
Authorization: Bearer {access_token}
```

### Space Context Headers

```
X-Space-Type: personal | organization
X-Space-ID: {space_uuid}
```

### Content Type

```
Content-Type: application/json
```

**Exception**: File uploads use `multipart/form-data` (browser sets boundary automatically)

---

## 14. Related Documentation

- [Redux Store Structure](../state/redux-store.md) - Frontend state management
- [Notebook Node](../../aether-be/nodes/notebook.md) - Backend notebook model
- [Document Node](../../aether-be/nodes/document.md) - Backend document model
- [User Node](../../aether-be/nodes/user.md) - Backend user model
- [Space Node](../../aether-be/nodes/space.md) - Backend space model
- [Keycloak JWT Structure](../../keycloak/tokens/jwt-structure.md) - Authentication tokens

---

## 15. Changelog

| Date | Version | Author | Changes |
|------|---------|--------|---------|
| 2026-01-05 | 1.0 | TAS Platform Team | Initial documentation of API response types |

---

**Maintained by**: TAS Platform Team
**Last Reviewed**: 2026-01-05
**Next Review**: 2026-02-05
