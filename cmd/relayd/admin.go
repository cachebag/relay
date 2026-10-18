package main

import (
	"context"
	"fmt"
	"io"

	"github.com/cachebag/relay/internal/store"
)

// ensureAdminToken makes sure relayd has a server admin token, the secret
// needed to create or list pools. On a fresh relay.db it makes one, stores
// only its hash, and prints the token once with a warning. With force it
// replaces whatever is there, which is how a lost token is recovered. It
// returns whether a new token was made; an existing token is left alone and
// nothing is printed.
func ensureAdminToken(ctx context.Context, srv store.ServerStore, out io.Writer, force bool) (bool, error) {
	hash, err := srv.AdminTokenHash(ctx)
	if err != nil {
		return false, err
	}
	if hash != "" && !force {
		return false, nil
	}

	token := store.NewToken()
	if err := srv.SetAdminTokenHash(ctx, store.HashToken(token)); err != nil {
		return false, err
	}

	fmt.Fprintf(out, "server admin token: %s\n", token)
	fmt.Fprintln(out, "warning: the server admin token is shown only once. Save it now.")
	fmt.Fprintln(out, "it is needed to create pools: relay pool create <name> --admin-token <token>")
	fmt.Fprintln(out, "lost it? run: relayd --new-admin-token")
	return true, nil
}
