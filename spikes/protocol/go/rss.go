package main

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RSS sampler: sums VmRSS of a pid and all descendants, tracks max per phase label.
type RSS struct {
	mu    sync.Mutex
	phase string
	max   map[string]int
	last  map[string]int
	order []string
}

func NewRSS() *RSS { return &RSS{max: map[string]int{}, last: map[string]int{}} }

func (r *RSS) Phase(p string) {
	r.mu.Lock()
	r.phase = p
	if _, ok := r.max[p]; !ok {
		r.order = append(r.order, p)
	}
	r.mu.Unlock()
}

func (r *RSS) Run(pid int, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-time.After(100 * time.Millisecond):
		}
		kb := treeRSS(pid)
		r.mu.Lock()
		if kb > r.max[r.phase] {
			r.max[r.phase] = kb
		}
		r.last[r.phase] = kb
		r.mu.Unlock()
	}
}

func vmRSS(pid int) int {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmRSS:") {
			f := strings.Fields(l)
			n, _ := strconv.Atoi(f[1])
			return n
		}
	}
	return 0
}

func children() map[int][]int {
	m := map[int][]int{}
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		s := string(b)
		i := strings.LastIndexByte(s, ')')
		f := strings.Fields(s[i+1:])
		pp, _ := strconv.Atoi(f[1])
		m[pp] = append(m[pp], pid)
	}
	return m
}

func treeRSS(pid int) int {
	ch := children()
	total := 0
	var walk func(int)
	walk = func(p int) {
		total += vmRSS(p)
		for _, c := range ch[p] {
			walk(c)
		}
	}
	walk(pid)
	return total
}
