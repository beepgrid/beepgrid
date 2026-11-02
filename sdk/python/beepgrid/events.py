"""Bounded full-event SSE consumption with explicit application checkpoints."""
import http.client
import json
import time
import urllib.error
import urllib.parse
import urllib.request

_MAX_FRAME = 8 * 1024 * 1024


def read_frames(response):
    data, event_id, kind, size = [], "", "", 0
    while True:
        line = response.readline(_MAX_FRAME + 1)
        if not line:
            return  # Discard incomplete frames after connection loss.
        size += len(line)
        if size > _MAX_FRAME:
            raise ValueError("Event frame exceeds limit")
        line = line.decode("utf-8").rstrip("\r\n")
        if not line:
            if event_id and data:
                if not event_id.isdecimal():
                    raise ValueError("Invalid event ID")
                sequence = int(event_id)
                if kind == "cursor":
                    yield sequence, None
                else:
                    event = json.loads("\n".join(data))
                    if not isinstance(event, dict) or event.get("sequence") != sequence or not isinstance(event.get("kind"), str):
                        raise ValueError("Event identity mismatch")
                    yield sequence, event
            data, event_id, kind, size = [], "", "", 0
        elif not line.startswith(":"):
            name, _, value = line.partition(":")
            value = value.removeprefix(" ")
            if name == "data": data.append(value)
            elif name == "id": event_id = value
            elif name == "event": kind = value


def read_events(client, *, after=0, summary=False, conversation_id="", stop=lambda: False):
    from . import BeepgridError, _open
    if isinstance(after, bool) or not isinstance(after, int) or after < 0:
        raise ValueError("Invalid event cursor")
    cursor = after
    while not stop():
        query = {"stream": "1", "after": str(cursor)}
        if summary: query["summary"] = "1"
        if conversation_id: query["conversation_id"] = conversation_id
        request = urllib.request.Request(client.base_url + "/api/v1/events?" + urllib.parse.urlencode(query),
            headers={"Authorization": "Bearer " + client._token, "Last-Event-ID": str(cursor), "User-Agent": "BeepgridPython/0.1.0"})
        try:
            with _open(request, timeout=max(35, client.timeout)) as response:
                if not response.headers.get("Content-Type", "").startswith("text/event-stream"):
                    raise BeepgridError("INVALID_EVENT_STREAM", "Expected an event stream", 502)
                for sequence, event in read_frames(response):
                    if stop(): return
                    if sequence > cursor:
                        if event is not None:
                            yield event
                        cursor = sequence
        except urllib.error.HTTPError as error:
            error.close()
            if error.code < 500 and error.code != 429:
                raise BeepgridError("EVENT_STREAM_REJECTED", "Event stream rejected", error.code) from error
        except (ValueError, UnicodeError) as error:
            raise BeepgridError("INVALID_EVENT_STREAM", "Invalid event frame", 502) from error
        except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.IncompleteRead):
            pass
        if not stop(): time.sleep(.1)
