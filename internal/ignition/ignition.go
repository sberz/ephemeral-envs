package ignition

import "context"

type TriggerRequest struct {
	Environment string
	Namespace   string
}

type Provider interface {
	Trigger(ctx context.Context, req TriggerRequest) error
}
