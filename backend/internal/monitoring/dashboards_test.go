package monitoring

import (
	"encoding/json"
	"testing"
)

func TestDashboardJSONValid(t *testing.T) {
	for name, raw := range map[string]string{
		"odoo-apps":  odooAppsDashboardJSON("sintaksa"),
		"postgresql": postgresqlDashboardJSON("sintaksa"),
	} {
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			t.Fatalf("%s dashboard JSON invalid: %v", name, err)
		}
	}
}
