package pkgmgr

import "sort"

func mergeCommands(groups ...[]Command) []Command {
	byID := map[string]Command{}
	for _, group := range groups {
		for _, cmd := range group {
			byID[cmd.ID] = cmd
		}
	}
	out := make([]Command, 0, len(byID))
	for _, cmd := range byID {
		out = append(out, cmd)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}
