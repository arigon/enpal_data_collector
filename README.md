# Enpal Data Collector

A Go library to fetch real-time data from Enpal solar energy devices via their Blazor WebSocket interface.

## Installation

```bash
go get github.com/arigon/enpal_data_collector
```

## Usage

### One-Shot Fetch

For single data retrieval (connects, fetches, disconnects):

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

rawJSON, data, err := enpal.FetchCollectorData(ctx, "http://192.168.1.123")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Devices: %d\n", len(data.DeviceCollections))
```

### Persistent Connection

For periodic data collection (recommended for intervals < 5 minutes):

```go
client := enpal.NewClient("http://192.168.1.123")

if err := client.Connect(ctx); err != nil {
    log.Fatal(err)
}
defer client.Close()

for {
    rawJSON, data, err := client.FetchData()
    if err != nil {
        log.Printf("Error: %v", err)
        break
    }
    
    // Process data...
    fmt.Printf("Collection: %s - %d devices\n", data.CollectionID, len(data.DeviceCollections))
    
    time.Sleep(30 * time.Second)
}
```

## API

### Functions

- `FetchCollectorData(ctx, baseURL)` - One-shot fetch (connects, gets data, disconnects)

### Client Methods

- `NewClient(baseURL)` - Create a new client
- `Connect(ctx)` - Establish WebSocket connection
- `FetchData()` - Fetch data (can be called multiple times)
- `Close()` - Close connection
- `IsConnected()` - Check connection status

## Data Structures

### CollectorData

```go
type CollectorData struct {
    CollectionID      string
    IoTDeviceID       string
    TimeStampUtc      string
    NumberDataPoints  map[string]DataPoint
    TextDataPoints    map[string]DataPoint
    DeviceCollections []DeviceCollection
    EnergyManagement  []EnergyManagement
    ErrorCodes        []ErrorCode
}
```

### DeviceCollection

Contains data from individual devices (inverters, batteries, wallboxes):

```go
type DeviceCollection struct {
    DeviceID         string
    IngestionKey     string
    DeviceClass      string  // "Inverter", "Battery", "Wallbox", etc.
    TimeStampUtc     string
    NumberDataPoints map[string]DataPoint
    TextDataPoints   map[string]DataPoint
}
```

### DataPoint

```go
type DataPoint struct {
    TimeStampUtcOfMeasurement string
    Unit                      string
    Value                     any  // float64 or string
}
```

## Finding Your Device IP

Your Enpal device should be accessible on your local network. Check your router's DHCP client list or use:

```bash
# Scan local network
nmap -sn 192.168.1.0/24
```

The device typically exposes a web interface at `http://<IP>/collector`.

## Example

See [example/main.go](./example/main.go) for a complete periodic collection example.

```bash
cd example
go run main.go
```

## Dependencies

- [gorilla/websocket](https://github.com/gorilla/websocket)
- [vmihailenco/msgpack](https://github.com/vmihailenco/msgpack)
