package main

import (
	"context"
	"errors"
	"fmt"
	"l36/internal/storage"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"l36/internal/handlers"

	"github.com/joho/godotenv"
)

const logo = `
  _         	         ________      ________
 | |        	        |_____   |    |  ______|
 | |         ________        /  /     | |______
 | |        |________|      |_  \     |  ____  |
 | |______               _____\  \    | |____| |
 |________|             |_________|   |________|`

func main() {
	os.MkdirAll("data", 0755)

	if err := godotenv.Load(); err != nil {
		fmt.Println("Warning: .env file not found, using default environment")
	}

	store := storage.GPageStorage
	dbPath := "data/l36.json"

	if err := store.Load(dbPath); err != nil {
		log.Printf("Failed to load data: %v", err)
	}

	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		for range ticker.C {
			if err := store.Dump(dbPath); err != nil {
				log.Printf("Auto-save failed: %v", err)
			}
		}
	}()

	port := os.Getenv("PORT")
	if port == "" {
		port = "1236"
	}
	addr := ":" + port

	mux := http.NewServeMux()

	handlers.RegisterRoutes(mux)

	wrappedHandler := handlers.Logger(mux)

	srv := &http.Server{
		Addr:    addr,
		Handler: wrappedHandler,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Print(logo)
		fmt.Printf("\n [%s] STORAGE ENGINE STARTED", "L36")
		fmt.Printf("\n [%s] %d SHARDS INITIALIZED", "SYSTEM", 36)
		fmt.Printf("\n [%s] READY FOR HIGH LOAD", "STATUS")
		fmt.Printf("\n [%s] PORT: %s\n\n", "INFO", port)
		fmt.Printf("🚀 L-36 Instance online at http://localhost%s\n", addr)

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Critical error: %v", err)
		}
	}()

	<-stop
	fmt.Println("\n🛑 Stop signal received. Terminating active sessions...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Forced shutdown error: %v", err)
	}

	fmt.Println("Finalizing data dump...")
	if err := store.Dump(dbPath); err != nil {
		log.Printf("Final dump failed: %v", err)
	}

	fmt.Println("👋 L-36 secure shutdown complete. Standby.")
}
