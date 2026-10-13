package main

import "testing"

func TestVersionTemplate(t *testing.T) {
	originalVersion, originalCommit, originalBuildTime := Version, Commit, BuildTime
	t.Cleanup(func() {
		Version, Commit, BuildTime = originalVersion, originalCommit, originalBuildTime
	})

	Version = "v1.2.3"
	Commit = "abc1234"
	BuildTime = "2026-10-13T12:34:56Z"

	const want = "quark version v1.2.3\ncommit abc1234\nbuilt 2026-10-13T12:34:56Z\n"
	if got := versionTemplate(); got != want {
		t.Fatalf("versionTemplate() = %q, want %q", got, want)
	}
}
