package tui

import (
	"strings"
	"testing"

	"memhogs.kjanat.dev/internal/collector"
)

func TestAmbiguousApplicationGroupRequiresIndividualSelection(t *testing.T) {
	app := instanceApp("electron", 10, 1, 8192, 10, 11)
	app.Children[1].Context.ApplicationPID = 20 // another application was mixed in
	m := testModel(app)
	m = m.beginKill(false)
	require(t, !m.killConfirm && strings.Contains(m.status, "ambiguous"), "mixed application accepted for bulk termination")
	m.expanded[app.GroupID] = true
	m.cursor = 2
	m = m.beginKill(false)
	require(t, m.killConfirm && m.killTarget.Child.PID == 11, "individual PID termination must remain available")
}

func TestExplicitBroadExecutableScopeIsDisclosed(t *testing.T) {
	app := instanceApp("electron", 10, 1, 8192, 10, 11)
	app.Children[1].Context.ApplicationPID = 20
	app.GroupID = testGroupID("/usr/bin/electron")
	app.Root = collector.ProcessIdentity{}
	m := testModel(app).beginKill(false)
	modal := m.viewKillModal()
	require(t, m.killConfirm && strings.Contains(modal, "Grouping: executable") && strings.Contains(modal, "all listed instances"), "explicit broad mode not disclosed")
	require(t, strings.Contains(modal, "Procs:   2") && strings.Contains(modal, "kill -TERM 10 11"), "captured count or PID list incorrect")
}

func TestTreeKillRequiresRootInVerifiedAncestorChain(t *testing.T) {
	app := instanceApp("zed-editor", 10, 1, 8192, 10, 11)
	app.GroupID.Mode = collector.GroupTree
	app.Children[1].Context.Ancestors = []collector.ProcessAncestor{{PID: 10, StartTime: 2}}
	require(t, !testModel(app).beginKill(false).killConfirm, "reused tree root accepted")
	app.Children[1].Context.Ancestors[0].StartTime = 1
	m := testModel(app).beginKill(false)
	require(t, m.killConfirm && strings.Contains(m.viewKillModal(), "Grouping: tree"), "verified tree scope rejected")
	app.GroupID.Root.StartTime++
	require(t, !testModel(app).beginKill(false).killConfirm, "mismatched typed and displayed root accepted")
}
