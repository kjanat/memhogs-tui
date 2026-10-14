package collector

// Native enumeration supplies parent PIDs and creation times. Build ownership
// from the complete enumeration before applying memory eligibility thresholds.
func groupNativeProcesses(processes map[int]*collectedProcess, totalKB int64, mode Grouping) []AppStat {
	for _, process := range processes {
		p := &process.detail
		if p.Executable == "" {
			p.ExecutableID.Path = process.name // explicit executable mode retains native name grouping
		}
		p.Context = &ProcessContext{
			Application: process.name, ApplicationPID: p.PID,
			ApplicationStart: p.StartTime, ApplicationSource: "native process record",
			ProjectDir: p.Cwd, ProjectSource: currentDirectorySource,
		}
		p.Context.Ancestors = processAncestors(*p, processes)
		if len(p.Context.Ancestors) > 0 {
			parent := p.Context.Ancestors[0]
			p.Context.Origin, p.Context.OriginPID = parent.Name, parent.PID
		}
	}
	return groupProcesses(processes, totalKB, mode)
}
