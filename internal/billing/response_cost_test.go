package billing

import "testing"

func TestResponseCostExtraction(t *testing.T) {
	rule := &ResponseCostRule{AmountPointer: "/gateway/cost/amount", StreamEvent: "message_stop", Currency: "CNY", ExchangeRate: "7.2"}
	if err := rule.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		amount string
		cents  int64
		valid  bool
	}{
		{`"7.2"`, 100, true}, {`"0.036"`, 1, true}, {`"0.003700000000"`, 0, true}, {`"0"`, 0, true},
		{`"-1"`, 0, false}, {`"NaN"`, 0, false}, {`"1e3"`, 0, false}, {`null`, 0, false}, {`1`, 0, false}, {`"1/2"`, 0, false},
	} {
		t.Run(tc.amount, func(t *testing.T) {
			cost := rule.Extract([]byte(`{"gateway":{"cost":{"amount":` + tc.amount + `}}}`))
			if (cost != nil) != tc.valid {
				t.Fatalf("费用有效性不正确：%+v", cost)
			}
			if cost != nil && cost.USDCents != tc.cents {
				t.Fatalf("换算错误：%+v", cost)
			}
		})
	}
	if rule.Extract([]byte(`{}`)) != nil {
		t.Fatal("缺失金额不能视为零")
	}
	for _, rate := range []string{"0", "-1", "NaN", "1/2"} {
		rule.ExchangeRate = rate
		if rule.Validate() == nil {
			t.Fatal("接受非法汇率")
		}
	}
}

func TestResponseCostConfigurationAndScope(t *testing.T) {
	valid := ResponseCostRule{AmountPointer: "/cost", StreamEvent: "message_stop", Currency: "USD", ExchangeRate: "1"}
	for _, change := range []func(*ResponseCostRule){
		func(r *ResponseCostRule) { r.Currency = "" },
		func(r *ResponseCostRule) { r.AmountPointer = "cost" },
		func(r *ResponseCostRule) { r.StreamEvent = "message_delta" },
		func(r *ResponseCostRule) { r.ExchangeRate = "7.2" },
	} {
		r := valid
		change(&r)
		if r.Validate() == nil {
			t.Fatalf("接受无效规则：%+v", r)
		}
	}
	calculator := NewCalculator(nil)
	calculator.ResponseCosts = map[string]ResponseCostRule{"test": valid}
	if calculator.ResponseCost("other/model") != nil || calculator.ResponseCost("model") != nil {
		t.Fatal("规则跨供应商生效")
	}
	rule := calculator.ResponseCost("test/model")
	rule.Currency = "CNY"
	if calculator.ResponseCost("test/model").Currency != "USD" {
		t.Fatal("规则快照被修改")
	}
}
