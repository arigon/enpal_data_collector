package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	enpal "github.com/arigon/enpal_data_collector"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create persistent client
	client := enpal.NewClient("http://192.168.1.123")

	fmt.Println("Connecting to Enpal device...")
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	fmt.Println("✅ Connected! Fetching data every 30 seconds...")
	fmt.Println("   Press Ctrl+C to stop")

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Fetch immediately, then every 30 seconds
	fetchAndPrint(client)

	for {
		select {
		case <-ticker.C:
			fetchAndPrint(client)
		case <-sigChan:
			fmt.Println("\nShutting down...")
			return
		}
	}
}

func fetchAndPrint(client *enpal.Client) {
	_, data, err := client.FetchData()
	if err != nil {
		log.Printf("❌ Error: %v", err)
		return
	}

	fmt.Printf("[%s] ✅ %s - %d devices\n",
		time.Now().Format("15:04:05"),
		data.CollectionID[:8],
		len(data.DeviceCollections))
}
