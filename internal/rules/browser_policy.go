package rules

import (
	"sort"

	"github.com/takets/tcc-local-connector/internal/constants"
)

func BuildBrowserPolicy(evaluation Evaluation) []string {
	unique := map[string]struct{}{}
	for _, id := range sortedRuleIDs(evaluation.ActiveRuleIDs) {
		if !evaluation.ActiveRuleIDs[id] {
			continue
		}
		for _, action := range evaluation.Rules[id].Ensure {
			if action.Type != constants.BrowserBlockActionType {
				continue
			}
			for _, domain := range action.Domains {
				unique[domain] = struct{}{}
			}
		}
	}

	domains := make([]string, 0, len(unique))
	for domain := range unique {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}
