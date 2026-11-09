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
	"strconv"
	"strings"
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
	dataDir := "data"
	os.MkdirAll(dataDir, 0755)

	if err := godotenv.Load(); err != nil {
		fmt.Println("Warning: .env file not found, using default environment")
	}

	store := storage.NewPageStorage()

	startLoad := time.Now()
	if err := store.Load(dataDir); err != nil {
		log.Printf("Failed to load shards: %v", err)
	}
	loadDuration := time.Since(startLoad)
	saveEvery := saveInterval(os.Getenv("SAVE_INTERVAL_SECONDS"))

	quit := make(chan struct{})
	store.StartFlusher(saveEvery, quit)

	port := os.Getenv("PORT")
	if port == "" {
		port = "1236"
	}
	addr := ":" + port

	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux, store)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Print(logo)
		fmt.Printf("\n [%s] STORAGE ENGINE STARTED", "L36")
		fmt.Printf("\n [%s] %d SHARDS INITIALIZED IN %v", "SYSTEM", 36, loadDuration)
		fmt.Printf("\n [%s] READY FOR HIGH LOAD", "STATUS")
		fmt.Printf("\n [%s] PORT: %s", "INFO", port)
		fmt.Printf("\n [%s] SAVE EVERY: %s\n\n", "INFO", saveEvery)
		fmt.Printf("🚀 L-36 Instance online at http://localhost%s\n", addr)

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Critical error: %v", err)
		}
	}()

	// Graceful Shutdown. In-flight requests finish before the last flush,
	// so a version accepted with 201 is not left only in memory.
	<-stop
	fmt.Println("\n🛑 Stop signal received. Terminating active sessions...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Forced shutdown error: %v", err)
	}

	fmt.Println("Finalizing shard logs...")
	start := time.Now()
	close(quit)
	store.FlushDone()
	fmt.Printf("Flush completed in %v\n", time.Since(start))

	fmt.Println("👋 L-36 secure shutdown complete. Standby.")
}

// saveInterval reads SAVE_INTERVAL_SECONDS. An empty value means 60 seconds.
func saveInterval(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Minute
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		log.Printf("SAVE_INTERVAL_SECONDS=%q is invalid, using 60", raw)
		return time.Minute
	}
	return time.Duration(seconds) * time.Second
}
