package collector

import "os/user"

func lookupAccount(uid string) string {
	account, err := user.LookupId(uid)
	if err != nil {
		return ""
	}
	return account.Username
}

// Resolve once per distinct account in a completed scan, including failed lookups.
func resolveAccountNames(apps []AppStat, lookup func(string) string) {
	names := make(map[string]string)
	for i := range apps {
		if process := apps[i].RootProcess; process != nil {
			resolveProcessAccount(process, names, lookup)
		}
		for j := range apps[i].Children {
			resolveProcessAccount(&apps[i].Children[j], names, lookup)
		}
	}
}

func resolveProcessAccount(process *ProcDetail, names map[string]string, lookup func(string) string) {
	if process.UID == "" {
		return
	}
	name, found := names[process.UID]
	if !found {
		name = lookup(process.UID)
		names[process.UID] = name
	}
	process.User = name
}
