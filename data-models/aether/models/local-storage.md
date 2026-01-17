# LocalStorage Schema - Aether Frontend

---
service: aether
model: LocalStorage Schema
database: Browser LocalStorage
version: 1.0
last_updated: 2026-01-05
author: TAS Platform Team
---

## 1. Overview

**Purpose**: Documents all browser localStorage keys used by the Aether frontend for client-side data persistence. LocalStorage provides cross-session persistence for authentication tokens, user preferences, and application state.

**Lifecycle**: Data persists across browser sessions until explicitly cleared by logout, browser cache clearing, or manual deletion.

**Ownership**: Aether Frontend (React 19 + TypeScript + Vite)

**Key Characteristics**:
- **Browser Storage**: Uses browser's `window.localStorage` API
- **Key-Value Pairs**: All data stored as strings (JSON for complex objects)
- **Domain Scoped**: Storage isolated per domain/origin
- **No Expiration**: Data persists indefinitely unless cleared
- **Size Limit**: ~5-10MB per origin (browser-dependent)
- **Synchronous Access**: Blocking read/write operations

---

## 2. Storage Categories

### 2.1 Authentication & Security
- `access_token` - Keycloak JWT access token
- `refresh_token` - Keycloak JWT refresh token

### 2.2 Application State
- `currentSpace` - Active workspace context

### 2.3 User Preferences
- `aether_theme` - UI theme preference
- `aether_sidebar_collapsed` - Sidebar state

---

## 3. Authentication Keys

### 3.1 access_token

**Purpose**: JWT access token for API authentication

**Type**: String (JWT)

**Set By**:
- `authSlice` - `loginUser` thunk
- `authSlice` - `refreshToken` thunk
- `aetherApi` - `refreshToken()` method

**Read By**:
- `authSlice` - `initializeAuth` thunk (on app load)
- `tokenStorage.getAccessToken()` (via aetherApi)

**Cleared By**:
- `authSlice` - `logoutUser` thunk
- `aetherApi` - `ensureValidToken()` (on expiration)

**Lifetime**: 5 minutes (300 seconds)

**Format**:
```
eyJhbGciOiJSUzI1NiIsInR5cCIgOiAiSldUIiwia2lkIiA6ICIxV21hWkpuRVZKYkdhUzlLOV93bVFNaDFnaWIwM0xGS3VETXJlVmVYVUxRIn0.eyJleHAiOjE3NjcyMDc2ODAsImlhdCI6MTc2NzIwNzM4MCwianRpIjoiYTZhNjRiZWQtYzljYi00MGU5LWEwZDgtZGQ2ZWNjMGY4ZWRlIiwiaXNzIjoiaHR0cHM6Ly9rZXljbG9hay50YXMuc2NoYXJiZXIuY29tL3JlYWxtcy9hZXRoZXIiLCJhdWQiOiJhY2NvdW50Iiwic3ViIjoiNmRiNjMzOTMtMWM4Yi00Nzg4LWE5M2EtMjBiMTAyMGU2MGY4IiwidHlwIjoiQmVhcmVyIiwiYXpwIjoiYWV0aGVyLWZyb250ZW5kIn0...
```

**Decoded Payload Example**:
```json
{
  "exp": 1767207680,
  "iat": 1767207380,
  "jti": "a6a64bed-c9cb-40e9-a0d8-dd6ecc0f8ede",
  "iss": "https://keycloak.tas.scharber.com/realms/aether",
  "aud": "account",
  "sub": "6db63393-1c8b-4788-a93a-20b1020e60f8",
  "typ": "Bearer",
  "azp": "aether-frontend",
  "session_state": "ca7e7d7c-f43c-47aa-a8f2-63c7116493a9",
  "realm_access": {
    "roles": ["offline_access", "uma_authorization", "default-roles-aether", "user"]
  },
  "scope": "email profile",
  "sid": "ca7e7d7c-f43c-47aa-a8f2-63c7116493a9",
  "email_verified": true,
  "name": "Test User",
  "preferred_username": "test-user-1767202926@example.com",
  "given_name": "Test",
  "family_name": "User",
  "email": "test-user-1767202926@example.com"
}
```

**Usage Example**:
```javascript
// Set token
localStorage.setItem('access_token', tokenData.access_token);

// Get token
const token = localStorage.getItem('access_token');

// Check expiration
const payload = JSON.parse(atob(token.split('.')[1]));
const isExpired = payload.exp < (Date.now() / 1000);

// Clear token
localStorage.removeItem('access_token');
```

**Security Considerations**:
- ⚠️ **XSS Vulnerability**: Tokens in localStorage are accessible to JavaScript, vulnerable to XSS attacks
- ✅ **Mitigation**: Short expiration (5 minutes), automatic refresh
- ⚠️ **Not HttpOnly**: Cannot use secure HttpOnly cookie pattern with Keycloak public clients
- ✅ **HTTPS Only**: Always use HTTPS in production to prevent token interception

---

### 3.2 refresh_token

**Purpose**: JWT refresh token for obtaining new access tokens

**Type**: String (JWT)

**Set By**:
- `authSlice` - `loginUser` thunk
- `authSlice` - `refreshToken` thunk
- `aetherApi` - `refreshToken()` method

**Read By**:
- `authSlice` - `initializeAuth` thunk
- `authSlice` - `refreshToken` thunk
- `aetherApi` - `refreshToken()` method

**Cleared By**:
- `authSlice` - `logoutUser` thunk
- `aetherApi` - `ensureValidToken()` (on refresh token expiration)

**Lifetime**: 30 minutes (1800 seconds)

**Format**: Same JWT structure as access_token

**Usage Example**:
```javascript
// Refresh access token
const refreshTokenValue = localStorage.getItem('refresh_token');

const response = await fetch(`${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}/protocol/openid-connect/token`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
  body: new URLSearchParams({
    client_id: 'aether-frontend',
    grant_type: 'refresh_token',
    refresh_token: refreshTokenValue
  })
});

const tokenData = await response.json();
localStorage.setItem('access_token', tokenData.access_token);
localStorage.setItem('refresh_token', tokenData.refresh_token);
```

**Security Considerations**:
- 🔴 **Critical**: More sensitive than access token (longer lifetime)
- ⚠️ **Rotation**: Refresh tokens rotate on each refresh (old token invalidated)
- ✅ **Single Use**: Each refresh token can only be used once

---

## 4. Application State Keys

### 4.1 currentSpace

**Purpose**: Active workspace context for space-based multi-tenancy

**Type**: JSON string (SpaceContext object)

**Set By**:
- `spacesSlice` - `switchSpace` thunk
- `spacesSlice` - `setCurrentSpace` reducer

**Read By**:
- `spacesSlice` - Initial state hydration (on app load)
- `aetherApi.getSpaceHeaders()` - For API request headers

**Cleared By**:
- `spacesSlice` - `resetSpaceState` reducer (on logout)

**Lifetime**: Persists until logout or space switch

**Schema**:
```typescript
interface SpaceContext {
  space_id: string;              // UUID
  space_type: "personal" | "organization";
  name: string;
  permissions: string[];         // ["read", "write", "delete"]
}
```

**Example Value**:
```json
{
  "space_id": "9855e094-36a6-4d3a-a4f5-d77da4614439",
  "space_type": "personal",
  "name": "Personal Workspace",
  "permissions": ["read", "write", "delete"]
}
```

**Usage Example**:
```javascript
// Set current space
const space = {
  space_id: "9855e094-36a6-4d3a-a4f5-d77da4614439",
  space_type: "personal",
  name: "Personal Workspace",
  permissions: ["read", "write", "delete"]
};
localStorage.setItem('currentSpace', JSON.stringify(space));

// Get current space
const savedSpace = localStorage.getItem('currentSpace');
if (savedSpace) {
  const currentSpace = JSON.parse(savedSpace);
  console.log(currentSpace.space_id); // "9855e094-36a6-4d3a-a4f5-d77da4614439"
}

// Clear on logout
localStorage.removeItem('currentSpace');
```

**Validation**:
```javascript
// Validate space_type before using
if (currentSpace.space_type !== 'personal' && currentSpace.space_type !== 'organization') {
  console.error('Invalid space_type:', currentSpace.space_type);
  localStorage.removeItem('currentSpace');
}
```

**API Integration**:
```javascript
// Use in API requests
const headers = {
  'X-Space-Type': currentSpace.space_type,
  'X-Space-ID': currentSpace.space_id
};
```

---

## 5. User Preference Keys

### 5.1 aether_theme

**Purpose**: UI theme preference (light/dark mode)

**Type**: String enum

**Set By**:
- `uiSlice` - `setTheme` reducer
- `uiSlice` - `toggleTheme` reducer

**Read By**:
- `uiSlice` - Initial state hydration (on app load)
- UI components for theme application

**Cleared By**: Never automatically cleared (persists indefinitely)

**Allowed Values**: `"light"` | `"dark"`

**Default**: `"light"`

**Example Value**:
```
"dark"
```

**Usage Example**:
```javascript
// Set theme
localStorage.setItem('aether_theme', 'dark');

// Get theme
const theme = localStorage.getItem('aether_theme') || 'light';

// Toggle theme
const currentTheme = localStorage.getItem('aether_theme') || 'light';
const newTheme = currentTheme === 'light' ? 'dark' : 'light';
localStorage.setItem('aether_theme', newTheme);
```

**Redux Integration**:
```javascript
// In uiSlice initial state
const initialState = {
  theme: localStorage.getItem('aether_theme') || 'light'
};

// In setTheme reducer
setTheme: (state, action) => {
  state.theme = action.payload;
  localStorage.setItem('aether_theme', action.payload);
}
```

---

### 5.2 aether_sidebar_collapsed

**Purpose**: Sidebar collapsed/expanded state

**Type**: String boolean

**Set By**:
- `uiSlice` - `toggleSidebar` reducer
- `uiSlice` - `setSidebarCollapsed` reducer

**Read By**:
- `uiSlice` - Initial state hydration (on app load)
- Sidebar component for initial render

**Cleared By**: Never automatically cleared

**Allowed Values**: `"true"` | `"false"`

**Default**: `false`

**Example Value**:
```
"true"
```

**Usage Example**:
```javascript
// Set sidebar state
localStorage.setItem('aether_sidebar_collapsed', 'true');

// Get sidebar state
const isCollapsed = localStorage.getItem('aether_sidebar_collapsed') === 'true';

// Toggle sidebar
const currentState = localStorage.getItem('aether_sidebar_collapsed') === 'true';
localStorage.setItem('aether_sidebar_collapsed', String(!currentState));
```

**Redux Integration**:
```javascript
// In uiSlice initial state
const initialState = {
  sidebarCollapsed: localStorage.getItem('aether_sidebar_collapsed') === 'true'
};

// In toggleSidebar reducer
toggleSidebar: (state) => {
  state.sidebarCollapsed = !state.sidebarCollapsed;
  localStorage.setItem('aether_sidebar_collapsed', state.sidebarCollapsed);
}
```

---

## 6. Complete Storage Map

| Key | Type | Category | Cleared On Logout | Size Estimate |
|-----|------|----------|-------------------|---------------|
| `access_token` | JWT String | Auth | ✅ Yes | ~1-2 KB |
| `refresh_token` | JWT String | Auth | ✅ Yes | ~1-2 KB |
| `currentSpace` | JSON String | State | ✅ Yes | ~200 bytes |
| `aether_theme` | String Enum | Preference | ❌ No | ~10 bytes |
| `aether_sidebar_collapsed` | String Boolean | Preference | ❌ No | ~10 bytes |

**Total Storage**: ~2-5 KB (well within browser limits)

---

## 7. Lifecycle Workflows

### 7.1 Initial App Load

```javascript
// 1. Initialize authentication
const token = localStorage.getItem('access_token');
const refreshToken = localStorage.getItem('refresh_token');

if (token) {
  // Check expiration
  const payload = JSON.parse(atob(token.split('.')[1]));
  const isExpired = payload.exp < (Date.now() / 1000);

  if (isExpired && refreshToken) {
    // Trigger refresh
    dispatch(refreshToken());
  } else if (!isExpired) {
    // Restore auth state
    dispatch(initializeAuth());
  }
}

// 2. Load current space
const savedSpace = localStorage.getItem('currentSpace');
if (savedSpace) {
  const currentSpace = JSON.parse(savedSpace);
  dispatch(setCurrentSpace(currentSpace));
}

// 3. Load UI preferences
const theme = localStorage.getItem('aether_theme') || 'light';
const sidebarCollapsed = localStorage.getItem('aether_sidebar_collapsed') === 'true';
dispatch(setTheme(theme));
dispatch(setSidebarCollapsed(sidebarCollapsed));
```

### 7.2 Login Flow

```javascript
// 1. Authenticate with Keycloak
const response = await fetch(`${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}/protocol/openid-connect/token`, {
  method: 'POST',
  body: new URLSearchParams({
    client_id: 'aether-frontend',
    username,
    password,
    grant_type: 'password'
  })
});

const tokenData = await response.json();

// 2. Store tokens
localStorage.setItem('access_token', tokenData.access_token);
localStorage.setItem('refresh_token', tokenData.refresh_token);

// 3. Load user's spaces
const spacesResponse = await fetch('/api/v1/users/me/spaces');
const { personalSpace } = await spacesResponse.json();

// 4. Set default space (personal)
localStorage.setItem('currentSpace', JSON.stringify(personalSpace));
```

### 7.3 Logout Flow

```javascript
// 1. Clear authentication tokens
localStorage.removeItem('access_token');
localStorage.removeItem('refresh_token');

// 2. Clear application state
localStorage.removeItem('currentSpace');

// 3. Preserve UI preferences
// aether_theme and aether_sidebar_collapsed are NOT cleared

// 4. Reset Redux state
dispatch(logoutUser());
dispatch(resetSpaceState());
```

### 7.4 Space Switching

```javascript
// 1. Validate new space
const response = await fetch('/api/v1/health', {
  headers: {
    'X-Space-Type': newSpace.space_type,
    'X-Space-ID': newSpace.space_id
  }
});

if (response.ok) {
  // 2. Update localStorage
  localStorage.setItem('currentSpace', JSON.stringify(newSpace));

  // 3. Update Redux state
  dispatch(switchSpace(newSpace));

  // 4. Reload space-specific data
  dispatch(fetchNotebooks());
  dispatch(fetchTeams());
}
```

---

## 8. Data Persistence Patterns

### 8.1 Immediate Persistence

Preferences are saved immediately on change:

```javascript
// Theme change
const setTheme = (theme) => {
  localStorage.setItem('aether_theme', theme);
  dispatch({ type: 'SET_THEME', payload: theme });
};
```

### 8.2 Deferred Persistence

Authentication tokens persist after successful API call:

```javascript
const refreshToken = async () => {
  const response = await fetch(tokenEndpoint, ...);
  const tokenData = await response.json();

  // Only persist if refresh succeeded
  localStorage.setItem('access_token', tokenData.access_token);
  localStorage.setItem('refresh_token', tokenData.refresh_token);
};
```

### 8.3 Validation Before Persistence

Space context validated before saving:

```javascript
const switchSpace = async (space) => {
  // Validate space access
  const response = await fetch('/api/v1/health', { headers: getSpaceHeaders(space) });

  if (!response.ok) {
    throw new Error('Invalid space access');
  }

  // Only persist if valid
  localStorage.setItem('currentSpace', JSON.stringify(space));
};
```

---

## 9. Security Best Practices

### 9.1 XSS Protection

**Risk**: LocalStorage is vulnerable to XSS attacks

**Mitigations**:
1. **Short token lifetimes**: Access tokens expire in 5 minutes
2. **Token rotation**: Refresh tokens rotate on use
3. **Content Security Policy**: Restrict inline scripts
4. **Input sanitization**: Sanitize all user input
5. **HTTPS only**: Prevent token interception

### 9.2 Token Refresh Strategy

**Preemptive Refresh**:
```javascript
// Refresh 2 minutes before expiration (120 seconds buffer)
if (tokenStorage.isAccessTokenExpired(2)) {
  await this.refreshToken();
}
```

**Automatic Retry**:
```javascript
// On 401, refresh token and retry request once
if (response.status === 401) {
  await this.refreshToken();
  // Retry with new token
  return this.makeRequest(url, config);
}
```

### 9.3 Data Validation

**Always validate JSON before use**:
```javascript
try {
  const currentSpace = JSON.parse(localStorage.getItem('currentSpace'));

  // Validate structure
  if (!currentSpace.space_id || !currentSpace.space_type) {
    throw new Error('Invalid space structure');
  }

  // Validate space_type enum
  if (currentSpace.space_type !== 'personal' && currentSpace.space_type !== 'organization') {
    throw new Error('Invalid space_type');
  }
} catch (error) {
  console.error('Invalid currentSpace in localStorage:', error);
  localStorage.removeItem('currentSpace');
}
```

---

## 10. Browser Compatibility

### Supported Browsers

| Browser | Version | LocalStorage Support |
|---------|---------|---------------------|
| Chrome | 4+ | ✅ Full |
| Firefox | 3.5+ | ✅ Full |
| Safari | 4+ | ✅ Full |
| Edge | All | ✅ Full |
| IE | 8+ | ✅ Full |

### Feature Detection

```javascript
// Check localStorage availability
function isLocalStorageAvailable() {
  try {
    const test = '__localStorage_test__';
    localStorage.setItem(test, test);
    localStorage.removeItem(test);
    return true;
  } catch (e) {
    return false;
  }
}

if (!isLocalStorageAvailable()) {
  console.error('LocalStorage not available - authentication will not persist');
}
```

### Incognito/Private Mode

**Behavior**: LocalStorage available but cleared on browser close

**Impact**:
- Users must re-authenticate on each browser session
- Preferences reset each session
- No persistent state

---

## 11. Migration & Cleanup

### 11.1 Legacy Key Migration

If key names change in future versions:

```javascript
// Migrate old key to new key
const oldToken = localStorage.getItem('old_access_token');
if (oldToken) {
  localStorage.setItem('access_token', oldToken);
  localStorage.removeItem('old_access_token');
}
```

### 11.2 Corrupted Data Cleanup

```javascript
// Clean up corrupted space data
try {
  const space = JSON.parse(localStorage.getItem('currentSpace'));
  if (!space.space_id) {
    localStorage.removeItem('currentSpace');
  }
} catch (e) {
  localStorage.removeItem('currentSpace');
}
```

### 11.3 Manual Clear All

For debugging or user-initiated clear:

```javascript
// Clear all Aether-related keys
const clearAetherStorage = () => {
  localStorage.removeItem('access_token');
  localStorage.removeItem('refresh_token');
  localStorage.removeItem('currentSpace');
  localStorage.removeItem('aether_theme');
  localStorage.removeItem('aether_sidebar_collapsed');
};
```

---

## 12. Debugging & Inspection

### Chrome DevTools

1. Open DevTools (F12)
2. Navigate to **Application** tab
3. Select **Local Storage** → `https://aether.tas.scharber.com`
4. View all key-value pairs

### Programmatic Inspection

```javascript
// List all localStorage keys
Object.keys(localStorage).forEach(key => {
  console.log(`${key}: ${localStorage.getItem(key)}`);
});

// Get all Aether keys
const aetherKeys = Object.keys(localStorage).filter(key =>
  key.startsWith('aether_') ||
  ['access_token', 'refresh_token', 'currentSpace'].includes(key)
);
```

---

## 13. Related Documentation

- [Redux Store Structure](../state/redux-store.md) - Frontend state management
- [API Response Types](../types/api-responses.md) - API response schemas
- [Keycloak JWT Structure](../../keycloak/tokens/jwt-structure.md) - Token anatomy
- [Space Node](../../aether-be/nodes/space.md) - Backend space model
- [User Node](../../aether-be/nodes/user.md) - Backend user model

---

## 14. Changelog

| Date | Version | Author | Changes |
|------|---------|--------|---------|
| 2026-01-05 | 1.0 | TAS Platform Team | Initial documentation of localStorage schema |

---

**Maintained by**: TAS Platform Team
**Last Reviewed**: 2026-01-05
**Next Review**: 2026-02-05
