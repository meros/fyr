"""V0.thread-score: the detectors read what is run, not what is written. Run: python3 -m unittest discover scripts/threadscore"""
import json, os, tempfile, unittest

import extract as x


def transcript(*events):
    f = tempfile.NamedTemporaryFile("w", suffix=".jsonl", delete=False)
    for e in events:
        f.write(json.dumps(e) + "\n")
    f.close()
    return f.name


def bash(i, cmd, mid=None):
    return {"type": "assistant", "cwd": "/w", "gitBranch": "feat", "message": {
        "id": mid or "m%d" % i, "model": "m", "content": [{"type": "tool_use", "id": "t%d" % i, "name": "Bash", "input": {"command": cmd}}]}}


def text(i, tx):
    return {"type": "assistant", "message": {"id": "x%d" % i, "model": "m", "content": [{"type": "text", "text": tx}]}}


def user(tx):
    return {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": tx}]}}


class Detectors(unittest.TestCase):
    def row(self, *events):
        p = transcript(*events)
        try:
            return x.analyse(p)
        finally:
            os.unlink(p)

    def test_merge_and_sudo_count_only_when_run(self):
        r = self.row(user("go"), bash(1, "gh pr merge 5 --squash"), bash(2, "sudo -n true"),
                     bash(3, "grep 'gh pr merge' f; cat > t <<'EOF'\nsudo rm x\ngh pr merge\nEOF"))
        self.assertEqual((r["v_gh_pr_merge"], r["v_sudo"]), (1, 1))

    def test_gh_polling(self):
        r = self.row(user("go"), bash(1, "until gh pr checks 3 | grep -q pass; do sleep 5; done"), bash(2, "gh run watch 9"))
        self.assertEqual(r["v_gh_poll"], 2)

    def test_parallel_gh_mutations_need_one_message(self):
        a = self.row(user("go"), bash(1, "gh pr comment 1 -b x", "m"), bash(2, "gh pr comment 2 -b y", "m"))
        b = self.row(user("go"), bash(1, "gh pr comment 1 -b x"), bash(2, "gh pr comment 2 -b y"))
        self.assertEqual((a["v_parallel_gh_mutations"], b["v_parallel_gh_mutations"]), (1, 0))

    def test_cd_into_a_home_folder_is_not_a_leak_but_a_message_is(self):
        ok = self.row(user("go"), bash(1, "cd /home/alice/proj && git commit -qm 'fix a thing'"))
        bad = self.row(user("go"), bash(1, "git commit -qm 'read /home/alice/proj/x'"))
        self.assertEqual((ok["v_homepath_in_commit"], bad["v_homepath_in_commit"]), (0, 1))

    def test_push_to_main_and_branch_switch(self):
        main = dict(bash(1, "git commit -qm x"), gitBranch="main")
        moved = dict(bash(2, "git switch -c f && git commit -qm x"), gitBranch="main")
        push = bash(3, "git push origin HEAD:main")
        r = self.row(user("go"), main, moved, push)
        self.assertEqual(r["v_git_on_main"], 2)

    def test_text_question_only_at_the_end_of_a_turn(self):
        r = self.row(user("go"), text(1, "Which one do you want me to pick for this task?"), bash(2, "ls"),
                     text(3, "Done, the change is in."), user("more"), text(4, "Shall I build the layout now, desktop and phone?"))
        self.assertEqual(r["v_text_question"], 1)

    def test_correction_and_duplicate_calls(self):
        r = self.row(user("go"), bash(1, "ls"), bash(2, "ls"), user("no, I said the other file"))
        self.assertEqual((r["corrections"], r["dup_calls"]), (1, 1))


if __name__ == "__main__":
    unittest.main()
