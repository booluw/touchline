package main

import (
	"log"
)

func main() {
	log.Println("Scheduler starting...")
	log.Printf("Tick cadences will be read from world_config table")
	log.Printf("Publishing WORLD_TICK events on configured schedule")

	// TODO: Initialize river client
	// TODO: Load tick cadences from world_config
	// TODO: Start robfig/cron scheduler
	// TODO: Publish WORLD_TICK events to event bus

	select {} // block forever
}
