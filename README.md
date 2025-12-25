# Enpal Data Collector

A Go library to fetch real-time data from Enpal solar energy devices via their Blazor WebSocket interface.

## Installation

```bash
go get github.com/arigon/enpal_data_collector
```

## Usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    enpal "github.com/arigon/enpal_data_collector"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Replace with your Enpal device IP
    rawJSON, data, err := enpal.FetchCollectorData(ctx, "http://192.168.1.123")
    if err != nil {
        log.Fatal(err)
    }

    if data != nil {
        fmt.Printf("Collection ID: %s\n", data.CollectionID)
        fmt.Printf("Timestamp: %s\n", data.TimeStampUtc)
        
        for key, dp := range data.NumberDataPoints {
            fmt.Printf("%s: %.2f %s\n", key, dp.Value, dp.Unit)
        }
    }
}
```

## Finding Your Device IP

Your Enpal device should be accessible on your local network. Check your router's DHCP client list or use network scanning tools to find it.

## Data Structures

### CollectorData

Main response containing:

- `CollectionID` - Unique collection identifier
- `IoTDeviceID` - Device identifier  
- `TimeStampUtc` - Timestamp of data collection
- `NumberDataPoints` - Map of numeric measurements (power, voltage, etc.)
- `TextDataPoints` - Map of text values
- `DeviceCollections` - Data from individual devices (inverters, batteries)
- `EnergyManagement` - Energy management system data
- `ErrorCodes` - Any active error codes

### DataPoint

```go
type DataPoint struct {
    TimeStampUtcOfMeasurement string
    Unit                      string
    Value                     any    // Can be float64 or string
}
```

## Example

See the [example](./example/main.go) for a complete usage example.

```bash
# Run the example
cd example
go run main.go
```

## Dependencies

- [gorilla/websocket](https://github.com/gorilla/websocket) - WebSocket client
- [vmihailenco/msgpack](https://github.com/vmihailenco/msgpack) - MessagePack encoding
