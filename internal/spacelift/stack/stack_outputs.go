package stack

import (
	"context"

	"github.com/BrightDotAi/kubectl-bai-config/internal/spacelift/authenticated"
	"github.com/pkg/errors"
)

type StackFragment struct {
	ID      string   `graphql:"id" json:"id,omitempty"`
	Labels  []string `graphql:"labels" json:"labels,omitempty"`
	Outputs []struct {
		ID    string `graphql:"id" json:"id,omitempty"`
		Value string `graphql:"value" json:"value,omitempty"`
	} `graphql:"outputs" json:"outputs,omitempty"`
}

type StackOutputsQuery struct {
	Stacks []StackFragment `graphql:"stacks" json:"stacks,omitempty"`
}

func GetStackOutputs() (*StackOutputsQuery, error) {
	var query StackOutputsQuery
	if err := authenticated.Client.Query(context.TODO(), &query, nil); err != nil {
		return nil, errors.Wrapf(err, "failed to query for stack outputs")
	}

	return &query, nil
}
