This is a great pivot. Hardcoding geographic bounds into a single function becomes unmanageable quickly. To support the NOAA client, "tpryan/uktidal", "tpryan/niwago", and any future regions dictated by your "tpryan/navalplan", we need to shift to a **Strategy/Registry Pattern**. 

Instead of one monolithic tool trying to manage everything, we will create a standard interface that each regional provider implements. A central manager will then evaluate the coordinates and hand off the request to the correct provider.

Here is the amended, scalable plan:

### Step 1: Define a Common Interface
In `tools/tides.go` (or a new `tides` sub-package if it gets large), define an interface that all regional tide clients must implement.

1. **`RegionalTideProvider` Interface**:
   ```go
   type RegionalTideProvider interface {
       // CanHandle quickly determines if the coordinate falls within the provider's supported region
       CanHandle(lat, lng float64) bool 
       
       // GetTides executes the actual fetch logic
       GetTides(lat, lng float64, dateStr string) ([]TideEvent, error)
   }
   ```

### Step 2: Configuration and Dependencies
You will need to pass the keys for both the UK and New Zealand services.

1. **`.env` and `config.go`**:
   Add placeholders and parsing logic for the new APIs.
   ```env
   NAVALPLAN_TIDAL_UKTIDAL_API_KEY=your-uk-tidal-key
   NAVALPLAN_TIDAL_NIWA_API_KEY=your-niwa-key
   ```
2. **Go Modules**:
   Fetch the new dependencies.
   ```bash
   go get github.com/tpryan/uktidal
   go get github.com/tpryan/niwago
   go mod tidy
   ```

### Step 3: Implement the Regional Providers
Create separate structs for each service that implement your `RegionalTideProvider` interface. 

1. **`NOAAProvider`**:
   * Wrap the existing NOAA logic.
   * **`CanHandle`**: Return `true` for North American bounding boxes, or make this the ultimate fallback if no other provider claims the coordinates.

2. **`UKProvider` (using "tpryan/uktidal")**:
   * Initialize with the `uktidal` client.
   * **`CanHandle`**: Implement a geographic bounding box check for the UK/Ireland.
   * **`GetTides`**: Implement the spatial search for the nearest station and fetch the 7-day events, as outlined in the previous plan.

3. **`NIWAProvider` (using "tpryan/niwago")**:
   * Initialize with the `niwago` client.
   * **`CanHandle`**: Implement a geographic bounding box check for New Zealand (roughly `Lat: -47.5 to -34.0`, `Lng: 165.0 to 179.0`).
   * **`GetTides`**: Implement station discovery and event fetching according to the `niwago` SDK's methods, mapping the results back to the standard `[]TideEvent` slice.

### Step 4: Build the Provider Registry
Update the main `TideProvider` (which acts as the LLM tool) to act as a manager for these regional providers.

1. **Update the Struct**:
   ```go
   type TideManager struct {
       providers []RegionalTideProvider
   }
   ```
2. **Initialize with Providers**:
   ```go
   func NewTideManager(noaaClient NOAAClient, ukKey, niwaKey string) *TideManager {
       return &TideManager{
           providers: []RegionalTideProvider{
               NewUKProvider(ukKey),
               NewNIWAProvider(niwaKey),
               NewNOAAProvider(noaaClient), // Evaluated last as a fallback
           },
       }
   }
   ```
3. **Refactor the Tool Execution**:
   When the LLM calls the tool with coordinates, iterate through the registry.
   ```go
   func (tm *TideManager) GetTides(args TideArgs) ([]TideEvent, error) {
       for _, p := range tm.providers {
           if p.CanHandle(args.Latitude, args.Longitude) {
               return p.GetTides(args.Latitude, args.Longitude, args.Date)
           }
       }
       return nil, fmt.Errorf("no tidal data provider supports the coordinates: %f, %f", args.Latitude, args.Longitude)
   }
   ```

### Step 5: Wire it Together in `main.go`
Update your service initialization to pass all required API keys to the new `TideManager`. Update the tool's description provided to the LLM so it knows it has global capabilities: `"Retrieves high and low tide predictions for a specific date. Currently supports North America, the UK, and New Zealand."`

### Step 6: Testing
With the Strategy pattern, testing becomes much easier.
1. **Unit Test `CanHandle`**: Test each provider's bounding box logic with a series of in-bounds and out-of-bounds coordinates.
2. **Unit Test `GetTides`**: Mock the underlying HTTP clients for "tpryan/uktidal" and "tpryan/niwago" to ensure the data maps correctly to the `TideEvent` struct.
3. **Manager Test**: Pass various global coordinates to `TideManager` and assert that the correct underlying provider was invoked.
```