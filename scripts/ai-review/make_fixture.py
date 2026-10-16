"""Create a disposable Git PR fixture. Never modifies an existing repository."""
import json
import os
import subprocess
import sys
from pathlib import Path

FILES = {
    "projects/deepseek-client/README.md": "# User service\nUserName accepts an optional User and returns a display name. See docs/api.md for the nil contract.\n",
    "projects/deepseek-client/docs/api.md": "# UserName contract\nUserName must return an empty string when User is nil.\nHTTP handlers call UserName with optional users. A missing authenticated user is represented by nil.\n",
    "projects/deepseek-client/user.go": 'package user\n\ntype User struct { Name string }\n\nfunc UserName(u *User) string {\n if u == nil { return "" }\n return u.Name\n}\n',
}


def create(directory):
    root = Path(directory).resolve()
    root.mkdir(parents=True, exist_ok=False)
    env = dict(os.environ)
    for key in list(env):
        if key.startswith("GIT_"):
            del env[key]
    env.update(GIT_AUTHOR_NAME="Review fixture", GIT_AUTHOR_EMAIL="fixture@example.invalid",
               GIT_COMMITTER_NAME="Review fixture", GIT_COMMITTER_EMAIL="fixture@example.invalid")
    def git(*args):
        return subprocess.check_output(
            ["git", "-C", str(root), "-c", "commit.gpgsign=false", "-c", "core.autocrlf=false",
             *args], env=env, stderr=subprocess.PIPE).decode().strip()
    git("init", "-q")
    for name, text in FILES.items():
        target = root / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text, encoding="utf-8")
    git("add", ".")
    git("commit", "-qm", "Document optional-user contract")
    base = git("rev-parse", "HEAD")
    target = root / "projects/deepseek-client/user.go"
    target.write_text(FILES["projects/deepseek-client/user.go"].replace(' if u == nil { return "" }\n', ""),
                      encoding="utf-8")
    git("add", ".")
    git("commit", "-qm", "Remove nil guard (intentional demonstration bug)")
    head = git("rev-parse", "HEAD")
    manifest = {"repo": str(root), "base_sha": base, "head_sha": head,
                "scope": "projects/deepseek-client", "expected_bug": "UserName(nil) panics after removing guard"}
    (root / "fixture.json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
    return manifest


if __name__ == "__main__":
    result = create(sys.argv[1])
    print("Created isolated fixture:", result["base_sha"], result["head_sha"])
