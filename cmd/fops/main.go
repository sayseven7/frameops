package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/cli"
	"github.com/sayseven7/frameops/internal/store/postgres"
)

// main keeps the only direct-database command in this file, apart from every
// other command: `bootstrap-first-admin` is the product's single documented
// exception to the API being the entry point, and `internal/cli` therefore
// carries no database dependency at all.
func main() {
	if len(os.Args) > 1 && os.Args[1] == "bootstrap-first-admin" {
		bootstrapFirstAdmin(os.Args[2:])
		return
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}

func bootstrapFirstAdmin(args []string) {
	flags := flag.NewFlagSet("bootstrap-first-admin", flag.ExitOnError)
	tokenFile := flags.String("token-file", "", "owner-only bootstrap token file")
	passwordFile := flags.String("password-file", "", "local password file")
	organization := flags.String("organization", "", "organization name")
	name := flags.String("name", "", "admin display name")
	email := flags.String("email", "", "admin email")
	_ = flags.Parse(args)
	password, err := os.ReadFile(*passwordFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read password file:", err)
		os.Exit(1)
	}
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "FRAMEOPS_DATABASE_URL must be set explicitly")
		os.Exit(1)
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()
	if _, err := postgres.BootstrapFirstAdmin(context.Background(), pool, postgres.BootstrapInput{OrganizationName: *organization, DisplayName: *name, Email: *email, Password: strings.TrimSpace(string(password)), TokenFile: *tokenFile}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
