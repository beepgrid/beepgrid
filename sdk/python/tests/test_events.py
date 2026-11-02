import io
import json
import unittest
from unittest.mock import patch
from beepgrid import Client, NotifError
from beepgrid.events import read_frames


def frame(n):
    return ('id: %d\r\ndata: %s\r\n\r\n' % (n, json.dumps({"sequence": n, "kind": "message.created", "data": {"body": "héllo"}}))).encode()


class EventTests(unittest.TestCase):
    def test_resume_complete_frames_and_skip_hidden_cursor(self):
        class Stream(io.BytesIO):
            headers = {"Content-Type": "text/event-stream"}
        requests = []
        def open_stream(request, **kwargs):
            requests.append(request)
            if len(requests) == 1:
                return Stream(frame(1) + b'id: 2\nevent: cursor\ndata: {}\n\nid: 3\ndata: {"partial":')
            return Stream(frame(1) + frame(3))
        with patch("beepgrid._open", side_effect=open_stream):
            stream = Client("http://localhost", "fixture").events()
            self.assertEqual(next(stream)["sequence"], 1)
            self.assertEqual(next(stream)["sequence"], 3)
            stream.close()
        self.assertEqual(len(requests), 2)
        self.assertEqual(requests[1].get_header("Last-event-id"), "2")
        self.assertIn("after=2", requests[1].full_url)

    def test_invalid_and_partial_frames(self):
        self.assertEqual(list(read_frames(io.BytesIO(b'id: 1\ndata: {"partial":'))), [])
        for raw in [b'id: 1\ndata: {"sequence":2,"kind":"bad"}\n\n', b'data: ' + b'x' * (8 * 1024 * 1024)]:
            with self.assertRaises(ValueError):
                list(read_frames(io.BytesIO(raw)))

    def test_revocation_is_terminal(self):
        import urllib.error
        error = urllib.error.HTTPError("http://localhost", 403, "denied", {}, None)
        with patch("beepgrid._open", side_effect=error) as call:
            with self.assertRaises(NotifError) as caught:
                next(Client("http://localhost", "fixture").events())
            self.assertEqual(caught.exception.status, 403)
            self.assertEqual(call.call_count, 1)
