// Command devseed is `make dev-seed` (docs/adr/0038 D7): until a login
// exists, it gives a developer what a login would — a person, a team, an
// admin membership and a token — in the database of `make postgres-up`. It
// writes through the test fixture over the administrative connection and is
// never part of the binary.
//
// Every run reuses the person and the team and creates a fresh token, which
// it prints once.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/guided-traffic/cowork/backend/internal/domain"
	"github.com/guided-traffic/cowork/backend/test/fixture"
)

const envDatabaseURL = "COWORK_DEV_SEED_DATABASE_URL"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "dev-seed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	username := flag.String("username", "dev", "the person's username")
	slug := flag.String("team", "dev", "the team's slug")
	agent := flag.Bool("agent", true, "create an agent token (scope write, every capability) instead of a plain admin-scope one")
	flag.Parse()

	databaseURL := os.Getenv(envDatabaseURL)
	if databaseURL == "" {
		return fmt.Errorf("%s is not set: `make dev-seed` sets it to the administrative URL of `make postgres-up`", envDatabaseURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f, err := fixture.Connect(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer f.Close()

	person, err := f.Person(ctx, *username, "Developer")
	if err != nil {
		return err
	}
	team, err := f.Tenant(ctx, *slug, "Development")
	if err != nil {
		return err
	}
	if err := f.Member(ctx, team, person, domain.RoleAdmin); err != nil {
		return err
	}
	spec := fixture.TokenSpec{UserID: person, Name: "dev-seed", Scope: domain.ScopeAdmin}
	if *agent {
		spec.Scope, spec.Agent = domain.ScopeWrite, true
	}
	token, _, err := f.Token(ctx, spec)
	if err != nil {
		return err
	}
	fmt.Printf("person:  local:%s (admin of %s)\n", *username, *slug)
	fmt.Printf("team:    %s\n", *slug)
	fmt.Printf("token:   %s\n", token)
	fmt.Printf("scope:   %s, agent: %t, expires in 90 days\n", spec.Scope, spec.Agent)
	return nil
}
