package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

var common = []string{"--model", "haiku", "--setting-sources=", "--strict-mcp-config",
	"--permission-prompt-tool", "stdio", "--permission-mode", "default", "--tools", "Bash,AskUserQuestion"}

func must(err error) {
	if err != nil {
		fmt.Println("FATAL:", err)
		os.Exit(1)
	}
}

func ms(d time.Duration) string { return fmt.Sprintf("%dms", d.Milliseconds()) }

func main() {
	log, _ := os.Create(os.Args[1] + ".log")
	defer log.Close()
	switch os.Args[1] {
	case "main":
		runMain(log)
	case "resume":
		runResume(log, os.Args[2])
	case "ask":
		runAsk(log)
	}
}

func print(m Msg) { fmt.Println("   ", Summ(m)) }

func runMain(log *os.File) {
	t0 := time.Now()
	c, err := Start(log, common...)
	must(err)
	rss := NewRSS()
	stop := make(chan struct{})
	rss.Phase("startup")
	go rss.Run(c.Pid(), stop)

	hookCalls := 0
	c.OnHook = func(req Msg) Msg {
		hookCalls++
		in, _ := req["input"].(Msg)
		ti, _ := in["tool_input"].(Msg)
		cmd := fmt.Sprint(ti["command"])
		dec := "allow"
		if strings.Contains(cmd, "deny-me") {
			dec = "deny"
		}
		fmt.Printf("    [hook_callback id=%v event=%v tool=%v cmd=%q -> %s]\n", req["callback_id"], in["hook_event_name"], in["tool_name"], cmd, dec)
		return Msg{"hookSpecificOutput": Msg{"hookEventName": "PreToolUse", "permissionDecision": dec,
			"permissionDecisionReason": "decided by Go spike"}}
	}
	c.OnCanUseTool = func(req Msg) Msg {
		fmt.Printf("    [can_use_tool tool=%v input=%v -> allow]\n", req["tool_name"], req["input"])
		return Msg{"behavior": "allow", "updatedInput": req["input"]}
	}

	r, err := c.Request(Msg{"subtype": "initialize", "hooks": Msg{
		"PreToolUse": []Msg{{"matcher": "Bash", "hookCallbackIds": []string{"hook_0"}}}}}, 30*time.Second)
	must(err)
	resp, _ := r["response"].(Msg)
	keys := []string{}
	for k := range resp {
		keys = append(keys, k)
	}
	fmt.Printf("A2 initialize ok in %s; response keys=%v\n", ms(time.Since(t0)), keys)
	if models, ok := resp["models"].([]any); ok {
		fmt.Printf("   models offered: %d\n", len(models))
	}

	idle := func(label string) {
		rss.Phase(label)
		time.Sleep(2 * time.Second)
	}
	idle("idle-after-init")

	var sessionID string
	capture := func(m Msg) {
		print(m)
		if m["type"] == "system" && m["subtype"] == "init" {
			sessionID = fmt.Sprint(m["session_id"])
		}
	}
	turn := func(label, text string) Msg {
		rss.Phase("turn:" + label)
		ts := time.Now()
		must(c.SendUser(text))
		res, err := c.UntilResult(120*time.Second, capture)
		must(err)
		mu, _ := json.Marshal(res["modelUsage"])
		fmt.Printf("  [%s] result in %s, num_turns=%v modelUsage=%s\n", label, ms(time.Since(ts)), res["num_turns"], trunc(string(mu), 200))
		idle("idle-after:" + label)
		return res
	}

	fmt.Println("A1 turn 1")
	turn("t1", "Remember the secret word PELICAN. Reply only: OK")
	fmt.Println("A1 turn 2 (same process)")
	turn("t2", "What was the secret word? One word.")
	fmt.Println("A2 hook deny")
	turn("deny", "Run this exact bash command once: echo deny-me . Then report in one line what happened. Do not retry.")
	fmt.Println("A2 hook allow")
	turn("allow", "Run this exact bash command once: echo allow-me . Then report its output in one line.")
	fmt.Printf("A2 hook calls total=%d\n", hookCalls)

	fmt.Println("A4 interrupt")
	rss.Phase("turn:interrupt")
	ts := time.Now()
	must(c.SendUser("Run the bash command: sleep 20 && echo done . Then write the numbers one to fifty in words, one per line."))
	go func() {
		time.Sleep(6 * time.Second)
		ti := time.Now()
		r, err := c.Request(Msg{"subtype": "interrupt"}, 20*time.Second)
		fmt.Printf("    [interrupt ack in %s err=%v resp=%v]\n", ms(time.Since(ti)), err, r)
	}()
	res, err := c.UntilResult(120*time.Second, print)
	must(err)
	fmt.Printf("  interrupted turn ended %s after send; subtype=%v is_error=%v\n", ms(time.Since(ts)), res["subtype"], res["is_error"])
	idle("idle-after:interrupt")

	fmt.Println("A4 set_model sonnet")
	r, err = c.Request(Msg{"subtype": "set_model", "model": "sonnet"}, 20*time.Second)
	fmt.Printf("  set_model resp=%v err=%v\n", r, err)
	turn("after-set-model", "Reply only: ok")

	fmt.Println("get_usage / set_permission_mode probes")
	r, err = c.Request(Msg{"subtype": "get_usage"}, 20*time.Second)
	b, _ := json.Marshal(r)
	fmt.Printf("  get_usage err=%v resp=%s\n", err, trunc(string(b), 300))
	r, err = c.Request(Msg{"subtype": "set_permission_mode", "mode": "acceptEdits"}, 20*time.Second)
	fmt.Printf("  set_permission_mode err=%v resp=%v\n", err, r)

	close(stop)
	fmt.Println("A6 RSS (kB, process tree), per phase max / last:")
	for _, p := range rss.order {
		fmt.Printf("   %-26s max=%7d last=%7d\n", p, rss.max[p], rss.last[p])
	}
	fmt.Println("A5 kill -9 pid", c.Pid(), "session", sessionID)
	c.Kill()
	os.WriteFile("session_id", []byte(sessionID), 0644)
}

func runResume(log *os.File, id string) {
	t0 := time.Now()
	c, err := Start(log, append(common, "--resume", id)...)
	must(err)
	r, err := c.Request(Msg{"subtype": "initialize"}, 30*time.Second)
	must(err)
	_ = r
	tInit := time.Since(t0)
	ts := time.Now()
	must(c.SendUser("What was the secret word I told you earlier? One word."))
	first := time.Duration(0)
	res, err := c.UntilResult(120*time.Second, func(m Msg) {
		if first == 0 {
			first = time.Since(t0)
		}
		if m["type"] == "system" && m["subtype"] == "init" {
			fmt.Printf("    init session_id=%v (resumed id=%s)\n", m["session_id"], id)
		}
		print(m)
	})
	must(err)
	fmt.Printf("A5 spawn->initialize ack %s; spawn->first message %s; send->result %s; total %s\n",
		ms(tInit), ms(first), ms(time.Since(ts)), ms(time.Since(t0)))
	fmt.Println("   answer:", res["result"])
	c.CloseAndWait()
}

func runAsk(log *os.File) {
	c, err := Start(log, common...)
	must(err)
	c.OnCanUseTool = func(req Msg) Msg {
		in, _ := req["input"].(Msg)
		b, _ := json.Marshal(in)
		fmt.Printf("    [can_use_tool tool=%v input=%s]\n", req["tool_name"], trunc(string(b), 300))
		if req["tool_name"] != "AskUserQuestion" {
			return Msg{"behavior": "allow", "updatedInput": in}
		}
		answers := Msg{}
		qs, _ := in["questions"].([]any)
		for _, q := range qs {
			qm, _ := q.(Msg)
			answers[fmt.Sprint(qm["question"])] = "Blue"
		}
		in["answers"] = answers
		return Msg{"behavior": "allow", "updatedInput": in}
	}
	_, err = c.Request(Msg{"subtype": "initialize"}, 30*time.Second)
	must(err)
	must(c.SendUser("Use the AskUserQuestion tool to ask me whether I prefer Red or Blue (two options). Then tell me in one line what I chose."))
	res, err := c.UntilResult(120*time.Second, print)
	must(err)
	fmt.Println("A3 final:", res["result"])
	c.CloseAndWait()
}
