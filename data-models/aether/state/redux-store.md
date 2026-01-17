# Redux Store Structure - Aether Frontend

---
service: aether
model: Redux Store
database: Browser State (Redux Toolkit)
version: 1.0
last_updated: 2026-01-05
author: TAS Platform Team
---

## 1. Overview

**Purpose**: Central state management for the Aether frontend application using Redux Toolkit. The store manages authentication, notebook data, user information, teams, organizations, spaces, and UI state across the entire application.

**Lifecycle**: Initialized when the application loads, persists authentication state to localStorage, and updates reactively based on user actions and API responses.

**Ownership**: Aether Frontend (React 19 + TypeScript + Vite)

**Key Characteristics**:
- **7 Redux Slices**: auth, notebooks, organizations, teams, spaces, ui, users
- **Async Thunks**: API operations handled with createAsyncThunk from Redux Toolkit
- **localStorage Persistence**: Authentication tokens and UI preferences persisted across sessions
- **Keycloak Integration**: JWT-based authentication with token refresh flows
- **Space-Based Isolation**: Current space context propagated via HTTP headers

---

## 2. Store Configuration

### Store Setup

**File**: `src/store/store.js`

```javascript
import { configureStore } from '@reduxjs/toolkit';
import notebooksReducer from './slices/notebooksSlice.js';
import authReducer from './slices/authSlice.js';
import uiReducer from './slices/uiSlice.js';
import teamsReducer from './slices/teamsSlice.js';
import organizationsReducer from './slices/organizationsSlice.js';
import usersReducer from './slices/usersReducer.js';
import spacesReducer from './slices/spacesSlice.js';

const store = configureStore({
  reducer: {
    notebooks: notebooksReducer,
    auth: authReducer,
    ui: uiReducer,
    teams: teamsReducer,
    organizations: organizationsReducer,
    users: usersReducer,
    spaces: spacesReducer
  },
  middleware: (getDefaultMiddleware) =>
    getDefaultMiddleware().concat(syncMiddleware),
});
```

### Middleware

- **Default Middleware**: Redux Toolkit default middleware (serialization checks, immutability checks)
- **syncMiddleware**: Custom middleware for cross-slice synchronization

---

## 3. Slice Definitions

### 3.1 Authentication Slice (authSlice)

**File**: `src/store/slices/authSlice.js` (7,475 lines)

#### State Shape

```typescript
interface AuthState {
  user: {
    id: string;           // Keycloak sub (UUID)
    username: string;     // preferred_username from token
    name: string;         // Full name from token
    email: string;        // Email from token
  } | null;
  token: string | null;         // JWT access token
  refreshToken: string | null;  // JWT refresh token
  isAuthenticated: boolean;     // Authentication status
  loading: boolean;             // Async operation in progress
  error: string | null;         // Error message from failed operations
  initialized: boolean;         // Auth initialization complete
}
```

#### Async Thunks

| Thunk | Purpose | Keycloak Endpoint | localStorage Impact |
|-------|---------|-------------------|---------------------|
| `loginUser` | Authenticate user with username/password | `/realms/{realm}/protocol/openid-connect/token` | Stores `access_token`, `refresh_token` |
| `refreshToken` | Refresh expired access token | `/realms/{realm}/protocol/openid-connect/token` | Updates `access_token`, `refresh_token` |
| `logoutUser` | Clear authentication state | None (client-side) | Removes `access_token`, `refresh_token` |
| `initializeAuth` | Restore auth from localStorage on app load | None (reads localStorage) | Reads `access_token`, `refresh_token` |

#### Key Reducers

- `clearError`: Clear authentication error
- `setToken`: Manually set token and authentication status

#### Token Handling

**JWT Parsing**:
```javascript
// Parse user info from JWT payload
const tokenPayload = JSON.parse(atob(tokenData.access_token.split('.')[1]));
```

**Token Expiration Check**:
```javascript
const now = Date.now() / 1000;
if (tokenPayload.exp < now) {
  // Token expired, trigger refresh
}
```

**Keycloak Integration**:
- **Client ID**: `admin-cli`
- **Grant Type**: `password` (Direct Access Grant)
- **Realm**: Configurable via `VITE_KEYCLOAK_REALM` (default: `aether`)
- **URL**: Configurable via `VITE_KEYCLOAK_URL` (default: `window.location.origin`)

---

### 3.2 Notebooks Slice (notebooksSlice)

**File**: `src/store/slices/notebooksSlice.js` (19,289 lines)

#### State Shape

```typescript
interface NotebooksState {
  data: Notebook[];             // Array of notebook objects
  currentNotebook: Notebook | null;  // Selected notebook for detail view
  loading: boolean;             // Async operation in progress
  error: string | null;         // Error message
  total: number;                // Total notebooks count (for pagination)
  hasMore: boolean;             // More notebooks available
}

interface Notebook {
  id: string;                   // UUID
  name: string;
  description: string;
  visibility: 'private' | 'shared' | 'public';
  parentId: string | null;      // Hierarchical parent notebook
  spaceId: string;              // Space isolation
  ownerId: string;              // User who created the notebook
  documentCount: number;        // Number of documents
  totalSizeBytes: number;       // Total size of documents
  complianceSettings: object;   // JSONB compliance configuration
  createdAt: string;            // ISO 8601 timestamp
  updatedAt: string;            // ISO 8601 timestamp
  deletedAt: string | null;     // Soft delete timestamp
}
```

#### Async Thunks

| Thunk | Purpose | Backend Endpoint | Side Effects |
|-------|---------|------------------|--------------|
| `fetchNotebooks` | Load notebooks with pagination | `GET /api/v1/notebooks` | Updates `data`, `total`, `hasMore` |
| `createNotebook` | Create new notebook | `POST /api/v1/notebooks` | Adds to `data` array |
| `updateNotebook` | Update notebook properties | `PUT /api/v1/notebooks/{id}` | Merges updates into existing notebook |
| `deleteNotebook` | Soft delete notebook | `DELETE /api/v1/notebooks/{id}` | Removes from `data` array |
| `shareNotebookWithTeam` | Share notebook with team | `POST /api/v1/notebooks/{id}/share/team` | Updates notebook `shares` |
| `unshareNotebookFromTeam` | Unshare from team | `DELETE /api/v1/notebooks/{id}/share/team/{teamId}` | Removes from `shares` |
| `shareNotebookWithOrganization` | Share with organization | `POST /api/v1/notebooks/{id}/share/organization` | Updates notebook `shares` |
| `unshareNotebookFromOrganization` | Unshare from organization | `DELETE /api/v1/notebooks/{id}/share/org/{orgId}` | Removes from `shares` |

#### Special Handling: complianceSettings

The backend returns `complianceSettings` as a string that must be parsed:

```javascript
// Parse complianceSettings if it was sent as string
const parsedUpdates = { ...updates };
if (typeof parsedUpdates.complianceSettings === 'string') {
  try {
    parsedUpdates.complianceSettings = JSON.parse(parsedUpdates.complianceSettings);
  } catch (e) {
    console.error('Failed to parse compliance settings in update:', e);
  }
}
```

#### Pagination

```javascript
// Default pagination
const options = { limit: 20, offset: 0 };
```

---

### 3.3 Spaces Slice (spacesSlice)

**File**: `src/store/slices/spacesSlice.js` (4,871 lines)

#### State Shape

```typescript
interface SpacesState {
  currentSpace: Space | null;   // Active workspace context
  availableSpaces: {
    personalSpace: Space | null;          // User's personal space
    organizationSpaces: Space[];          // Organization spaces user has access to
  };
  loading: boolean;
  error: string | null;
  initialized: boolean;         // Space initialization complete
}

interface Space {
  space_id: string;             // UUID (tenant_id equivalent)
  space_type: 'personal' | 'organization';
  name: string;
  permissions: string[];        // User permissions in this space
}
```

#### Async Thunks

| Thunk | Purpose | Backend Endpoint | localStorage Impact |
|-------|---------|------------------|---------------------|
| `loadAvailableSpaces` | Load all spaces user can access | `GET /api/v1/users/me/spaces` | None |
| `switchSpace` | Change active space context | `GET /api/v1/health` (validation) | Stores `currentSpace` |

#### Space Headers for API Requests

**Helper Function**: `getSpaceHeaders(currentSpace)`

All API requests to the backend include space context headers:

```javascript
const headers = {
  'X-Space-Type': currentSpace.space_type,  // 'personal' or 'organization'
  'X-Space-ID': currentSpace.space_id        // UUID
};
```

**Backend Isolation**: These headers enforce space-based multi-tenancy in the backend Neo4j queries.

#### Reducers

- `setCurrentSpace`: Set current space without async validation (for localStorage initialization)
- `clearSpaceError`: Clear space error
- `resetSpaceState`: Reset to initial state (for logout)

#### Space Validation

When switching spaces, the frontend validates access by making a test request:

```javascript
await aetherApi.request('/health', { headers });
```

If the request succeeds, the space is valid and stored.

---

### 3.4 UI Slice (uiSlice)

**File**: `src/store/slices/uiSlice.js` (8,326 lines)

#### State Shape

```typescript
interface UIState {
  modals: {
    createNotebook: boolean;
    notebookDetail: boolean;
    uploadDocument: boolean;
    notebookSettings: boolean;
    notebookManager: boolean;
    exportData: boolean;
    contentsView: boolean;
    onboarding: boolean;
  };
  onboarding: {
    hasCompletedOnboarding: boolean;
    shouldAutoTrigger: boolean;
    isLoading: boolean;
    error: string | null;
  };
  notifications: Notification[];
  theme: 'light' | 'dark';
  sidebarCollapsed: boolean;
  viewMode: 'cards' | 'tree' | 'detail';
  loading: {
    global: boolean;
    notebooks: boolean;
    auth: boolean;
  };
}

interface Notification {
  id: number;
  type: 'info' | 'success' | 'warning' | 'error';
  title: string;
  message: string;
  duration: number;      // milliseconds
  timestamp: number;     // Date.now()
}
```

#### Async Thunks

| Thunk | Purpose | Backend Endpoint |
|-------|---------|------------------|
| `fetchOnboardingStatus` | Check if user completed tutorial | `GET /api/v1/onboarding/status` |
| `markOnboardingComplete` | Mark tutorial as complete | `POST /api/v1/onboarding/complete` |
| `resetOnboarding` | Reset tutorial (for testing) | `POST /api/v1/onboarding/reset` |

#### Reducers

**Modal Management**:
- `openModal(modalName)`: Open specific modal
- `closeModal(modalName)`: Close specific modal
- `closeAllModals()`: Close all modals
- `openOnboardingModal()`: Open onboarding tutorial
- `closeOnboardingModal()`: Close onboarding tutorial
- `clearOnboardingError()`: Clear onboarding error

**Notification Management**:
- `addNotification({ type, title, message, duration })`: Show notification
- `removeNotification(id)`: Dismiss notification
- `clearAllNotifications()`: Clear all notifications

**Theme Management**:
- `setTheme('light' | 'dark')`: Set theme (persists to localStorage)
- `toggleTheme()`: Switch between light/dark

**Sidebar Management**:
- `toggleSidebar()`: Collapse/expand sidebar
- `setSidebarCollapsed(boolean)`: Set sidebar state

**View Mode Management**:
- `setViewMode('cards' | 'tree' | 'detail')`: Change notebook view mode

#### localStorage Persistence

```javascript
// Theme
localStorage.setItem('aether_theme', theme);

// Sidebar state
localStorage.setItem('aether_sidebar_collapsed', sidebarCollapsed);
```

#### Notification Auto-Increment

```javascript
let notificationId = 0;

const notification = {
  id: ++notificationId,
  // ...
};
```

---

### 3.5 Teams Slice (teamsSlice)

**File**: `src/store/slices/teamsSlice.js` (16,187 lines)

#### State Shape

```typescript
interface TeamsState {
  data: Team[];                 // All teams user has access to
  currentTeam: Team | null;     // Selected team for detail view
  loading: boolean;
  error: string | null;
}

interface Team {
  id: string;                   // UUID
  name: string;
  description: string;
  spaceId: string;              // Organization space this team belongs to
  organizationId: string | null; // Parent organization
  createdAt: string;
  updatedAt: string;
  members: TeamMember[];
}

interface TeamMember {
  userId: string;
  role: 'owner' | 'admin' | 'member';
  joinedAt: string;
  invitedBy: string;
}
```

#### Async Thunks

| Thunk | Purpose | Backend Endpoint |
|-------|---------|------------------|
| `fetchTeams` | Load all teams | `GET /api/v1/teams` |
| `createTeam` | Create new team | `POST /api/v1/teams` |
| `updateTeam` | Update team properties | `PUT /api/v1/teams/{id}` |
| `deleteTeam` | Delete team | `DELETE /api/v1/teams/{id}` |
| `addTeamMember` | Add user to team | `POST /api/v1/teams/{id}/members` |
| `removeTeamMember` | Remove user from team | `DELETE /api/v1/teams/{id}/members/{userId}` |
| `updateTeamMemberRole` | Change member role | `PUT /api/v1/teams/{id}/members/{userId}/role` |

#### Team-Notebook Relationship

Teams can have notebooks shared with them (see notebooksSlice `shareNotebookWithTeam`).

---

### 3.6 Organizations Slice (organizationsSlice)

**File**: `src/store/slices/organizationsSlice.js` (16,417 lines)

#### State Shape

```typescript
interface OrganizationsState {
  data: Organization[];         // All organizations user belongs to
  currentOrganization: Organization | null;
  loading: boolean;
  error: string | null;
}

interface Organization {
  id: string;                   // UUID
  name: string;
  description: string;
  spaceId: string;              // Organization space
  createdAt: string;
  updatedAt: string;
  members: OrganizationMember[];
  teams: Team[];                // Teams within this organization
}

interface OrganizationMember {
  userId: string;
  role: 'owner' | 'admin' | 'member';
  title: string;                // Job title
  department: string;           // Department name
  joinedAt: string;
  invitedBy: string;
}
```

#### Async Thunks

| Thunk | Purpose | Backend Endpoint |
|-------|---------|------------------|
| `fetchOrganizations` | Load all organizations | `GET /api/v1/organizations` |
| `createOrganization` | Create new organization | `POST /api/v1/organizations` |
| `updateOrganization` | Update organization properties | `PUT /api/v1/organizations/{id}` |
| `deleteOrganization` | Delete organization | `DELETE /api/v1/organizations/{id}` |
| `addOrganizationMember` | Add user to organization | `POST /api/v1/organizations/{id}/members` |
| `removeOrganizationMember` | Remove user from organization | `DELETE /api/v1/organizations/{id}/members/{userId}` |
| `updateOrganizationMemberRole` | Change member role | `PUT /api/v1/organizations/{id}/members/{userId}/role` |

#### Organization-Space Relationship

**1:1 Mapping**: Each organization has exactly one space (`spaceId`). When a user switches to an organization space, notebooks and resources are isolated to that organization.

---

### 3.7 Users Slice (usersSlice)

**File**: `src/store/slices/usersSlice.js` (22,586 lines)

#### State Shape

```typescript
interface UsersState {
  data: User[];                 // All users (for admin/team management)
  currentUser: User | null;     // Full profile of current user
  loading: boolean;
  error: string | null;
}

interface User {
  id: string;                   // UUID (synced with Keycloak)
  username: string;
  email: string;
  name: string;
  tenantId: string;             // Multi-tenancy identifier
  personalSpaceId: string;      // User's personal space
  status: 'active' | 'suspended' | 'deleted';
  onboardingStatus: 'not_started' | 'in_progress' | 'completed';
  createdAt: string;
  updatedAt: string;
}
```

#### Async Thunks

| Thunk | Purpose | Backend Endpoint |
|-------|---------|------------------|
| `fetchCurrentUser` | Load full profile of authenticated user | `GET /api/v1/users/me` |
| `updateCurrentUser` | Update current user profile | `PUT /api/v1/users/me` |
| `fetchUsers` | Load all users (admin) | `GET /api/v1/users` |

---

## 4. Cross-Slice Integration Patterns

### Authentication → All Slices

When `loginUser` succeeds:
1. `authSlice` stores JWT token
2. All subsequent API requests include `Authorization: Bearer {token}` header
3. `spacesSlice` loads available spaces
4. `notebooksSlice` fetches notebooks for current space
5. `usersSlice` fetches current user profile

### Space Switching → Notebooks, Teams, Organizations

When `switchSpace` is called:
1. `spacesSlice` validates and stores new space
2. `notebooksSlice` clears data and refetches for new space
3. `teamsSlice` refetches teams for new space (if organization)
4. `organizationsSlice` updates current organization

### Logout → All Slices

When `logoutUser` is called:
1. `authSlice` clears tokens and user
2. `spacesSlice.resetSpaceState()` clears space context
3. `notebooksSlice` clears all notebook data
4. `teamsSlice` clears teams
5. `organizationsSlice` clears organizations
6. `usersSlice` clears user data
7. `uiSlice` resets to initial state

---

## 5. localStorage Schema

### Keys and Values

| Key | Type | Purpose | Set By |
|-----|------|---------|--------|
| `access_token` | string (JWT) | Keycloak access token | authSlice (loginUser, refreshToken) |
| `refresh_token` | string (JWT) | Keycloak refresh token | authSlice (loginUser, refreshToken) |
| `currentSpace` | JSON string | Current space context | spacesSlice (switchSpace) |
| `aether_theme` | 'light' \| 'dark' | UI theme preference | uiSlice (setTheme, toggleTheme) |
| `aether_sidebar_collapsed` | 'true' \| 'false' | Sidebar state | uiSlice (toggleSidebar, setSidebarCollapsed) |

### Example Values

```javascript
// access_token
"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiI5MGQ1ZDIzZS1hYzIzLTQzMGMtODQ0ZC00ZjdjMmEwZGNiMDYi..."

// refresh_token
"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3NjcyMDc2ODAsImlhdCI6MTc2NzIwNzM4MCwi..."

// currentSpace
'{"space_id":"9855e094-36a6-4d3a-a4f5-d77da4614439","space_type":"personal","name":"Personal Workspace","permissions":["read","write","delete"]}'

// aether_theme
"dark"

// aether_sidebar_collapsed
"true"
```

---

## 6. API Integration

### Aether API Client

**File**: `src/services/aetherApi.js`

All Redux thunks use the centralized API client which:
- Adds `Authorization` header from Redux state
- Adds `X-Space-Type` and `X-Space-ID` headers from current space
- Handles token refresh on 401 errors
- Provides consistent error handling

### Request Flow

```
1. Component dispatches Redux thunk
2. Thunk calls aetherApi method
3. aetherApi adds headers (auth + space)
4. Backend processes request with space isolation
5. Response returned to thunk
6. Thunk updates Redux state
7. Component re-renders with new state
```

---

## 7. Performance Considerations

### Pagination

Notebooks slice supports pagination to avoid loading all notebooks:

```javascript
await dispatch(fetchNotebooks({ limit: 20, offset: 0 }));
```

### Selective Updates

`updateNotebook` merges updates instead of replacing entire object:

```javascript
return { ...existingNotebook, ...response.data, ...parsedUpdates };
```

### Caching Strategy

- **No TTL**: Redux state is in-memory only
- **Refetch on space switch**: Data invalidated when switching spaces
- **Manual refresh**: User can pull-to-refresh notebooks list

---

## 8. Security & Compliance

### Token Storage

**Security Consideration**: Access tokens stored in localStorage are vulnerable to XSS attacks.

**Mitigation**:
- Short-lived access tokens (5 minutes)
- Automatic refresh token rotation
- HttpOnly cookie option not available (Keycloak limitation with public clients)

### Space Isolation

All API requests include space headers to enforce multi-tenancy:

```javascript
'X-Space-Type': currentSpace.space_type
'X-Space-ID': currentSpace.space_id
```

Backend validates these headers against Neo4j `space_id` properties.

### Sensitive Data

| Field | Sensitivity | Stored In | Expiration |
|-------|-------------|-----------|------------|
| `access_token` | High | localStorage | 5 minutes |
| `refresh_token` | Critical | localStorage | 30 days |
| User email | Medium | Redux state | Until logout |
| Notebook data | Medium | Redux state | Until space switch |

---

## 9. Migration History

### Version 1.0 (2026-01-05)
- Initial Redux Toolkit implementation
- 7 slices: auth, notebooks, spaces, ui, teams, organizations, users
- Keycloak authentication integration
- Space-based multi-tenancy
- localStorage persistence for auth and UI preferences

---

## 10. Known Issues & Limitations

### Issue 1: localStorage Token Security
**Description**: JWT tokens stored in localStorage are vulnerable to XSS attacks.
**Workaround**: Use short token expiration times (5 minutes) and automatic refresh.
**Tracking**: Consider migrating to HttpOnly cookies when Keycloak supports it for public clients.

### Issue 2: complianceSettings Parsing
**Description**: Backend returns `complianceSettings` as string instead of JSON object.
**Workaround**: Manual JSON.parse() in `updateNotebook` thunk.
**Impact**: Extra parsing logic, potential parse errors.
**Future**: Backend should return JSONB fields as objects.

### Limitation 1: No Offline Support
**Description**: Redux state is in-memory only, no offline persistence.
**Impact**: All data lost on page reload (except auth tokens).
**Future**: Consider Redux Persist for critical data.

### Limitation 2: No Optimistic Updates
**Description**: UI waits for backend response before updating state.
**Impact**: Slower perceived performance on slow networks.
**Future**: Implement optimistic updates with rollback on error.

---

## 11. State Selectors

### Recommended Selectors

```javascript
// Get authenticated user
const selectUser = (state) => state.auth.user;

// Get current space context
const selectCurrentSpace = (state) => state.spaces.currentSpace;

// Get all notebooks for current space
const selectNotebooks = (state) => state.notebooks.data;

// Get current notebook (detail view)
const selectCurrentNotebook = (state) => state.notebooks.currentNotebook;

// Check if user is authenticated
const selectIsAuthenticated = (state) => state.auth.isAuthenticated;

// Get notifications for display
const selectNotifications = (state) => state.ui.notifications;

// Get theme preference
const selectTheme = (state) => state.ui.theme;
```

### Memoized Selectors (createSelector)

Some slices use `createSelector` from Redux Toolkit for performance:

```javascript
import { createSelector } from '@reduxjs/toolkit';

// Example: Get notebooks by visibility
const selectNotebooksByVisibility = createSelector(
  [selectNotebooks, (state, visibility) => visibility],
  (notebooks, visibility) => notebooks.filter(nb => nb.visibility === visibility)
);
```

---

## 12. Redux DevTools Integration

**Browser Extension**: Redux DevTools Extension for Chrome/Firefox

**Features**:
- Time-travel debugging
- Action replay
- State snapshots
- Performance monitoring

**Configuration**: Redux Toolkit automatically enables DevTools in development mode.

---

## 13. Related Documentation

- [API Response Types](../types/api-responses.md) (Phase 3)
- [Component Props Interfaces](../types/component-props.md) (Phase 3)
- [LocalStorage Schema](../models/local-storage.md) (Phase 3)
- [Keycloak JWT Structure](../../keycloak/tokens/jwt-structure.md)
- [User Node](../../aether-be/nodes/user.md)
- [Notebook Node](../../aether-be/nodes/notebook.md)
- [Space Node](../../aether-be/nodes/space.md)
- [Cross-Service ID Mapping](../../cross-service/mappings/id-mapping-chain.md)

---

## 14. Changelog

| Date | Version | Author | Changes |
|------|---------|--------|---------|
| 2026-01-05 | 1.0 | TAS Platform Team | Initial documentation of Redux store structure |

---

**Maintained by**: TAS Platform Team
**Last Reviewed**: 2026-01-05
**Next Review**: 2026-02-05
