Here is the updated implementation plan for `navalplan`. This plan assumes the database schema now uses a `person` table and strictly uses `Person` terminology throughout the stack, mirroring `navallog` but adapted for `navalplan`'s simpler requirements (no invitations).

### **Phase 1: Context & Models**

1. **Create `code/app/backend/context/context.go**`
* **Goal**: Pass authenticated `Person` data through the request context.
* **Implementation**:
* Define a context key (e.g., `personKey`).
* Implement `AddPersonToContext(ctx, person)` and `GetPersonFromContext(ctx)`.
* *Reference*: See `navallog/code/app/backend/context/context.go`.




2. **Update `code/app/backend/models/models.go**`
* **Goal**: Define data structures for authentication.
* **Add Structs**:
```go
type Person struct {
    ID         int64     `json:"id" db:"id"`
    GoogleID   string    `json:"google_id" db:"google_id"`
    Email      string    `json:"email" db:"email"`
    Name       string    `json:"name" db:"name"`
    PictureURL string    `json:"picture_url" db:"picture_url"`
    // Add CreatedAt/IsAdmin only if present in your new schema
}

type Session struct {
    Token     string    `json:"token" db:"token"`
    PersonID  int64     `json:"person_id" db:"person_id"`
    CreatedAt time.Time `json:"created_at" db:"created_at"`
    ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
}

```





### **Phase 2: Datastore Layer**

1. **Create `code/app/backend/datastore/person.go**`
* **Goal**: Database operations for the `person` table.
* **Methods to Implement**:
* `FindPersonByGoogleID(ctx, googleID)`: Returns `*models.Person`.
* `GetPersonByID(ctx, id)`: Returns `*models.Person`.
* `CreatePerson(ctx, googleID, email, name, pictureURL)`: Inserts a new row. **Note**: Unlike `navallog`, do not check for an invitation code; just insert the user.
* `UpdatePersonName(ctx, id, name)`: Updates the display name.




2. **Create `code/app/backend/datastore/sessions.go**`
* **Goal**: Manage session tokens.
* **Methods to Implement**:
* `CreateSession(ctx, token, personID, expiresAt)`
* `GetSession(ctx, token)`: Returns `*models.Session`.
* `DeleteSession(ctx, token)`


* *Reference*: Port strictly from `navallog/code/app/backend/datastore/session.go`, ensuring SQL queries reference `person_id`.


3. **Update `code/app/backend/datastore/interface.go**`
* Add the new methods above to the `Store` interface so they can be accessed via the `Server` struct.



### **Phase 3: Auth Handlers & Middleware**

1. **Create `code/app/backend/server/auth.go**`
* **Goal**: Handle OAuth flow and session management.
* **Google OAuth Config**: Setup `oauth2.Config` using env vars (`GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URL`).
* **Handlers**:
* `oauthGoogleLogin`: Generates state and redirects to Google.
* `oauthGoogleCallback`:
1. Exchanges code for token.
2. Fetches user info from Google.
3. Calls `store.FindPersonByGoogleID`.
4. **If not found**: Call `store.CreatePerson` (Auto-registration).
5. Generates a session UUID.
6. Calls `store.CreateSession`.
7. Sets a `navalplan_session` HttpOnly cookie.
8. Redirects to home.


* `oauthLogout`: Deletes session from DB and clears cookie.


* **Middleware**:
* `requireAuth`: Checks for `navalplan_session` cookie -> Looks up Session -> Looks up Person -> Calls `context.AddPersonToContext`.




2. **Create `code/app/backend/server/handlers/person.go**`
* **Goal**: API for the frontend to get/update the current user.
* **Struct**: `PersonHandler{store, log}`.
* **Methods**:
* `Get(w, r)`: returns the `Person` found in context.
* `Update(w, r)`: Accepts JSON `{ "name": "..." }`, calls `store.UpdatePersonName`.





### **Phase 4: Server Wiring**

1. **Update `code/app/backend/server/server.go**`
* Initialize `PersonHandler`.
* Register Routes:
```go
// Auth Routes
r.Get("/auth/google/login", s.oauthGoogleLogin)
r.Get("/auth/google/callback", s.oauthGoogleCallback)
r.Get("/auth/logout", s.oauthLogout)

// Protected API Routes
r.Route("/api/v1/person", func(r chi.Router) {
    r.Use(s.requireAuth)
    r.Get("/", s.Handlers.Person.Get)
    r.Put("/", s.Handlers.Person.Update)
})

```





### **Phase 5: Frontend**

1. **Update `code/app/frontend/js/api.js**`
* Ensure `apiFetch` detects `401 Unauthorized` responses and updates the UI state (e.g., triggers a "logged out" view).


2. **Create `code/app/frontend/js/auth.js**`
* **Function**: `checkSession()`
* **Logic**: Call `GET /api/v1/person`.
* **Success**: Update UI to show "Logged in as [Name]" + Logout button.
* **Fail**: Show "Login" button (links to `/auth/google/login`).




3. **UI Updates**
* Call `checkSession()` in your main entry point.
* Add a simple Profile Modal or form to edit the name, calling `PUT /api/v1/person`.