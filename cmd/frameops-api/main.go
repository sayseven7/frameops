package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/httpapi"
	"github.com/sayseven7/frameops/internal/store/objectstore"
)

func main() {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "FRAMEOPS_DATABASE_URL must be set explicitly")
		os.Exit(1)
	}
	evidence, err := objectstore.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := evidence.EnsureBucket(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()
	address := os.Getenv("FRAMEOPS_HTTP_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	if err := http.ListenAndServe(address, httpapi.New(pool, evidence)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
