import copy
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location("publish", Path(__file__).with_name("publish.py"))
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.head, self.base = "a" * 40, "b" * 40
        self.event = {"number": 7, "pull_request": {
            "head": {"sha": self.head, "repo": {"full_name": "owner/repo"}},
            "base": {"sha": self.base, "repo": {"full_name": "owner/repo"}}}}
        (self.directory / "input.json").write_text(json.dumps(
            {"head_sha": self.head, "base_sha": self.base}), encoding="utf-8")
        (self.directory / "review.md").write_text(publisher.MARKER + "\nReview", encoding="utf-8")
        self.calls = []
        self.comments = []
        self.fresh_count = 0
        self.change_after_pagination = False

    def api(self, method, route, body=None):
        self.calls.append((method, route, body))
        if method != "GET":
            return {"id": 100}
        if "/comments?" in route:
            return self.comments
        self.fresh_count += 1
        return {"state": "open", "head": {"sha": "c"*40 if self.change_after_pagination and self.fresh_count > 1 else self.head},
                "base": {"sha": self.base}}

    def run_publish(self):
        return publisher.publish(self.directory, self.event, "owner/repo", self.api)

    def test_create(self):
        self.assertEqual(self.run_publish(), "Published validated review")
        self.assertEqual(self.calls[-1][0:2], ("POST", "/repos/owner/repo/issues/7/comments"))

    def test_updates_only_bot(self):
        self.comments = [
            {"id": 10, "user": {"login": "person", "type": "User"}, "body": publisher.MARKER+"\nHuman"},
            {"id": 11, "user": {"login": "github-actions[bot]", "type": "Bot"}, "body": publisher.MARKER+"\nOld"}]
        self.run_publish()
        self.assertEqual(self.calls[-1][0:2], ("PATCH", "/repos/owner/repo/issues/comments/11"))

    def test_fork_rejected_without_network(self):
        self.event["pull_request"]["head"]["repo"]["full_name"] = "fork/repo"
        with self.assertRaises(ValueError):
            self.run_publish()
        self.assertEqual(self.calls, [])

    def test_stale_before_write(self):
        self.change_after_pagination = True
        self.assertTrue(self.run_publish().startswith("Skipped"))
        self.assertTrue(all(c[0] == "GET" for c in self.calls))

    def test_wrong_evidence(self):
        (self.directory / "input.json").write_text(json.dumps(
            {"head_sha": "c"*40, "base_sha": self.base}), encoding="utf-8")
        with self.assertRaises(ValueError):
            self.run_publish()
        self.assertEqual(self.calls, [])


if __name__ == "__main__":
    unittest.main()
