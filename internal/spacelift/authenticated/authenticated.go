package authenticated

import (
	"github.com/spacelift-io/spacectl/client"
	"github.com/spacelift-io/spacectl/client/session"
)

// Client is the authenticated client that can be used by all CLI commands.
var Client client.Client

// Ensure is a way of ensuring that the Client exists, and it meant to be used
// as a Before action for commands that need it.
func Ensure(creds session.StoredCredentials) error {
	ctx, httpClient := session.Defaults()

	session, err := creds.Session(ctx, httpClient)
	if err != nil {
		return err
	}

	Client = client.New(httpClient, session)

	return nil
}
