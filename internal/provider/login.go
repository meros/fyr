package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/launchenv"
	"github.com/meros/fyr/internal/pty"
)

// SPEC 0.5 V0.53: login through Fyr. For a CLI-login account Fyr runs the
// adapter's own login (cli.Loginer) and shows its link in Settings; the owner
// signs in in the browser and pastes the code back. A token login runs in a
// private throwaway folder and the token goes straight into the secret store;
// a folder login runs in the account's own config folder, where the CLI keeps
// and refreshes its login. The output is read in memory only: it is never
// logged, stored or sent to a client, except the sign-in link and a redacted
// error line.

// LoginTimeout ends a login nobody finishes.
const LoginTimeout = 10 * time.Minute

// loginKeep is how long a finished login stays visible.
const loginKeep = 5 * time.Minute

// Login states.
const (
	LoginStarting  = "starting"  // the CLI has not shown its link yet
	LoginWaiting   = "waiting"   // the link is shown; waiting for the code
	LoginChecking  = "checking"  // the code was sent
	LoginDone      = "done"      // logged in (or the token is stored)
	LoginFailed    = "failed"    // the CLI ended without a login
	LoginCancelled = "cancelled" // the owner cancelled
	LoginTimedOut  = "timeout"   // nobody finished it in time
)

// LoginDTO is what clients see of a login: never the CLI's output, never a
// token or the code.
type LoginDTO struct {
	Kind    string    `json:"kind"` // "token" or "folder"
	State   string    `json:"state"`
	URL     string    `json:"url,omitempty"`
	Message string    `json:"message,omitempty"`
	Expires time.Time `json:"expires"`
}

// Active: the login still runs.
func (d LoginDTO) Active() bool {
	return d.State == LoginStarting || d.State == LoginWaiting || d.State == LoginChecking
}

type loginRun struct {
	mu       sync.Mutex
	kind     string
	adapter  cli.Adapter
	loginer  cli.Loginer
	state    string
	url      string
	msg      string
	expires  time.Time
	ended    time.Time
	in       io.Writer
	terminal bool
	mark     int // output length when the code was sent
	outLen   int // output length so far
	cancel   context.CancelFunc
	byOwner  bool
}

func (l *loginRun) dto() LoginDTO {
	l.mu.Lock()
	defer l.mu.Unlock()
	return LoginDTO{Kind: l.kind, State: l.state, URL: l.url, Message: l.msg, Expires: l.expires}
}

func (l *loginRun) active() bool { return l.dto().Active() }

// login is the provider's current or recent login, if any.
func (r *Registry) login(id string) *LoginDTO {
	r.lmu.Lock()
	l := r.logins[id]
	r.lmu.Unlock()
	if l == nil {
		return nil
	}
	l.mu.Lock()
	ended := l.ended
	l.mu.Unlock()
	if !ended.IsZero() && time.Since(ended) > loginKeep {
		return nil
	}
	d := l.dto()
	return &d
}

func (r *Registry) loginTimeout() time.Duration {
	if r.LoginTimeout > 0 {
		return r.LoginTimeout
	}
	return LoginTimeout
}

// startLogin runs provider.login.
func (r *Registry) startLogin(id string) api.Reply {
	p, ok := r.get(id)
	if !ok {
		return api.Reply{Error: "no provider " + id, Status: 404}
	}
	if p.Auth.Kind != cli.CLILogin {
		return fail(errors.New(p.Name + " is not a CLI login account; paste its key instead"))
	}
	ad := AdapterFor(p.CLI)
	ln, ok := ad.(cli.Loginer)
	if !ok {
		return fail(errors.New(p.Name + " cannot sign in through Fyr"))
	}
	r.lmu.Lock()
	if r.logins == nil {
		r.logins = map[string]*loginRun{}
	}
	if l := r.logins[id]; l != nil && l.active() {
		r.lmu.Unlock()
		return api.Reply{Error: "A login for " + p.Name + " is already running", Status: 409}
	}
	kind := "folder"
	if p.TokenLogin {
		kind = "token"
	}
	l := &loginRun{kind: kind, adapter: ad, loginer: ln, state: LoginStarting, expires: time.Now().Add(r.loginTimeout())}
	r.logins[id] = l
	r.lmu.Unlock()
	if err := r.runLogin(p, l); err != nil {
		l.mu.Lock()
		l.state, l.msg, l.ended = LoginFailed, err.Error(), time.Now()
		l.mu.Unlock()
		return fail(err)
	}
	log.Printf("providers: login for %s started (%s)", id, kind)
	return api.Reply{Result: l.dto()}
}

func (r *Registry) runLogin(p Provider, l *loginRun) error {
	// Where the CLI runs. A token account's folder may hold another
	// account's login (V0.50), so setup-token runs in a private throwaway
	// folder and never touches it; a folder account logs in in its own
	// folder ("" is the CLI's default).
	var dir, cwd, tmp string
	if l.kind == "token" {
		t, err := os.MkdirTemp(r.LoginTemp, "fyr-login-")
		if err != nil {
			return fmt.Errorf("could not make a folder for the login: %w", err)
		}
		tmp, dir, cwd = t, t, t
	} else {
		cwd = r.dirFor(p.CLI, p.Auth.ConfigDir)
		if err := os.MkdirAll(cwd, 0o700); err != nil {
			return fmt.Errorf("could not make the config folder: %w", err)
		}
		if p.Auth.ConfigDir != "" {
			dir = cwd
		}
	}
	base := r.LoginEnv
	if base == nil {
		base = launchenv.AgentEnv(l.adapter.Caps().CredentialVars) // #167, V0.review-hangs (1)
	}
	spec := l.loginer.LoginSpec(base, dir, l.kind == "token")
	bin := r.LoginBinary
	if bin == "" {
		bin = spec.Binary
	}
	cmd := exec.Command(bin, spec.Args...)
	cmd.Env = spec.Env
	cmd.Dir = cwd
	var out io.ReadCloser
	if spec.Terminal {
		m, err := pty.Start(cmd, 400, 40)
		if err != nil {
			os.RemoveAll(tmp)
			return fmt.Errorf("could not start the login: %w", err)
		}
		out, l.in, l.terminal = m, m, true
	} else {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		in, err := cmd.StdinPipe()
		if err != nil {
			os.RemoveAll(tmp)
			return err
		}
		pr, pw, err := os.Pipe()
		if err != nil {
			os.RemoveAll(tmp)
			return err
		}
		cmd.Stdout, cmd.Stderr = pw, pw
		if err := cmd.Start(); err != nil {
			pr.Close()
			pw.Close()
			os.RemoveAll(tmp)
			if errors.Is(err, exec.ErrNotFound) {
				return errors.New(spec.Binary + " is not installed on this host")
			}
			return fmt.Errorf("could not start the login: %w", err)
		}
		pw.Close()
		out, l.in = pr, in
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.loginTimeout())
	l.cancel = cancel
	pid := cmd.Process.Pid
	go func() {
		<-ctx.Done()
		syscall.Kill(-pid, syscall.SIGKILL) // the CLI and anything it started
	}()
	go r.watchLogin(p.ID, l, cmd, out, ctx, cancel, cwd, tmp)
	return nil
}

// watchLogin reads the CLI's output until it ends, then settles the login.
func (r *Registry) watchLogin(id string, l *loginRun, cmd *exec.Cmd, out io.ReadCloser, ctx context.Context, cancel context.CancelFunc, cwd, tmp string) {
	defer cancel()
	buf := make([]byte, 0, 8<<10)
	chunk := make([]byte, 4<<10)
	defer func() {
		// Nothing of the output outlives the login.
		clear(buf[:cap(buf)])
		clear(chunk)
	}()
	stored := false
	for {
		n, err := out.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			if len(buf) > 256<<10 { // a runaway screen: keep the tail
				keep := append([]byte(nil), buf[len(buf)-(128<<10):]...)
				clear(buf)
				l.mu.Lock()
				l.mark = max(0, l.mark-(len(buf)-len(keep)))
				l.mu.Unlock()
				buf = keep
			}
			l.mu.Lock()
			l.outLen = len(buf)
			l.mu.Unlock()
			if !stored && r.readLogin(id, l, buf, false) {
				stored = true
				cancel() // the token is kept; the CLI has nothing more to give
			}
		}
		if err != nil {
			break
		}
	}
	out.Close()
	werr := cmd.Wait()
	if !stored && l.kind == "token" {
		stored = r.readLogin(id, l, buf, true)
	}
	if tmp != "" {
		os.RemoveAll(tmp)
	}
	l.mu.Lock()
	switch {
	case stored:
		l.state, l.msg = LoginDone, ""
	case l.byOwner:
		l.state, l.msg = LoginCancelled, ""
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		l.state, l.msg = LoginTimedOut, fmt.Sprintf("The login timed out after %d minutes. Start it again.", int(r.loginTimeout().Minutes()))
	case l.kind == "folder" && werr == nil && l.adapter.LoggedIn(cli.Auth{Kind: cli.CLILogin, ConfigDir: cwd}):
		l.state, l.msg = LoginDone, ""
	default:
		msg := l.loginer.ParseLogin(buf[min(l.mark, len(buf)):], true).Line
		if msg == "" {
			msg = "The login ended without signing in."
			if l.kind == "token" {
				msg = "claude setup-token ended without a token."
			}
		}
		l.state, l.msg = LoginFailed, msg
	}
	l.ended = time.Now()
	state := l.state
	l.mu.Unlock()
	log.Printf("providers: login for %s %s", id, state)
	r.changed()
}

// readLogin updates the login from the output so far; true once a token
// is in the secret store.
func (r *Registry) readLogin(id string, l *loginRun, buf []byte, done bool) bool {
	o := l.loginer.ParseLogin(buf, done)
	if o.Token != "" {
		if err := r.storeToken(id, o.Token); err != nil {
			l.mu.Lock()
			l.msg = "The token could not be stored: " + l.loginer.RedactLogin(err.Error())
			l.mu.Unlock()
			return false
		}
		r.changed()
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if o.URL != "" {
		l.url = o.URL // the latest: a retry shows a new link
	}
	if l.state == LoginStarting && l.url != "" {
		l.state = LoginWaiting
	}
	if l.state == LoginChecking {
		// After the code: an error line means it can be tried again.
		after := l.loginer.ParseLogin(buf[min(l.mark, len(buf)):], false)
		if after.Line != "" {
			l.state, l.msg = LoginWaiting, after.Line
			if after.Retry && l.terminal {
				// setup-token starts over on Enter, with a new link.
				io.WriteString(l.in, "\r")
				l.mark = len(buf)
				l.msg += " A new link is ready."
			}
		}
	}
	return false
}

// storeToken keeps a login token as the provider's secret.
func (r *Registry) storeToken(id, tok string) error {
	p, ok := r.get(id)
	if !ok {
		return errors.New("the provider is gone")
	}
	if p.Auth.Secret == "" {
		p.Auth.Secret = SecretName(p.ID)
	}
	if err := r.Secrets.Put(p.Auth.Secret, tok); err != nil {
		return err
	}
	p.KeyRef = ""
	if rep := r.replace(p); rep.Error != "" {
		return errors.New(rep.Error)
	}
	return nil
}

// loginCode runs provider.loginCode: the code the sign-in page showed.
func (r *Registry) loginCode(id, code string) api.Reply {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 4096 || strings.ContainsAny(code, "\r\n\x03\x04\x1b") {
		return fail(errors.New("paste the code the sign-in page shows"))
	}
	r.lmu.Lock()
	l := r.logins[id]
	r.lmu.Unlock()
	if l == nil || !l.active() {
		return api.Reply{Error: "no login is running; start it again", Status: 409}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state == LoginStarting {
		return api.Reply{Error: "the sign-in link is not ready yet", Status: 409}
	}
	end := "\n"
	if l.terminal {
		end = "\r"
	}
	if _, err := io.WriteString(l.in, code); err != nil {
		return fail(errors.New("the login has ended; start it again"))
	}
	if l.terminal {
		// A terminal screen takes a pasted line, then Enter as its own key.
		time.Sleep(150 * time.Millisecond)
	}
	if _, err := io.WriteString(l.in, end); err != nil {
		return fail(errors.New("the login has ended; start it again"))
	}
	l.state, l.msg = LoginChecking, ""
	l.mark = l.outLen // only output after the code counts
	return api.Reply{}
}

// cancelLogin runs provider.loginCancel.
func (r *Registry) cancelLogin(id string) api.Reply {
	r.lmu.Lock()
	l := r.logins[id]
	r.lmu.Unlock()
	if l == nil || !l.active() {
		return api.Reply{}
	}
	l.mu.Lock()
	l.byOwner = true
	c := l.cancel
	l.mu.Unlock()
	if c != nil {
		c()
	}
	return api.Reply{}
}
