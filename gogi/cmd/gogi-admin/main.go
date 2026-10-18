// gogi-admin is how the operator of a gogi deployment registers the applications that use it and
// manages their keys. It works on gogi's database, DATABASE_URL, e.g. in the gateway's container:
//
//	docker compose exec gateway /root/gogi-admin register qubit-notes
package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"gogi/gogi/applications"
	"gogi/gogi/storage/postgres"
	"gogi/gogi/utils"
)

const usage = `usage: gogi-admin <command>

  register <application>            register an application and print its first key
  list                              list the applications and their keys
  new-key <application>             give an application another key and print it
  revoke-key <application> <key>    stop a key working; the key, or what list shows of it
  disable <application>             stop all of an application's keys working
  enable <application>              let a disabled application's keys work again
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gogi-admin:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	pool, err := postgres.NewPool(utils.GetDatabaseURL())
	if err != nil {
		return err
	}
	defer pool.Close()
	repo := applications.NewRepository(pool)
	ctx := context.Background()

	need := func(n int) error {
		if len(args) != n+1 {
			return fmt.Errorf("%s needs %d argument(s)\n\n%s", args[0], n, usage)
		}
		return nil
	}

	switch args[0] {
	case "register":
		if err := need(1); err != nil {
			return err
		}
		key, err := repo.Register(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf("Registered %s. Its key, shown only now:\n%s\n", args[1], key)
	case "new-key":
		if err := need(1); err != nil {
			return err
		}
		key, err := repo.NewKey(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf("A new key of %s, shown only now; its other keys work until they are revoked:\n%s\n", args[1], key)
	case "revoke-key":
		if err := need(2); err != nil {
			return err
		}
		if err := repo.RevokeKey(ctx, args[1], args[2]); err != nil {
			return err
		}
		fmt.Printf("Revoked the key %s… of %s\n", applications.Shown(args[2]), args[1])
	case "disable", "enable":
		if err := need(1); err != nil {
			return err
		}
		if err := repo.SetDisabled(ctx, args[1], args[0] == "disable"); err != nil {
			return err
		}
		fmt.Printf("%s is %sd\n", args[1], args[0])
	case "list":
		return list(ctx, repo)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
	return nil
}

func list(ctx context.Context, repo *applications.Repository) error {
	listed, err := repo.List(ctx)
	if err != nil {
		return err
	}
	out := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(out, "APPLICATION\tSTATE\tKEY\tCREATED\tREVOKED")
	for _, application := range listed {
		state := "enabled"
		if application.Disabled {
			state = "disabled"
		}
		if len(application.Keys) == 0 {
			fmt.Fprintf(out, "%s\t%s\t-\t-\t-\n", application.Name, state)
		}
		for _, key := range application.Keys {
			revoked := "-"
			if key.RevokedAt != nil {
				revoked = key.RevokedAt.Format(time.RFC3339)
			}
			fmt.Fprintf(out, "%s\t%s\t%s…\t%s\t%s\n", application.Name, state, key.Shown, key.CreatedAt.Format(time.RFC3339), revoked)
		}
	}
	return out.Flush()
}
