"""Tests for infra/mail/colliedesk-rcpt-policy.py, the Postfix policy service that asks Colliedesk
whether a hosted recipient exists before the MTA accepts RCPT TO."""
import importlib.util
import io
import pathlib
import unittest

SCRIPT = pathlib.Path(__file__).resolve().parent.parent / "infra/mail/colliedesk-rcpt-policy.py"
spec = importlib.util.spec_from_file_location("rcpt_policy", SCRIPT)
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)


def request(recipient, state="RCPT"):
    return f"request=smtpd_access_policy\nprotocol_state={state}\nrecipient={recipient}\nsender=a@b.example\n\n"


class Answers(unittest.TestCase):
    def run_policy(self, stdin, status=None, error=None):
        calls = []

        def check(recipient):
            calls.append(recipient)
            if error:
                raise error
            return status

        out = io.StringIO()
        policy.serve(io.StringIO(stdin), out, check)
        return out.getvalue(), calls

    def test_known_recipient_is_left_to_the_next_restriction(self):
        out, calls = self.run_policy(request("support@acme.colliedesk.com"), status=204)
        self.assertEqual(out, "action=DUNNO\n\n")
        self.assertEqual(calls, ["support@acme.colliedesk.com"])

    def test_unknown_recipient_is_refused_permanently(self):
        out, _ = self.run_policy(request("support@nobody.colliedesk.com"), status=404)
        self.assertEqual(out, "action=550 5.1.1 No Colliedesk workspace receives mail at this address\n\n")

    def test_server_trouble_defers_instead_of_refusing(self):
        for status in (401, 429, 500, 503):
            out, _ = self.run_policy(request("support@acme.colliedesk.com"), status=status)
            self.assertEqual(out, "action=451 4.3.0 Colliedesk cannot check this recipient now, try again later\n\n", status)
        out, _ = self.run_policy(request("support@acme.colliedesk.com"), error=OSError("refused"))
        self.assertEqual(out, "action=451 4.3.0 Colliedesk cannot check this recipient now, try again later\n\n")

    def test_only_rcpt_state_is_checked(self):
        out, calls = self.run_policy(request("", state="DATA"))
        self.assertEqual(out, "action=DUNNO\n\n")
        self.assertEqual(calls, [])

    def test_one_process_answers_many_requests_in_order(self):
        statuses = iter([204, 404])
        calls = []

        def check(recipient):
            calls.append(recipient)
            return next(statuses)

        out = io.StringIO()
        policy.serve(io.StringIO(request("a@x.colliedesk.com") + request("b@y.colliedesk.com")), out, check)
        self.assertEqual(out.getvalue().count("\n\n"), 2)
        self.assertTrue(out.getvalue().startswith("action=DUNNO\n\naction=550 5.1.1"))
        self.assertEqual(calls, ["a@x.colliedesk.com", "b@y.colliedesk.com"])


if __name__ == "__main__":
    unittest.main()
