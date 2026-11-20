/*
 * fyr-host is the first process of Fyr's launchd agent (decision 0037). The
 * first process answers to macOS for everything under it, and a permission
 * (App Management, Full Disk Access) is granted to its signature. It is
 * signed with Fyr's certificate like every build, so the grant survives
 * updates; it holds the launcher as its child, passes signals on and exits
 * as the launcher did (as the script install's host does).
 *
 *	fyr-host [-data DIR] [-log FILE] [fyrd flags...]
 *
 * It runs <bundle>/Contents/Resources/fyr -data DIR -seed <bundle>/.../fyrd
 * with the flags after its own. A leading ~/ in any argument is the home
 * folder: the agent's plist sits in the bundle and cannot name it.
 */
#include <errno.h>
#include <fcntl.h>
#include <libgen.h>
#include <limits.h>
#include <mach-o/dyld.h>
#include <poll.h>
#include <pwd.h>
#include <signal.h>
#include <spawn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

extern char **environ;
static volatile pid_t child;
static const char *home;

static void forward(int sig) {
	if (child > 0)
		kill(child, sig);
}

static char *expand(const char *a) {
	char *s;
	if (strncmp(a, "~/", 2) != 0)
		return strdup(a);
	if (asprintf(&s, "%s/%s", home, a + 2) < 0)
		exit(70);
	return s;
}

static void mkdirs(char *p) {
	for (char *s = p + 1; *s; s++)
		if (*s == '/') {
			*s = 0;
			mkdir(p, 0755);
			*s = '/';
		}
}

/* loginPath is the PATH of the owner's login shell, where Homebrew, nvm and
 * the like put the tools agents run (git, gh, claude, node). launchd starts
 * agents with a bare PATH. A shell that does not answer in 5 s is left. */
static char *loginPath(void) {
	static char buf[8192];
	char *sh = getenv("SHELL");
	struct passwd *pw = getpwuid(getuid());
	int fd[2], st;
	size_t n = 0;
	pid_t pid;
	posix_spawn_file_actions_t fa;
	if ((!sh || !*sh) && pw)
		sh = pw->pw_shell;
	if (!sh || !*sh || pipe(fd) < 0)
		return 0;
	/* Interactive too, as rc files (.zshrc) often set it; the markers skip
	 * whatever they print. */
	char *argv[] = {sh, "-i", "-l", "-c", "printf '\\n<fyr-path>%s</fyr-path>' \"$PATH\"", 0};
	posix_spawn_file_actions_init(&fa);
	posix_spawn_file_actions_adddup2(&fa, fd[1], 1);
	posix_spawn_file_actions_addopen(&fa, 0, "/dev/null", O_RDONLY, 0);
	posix_spawn_file_actions_addclose(&fa, fd[0]);
	if (posix_spawn(&pid, sh, &fa, 0, argv, environ) != 0) {
		close(fd[0]);
		close(fd[1]);
		return 0;
	}
	close(fd[1]);
	time_t end = time(0) + 5;
	struct pollfd p = {fd[0], POLLIN, 0};
	while (n < sizeof buf - 1 && time(0) < end && poll(&p, 1, 1000) >= 0) {
		if (!p.revents)
			continue;
		ssize_t r = read(fd[0], buf + n, sizeof buf - 1 - n);
		if (r <= 0)
			break;
		n += r;
	}
	close(fd[0]);
	kill(pid, SIGKILL);
	waitpid(pid, &st, 0);
	buf[n] = 0;
	char *a = strstr(buf, "<fyr-path>"), *b;
	if (!a || !(b = strstr(a, "</fyr-path>")) || b == a + 10)
		return 0;
	*b = 0;
	return a + 10;
}

int main(int argc, char **argv) {
	int sigs[] = {SIGTERM, SIGINT, SIGHUP};
	char exe[PATH_MAX], real[PATH_MAX], *res, *launcher, *seed, *data = 0, *logf = 0, *path, *base;
	uint32_t size = sizeof exe;
	sigset_t block, none;
	struct sigaction sa = {0};
	struct passwd *pw = getpwuid(getuid());
	posix_spawnattr_t attr;
	pid_t pid;
	int i, err, st, n = 0;

	home = getenv("HOME");
	if ((!home || !*home) && pw)
		home = pw->pw_dir;
	if (!home)
		return 78;
	setenv("HOME", home, 1);
	chdir(home);
	for (i = 1; i + 1 < argc; i += 2) {
		if (!strcmp(argv[i], "-data"))
			data = expand(argv[i + 1]);
		else if (!strcmp(argv[i], "-log"))
			logf = expand(argv[i + 1]);
		else
			break;
	}
	if (!data)
		data = expand("~/.local/share/fyr");
	if (!logf)
		logf = expand("~/Library/Logs/fyr.log");
	mkdirs(logf);
	mkdirs(data);
	mkdir(data, 0700); /* a new install: the launcher's runner socket goes here */
	int lfd = open(logf, O_WRONLY | O_CREAT | O_APPEND, 0644);
	if (lfd >= 0) {
		dup2(lfd, 1);
		dup2(lfd, 2);
		close(lfd);
	}

	if (_NSGetExecutablePath(exe, &size) != 0 || !realpath(exe, real))
		return 71;
	res = dirname(dirname(real)); /* .../Contents */
	if (asprintf(&launcher, "%s/Resources/fyr", res) < 0 || asprintf(&seed, "%s/Resources/fyrd", res) < 0)
		return 70;

	base = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin";
	if ((path = loginPath()) != 0) {
		char *p;
		if (asprintf(&p, "%s:%s", path, base) < 0)
			return 70;
		setenv("PATH", p, 1);
	} else {
		setenv("PATH", base, 1);
	}

	char **args = calloc(argc + 6, sizeof *args);
	args[n++] = "fyr";
	args[n++] = "-data";
	args[n++] = data;
	args[n++] = "-seed";
	args[n++] = seed;
	if (i < argc)
		args[n++] = "--"; /* the rest are fyrd's, which the launcher passes on */
	for (; i < argc; i++)
		args[n++] = expand(argv[i]);

	sigemptyset(&block);
	sigemptyset(&none);
	sa.sa_handler = forward;
	for (i = 0; i < 3; i++)
		sigaddset(&block, sigs[i]);
	sigprocmask(SIG_BLOCK, &block, 0); /* a signal waits until child is set */
	for (i = 0; i < 3; i++)
		sigaction(sigs[i], &sa, 0);
	posix_spawnattr_init(&attr);
	posix_spawnattr_setsigmask(&attr, &none);
	posix_spawnattr_setflags(&attr, POSIX_SPAWN_SETSIGMASK);
	err = posix_spawn(&pid, launcher, 0, &attr, args, environ);
	if (err) {
		fprintf(stderr, "fyr-host: cannot start %s: %s\n", launcher, strerror(err));
		return 127;
	}
	child = pid;
	sigprocmask(SIG_UNBLOCK, &block, 0);
	while (waitpid(pid, &st, 0) < 0)
		if (errno != EINTR)
			return 1;
	return WIFEXITED(st) ? WEXITSTATUS(st) : 128 + WTERMSIG(st);
}
