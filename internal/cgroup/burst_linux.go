package cgroup

import (
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// burstCPU is the CPU time one burst aims at.
const burstCPU = 5 * time.Millisecond

var (
	calOnce sync.Once
	calN    int
)

func threadCPU() time.Duration {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &ts); err != nil {
		return 0
	}
	return time.Duration(ts.Nano())
}

var sink uint64

func spin(n int) {
	x := sink
	for i := 0; i < n; i++ {
		x = x*6364136223846793005 + 1442695040888963407
	}
	sink = x
}

// calibrate finds the loop count of about burstCPU of CPU time; CPU time,
// not wall time, so a busy host calibrates the same.
func calibrate() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	n := 100_000
	for i := 0; i < 20; i++ {
		c0 := threadCPU()
		spin(n)
		d := threadCPU() - c0
		if d >= burstCPU/2 {
			calN = int(float64(n) * float64(burstCPU) / float64(d))
			return
		}
		n *= 2
	}
	calN = n
}

// CanBurst reports a CPU clock per thread.
func CanBurst() bool { return threadCPU() > 0 }

// Burst runs the fixed burst once and returns wall time ÷ CPU time: 1.0 on
// an idle host, 3.0 when it got a third of the CPU it asked for.
func Burst() (float64, bool) {
	calOnce.Do(calibrate)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	w0, c0 := time.Now(), threadCPU()
	spin(calN)
	c, w := threadCPU()-c0, time.Since(w0)
	if c <= 0 {
		return 0, false
	}
	return max(1, float64(w)/float64(c)), true
}

// dieWithParent ends a probe child when fyrd ends.
func dieWithParent(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
}

func intoCgroup(cmd *exec.Cmd, f *os.File) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = int(f.Fd())
}
