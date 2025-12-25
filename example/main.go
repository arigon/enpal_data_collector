package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	enpal "github.com/arigon/enpal_data_collector"
)

func main() {
	baseURL := "http://192.168.1.123" // Replace with your Enpal device IP

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("Connecting to Enpal device at %s...\n", baseURL)

	rawJSON, data, err := enpal.FetchCollectorData(ctx, baseURL)
	if err != nil {
		log.Fatalf("Failed to fetch data: %v", err)
	}

	if data != nil {
		fmt.Printf("\n=== Collector Data ===\n")
		fmt.Printf("Collection ID: %s\n", data.CollectionID)
		fmt.Printf("IoT Device ID: %s\n", data.IoTDeviceID)
		fmt.Printf("Timestamp: %s\n", data.TimeStampUtc)

		// Print number data points
		if len(data.NumberDataPoints) > 0 {
			fmt.Printf("\n--- Number Data Points ---\n")
			for key, dp := range data.NumberDataPoints {
				fmt.Printf("  %s: %v %s\n", key, dp.Value, dp.Unit)
			}
		}

		// Print device collections
		if len(data.DeviceCollections) > 0 {
			fmt.Printf("\n--- Device Collections ---\n")
			for _, dc := range data.DeviceCollections {
				fmt.Printf("  Device: %s (%s)\n", dc.DeviceID, dc.DeviceClass)
				for key, dp := range dc.NumberDataPoints {
					fmt.Printf("    %s: %v %s\n", key, dp.Value, dp.Unit)
				}
			}
		}

		// Print energy management data
		if len(data.EnergyManagement) > 0 {
			fmt.Printf("\n--- Energy Management ---\n")
			for _, em := range data.EnergyManagement {
				fmt.Printf("  ID: %s\n", em.EnergyManagementID)
				for key, dp := range em.NumberDataPoints {
					fmt.Printf("    %s: %v %s\n", key, dp.Value, dp.Unit)
				}
			}
		}
	} else {
		// Print raw JSON if parsing failed
		fmt.Println("\nRaw JSON response:")
		var prettyJSON map[string]interface{}
		if err := json.Unmarshal([]byte(rawJSON), &prettyJSON); err == nil {
			formatted, _ := json.MarshalIndent(prettyJSON, "", "  ")
			fmt.Println(string(formatted))
		} else {
			fmt.Println(rawJSON)
		}
	}
}
