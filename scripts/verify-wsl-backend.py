#!/usr/bin/env python3

import argparse
import json
import os
from pathlib import Path
import selectors
import shutil
import subprocess
import sys
import time


class LineReader:
    def __init__(self, stream):
        self.stream = stream
        self.buffer = bytearray()
        self.selector = selectors.DefaultSelector()
        self.selector.register(stream, selectors.EVENT_READ)

    def readline(self, timeout_seconds):
        deadline = time.monotonic() + timeout_seconds
        while True:
            newline = self.buffer.find(b"\n")
            if newline >= 0:
                line = bytes(self.buffer[:newline])
                del self.buffer[: newline + 1]
                return line

            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError("timed out waiting for backend output")
            if not self.selector.select(remaining):
                raise TimeoutError("timed out waiting for backend output")

            chunk = os.read(self.stream.fileno(), 64 * 1024)
            if not chunk:
                if self.buffer:
                    line = bytes(self.buffer)
                    self.buffer.clear()
                    return line
                raise EOFError("backend stdout closed")
            self.buffer.extend(chunk)


def encode(message):
    return json.dumps(message, ensure_ascii=False, separators=(",", ":")).encode() + b"\n"


def run_case(label, command, request_count):
    started = time.monotonic()
    process = subprocess.Popen(
        command,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        bufsize=0,
    )
    reader = LineReader(process.stdout)
    ready = json.loads(reader.readline(10))
    assert ready["event"] == "ready", ready
    assert ready["data"]["protocol_version"] == 1, ready

    requests = [
        {"version": 1, "id": "large-echo", "method": "echo", "params": {"value": "x" * 8192}},
        {
            "version": 1,
            "id": "slow",
            "method": "sleep",
            "params": {"milliseconds": 5000},
        },
        {"version": 1, "id": "cancel-slow", "method": "cancel", "params": {"id": "slow"}},
        {"version": 1, "id": "tcc2-probe", "method": "tcc2_probe"},
    ]
    requests.extend(
        {"version": 1, "id": f"health-{index:04d}", "method": "health"}
        for index in range(request_count)
    )
    for request in requests:
        process.stdin.write(encode(request))
    process.stdin.flush()

    responses = {}
    for _ in requests:
        response = json.loads(reader.readline(20))
        response_id = response.get("id")
        assert response_id not in responses, response_id
        responses[response_id] = response

    assert responses["large-echo"].get("error") is None, responses["large-echo"]
    assert responses["cancel-slow"].get("error") is None, responses["cancel-slow"]
    assert responses["slow"]["error"]["code"] == "cancelled", responses["slow"]

    probe = responses["tcc2-probe"]
    assert probe.get("error") is None, probe
    probe_result = probe["result"]
    assert probe_result["server_name"] == "taskchute-cloud-2", probe_result
    assert probe_result["has_get_taskchute"] is True, probe_result
    assert probe_result["process_exit_clean"] is True, probe_result

    for index in range(request_count):
        response = responses[f"health-{index:04d}"]
        assert response["result"]["status"] == "ok", response

    process.stdin.close()
    return_code = process.wait(timeout=10)
    stderr = process.stderr.read().decode(errors="replace")
    assert return_code == 0, (return_code, stderr)
    assert not stderr.strip(), stderr

    return {
        "case": label,
        "requests": len(requests),
        "tool_count": probe_result["tool_count"],
        "elapsed_ms": round((time.monotonic() - started) * 1000),
        "clean_exit": True,
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--backend", required=True)
    parser.add_argument("--tcc2", default=shutil.which("tcc2"))
    parser.add_argument("--distribution", default=os.environ.get("WSL_DISTRO_NAME"))
    parser.add_argument("--user", default=os.environ.get("USER"))
    parser.add_argument("--requests", type=int, default=1000)
    args = parser.parse_args()

    backend = Path(args.backend).resolve()
    assert backend.is_file(), backend
    assert args.tcc2, "tcc2 was not found"
    tcc2 = Path(args.tcc2).resolve()
    assert tcc2.is_file(), tcc2
    assert args.distribution, "WSL distribution is required"
    assert args.user, "WSL user is required"
    assert args.requests > 0, args.requests

    direct = [
        str(backend),
        "serve",
        "--stdio",
        "--tcc2-executable",
        str(tcc2),
    ]
    wsl_executable = shutil.which("wsl.exe")
    assert wsl_executable, "wsl.exe was not found"
    via_wsl = [
        wsl_executable,
        "--distribution",
        args.distribution,
        "--user",
        args.user,
        "--cd",
        str(backend.parent.parent),
        "--exec",
        str(backend),
        "serve",
        "--stdio",
        "--tcc2-executable",
        str(tcc2),
    ]

    summaries = [
        run_case("direct", direct, args.requests),
        run_case("wsl.exe", via_wsl, args.requests),
    ]
    for summary in summaries:
        print(json.dumps(summary, ensure_ascii=False, sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(f"FAIL: {error}", file=sys.stderr)
        raise
