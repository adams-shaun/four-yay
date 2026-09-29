# ManaBrew client fixtures

`capture.json` is recorded from a real `gorged -manabrew -vsbot` by
`capture.py`: it plays vs-bot games through the NATIVE intent route and, at
every decision of the human seat, stores the native view + decision and the
ManaBrew `GET …/state` answer (state + open prompt) for the same instant. The
committed file keeps a few short runs (start, cast, bot spell, attack, block,
discard) and blanks every `text` field (no Forge-derived text in fixtures).

Regenerate (server on an engine-task port):

    python3 capture.py http://127.0.0.1:8096 cap.json mono-red-prowess mono-green-stompy 9

then pick runs as `[{"run": name, "records": [...]}]`, blanking `text`.
