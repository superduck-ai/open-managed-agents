package metering

import "github.com/superduck-ai/open-managed-agents/internal/billing"

func (o *observation) responseFrame(data []byte) {
	if o.terminal || o.failed || o.complete {
		return
	}
	response, _, terminal, err := billing.ParseResponse(data, o.streaming)
	if err != nil {
		o.failed = true
		return
	}
	if !terminal {
		return
	}
	usage, valid := response.Usage.BillingUsage()
	if !valid || (response.Status == "completed" && !usage.Complete()) {
		o.failed = true
		return
	}
	o.usage = usage
	o.complete = response.Status == "completed"
	if response.Status == "failed" {
		o.failed = true
	}
	o.terminal = true
}
