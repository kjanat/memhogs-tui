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
		for j := range apps[i].Children {
			process := &apps[i].Children[j]
			if process.UID == "" {
				continue
			}
			name, found := names[process.UID]
			if !found {
				name = lookup(process.UID)
				names[process.UID] = name
			}
			process.User = name
		}
	}
}
