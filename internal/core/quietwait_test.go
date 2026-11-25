package core

import "testing"

// V0.quiet-remote-wait: the shapes of the 25 Nov waits, and local waits that
// keep the short count.
func TestRemoteWait(t *testing.T) {
	remote := []string{
		"deploy-status --env production --wait 498ebcb9 2>&1 | tail -3",
		"cd /src/acme && timeout 5400 deploy-status --workload web --wait=252418bb 2>&1 | tail -5",
		"run-brief --commit 498ebcb9 --wait 2>&1 | tail -8",
		"timeout 900 gh run watch 45174876684 -R acme/infra --interval 60 >/dev/null 2>&1; gh run view 45174876684 --json status",
		"gh pr checks 12 --watch --interval 60",
		"kubectl -n acme wait --for=condition=complete --timeout=7000s job/collect 2>&1 | tail -2",
		"kubectl rollout status deploy/web -n acme",
		`timeout 1700 bash -c "until gcloud run services describe web --format='value(image)' | grep -q abc; do sleep 60; done"`,
		"while ! curl -fs https://example.com/healthz; do sleep 30; done",
	}
	for _, c := range remote {
		if !RemoteWait(c) {
			t.Errorf("not a remote wait: %s", c)
		}
	}
	local := []string{
		"until false; do sleep 15; done",
		"until [ -f out/done ]; do sleep 5; done",
		"while kill -0 1234; do sleep 10; done",
		"make test 2>&1 | tail -20",
		"gh pr view 12 --json state",
		"kubectl get pods -n acme",
		"npm run build -- --watch",
		"",
	}
	for _, c := range local {
		if RemoteWait(c) {
			t.Errorf("read as a remote wait: %s", c)
		}
	}
}
