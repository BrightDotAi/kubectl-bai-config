package authenticated

import (
	"context"
	"net/http"

	"github.com/spacelift-io/spacectl/client"
	"github.com/spacelift-io/spacectl/client/session"
)

// Client is the authenticated client that can be used by all CLI commands.
var Client client.Client

// Ensure initializes the Spacelift client using the provided credentials.
func Ensure(creds session.StoredCredentials) error {
	// Create a new HTTP client
	httpClient := &http.Client{}

	// Create a new session using the credentials
	sess, err := creds.Session(context.Background(), httpClient)
	if err != nil {
		return err
	}

	// Initialize the Spacelift client
	Client = client.New(httpClient, sess)
	return nil
}
