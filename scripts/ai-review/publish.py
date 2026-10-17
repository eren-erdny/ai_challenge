"""Fixed GitHub publisher: no model-selected APIs, paths or commands."""
import json
import os
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path

MARKER = "<!-- deepseek-ai-review -->"
SHA = re.compile(r"^[0-9a-f]{40}(?:[0-9a-f]{24})?$")
REPO = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")


def api(method, route, body=None):
    payload = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(
        "https://api.github.com" + route, data=payload, method=method,
        headers={"Authorization": "Bearer " + os.environ["GITHUB_TOKEN"],
                 "Accept": "application/vnd.github+json",
                 "X-GitHub-Api-Version": "2022-11-28",
                 "Content-Type": "application/json"})
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            return None
    with urllib.request.build_opener(NoRedirect).open(request, timeout=30) as response:
        return json.load(response)


def publish(directory, event, repository, request=api):
    pr, number = event["pull_request"], event["number"]
    if not REPO.fullmatch(repository) or type(number) is not int or number <= 0:
        raise ValueError("Invalid repository or PR number")
    if pr["head"]["repo"]["full_name"] != repository or pr["base"]["repo"]["full_name"] != repository:
        raise ValueError("Only same-repository PRs supported")
    head, base = pr["head"]["sha"], pr["base"]["sha"]
    if not SHA.fullmatch(head) or not SHA.fullmatch(base):
        raise ValueError("Full SHAs required")
    directory = Path(directory)
    evidence = json.loads((directory / "input.json").read_text(encoding="utf-8-sig"))
    text = (directory / "review.md").read_text(encoding="utf-8-sig")
    if (evidence["head_sha"], evidence["base_sha"]) != (head, base):
        raise ValueError("Review commit mismatch")
    if not text.startswith(MARKER + "\n") or len(text) > 60000:
        raise ValueError("Invalid review report")
    route = "/repos/" + repository
    def fresh():
        current = request("GET", f"{route}/pulls/{number}")
        return current["state"] == "open" and current["head"]["sha"] == head and current["base"]["sha"] == base
    if not fresh():
        return "Skipped: PR closed or commits changed"
    existing = None
    for page in range(1, 11):
        comments = request("GET", f"{route}/issues/{number}/comments?per_page=100&page={page}")
        for comment in comments:
            if (comment["user"]["login"] == "github-actions[bot]"
                    and comment["user"]["type"] == "Bot"
                    and comment.get("body", "").startswith(MARKER + "\n")):
                existing = comment["id"]
        if len(comments) < 100:
            break
    else:
        raise ValueError("Comment pagination exceeded")
    if not fresh():
        return "Skipped: PR changed before publication"
    if existing is not None:
        if type(existing) is not int or existing <= 0:
            raise ValueError("Invalid comment identifier")
        request("PATCH", f"{route}/issues/comments/{existing}", {"body": text})
    else:
        request("POST", f"{route}/issues/{number}/comments", {"body": text})
    return "Published validated review"


def main():
    try:
        event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text(encoding="utf-8"))
        result = publish(sys.argv[1], event, os.environ["GITHUB_REPOSITORY"])
        print(result)
        summary = os.environ.get("GITHUB_STEP_SUMMARY")
        if summary:
            with open(summary, "a", encoding="utf-8") as output:
                output.write(result + "\n\n")
                output.write((Path(sys.argv[1]) / "review.md").read_text(encoding="utf-8-sig"))
        return 0
    except (KeyError, IndexError, TypeError, ValueError, OSError, urllib.error.URLError):
        print("Review publication failed; inspect saved artifacts.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
