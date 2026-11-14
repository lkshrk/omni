package app

import (
	"regexp"
	"sort"
)

var envReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func (a *App) AgentsEnv() (map[string][]string, error) {
	lock, err := readAPMLockfile()
	if err != nil {
		return nil, err
	}
	result := map[string][]string{}
	for client, servers := range lock.MCPTargetServers {
		names := map[string]bool{}
		for _, server := range servers {
			cfg := lock.MCPConfigs[server]
			values := append([]string{}, cfg.Args...)
			for _, v := range cfg.Env {
				values = append(values, v)
			}
			for _, v := range cfg.Headers {
				values = append(values, v)
			}
			for _, v := range values {
				for _, m := range envReference.FindAllStringSubmatch(v, -1) {
					names[m[1]] = true
				}
			}
		}
		if len(names) == 0 {
			continue
		}
		sorted := make([]string, 0, len(names))
		for n := range names {
			sorted = append(sorted, n)
		}
		sort.Strings(sorted)
		result[client] = sorted
	}
	return result, nil
}
