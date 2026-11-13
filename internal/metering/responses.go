package metering

import "github.com/superduck-ai/open-managed-agents/internal/billing"

func (o *observation) responseFrame(data []byte) {
	if o.terminal || o.failed || o.complete {
		return
	}
	response, raw, terminal, err := billing.ParseResponse(data, o.streaming)
	if err != nil {
		o.failed = true
		return
	}
	if !terminal {
		return
	}
	o.cost = o.rule.Extract(raw)
	usage, valid := response.Usage.BillingUsage()
	if !valid {
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
