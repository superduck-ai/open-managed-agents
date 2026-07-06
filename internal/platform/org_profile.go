package platform

type OrganizationPhysicalAddress struct {
	Line1      string  `json:"line1"`
	Line2      *string `json:"line2"`
	City       string  `json:"city"`
	State      string  `json:"state"`
	Country    string  `json:"country"`
	PostalCode string  `json:"postal_code"`
}

type OrganizationTaxID struct {
	Type    string  `json:"type"`
	Value   string  `json:"value"`
	Country *string `json:"country"`
}

type OrganizationProfile struct {
	PhysicalAddress *OrganizationPhysicalAddress `json:"physical_address"`
	Website         *string                      `json:"website"`
	Industry        *string                      `json:"industry"`
	TaxID           *OrganizationTaxID           `json:"tax_id"`
	BillTo          *string                      `json:"bill_to"`
}
