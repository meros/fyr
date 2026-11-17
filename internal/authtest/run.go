package authtest

import "testing"

// Run is for a package's TestMain: it runs the tests and removes the
// binaries the harness built.
//
//	func TestMain(m *testing.M) { os.Exit(authtest.Run(m)) }
func Run(m *testing.M) int {
	defer Cleanup()
	return m.Run()
}
